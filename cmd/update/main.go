// update 是自更新工具（需 root）：从 GitHub Releases 拉取预构建产物
// "<prefix>.tar.gz" + "<prefix>.sha256"，校验后原子替换 bin/ 与 deploy/，
// 最后重启 systemd 服务。运行时状态写入 config/.update/state.json，
// 供 webui 的 /api/update/status 轮询展示。
//
//	用法: update --root <项目根> --current <当前版本> [--dry-run]
package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	root      string
	current   string
	dryRun    bool
	statePath string
	staging   string
)

type apiRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

type state struct {
	Phase   string `json:"phase"` // idle|checking|downloading|verifying|swapping|restarting|done|error
	Message string `json:"message"`
	Done    bool   `json:"done"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Current string `json:"current"`
	Latest  string `json:"latest,omitempty"`
	TS      string `json:"ts"`
}

func writeState(s state) {
	s.TS = time.Now().Format(time.RFC3339)
	dir := filepath.Dir(statePath)
	_ = os.MkdirAll(dir, 0o755)
	tmp := statePath + ".tmp"
	data, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(tmp, data, 0o644); err == nil {
		_ = os.Rename(tmp, statePath)
	}
}

func setPhase(phase, msg string) {
	writeState(state{Phase: phase, Message: msg, Current: current, Done: phase == "done" || phase == "error", OK: phase == "done"})
	if phase == "error" {
		writeState(state{Phase: "error", Message: msg, Current: current, Done: true, OK: false, Error: msg, Latest: latest})
	}
	fmt.Println("[" + phase + "] " + msg)
}

var latest string

func main() {
	var (
		cur  = flag.String("current", "", "当前版本（webui 传入）")
		dry  = flag.Bool("dry-run", false, "只打印执行计划，不做任何改动")
		root = flag.String("root", "", "项目根目录")
	)
	flag.Parse()
	current = *cur
	dryRun = *dry
	if *root == "" {
		cwd, _ := os.Getwd()
		*root = findRoot(cwd)
	}
	rootDir := *root
	staging = filepath.Join(rootDir, "config", ".update")
	statePath = filepath.Join(staging, "state.json")

	if rootDir == "" {
		setPhase("error", "未找到项目根目录")
		os.Exit(1)
	}

	// 1) 解析发布信息
	setPhase("checking", "查询最新版本…")
	env := loadEnv(rootDir)
	repo := env.Update.Repo
	if repo == "" {
		repo = "cyqmq/steam302-web"
	}
	prefix := env.Update.AssetPrefix
	if prefix == "" {
		prefix = "steam302-web-linux-amd64"
	}
	tarName, tarURL, shaName, shaURL := "", "", "", ""
	latest = current
	if env.Update.URLOverride != "" {
		setPhase("checking", "使用自定义更新源（url_override）")
		tarName = "steam302-web-custom.tar.gz"
		tarURL = env.Update.URLOverride
		shaName = "steam302-web-custom.tar.gz.sha256"
		shaURL = env.Update.URLOverride + ".sha256"
	} else {
		tag, n, u, sn, su := fetchRelease(repo, prefix)
		if tag == "" {
			setPhase("error", "查询 GitHub 最新版本失败（网络或仓库不可达）")
			os.Exit(1)
		}
		latest = strings.TrimPrefix(tag, "v")
		tarName, tarURL, shaName, shaURL = n, u, sn, su
	}
	setPhase("checking", fmt.Sprintf("最新版本 v%s（当前 v%s）", latest, current))

	if differentVersion(current, latest) == false {
		setPhase("done", "已是最新版本，无需更新")
		return
	}

	if dryRun {
		fmt.Println("--- 更新计划（--dry-run）---")
		fmt.Printf("  Release: %s v%s\n", repo, latest)
		fmt.Printf("  下载:    %s\n", tarURL)
		fmt.Printf("  校验:    %s\n", shaURL)
		fmt.Printf("  替换:    bin/ 与 deploy/ 下所有文件（原子 rename）\n")
		fmt.Printf("  备份:    %s\n", filepath.Join(staging, "backup-<时间戳>"))
		fmt.Printf("  重启:    steam302-web-caddy steam302-web-fwd steam302-web-webui\n")
		return
	}

	if os.Geteuid() != 0 {
		setPhase("error", "需要 root 权限（请在 systemd 服务或 sudo 下执行）")
		os.Exit(1)
	}

	// 2) 下载 + 校验
	setPhase("downloading", "下载发布产物…")
	tarPath := filepath.Join(staging, tarName)
	shaPath := filepath.Join(staging, shaName)
	if err := download(tarURL, tarPath); err != nil {
		setPhase("error", "下载失败: "+err.Error())
		os.Exit(1)
	}
	if !env.Update.SkipVerify {
		setPhase("verifying", "校验 sha256…")
		if err := download(shaURL, shaPath); err != nil {
			setPhase("error", "下载校验文件失败: "+err.Error())
			os.Exit(1)
		}
		sum, err := readSum(shaPath)
		if err != nil {
			setPhase("error", "解析校验文件失败: "+err.Error())
			os.Exit(1)
		}
		if err := verifySHA256(tarPath, sum); err != nil {
			setPhase("error", "sha256 校验失败: "+err.Error())
			os.Exit(1)
		}
	}

	// 3) 备份并原子替换
	setPhase("swapping", "备份并替换二进制…")
	backupDir := filepath.Join(staging, "backup-"+time.Now().Format("20060102-150405"))
	if err := swap(rootDir, tarPath, backupDir); err != nil {
		setPhase("error", "替换失败（已回滚）: "+err.Error())
		os.Exit(1)
	}

	// 4) 重启服务
	setPhase("restarting", "重启服务…")
	if err := restartUnits(); err != nil {
		setPhase("error", "服务重启失败: "+err.Error())
		os.Exit(1)
	}

	setPhase("done", "更新完成，已升级到 v"+latest)
	fmt.Println("提醒：Web 管理端自身已重启，如页面断开请刷新重连。")
}

func findRoot(cwd string) string {
	for {
		if _, err := os.Stat(filepath.Join(cwd, "config", "rules")); err == nil {
			return cwd
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return ""
		}
		cwd = parent
	}
}

// envUpdate 提取 update 配置段（独立小结构，避免依赖 rules 包层级）。
type envUpdate struct {
	Repo        string `json:"repo,omitempty"`
	AssetPrefix string `json:"asset_prefix,omitempty"`
	URLOverride string `json:"url_override,omitempty"`
	SkipVerify  bool   `json:"skip_verify,omitempty"`
}

// loadEnv 读取 config/env.json 的 update 段。
func loadEnv(root string) struct{ Update envUpdate } {
	var e struct{ Update envUpdate }
	data, err := os.ReadFile(filepath.Join(root, "config", "env.json"))
	if err == nil {
		_ = json.Unmarshal(data, &e)
	}
	return e
}

// fetchRelease 查询 releases/latest 资产，返回 tag 与匹配 "<prefix>.tar.gz" /
// "<prefix>.sha256" 的 URL。
func fetchRelease(repo, prefix string) (tag, tarName, tarURL, shaName, shaURL string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	api := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, api, nil)
	if err != nil {
		return "", "", "", "", ""
	}
	req.Header.Set("User-Agent", "steam302-web/update")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", "", "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", "", "", ""
	}
	var rel apiRelease
	if json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&rel) != nil {
		return "", "", "", "", ""
	}
	tag = rel.TagName
	for _, a := range rel.Assets {
		switch {
		case a.Name == prefix+".tar.gz" && tarURL == "":
			tarName, tarURL = a.Name, a.URL
		case a.Name == prefix+".sha256" && shaURL == "":
			shaName, shaURL = a.Name, a.URL
		case strings.HasPrefix(a.Name, prefix) && strings.HasSuffix(a.Name, ".tar.gz") && tarURL == "":
			tarName, tarURL = a.Name, a.URL
		}
	}
	return tag, tarName, tarURL, shaName, shaURL
}

func download(url, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "steam302-web/update")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 512<<20)); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// readSum 解析 "<hex>  <name>" 行，返回 hex 摘要。
func readSum(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "", fmt.Errorf("校验文件为空")
	}
	sum := strings.TrimPrefix(fields[0], "SHA256:")
	sum = strings.TrimSpace(strings.TrimPrefix(sum, "("))
	sum = strings.TrimSpace(strings.TrimSuffix(sum, ")"))
	if _, err := hex.DecodeString(sum); err != nil {
		return "", fmt.Errorf("非法 sha256: %s", sum)
	}
	return strings.ToLower(sum), nil
}

func verifySHA256(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("期望 %s，实际 %s", want, got)
	}
	return nil
}

// fileJob 是一次原子文件名替换：dst（目标）→ backed（备份）。
type fileJob struct {
	dst, backed string
}

// swap 解包发布物，原子替换 bin/ 与 deploy/；被替换的旧文件备份到
// backupDir（展平文件名）。任一失败时按序回滚。
func swap(rootDir, tarPath, backupDir string) error {
	_ = os.MkdirAll(backupDir, 0o755)
	var jobs []fileJob
	if err := extractPlan(rootDir, tarPath, func(dst string) {
		backed := filepath.Join(backupDir, filepath.Base(dst))
		jobs = append(jobs, fileJob{dst: dst, backed: backed})
	}); err != nil {
		return err
	}
	rollback := func() {
		for i := len(jobs) - 1; i >= 0; i-- {
			j := jobs[i]
			if _, err := os.Stat(j.backed); err == nil {
				_ = os.Rename(j.backed, j.dst)
			} else {
				_ = os.Remove(j.dst)
			}
		}
	}
	for i := range jobs {
		j := &jobs[i]
		if _, err := os.Stat(j.dst); err == nil {
			if err := os.Rename(j.dst, j.backed); err != nil {
				rollback()
				return fmt.Errorf("备份 %s 失败: %w", j.dst, err)
			}
		}
		if err := os.Rename(j.dst+".incoming", j.dst); err != nil {
			rollback()
			return fmt.Errorf("替换 %s 失败: %w", j.dst, err)
		}
	}
	_ = os.RemoveAll(filepath.Join(staging, "stage"))
	_ = os.Remove(tarPath)
	return nil
}

// extractPlan 把 tar.gz 中 bin/ 与 deploy/ 的文件解包到 <dst>.incoming，
// 逐个回调目标绝对路径。
func extractPlan(rootDir, tarPath string, visit func(dst string)) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer zr.Close()
	stage := filepath.Join(staging, "stage")
	_ = os.RemoveAll(stage)
	tr := tar.NewReader(zr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(hdr.Name, "./"), "/"))
		dst := ""
		switch {
		case name == "bin" || strings.HasPrefix(name, "bin/"):
			dst = filepath.Join(rootDir, filepath.FromSlash(name))
		case name == "deploy" || strings.HasPrefix(name, "deploy/"):
			dst = filepath.Join(rootDir, filepath.FromSlash(name))
		default:
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		incoming := dst + ".incoming"
		out, err := os.OpenFile(incoming, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.CopyN(out, tr, hdr.Size); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
		visit(dst)
	}
	return nil
}

func restartUnits() error {
	for _, u := range []string{"steam302-web-caddy.service", "steam302-web-fwd.service", "steam302-web-webui.service"} {
		cmd := exec.Command("systemctl", "restart", u)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %v %s", u, err, out)
		}
	}
	return nil
}

func differentVersion(cur, latest string) bool {
	ck := strings.Split(strings.TrimPrefix(cur, "v"), ".")
	lk := strings.Split(strings.TrimPrefix(latest, "v"), ".")
	n := len(ck)
	if len(lk) > n {
		n = len(lk)
	}
	for i := 0; i < n; i++ {
		a, b := 0, 0
		if i < len(ck) {
			fmt.Sscanf(ck[i], "%d", &a)
		}
		if i < len(lk) {
			fmt.Sscanf(lk[i], "%d", &b)
		}
		if a != b {
			return a < b
		}
	}
	return false
}
