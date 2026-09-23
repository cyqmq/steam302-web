package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"steam302-web/internal/fwd"
	"steam302-web/internal/rules"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	flagSet := flag.NewFlagSet(cmd, flag.ExitOnError)
	root := flagSet.String("root", "", "项目根目录（默认从 CWD 向上查找）")
	if err := flagSet.Parse(os.Args[2:]); err != nil {
		os.Exit(2)
	}
	rootDir := resolveRoot(*root)

	env, err := rules.LoadEnv(filepath.Join(rootDir, "config", "env.json"))
	if err != nil {
		fatal("加载 env.json: %v", err)
	}
	cf := fwd.Config{
		Bind:      env.Fwd.Bind,
		PidFile:   abs(rootDir, env.Fwd.PidFile),
		LogFile:   abs(rootDir, env.Fwd.LogFile),
		Mappings:  maps(env.Fwd.Mappings),
		AdminAddr: env.Fwd.AdminAddr,
	}
	daemon := &fwd.Daemon{
		Bind:        cf.Bind,
		PidFile:     cf.PidFile,
		LogFile:     cf.LogFile,
		MaxLogBytes: cf.LogMaxBytes,
		WorkDir:     rootDir,
	}

	switch cmd {
	case "run":
		run(cf)
	case "up":
		started, err := daemon.Up()
		if err != nil {
			fatal("启动失败: %v", err)
		}
		if started {
			fmt.Println("已在运行")
		} else {
			fmt.Printf("已启动转发进程（pidfile: %s）\n", cf.PidFile)
		}
	case "down":
		stopped, err := daemon.Down()
		if err != nil {
			fatal("停止失败: %v", err)
		}
		if stopped {
			fmt.Println("已停止转发")
		} else {
			fmt.Println("未在运行")
		}
	case "status":
		pid, running := daemon.Status()
		if running {
			fmt.Printf("运行中 pid=%d\n", pid)
		} else {
			fmt.Println("未运行")
		}
		for _, m := range cf.Mappings {
			fmt.Printf("  %s → %s:%d\n", netAddr(cf.Bind, m.From), cf.Bind, m.To)
		}
	default:
		fatal("未知子命令: %s", cmd)
	}
}

func run(cf fwd.Config) {
	if cf.IsZero() {
		fatal("env.json 未配置 fwd.mappings")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fmt.Printf("s302fwd: 监听 %s（Ctrl-C 退出）\n", cf.Bind)
	for _, m := range cf.Mappings {
		fmt.Printf("  %s → %s:%d\n", netAddr(cf.Bind, m.From), cf.Bind, m.To)
	}
	if err := fwd.Serve(ctx, cf); err != nil {
		fatal("%v", err)
	}
}

func maps(in []rules.FwdMap) []fwd.Mapping {
	out := make([]fwd.Mapping, 0, len(in))
	for _, m := range in {
		out = append(out, fwd.Mapping{From: m.From, To: m.To, UDP: m.UDP})
	}
	return out
}

func resolveRoot(root string) string {
	if root != "" {
		return root
	}
	cwd, err := os.Getwd()
	if err != nil {
		fatal("%v", err)
	}
	return rules.FindRoot(cwd)
}

func netAddr(bind string, port int) string {
	if bind == "" {
		bind = "127.0.0.1"
	}
	return fmt.Sprintf("%s:%d", bind, port)
}

func abs(root, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(root, p)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "s302fwd: "+format+"\n", args...)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: s302fwd <run|up|down|status> [--root DIR]")
	fmt.Fprintln(os.Stderr, "  run      前台运行端口转发（Ctrl-C 退出）")
	fmt.Fprintln(os.Stderr, "  up       启动转发 daemon（写 pidfile，脱离会话）")
	fmt.Fprintln(os.Stderr, "  down     停止 daemon")
	fmt.Fprintln(os.Stderr, "  status   查看运行状态")
	fmt.Fprintln(os.Stderr, "转发规则来自 config/env.json -> fwd（默认 443→25584、80→24196）")
	os.Exit(2)
}
