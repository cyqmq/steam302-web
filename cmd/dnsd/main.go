// dnsd 是本机 DNS 重定向服务：S302 劫持域名返回 127.0.0.1，其余域名转发
// 上游 DNS。默认监听 127.0.0.1:53（UDP+TCP），仅依赖 miekg/dns。
package main

import (
	"context"
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
		listen   = flag.String("listen", "127.0.0.1:53", "监听地址（UDP+TCP）")
		upstream = flag.String("upstream", "", "上游 DNS 逗号分隔列表（默认读取 /etc/resolv.conf）")
		ttl      = flag.Uint("ttl", 600, "劫持应答 TTL（秒）")
		answer   = flag.String("answer", "127.0.0.1", "劫持域名应答 IP")
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
	if *upstream != "" {
		opts = append(opts, dnsd.WithUpstreams(strings.Split(*upstream, ",")))
	}
	opts = append(opts,
		dnsd.WithTTL(uint32(*ttl)),
		dnsd.WithAnswerIP(*answer),
		dnsd.WithLogger(log.New(os.Stderr, "dnsd: ", log.LstdFlags)),
	)

	srv := dnsd.New(*listen, domains, opts...)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("dnsd 启动 %s（劫持 %d 个域名，TTL=%ds，上游=%v）", *listen, len(domains), *ttl, srv.Upstreams())
	if err := srv.Serve(ctx); err != nil {
		fatal("%v", err)
	}
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
