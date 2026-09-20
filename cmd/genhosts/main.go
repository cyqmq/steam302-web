package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"steam302-web/internal/hosts"
	"steam302-web/internal/rules"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	flagSet := flag.NewFlagSet(cmd, flag.ExitOnError)
	root := flagSet.String("root", "", "项目根目录（默认从 CWD 向上查找）")
	target := flagSet.String("target", "", "目标 hosts 文件（默认取 env.json 或 /etc/hosts）")
	dryRun := flagSet.Bool("dry-run", false, "只打印结果不落盘")
	if err := flagSet.Parse(args); err != nil {
		os.Exit(2)
	}

	rootDir := resolveRoot(*root)
	env, err := rules.LoadEnv(filepath.Join(rootDir, "config", "env.json"))
	if err != nil {
		fatal("加载 env.json: %v", err)
	}
	path := *target
	if path == "" {
		path = env.Hosts.File
		if path == "" {
			path = "/etc/hosts"
		}
	}
	marker := env.Hosts.Marker
	if marker == "" {
		marker = "#S302X"
	}
	backupDir := env.Hosts.BackupDir
	if backupDir == "" {
		backupDir = filepath.Join(rootDir, "config", "hosts_backup")
	}

	switch cmd {
	case "apply":
		if flagSet.NArg() != 1 {
			fatal("apply 需要一个 hosts 片段文件参数")
		}
		block, err := os.ReadFile(flagSet.Arg(0))
		if err != nil {
			fatal("读取片段: %v", err)
		}
		if !*dryRun {
			if err := hosts.Backup(path, backupDir); err != nil {
				fatal("备份 %s: %v", path, err)
			}
		}
		next, err := hosts.MustApply(path, marker, string(block), *dryRun)
		if err != nil {
			fatal("应用: %v", err)
		}
		report(*dryRun, path, marker, next)
	case "remove":
		if !*dryRun {
			if err := hosts.Backup(path, backupDir); err != nil {
				fatal("备份 %s: %v", path, err)
			}
		}
		next, err := hosts.MustRemove(path, marker, *dryRun)
		if err != nil {
			fatal("移除: %v", err)
		}
		report(*dryRun, path, marker, next)
	case "revert":
		if err := hosts.Revert(path, backupDir); err != nil {
			fatal("回滚: %v", err)
		}
		fmt.Printf("已回滚 %s 至备份\n", path)
	case "status":
		cur, err := hosts.Read(path)
		if err != nil {
			fatal("读取 %s: %v", path, err)
		}
		_, ours := hosts.Split(cur, marker)
		fmt.Printf("%s: %d 条 %s 规则生效\n", path, len(ours), marker)
		for _, l := range ours {
			fmt.Println("  " + l)
		}
	default:
		fatal("未知子命令: %s", cmd)
	}
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

func report(dryRun bool, path, marker, content string) {
	verb := "将写入"
	if dryRun {
		verb = "（dry-run，未落盘）"
	} else {
		verb = "已写入"
	}
	fmt.Printf("%s %s\n", verb, path)
	_, ours := hosts.Split(content, marker)
	fmt.Printf("  marker(%s) 规则行: %d\n", marker, len(ours))
	if dryRun {
		if strings.TrimSpace(content) != "" {
			fmt.Println(strings.TrimRight(content, "\n"))
		}
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "genhosts: "+format+"\n", args...)
	os.Exit(1)
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: genhosts <apply|remove|revert|status> [flags] <args>")
	fmt.Fprintln(os.Stderr, "  apply <hosts片段文件>  合并写入（先备份，移除旧 marker 行后追加）")
	fmt.Fprintln(os.Stderr, "  remove                 移除本工具写过的 marker 行")
	fmt.Fprintln(os.Stderr, "  revert                 从备份恢复原 hosts")
	fmt.Fprintln(os.Stderr, "  status                 查看生效的 marker 行")
	fmt.Fprintln(os.Stderr, "  flags: --root DIR --target FILE --dry-run")
	os.Exit(2)
}
