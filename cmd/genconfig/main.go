package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"steam302-web/internal/rules"
)

const rulesDir = "config/rules"

func main() {
	var (
		all        = flag.Bool("all", false, "渲染全部规则（含默认关闭）")
		caddyfile  = flag.String("caddyfile", "", "输出 Caddyfile 路径（默认 <root>/Caddyfile）")
		hostsOut   = flag.String("hosts", "", "输出 hosts 片段路径（可选）")
		printOut   = flag.Bool("print", false, "打印 Caddyfile 到 stdout")
		noValidate = flag.Bool("no-validate", false, "跳过 caddy adapt 校验")
		root       = flag.String("root", "", "项目根目录（默认从 CWD 向上查找）")
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

	env, err := rules.LoadEnv(filepath.Join(rootDir, "config", "env.json"))
	if err != nil {
		fatal("加载 env.json: %v", err)
	}

	rs, err := rules.LoadRules(filepath.Join(rootDir, rulesDir), *all)
	if err != nil {
		fatal("加载规则: %v", err)
	}
	if len(rs) == 0 {
		fmt.Fprintln(os.Stderr, "没有可用规则（全部为 enabled=false？用 --all）")
		os.Exit(2)
	}

	for _, r := range rs {
		for _, p := range rules.CheckNeedsFiles(rootDir, r) {
			fmt.Fprintf(os.Stderr, "[%s] 缺少依赖文件: %s（该服务将无法渲染，先创建或改 enabled=false）\n", r.ID, p)
		}
	}

	cf := rules.GenerateCaddyfile(env, rs)
	if *printOut {
		fmt.Print(cf)
		return
	}

	outPath := *caddyfile
	if outPath == "" {
		outPath = filepath.Join(rootDir, "Caddyfile")
	}
	if err := os.WriteFile(outPath, []byte(cf), 0o644); err != nil {
		fatal("写入 %s: %v", outPath, err)
	}
	fmt.Printf("Caddyfile 已生成: %s  (启用 %d 条规则)\n", outPath, len(rs))

	if *hostsOut != "" {
		h := rules.GenerateHosts(env, rs)
		if err := os.WriteFile(*hostsOut, []byte(h), 0o644); err != nil {
			fatal("写入 %s: %v", *hostsOut, err)
		}
		fmt.Printf("hosts 片段已生成: %s\n", *hostsOut)
	}

	if !*noValidate {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "caddy", "adapt", "--config", outPath, "--validate")
		combined, err := cmd.CombinedOutput()
		if err != nil {
			fmt.Fprintln(os.Stderr, "caddy adapt 校验失败:")
			fmt.Fprintln(os.Stderr, strings.TrimSpace(string(combined)))
			os.Exit(1)
		}
		fmt.Println("caddy adapt --validate: OK")
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "genconfig: "+format+"\n", args...)
	os.Exit(1)
}
