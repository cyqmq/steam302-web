package main

import (
	"flag"
	"fmt"
	"os"
	"sort"

	"steam302-web/internal/pki"
	"steam302-web/internal/rules"
)

func main() {
	var (
		rootDir     = flag.String("root", "", "项目根目录（默认从 CWD 向上查找）")
		outDir      = flag.String("out", "", "证书输出目录（默认 <root>/config/certs）")
		org         = flag.String("org", "steam302-web Local", "证书组织名")
		caYears     = flag.Int("ca-years", 10, "CA 有效期（年）")
		leafDays    = flag.Int("leaf-days", 365, "叶证书有效期（天）")
		enabledOnly = flag.Bool("enabled-only", false, "仅用默认启用规则的域名作为 SAN（默认覆盖全部规则）")
		resetRoot   = flag.Bool("reset-root", false, "仅重置根证书（CA），叶证书一并重新签发")
		resetLeaf   = flag.Bool("reset-leaf", false, "仅重置网站证书（叶证书），保留现有 CA")
	)
	flag.Parse()

	root := *rootDir
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fatal("%v", err)
		}
		root = rules.FindRoot(cwd)
	}
	if root == "" {
		fatal("未找到 config/rules 目录（请从项目目录运行或用 --root 指定）")
	}

	rs, err := rules.LoadRules(fmt.Sprintf("%s/config/rules", root), true)
	if err != nil {
		fatal("加载规则: %v", err)
	}
	hosts := map[string]bool{}
	for _, r := range rs {
		if *enabledOnly && !r.Enabled {
			continue
		}
		for _, s := range r.Sites {
			for _, h := range s.Hosts {
				hosts[h] = true
			}
		}
	}
	if len(hosts) == 0 {
		fatal("没有可用的域名（SAN 为空）")
	}
	list := make([]string, 0, len(hosts))
	for h := range hosts {
		list = append(list, h)
	}
	sort.Strings(list)

	out := *outDir
	if out == "" {
		out = fmt.Sprintf("%s/config/certs", root)
	}

	var (
		ca      *pki.CA
		keepCA  bool // 仅重置叶证书，保留现有 CA
		writeCA bool // 是否写回 ca.pem / ca.key
	)
	keepCA = *resetLeaf && !*resetRoot
	writeCA = !keepCA
	if keepCA {
		ca, err = pki.LoadCA(out+"/ca.pem", out+"/ca.key")
		if err != nil {
			fatal("加载现有 CA: %v（如 CA 缺失请去掉 --reset-leaf 全量生成）", err)
		}
	} else {
		ca, err = pki.GenerateCA(*org, *caYears)
		if err != nil {
			fatal("生成 CA: %v", err)
		}
	}
	certPEM, keyPEM, err := ca.IssueLeaf(*org, list, *leafDays)
	if err != nil {
		fatal("签发叶证书: %v", err)
	}

	if writeCA {
		if err := pki.WriteFile(out+"/ca.pem", ca.CertPEM(), 0o644); err != nil {
			fatal("写入 ca.pem: %v", err)
		}
		caKeyPEM, err := pki.MarshalECPrivateKeyPEM(ca.Key)
		if err != nil {
			fatal("编码 CA 私钥: %v", err)
		}
		if err := pki.WriteFile(out+"/ca.key", caKeyPEM, 0o600); err != nil {
			fatal("写入 ca.key: %v", err)
		}
	}
	if err := pki.WriteFile(out+"/leaf.pem", certPEM, 0o644); err != nil {
		fatal("写入 leaf.pem: %v", err)
	}
	if err := pki.WriteFile(out+"/leaf.key", keyPEM, 0o600); err != nil {
		fatal("写入 leaf.key: %v", err)
	}
	caLabel := "新"
	if keepCA {
		caLabel = "现有（保留）"
	}
	fmt.Printf("证书已生成到 %s/\n", out)
	fmt.Printf("  ca.pem + ca.key   (%s CA，%d 年)\n", caLabel, *caYears)
	fmt.Printf("  leaf.pem + leaf.key (新，SAN %d 个域名，%d 天)\n", len(list), *leafDays)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "genpki: "+format+"\n", args...)
	os.Exit(1)
}
