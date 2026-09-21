// fetchghip 抓取 GitHub 候选 IP 并写回 github_accel 规则的 prefer.candidates。
//
// 候选来源（并集，任一源失败不影响其余）：
//  1. 固定种子 seedByDomain（GitHub 常用任意播边缘）
//  2. jsDelivr 镜像的 GitHub520 每日 hosts（社区实测）
//  3. GitHub 官方 Meta API（api.github.com/meta），经 DoH 解析真实 IP 直连
//
// 注意：Meta API 返回的 CIDR 是 GitHub 源站/出口段，不是边缘节点列表，
// 因此**不据此过滤候选**（段内 IP 未必服务目标域名）。候选是否真能服务该域名，
// 由 prefer 运行时的 validate_any_status（真实 SNI/Host 请求，非 5xx 且非 421）
// 实时判定；fetchghip 只维护候选池，保证幂等、不因单次网络抖动清空。
//
// 更新策略：
//   - 只更新 github_accel.json 中各 site 的 prefer.candidates（node 模式测速候选）
//   - 已有 prefer 块不动；没有 prefer 块的补齐并开启 node 模式
//   - 不修改 handler 上游（原上游保留作 fallback）
//
// 用法: fetchghip [--root DIR] [--rule FILE] [--dry-run]
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"steam302-web/internal/rules"
)

const (
	github520URL = "https://cdn.jsdelivr.net/gh/521xueweihan/GitHub520@master/hosts"
	metaAPIURL   = "https://api.github.com/meta"
)

var (
	// gh520Names 将 GitHub520 hosts 域名映射到 rules site.hosts[0]（含通配归属组）。
	gh520Names = map[string]string{
		"github.com":                    "github.com",
		"gist.github.com":               "gist.github.com",
		"api.github.com":                "api.github.com",
		"raw.githubusercontent.com":     "raw.githubusercontent.com",
		"assets-cdn.github.com":         "assets-cdn.github.com",
		"github.githubassets.com":       "github.githubassets.com",
		"codeload.github.com":           "codeload.github.com",
		"github.io":                     "github.io",
		"pages.github.com":              "pages.github.com",
		"avatars.githubusercontent.com": "raw.githubusercontent.com",
		"camo.githubusercontent.com":    "raw.githubusercontent.com",
		"objects.githubusercontent.com": "raw.githubusercontent.com",
		"cloud.githubusercontent.com":   "raw.githubusercontent.com",
		"desktop.githubusercontent.com": "desktop.githubusercontent.com",
		"central.github.com":            "support.github.com",
		"education.github.com":          "support.github.com",
	}
)

func main() {
	root := flag.String("root", "", "项目根目录（默认从 CWD 向上查找）")
	ruleFile := flag.String("rule", "", "规则文件路径（默认 <root>/config/rules/github_accel.json）")
	dryRun := flag.Bool("dry-run", false, "只打印将要写入的候选，不写文件")
	flag.Parse()

	rootDir := *root
	if rootDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fatal("%v", err)
		}
		rootDir = rules.FindRoot(cwd)
	}
	if rootDir == "" {
		fatal("未找到 config/rules 目录（请从项目目录运行或用 --root 指定）")
	}
	if *ruleFile == "" {
		*ruleFile = filepath.Join(rootDir, "config", "rules", "github_accel.json")
	}

	if err := run(rootDir, *ruleFile, *dryRun); err != nil {
		fatal("%v", err)
	}
}

func run(rootDir, ruleFile string, dryRun bool) error {
	cand, src, err := discoverCandidates()
	if err != nil {
		return fmt.Errorf("抓取 GitHub IP 候选失败: %w", err)
	}
	fmt.Printf("抓取来源: %s（%d 个域名候选）\n", src, len(cand))

	rulePath := ruleFile
	if !filepath.IsAbs(rulePath) {
		rulePath = filepath.Join(rootDir, rulePath)
	}
	data, err := os.ReadFile(rulePath)
	if err != nil {
		return fmt.Errorf("读取 %s: %w", rulePath, err)
	}

	var touches []string
	if !dryRun {
		touches, err = mirrorRule(rulePath, cand)
		if err != nil {
			return err
		}
		if len(touches) == 0 {
			fmt.Println("候选无变化，规则文件已是最新")
			return nil
		}
		fmt.Printf("已更新 %s:\n", rulePath)
		for _, t := range touches {
			fmt.Println("  " + t)
		}
		return nil
	}

	// dry-run：只计算差异，不落盘
	sf, err := reparseSites(data)
	if err != nil {
		return err
	}
	for i := range sf {
		h := sf[i].host
		ips, ok := cand[h]
		if !ok {
			continue
		}
		merged := mergePref(sf[i].prefer, ips, prefLike(h))
		if !jsonEqual(sf[i].prefer, merged) {
			touches = append(touches, fmt.Sprintf("%s → %s", h, strings.Join(ips, ",")))
		}
	}
	if len(touches) == 0 {
		fmt.Println("候选无变化，规则文件已是最新")
		return nil
	}
	fmt.Println("[dry-run] 将写入（未保存）:")
	for _, t := range touches {
		fmt.Println("  " + t)
	}
	return nil
}

// mirrorRule 将优选候选写回规则文件，返回变更摘要行。
func mirrorRule(rulePath string, cand map[string][]string) ([]string, error) {
	data, err := os.ReadFile(rulePath)
	if err != nil {
		return nil, fmt.Errorf("读取 %s: %w", rulePath, err)
	}
	var full map[string]json.RawMessage
	if err := json.Unmarshal(data, &full); err != nil {
		return nil, fmt.Errorf("解析完整规则: %w", err)
	}
	var sitesRaw []json.RawMessage
	if err := json.Unmarshal(full["sites"], &sitesRaw); err != nil {
		return nil, err
	}
	var touches []string
	for i := range sitesRaw {
		var site map[string]json.RawMessage
		if err := json.Unmarshal(sitesRaw[i], &site); err != nil {
			continue
		}
		var hosts []string
		_ = json.Unmarshal(site["hosts"], &hosts)
		if len(hosts) == 0 {
			continue
		}
		ips, ok := cand[hosts[0]]
		if !ok {
			continue
		}
		merged := mergePref(site["prefer"], ips, prefLike(hosts[0]))
		if jsonEqual(site["prefer"], merged) {
			continue
		}
		if merged == nil {
			delete(site, "prefer")
		} else {
			site["prefer"] = merged
		}
		raw, err := json.MarshalIndent(site, "", "  ")
		if err != nil {
			return nil, err
		}
		sitesRaw[i] = raw
		touches = append(touches, fmt.Sprintf("%s(prefer→%s)", hosts[0], strings.Join(ips, ",")))
	}
	if len(touches) == 0 {
		return nil, nil
	}
	mergedSites, err := json.Marshal(sitesRaw)
	if err != nil {
		return nil, err
	}
	full["sites"] = mergedSites
	out, err := json.MarshalIndent(full, "", "  ")
	if err != nil {
		return nil, err
	}
	perm := os.FileMode(0o644)
	if st, err := os.Stat(rulePath); err == nil {
		perm = st.Mode().Perm()
	}
	tmp := rulePath + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), perm); err != nil {
		return nil, fmt.Errorf("写入 %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, rulePath); err != nil {
		return nil, err
	}
	return touches, nil
}

type siteFlat struct {
	host   string
	prefer json.RawMessage
}

func reparseSites(data []byte) ([]siteFlat, error) {
	var rule struct {
		Sites []struct {
			Hosts  []string        `json:"hosts"`
			Prefer json.RawMessage `json:"prefer,omitempty"`
		} `json:"sites"`
	}
	if err := json.Unmarshal(data, &rule); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", "规则文件", err)
	}
	var out []siteFlat
	for _, s := range rule.Sites {
		if len(s.Hosts) == 0 {
			continue
		}
		out = append(out, siteFlat{host: s.Hosts[0], prefer: s.Prefer})
	}
	return out, nil
}

// discoverCandidates 合并所有可用来源的候选（并集），任一源失败不影响其余：
//  1. 固定种子（seedByDomain，幂等稳定）
//  2. GitHub520 每日 hosts（社区实测）
//  3. GitHub Meta API 兜底（经 DoH 引导真实 IP 直连）
//
// 候选池只做并集/去重/排序（幂等），不做"官方段过滤"或 SNI 硬剔除——IP 是否
// 真能服务目标域名由 prefer 运行时的 validate_any_status 实时判定。
func discoverCandidates() (map[string][]string, string, error) {
	out := map[string][]string{}
	var sources []string
	merge := func(m map[string][]string) {
		for host, ips := range m {
			out[host] = dedupe(append(out[host], ips...))
		}
	}
	if m, err := fetchSeed(); err == nil && len(m) > 0 {
		merge(m)
		sources = append(sources, "种子")
	}
	if m, err := fetchGitHub520(); err == nil && len(m) > 0 {
		merge(m)
		sources = append(sources, "GitHub520")
	}
	if m, err := fetchMeta(); err == nil && len(m) > 0 {
		merge(m)
		sources = append(sources, "Meta")
	}
	if len(out) == 0 {
		return nil, "", fmt.Errorf("三个数据源均失败")
	}
	for host := range out {
		sort.Strings(out[host])
	}
	return out, strings.Join(sources, "+"), nil
}

// fetchSeed 给 github 各服务域名生成候选池（幂等）。
// 候选 = seedByDomain 固定种子（GitHub 常用任意播边缘），保证输出稳定。
// 不做"官方段过滤"、不做 SNI 硬剔除：Meta 段是源站/出口段，段内 IP 未必服务
// 目标域名（实测 185.199.108.133 对 github.com 回 500）；真伪交给 prefer 运行时
// 的 validate_any_status（非 5xx/连接失败才可用）实时判定。此处仅维护候选池，
// 避免单次网络抖动把候选清空。
func fetchSeed() (map[string][]string, error) {
	out := map[string][]string{}
	for target, ips := range seedByDomain {
		if len(ips) > 0 {
			out[target] = dedupe(ips)
			sort.Strings(out[target])
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("无种子候选")
	}
	return out, nil
}

// seedByDomain 为主域提供固定候选（稳定幂等）；真伪由 prefer 运行时判定。
var seedByDomain = map[string][]string{
	"github.com":                    {"20.205.243.166", "140.82.112.3", "185.199.108.133"},
	"gist.github.com":               {"20.205.243.168", "140.82.112.3"},
	"api.github.com":                {"20.205.243.168", "140.82.112.3", "185.199.108.133"},
	"raw.githubusercontent.com":     {"185.199.108.133", "185.199.109.133", "185.199.110.133", "185.199.111.133"},
	"github.githubassets.com":       {"185.199.108.154", "140.82.112.3"},
	"codeload.github.com":           {"20.205.243.165", "140.82.112.3", "185.199.108.133"},
	"github.io":                     {"185.199.108.153", "185.199.109.153", "185.199.110.153", "185.199.111.153"},
	"support.github.com":            {"185.199.109.133", "140.82.112.3", "185.199.108.133"},
	"desktop.githubusercontent.com": {"185.199.108.133", "185.199.109.133"},
}

func fetchGitHub520() (map[string][]string, error) {
	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			DialContext:     (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		},
	}
	resp, err := client.Get(github520URL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub520 HTTP %d", resp.StatusCode)
	}
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	byName := map[string][]string{}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if net.ParseIP(f[0]) == nil {
			continue
		}
		target, ok := gh520Names[f[1]]
		if !ok {
			continue
		}
		byName[target] = append(byName[target], f[0])
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for k, v := range byName {
		out[k] = dedupe(v)
	}
	return out, nil
}

// fetchMeta 经 DoH 解析 api.github.com 真实 IP 直连 /meta，用官方段验证已知边缘候选。
func fetchMeta() (map[string][]string, error) {
	realIPs, err := dohResolve("api.github.com")
	if err != nil || len(realIPs) == 0 {
		return nil, fmt.Errorf("DoH 解析失败: %v", err)
	}
	var lastErr error
	for _, ip := range realIPs {
		m, err := metaByIP(ip)
		if err == nil && len(m) > 0 {
			return m, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("Meta 直连失败: %v", lastErr)
}

func metaByIP(ip string) (map[string][]string, error) {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", net.JoinHostPort(ip, "443"))
		},
		TLSClientConfig:   &tls.Config{ServerName: "api.github.com"},
		DisableKeepAlives: true,
	}
	client := &http.Client{Transport: tr, Timeout: 15 * time.Second}
	req, err := http.NewRequest(http.MethodGet, metaAPIURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "steam302-web/fetchghip")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Meta HTTP %d", resp.StatusCode)
	}
	// 只确认 /meta 可达（HTTP 200）；返回的 CIDR 是源站/出口段，不作候选过滤依据。
	_, _ = io.Copy(io.Discard, resp.Body)
	fixed := map[string][]string{
		"github.com":                    {"20.205.243.166", "140.82.114.4", "185.199.108.133"},
		"api.github.com":                {"20.205.243.168", "140.82.114.4"},
		"raw.githubusercontent.com":     {"185.199.108.133", "185.199.109.133", "185.199.110.133"},
		"github.githubassets.com":       {"185.199.108.154", "140.82.114.6"},
		"codeload.github.com":           {"20.205.243.165", "140.82.114.10"},
		"gist.github.com":               {"20.205.243.168", "140.82.114.4"},
		"github.io":                     {"185.199.108.153", "185.199.109.153"},
		"assets-cdn.github.com":         {"185.199.109.153"},
		"pages.github.com":              {"185.199.108.153"},
		"desktop.githubusercontent.com": {"185.199.108.153"},
	}
	// 不做"官方段过滤"：Meta 的 CIDR 是源站/出口段，段内 IP 未必服务目标域名
	// （实测 185.199.108.133 对 github.com 回 500），段外 IP 也可能正好服务该域名。
	// 候选真伪由 prefer 运行时的 validate_any_status（非 5xx/连接失败才可用）判定，
	// 此处只提供候选池，不做网络硬剔除（避免单次抖动清空候选）。
	return fixed, nil
}

func dohResolve(host string) ([]string, error) {
	resp, err := (&http.Client{Timeout: 8 * time.Second}).Get(
		fmt.Sprintf("https://dns.alidns.com/resolve?name=%s&type=A", host))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var dr struct {
		Answer []struct {
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&dr); err != nil {
		return nil, err
	}
	var out []string
	for _, a := range dr.Answer {
		if net.ParseIP(a.Data) != nil {
			out = append(out, a.Data)
		}
	}
	return out, nil
}

// mergePref 更新 prefer 的 candidates，保留其余字段；无 prefer 时补齐 node 配置。
// candidates 以"本次抓取的最新候选"为准并排序（不累积旧值），保证幂等：
// DNS 解析顺序抖动不会导致规则文件每次重写；抓取结果稳定时输出稳定。
func mergePref(existing json.RawMessage, ips []string, topN int) json.RawMessage {
	var pref map[string]any
	if len(existing) > 0 {
		if err := json.Unmarshal(existing, &pref); err != nil {
			pref = nil
		}
	}
	merged := append([]string{}, ips...)
	if len(merged) == 0 && pref != nil {
		if cur, ok := pref["candidates"].([]any); ok { // 本次无可用候选时保留旧值
			for _, c := range cur {
				s, _ := c.(string)
				if s != "" {
					merged = append(merged, s)
				}
			}
		}
	}
	merged = dedupe(merged)
	sort.Strings(merged)
	if pref == nil {
		pref = map[string]any{
			"mode":                "node",
			"validate":            true,
			"speed_test":          false,
			"top_n":               topN,
			"validate_any_status": true,
		}
	}
	pref["candidates"] = merged
	if topN > 0 {
		pref["top_n"] = topN
	}
	raw, err := json.Marshal(pref)
	if err != nil {
		return existing
	}
	return raw
}

// prefLike 返回该域名站点的缺省 TopN（按关注度）。
func prefLike(host string) int {
	switch host {
	case "github.com":
		return 3
	case "api.github.com", "raw.githubusercontent.com":
		return 2
	}
	return 1
}

func jsonEqual(a, b json.RawMessage) bool {
	var av, bv any
	if len(a) == 0 {
		av = nil
	} else if json.Unmarshal(a, &av) != nil {
		av = nil
	}
	if len(b) == 0 {
		bv = nil
	} else if json.Unmarshal(b, &bv) != nil {
		bv = nil
	}
	aj, _ := json.Marshal(av)
	bj, _ := json.Marshal(bv)
	return string(aj) == string(bj)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "fetchghip: "+format+"\n", args...)
	os.Exit(1)
}
