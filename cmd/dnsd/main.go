// dnsd 是本机 DNS 重定向服务：S302 劫持域名返回 127.0.0.1，其余域名转发
// 上游 DNS。默认监听 127.0.0.1:53（UDP+TCP），仅依赖 miekg/dns。
//
// 参数优先取 config/env.json -> dns 段，命令行 flag 可覆盖（便于临时调试）。
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"steam302-web/internal/dnsd"
	"steam302-web/internal/rules"
)

const rulesDir = "config/rules"

func main() {
	var (
		root     = flag.String("root", "", "项目根目录（默认从 CWD 向上查找）")
		listen   = flag.String("listen", "", "监听地址（UDP+TCP），空则取 env.json dns.listen")
		upstream = flag.String("upstream", "", "上游 DNS 逗号分隔列表（空则取 env.json dns.upstream）")
		ttl      = flag.Uint("ttl", 0, "劫持应答 TTL（秒），0 则取 env.json dns.ttl")
		answer   = flag.String("answer", "", "劫持域名应答 IP（空则取 env.json dns.answer_ip）")
		userFile = flag.String("user-rules", "", "用户自定义解析规则文件（默认 config/dns_hosts.txt）")
		blFile   = flag.String("blacklist", "", "DNS 黑名单文件（每行一个域名，命中则不劫持直接转发上游；默认 config/dns_blacklist.txt）")
		queryLog = flag.String("query-log", "", "查询日志文件路径（空则按 env.json dns.query_log 判定）")
	)
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

	var env rules.Env
	if data, err := os.ReadFile(filepath.Join(rootDir, "config", "env.json")); err == nil {
		_ = json.Unmarshal(data, &env)
	}

	d := env.DNS
	if d.Listen == "" {
		d.Listen = "127.0.0.1:53"
	}
	if d.TTL == 0 {
		d.TTL = 600
	}
	if d.AnswerIP == "" {
		d.AnswerIP = "127.0.0.1"
	}

	// 命令行覆盖 env。
	if *listen != "" {
		d.Listen = *listen
	}
	if *ttl != 0 {
		d.TTL = uint32(*ttl)
	}
	if *answer != "" {
		d.AnswerIP = *answer
	}
	if *upstream != "" {
		d.Upstream = splitCSV(*upstream)
	}

	rs, err := rules.LoadRules(filepath.Join(rootDir, rulesDir), false)
	if err != nil {
		fatal("加载规则: %v", err)
	}
	bl, err := rules.LoadBlacklist(filepath.Join(rootDir, "config", "blacklist.json"))
	if err != nil {
		fatal("加载黑名单: %v", err)
	}
	if len(bl) > 0 {
		rs = rules.FilterBlacklist(rs, bl)
	}
	domains := collectHosts(rs)
	if len(domains) == 0 {
		fatal("没有可用规则，域名为空")
	}

	var opts []dnsd.Option
	if len(d.Upstream) > 0 {
		opts = append(opts, dnsd.WithUpstreams(d.Upstream))
	}
	opts = append(opts,
		dnsd.WithTTL(d.TTL),
		dnsd.WithAnswerIP(d.AnswerIP),
		dnsd.WithLogger(log.New(os.Stderr, "dnsd: ", log.LstdFlags)),
	)

	// 用户自定义解析规则（config/dns_hosts.txt，可选）。
	userPath := *userFile
	if userPath == "" && d.UserRulesDir != "" {
		userPath = d.UserRulesDir
	}
	if userPath == "" {
		userPath = filepath.Join(rootDir, "config", "dns_hosts.txt")
	}
	if d.UserRules {
		userHosts, n := loadUserHosts(userPath)
		if n > 0 {
			opts = append(opts, dnsd.WithUserHosts(userHosts))
			log.Printf("已加载用户解析规则 %d 条: %s", n, userPath)
		}
	}

	// DNS 黑名单（命中则跳过劫持、直接转发上游）。
	blPath := *blFile
	if blPath == "" && d.BlacklistFile != "" {
		blPath = d.BlacklistFile
	}
	if blPath == "" {
		blPath = filepath.Join(rootDir, "config", "dns_blacklist.txt")
	}
	if !filepath.IsAbs(blPath) {
		blPath = filepath.Join(rootDir, blPath)
	}
	if pats, n := loadHostList(blPath); n > 0 {
		opts = append(opts, dnsd.WithBlacklist(pats))
		log.Printf("已加载 DNS 黑名单 %d 条: %s", n, blPath)
	}

	// 查询日志：flag 显式给路径 > env 开启 > 关闭。
	if *queryLog != "" {
		opts = append(opts, dnsd.WithQueryLogger(newFileLogger(*queryLog)))
	} else if d.QueryLog {
		qf := d.QueryLogFile
		if qf == "" {
			qf = filepath.Join(rootDir, "config", "dnsd_queries.log")
		} else if !filepath.IsAbs(qf) {
			qf = filepath.Join(rootDir, qf)
		}
		opts = append(opts, dnsd.WithQueryLogger(newFileLogger(qf)))
	}

	srv := dnsd.New(d.Listen, domains, opts...)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("dnsd 启动 %s（劫持 %d 个域名，TTL=%ds，上游=%v，用户规则=%d）",
		d.Listen, len(domains), d.TTL, srv.Upstreams(), userPats)
	if err := srv.Serve(ctx); err != nil {
		fatal("%v", err)
	}
}

// userPats 与 loadUserHosts 共用的最新条数（仅用于启动日志）。
var userPats int

// loadHostList 解析纯域名列表文件：每行一个域名（可带 *. 通配），#/空行忽略。
func loadHostList(path string) ([]string, int) {
	if path == "" {
		return nil, 0
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, 0
	}
	defer f.Close()
	var pats []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		pats = append(pats, line)
	}
	return pats, len(pats)
}

// loadUserHosts 解析 dns_hosts.txt：每行 "<域名或 *.域名> <IP>"，#/空行忽略。
func loadUserHosts(path string) (map[string]string, int) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0
	}
	defer f.Close()
	m := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		m[fields[0]] = fields[1]
	}
	userPats = len(m)
	return m, len(m)
}

func splitCSV(in string) []string {
	var out []string
	for _, s := range strings.Split(in, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func newFileLogger(path string) *log.Logger {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	return log.New(f, "", 0)
}

func collectHosts(rs []rules.Rule) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rs {
		for _, s := range r.Sites {
			for _, h := range s.Hosts {
				if seen[h] {
					continue
				}
				seen[h] = true
				out = append(out, h)
			}
		}
	}
	return out
}

func fatal(format string, args ...any) {
	log.Printf("dnsd: "+format, args...)
	os.Exit(1)
}
