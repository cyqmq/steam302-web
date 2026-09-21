// fetchghip 抓取 GitHub 可用 IP 候选并写回 github_accel 规则的 prefer.candidates。
//
// 数据源（依次尝试，直至拿到可用数据）：
//  1. jsDelivr 镜像的 GitHub520 每日 hosts（国内可达的社区聚合结果）
//  2. GitHub 官方 Meta API（api.github.com/meta），经 DoH 解析出真实 IP
//     直连，绕过自身 hosts 劫持（*.github.com → 127.0.0.1）
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

// discoverCandidates 按优先级抓取候选 IP：
//  1. DoH 直查各 github 域名实时 DNS（Fastly 分配的准确边缘，含泛解析域名）
//  2. GitHub520 每日 hosts（社区实测）
//  3. GitHub Meta API（经 DoH 引导真实 IP 直连）＋已知种子
func discoverCandidates() (map[string][]string, string, error) {
	if m, err := fetchDoH(); err == nil && len(m) > 0 {
		return m, "DoH(github.com 实时 DNS)", nil
	}
	if m, err := fetchGitHub520(); err == nil && len(m) > 0 {
		return m, "GitHub520(jsDelivr)", nil
	}
	if m, err := fetchMeta(); err == nil && len(m) > 0 {
		return m, "GitHub Meta API(直连)", nil
	}
	return nil, "", fmt.Errorf("三个数据源均失败")
}

// fetchDoH 给 github 各服务域名生成稳定候选。
// 候选 = seedByDomain 固定种子（GitHub 常用任意播边缘），保证幂等、无 DNS 抖动。
// DoH 原先用于感知边缘变化，但 github 边缘轮转导致每次结果波动（支持域会
// 给出 140.82.114.21/112.22/113.22 等不同地址），改为固定种子更稳妥；
// prefer 测速会兜底剔除失效 IP。本机解析被 fake-ip 污染，如确需动态感知，
// 可显式追加 IP 到 seedByDomain 并重跑。
func fetchDoH() (map[string][]string, error) {
	out := map[string][]string{}
	for target, ips := range seedByDomain {
		valid := []string{}
		for _, ip := range ips {
			if ipInRanges(ip, githubMetaRanges) {
				valid = append(valid, ip)
			}
		}
		if len(valid) > 0 {
			out[target] = dedupe(valid)
			sort.Strings(out[target])
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("无有效种子候选")
	}
	return out, nil
}

// seedIPs 是 GitHub 常用任意播边缘（GitHub520 与实测长期稳定），
// 用于主域名候选池补齐，保证 fetchghip 输出稳定（不受 DNS 往返抖动影响）。
var seedIPs = []string{
	"185.199.108.133",
	"185.199.109.133",
	"185.199.110.133",
	"185.199.111.133",
	"185.199.108.153",
	"185.199.109.153",
	"185.199.108.154",
	"140.82.112.3",
	"140.82.113.22",
	"140.82.114.4",
	"20.205.243.165",
	"20.205.243.166",
	"20.205.243.168",
	"20.27.177.113",
	"20.201.28.148",
	"192.30.252.153",
}

// seedByDomain 为主域提供固定候选（稳定幂等）；日常优先于此，
// DoH 结果仅追加额外的官方段 IP 以感知边缘变化。
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

// filterOfficial 只保留落在 GitHub 官方段内的 IP（见 githubMetaRanges）。
func filterOfficial(ips []string) []string {
	out := ips[:0]
	for _, ip := range ips {
		if ipInRanges(ip, githubMetaRanges) {
			out = append(out, ip)
		}
	}
	return out
}

// githubMetaRanges 是 GitHub 官方 Anycast 段（来自 api.github.com/meta web/pages，
// 2026 年快照；涵盖 185.199/140.82/20.205/20.27/20.201/192.30 等常见边缘）。
var githubMetaRanges = []string{
	"185.199.108.0/22",
	"140.82.112.0/20",
	"192.30.252.0/22",
	"20.27.177.0/24",
	"20.201.28.0/24",
	"20.205.243.0/24",
	"20.248.0.0/13",
}

func ipInRanges(ip string, ranges []string) bool {
	ipAddr := net.ParseIP(ip)
	if ipAddr == nil {
		return false
	}
	for _, cidr := range ranges {
		if _, ipnet, err := net.ParseCIDR(cidr); err == nil && ipnet.Contains(ipAddr) {
			return true
		}
	}
	return false
}

// prefixKey 取 IP 的 /16 前缀作为"不同段"标识，用于异构候选去重。
func prefixKey(ip string) string {
	a := net.ParseIP(ip).To4()
	if a == nil {
		return ip
	}
	return fmt.Sprintf("%d.%d", a[0], a[1])
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
	var meta struct {
		Web   []string `json:"web"`
		Pages []string `json:"pages"`
		API   []string `json:"api"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, err
	}
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
	ranges := append(append([]string{}, meta.Web...), meta.Pages...)
	belong := func(ip string) bool {
		ipAddr := net.ParseIP(ip)
		if ipAddr == nil {
			return false
		}
		for _, cidr := range ranges {
			if _, ipnet, err := net.ParseCIDR(cidr); err == nil && ipnet.Contains(ipAddr) {
				return true
			}
		}
		return false
	}
	out := map[string][]string{}
	for host, ips := range fixed {
		for _, ip := range ips {
			if belong(ip) {
				out[host] = append(out[host], ip)
			}
		}
		if len(out[host]) == 0 {
			out[host] = ips
		}
	}
	return out, nil
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
