package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"steam302-web/internal/prefer"
	"steam302-web/internal/rules"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	root := fs.String("root", "", "项目根目录（默认从 CWD 向上查找）")
	cacheFile := fs.String("cache", "", "缓存路径（默认 <root>/config/prefer.json）")
	quick := fs.Bool("quick", false, "只做 TCP 延迟探测，跳过下载测速（用于开机自启）")
	debug := fs.Bool("debug", false, "打印候选存活率诊断")
	timeoutS := fs.Int("timeout", 25, "整体探测超时（秒）")
	ruleID := fs.String("rule", "", "仅处理指定规则 id")
	if err := fs.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}

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
	if *cacheFile == "" {
		*cacheFile = filepath.Join(rootDir, prefer.Path)
	}

	switch cmd {
	case "run":
		if err := runPrefer(rootDir, *cacheFile, *quick, *debug, time.Duration(*timeoutS)*time.Second, *ruleID); err != nil {
			fatal("%v", err)
		}
	case "show":
		if err := showPrefer(*cacheFile, *ruleID); err != nil {
			fatal("%v", err)
		}
	case "clear":
		if err := prefer.Remove(*cacheFile); err != nil {
			fatal("移除缓存: %v", err)
		}
		fmt.Printf("已清除 CDN 优选缓存: %s\n", *cacheFile)
	default:
		fatal("未知子命令: %s", cmd)
	}
}

func runPrefer(rootDir, cacheFile string, quick, debug bool, budget time.Duration, only string) error {
	env, err := rules.LoadEnv(filepath.Join(rootDir, "config", "env.json"))
	if err != nil {
		return fmt.Errorf("加载 env.json: %w", err)
	}
	global := env.Prefer
	if !global.Enabled {
		fmt.Println("env.prefer.enabled=false，跳过探测（开启后才会执行 CDN 优选）")
		return nil
	}
	rs, err := rules.LoadRules(filepath.Join(rootDir, "config", "rules"), false)
	if err != nil {
		return err
	}

	opts := prefer.Options{
		Port:            global.Port,
		LatencyTimeout:  time.Duration(global.LatencyTimeoutMS) * time.Millisecond,
		LatencyTries:    global.LatencyTries,
		Parallel:        global.Parallel,
		SpeedTest:       global.SpeedTest,
		DownloadURL:     global.DownloadURL,
		DownloadSize:    global.DownloadSize,
		DownloadTimeout: time.Duration(global.DownloadTimeoutS) * time.Second,
		MaxMbps:         global.MaxMbps,
		TopN:            global.TopN,
		SamplesPerCIDR:  global.SamplesPerCIDR,
	}
	opts.Inflate()
	if quick {
		opts.SpeedTest = false
		// 开机自启用低采样：CF 25 段×3=75 个 IP 延迟探测，几秒内完成
		if opts.SamplesPerCIDR > 3 {
			opts.SamplesPerCIDR = 3
		}
	}

	deadline := time.Now().Add(budget)
	// quick 模式只补缺失项、不覆盖已有项，保证开机不会把全量跑出的优质结果冲掉
	var entries []prefer.Entry
	if prev, err := prefer.Load(cacheFile); err != nil {
		return fmt.Errorf("读取缓存: %w", err)
	} else if prev != nil {
		entries = prev.Entries
	}
	have := map[string]bool{}
	for _, e := range entries {
		have[entryKey(e.RuleID, e.SiteIndex)] = true
	}

	for _, r := range rs {
		if only != "" && r.ID != only {
			continue
		}
		for i, site := range r.Sites {
			sp := site.Prefer
			if sp == nil {
				continue
			}
			if quick && have[entryKey(r.ID, i)] {
				fmt.Printf("[%s/site%d] quick 模式保留现有优选结果\n", r.ID, i)
				continue
			}
			if time.Now().After(deadline) {
				fmt.Printf("[%s] 超时跳过后续探测\n", r.ID)
				break
			}
			entry, diag, err := probeSite(r.ID, i, site, sp, opts, deadline)
			if err != nil {
				fmt.Printf("[%s/site%d] 跳过: %v\n", r.ID, i, err)
				continue
			}
			if debug && diag != nil {
				fmt.Printf("[%s/site%d] 候选=%d 延迟+校验存活=%d 测速=%d\n",
					r.ID, i, diag.Candidates, diag.Latency, diag.Speeded)
			}
			if entry == nil || len(entry.Ranked) == 0 {
				fmt.Printf("[%s/site%d] 无可用节点\n", r.ID, i)
				continue
			}
			entries = append(entries, *entry)
			have[entryKey(entry.RuleID, entry.SiteIndex)] = true
			show := []string{}
			for _, rk := range entry.Ranked {
				show = append(show, fmt.Sprintf("%s(d=%sms,s=%.2fMB/s)", rk.IP, f2s(rk.DelayMS), rk.SpeedMbps))
			}
			fmt.Printf("[%s] site%d %s => %s\n", entry.RuleID, entry.SiteIndex, entry.Mode, strings.Join(show, ", "))
		}
	}

	cache := prefer.Cache{SchemaVersion: prefer.SchemaVersion, UpdatedAt: time.Now(), Entries: entries}
	if err := prefer.Save(cacheFile, &cache); err != nil {
		return fmt.Errorf("写入缓存 %s: %w", cacheFile, err)
	}
	fmt.Printf("已写入 CDN 优选缓存: %s (共 %d 项)\n", cacheFile, len(cache.Entries))
	return nil
}

func entryKey(ruleID string, siteIdx int) string {
	return fmt.Sprintf("%s#%d", ruleID, siteIdx)
}

type siteDiag struct {
	Candidates int
	Latency    int
	Speeded    int
}

func probeSite(ruleID string, siteIdx int, site rules.Site, sp *rules.Prefer, opts prefer.Options, deadline time.Time) (*prefer.Entry, *siteDiag, error) {
	mode := sp.Mode
	if mode == "" {
		return nil, nil, fmt.Errorf("missing prefer.mode")
	}
	// site 级覆盖
	o := opts
	o.Port = sp.Port
	if sp.TopN > 0 {
		o.TopN = sp.TopN
	}
	if sp.SamplesPerCIDR > 0 {
		o.SamplesPerCIDR = sp.SamplesPerCIDR
	}
	if sp.SpeedTest != nil {
		o.SpeedTest = *sp.SpeedTest
	}
	if sp.DownloadURL != "" {
		o.DownloadURL = sp.DownloadURL
	}
	// node 模式默认不做下载测速：接入节点/边缘不支持任意 SNI（speed.cloudflare.com 等
	// 通用测速 URL 经它们会连接被重置），除非规则显式指定了 download_url。
	if mode == "node" && sp.DownloadURL == "" {
		o.SpeedTest = false
	}
	if sp.MaxMbps > 0 {
		o.MaxMbps = sp.MaxMbps
	}
	o.Inflate()
	if mode == "cf" {
		// 校验这些 Anycast IP 是否真的能服务该 site 的域名（避免 502）
		if len(site.Hosts) > 0 {
			o.ValidateHost = site.Hosts[0]
		}
	}

	candidates := sp.Candidates
	if mode == "node" && len(candidates) == 0 {
		// 缺省候选 = 第一个 reverse_proxy handler 的静态上游 + 动态上游 host
		for _, h := range site.Handlers {
			if h.Type != "reverse_proxy" {
				continue
			}
			candidates = append(candidates, h.Upstreams...)
			for _, d := range h.DynamicUpstreams {
				candidates = append(candidates, d.Host)
			}
			break
		}
	}
	if len(candidates) == 0 && mode == "node" {
		return nil, nil, fmt.Errorf("node 模式缺少候选")
	}
	if mode == "cf" && len(sp.CIDRs) == 0 {
		return nil, nil, fmt.Errorf("cf 模式缺少 cidrs")
	}

	// cf 模式：能服务该域名的边缘只占少数，采样可能抽空。
	// 抽空就加倍采样重试（最多 3 轮），确保稳定选出可用 IP。
	samples := o.SamplesPerCIDR
	if mode == "cf" && samples < 8 {
		samples = 24
	}
	var diag *siteDiag
	lastErr := error(nil)
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			samples *= 2 // 24 → 48；最多两轮，避免 2400 个 IP 的失控规模
		}
		entry, d, err := probeSiteOnce(ruleID, siteIdx, site, sp, o, deadline, samples)
		diag = d
		if err != nil {
			lastErr = err
			if strings.Contains(err.Error(), "时间预算") {
				return nil, diag, err
			}
			continue
		}
		if entry != nil && len(entry.Ranked) > 0 {
			return entry, diag, nil
		}
		if mode != "cf" {
			return nil, diag, nil
		}
	}
	if mode == "cf" {
		return nil, diag, fmt.Errorf("cf 采样仍无可用节点: %v", lastErr)
	}
	return nil, diag, nil
}

func probeSiteOnce(ruleID string, siteIdx int, site rules.Site, sp *rules.Prefer, base prefer.Options, deadline time.Time, samples int) (*prefer.Entry, *siteDiag, error) {
	mode := sp.Mode
	o := base
	o.SamplesPerCIDR = samples
	o.Inflate()
	if mode == "cf" && len(site.Hosts) > 0 {
		o.ValidateHost = site.Hosts[0]
	}

	candidates := sp.Candidates
	if mode == "node" && len(candidates) == 0 {
		for _, h := range site.Handlers {
			if h.Type != "reverse_proxy" {
				continue
			}
			candidates = append(candidates, h.Upstreams...)
			for _, d := range h.DynamicUpstreams {
				candidates = append(candidates, d.Host)
			}
			break
		}
	}
	if len(candidates) == 0 && mode == "node" {
		return nil, nil, fmt.Errorf("node 模式缺少候选")
	}

	ips, err := o.ResolveCandidates(mode, candidates, sp.CIDRs, samples)
	if err != nil {
		return nil, nil, err
	}
	if len(ips) == 0 {
		return nil, nil, fmt.Errorf("无候选 IP")
	}
	diag := &siteDiag{Candidates: len(ips)}

	left := time.Until(deadline)
	if left < 500*time.Millisecond {
		return nil, diag, fmt.Errorf("时间预算不足")
	}
	results := prefer.Probe(ips, &o)
	for _, r := range results {
		if r.OK {
			diag.Latency++
		}
		if r.SpeedMbps > 0 {
			diag.Speeded++
		}
	}
	ranked := prefer.RankTop(results, o.TopN)
	if len(ranked) == 0 {
		return nil, diag, nil
	}
	return &prefer.Entry{RuleID: ruleID, SiteIndex: siteIdx, Mode: mode, Ranked: ranked}, diag, nil
}

func showPrefer(cacheFile, only string) error {
	c, err := prefer.Load(cacheFile)
	if err != nil {
		return err
	}
	if c == nil {
		fmt.Println("无 CDN 优选缓存")
		return nil
	}
	for _, e := range c.Entries {
		if only != "" && e.RuleID != only {
			continue
		}
		for _, rk := range e.Ranked {
			fmt.Printf("%s/site%d %s %s d=%.1fms s=%.2fMB/s\n",
				e.RuleID, e.SiteIndex, e.Mode, rk.IP, rk.DelayMS, rk.SpeedMbps)
		}
	}
	return nil
}

func f2s(v float64) string {
	return fmt.Sprintf("%.1f", v)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "prefer: "+format+"\n", args...)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: prefer <run|show|clear> [flags]")
	fmt.Fprintln(os.Stderr, "  run    对启用 CDN 优选的规则做测速并把 Top-N 写入缓存")
	fmt.Fprintln(os.Stderr, "  show   查看缓存")
	fmt.Fprintln(os.Stderr, "  clear  清除缓存")
	fmt.Fprintln(os.Stderr, "  flags: --root DIR --cache FILE --quick --timeout SEC --rule ID")
	os.Exit(2)
}
