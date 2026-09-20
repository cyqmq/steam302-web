package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"steam302-web/internal/hosts"
	"steam302-web/internal/rules"
)

// apply 一键应用：重生成（Caddyfile/S302.hosts）→ 合并写入 /etc/hosts →
// 重启 caddy 与 s302fwd。需要 root 权限（systemd 单元或 sudo）。
func main() {
	root := flag.String("root", "", "项目根目录（默认从 CWD 向上查找）")
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
		fatal("未找到 config/rules（请从项目目录运行或用 --root 指定）")
	}

	gen := filepath.Join(rootDir, "bin", "genconfig")
	if st, err := os.Stat(gen); err != nil || st.IsDir() {
		fatal("缺少 bin/genconfig（先 build）：%v", err)
	}
	hsFile := filepath.Join(rootDir, "S302.hosts")

	env, err := rules.LoadEnv(filepath.Join(rootDir, "config", "env.json"))
	if err != nil {
		fatal("加载 env.json: %v", err)
	}
	hsPath := env.Hosts.File
	if hsPath == "" {
		hsPath = "/etc/hosts"
	}
	marker := env.Hosts.Marker
	if marker == "" {
		marker = "#S302X"
	}
	backupDir := env.Hosts.BackupDir
	if backupDir == "" {
		backupDir = filepath.Join(rootDir, "config", "hosts_backup")
	}
	backupKeep := env.Hosts.BackupKeep
	if backupKeep == 0 {
		backupKeep = 100
	}

	fmt.Println("[1/3] bin/genconfig 重生成配置…")
	if out, err := exec.Command(gen, "--root", rootDir).CombinedOutput(); err != nil {
		fatal("genconfig: %v\n%s", err, out)
	}
	block, err := os.ReadFile(hsFile)
	if err != nil {
		fatal("读取 %s: %v", hsFile, err)
	}

	fmt.Printf("[2/3] 写入 %s（marker %s，先快照保留 %d 份）…\n", hsPath, marker, backupKeep)
	if err := hosts.Snapshot(hsPath, backupDir, backupKeep); err != nil {
		fatal("快照 %s: %v", hsPath, err)
	}
	if _, err := hosts.MustApply(hsPath, marker, string(block), false); err != nil {
		fatal("写入 %s: %v", hsPath, err)
	}

	fmt.Println("[3/3] 重启 steam302-web-caddy / steam302-web-fwd…")
	if out, err := exec.Command("systemctl", "restart", "steam302-web-caddy", "steam302-web-fwd").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "apply: systemctl 重启失败（请手动重启服务）: %v\n%s", err, out)
		os.Exit(1)
	}
	fmt.Println("已应用：配置重生成、/etc/hosts 已更新、caddy & s302fwd 已重启。")
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "apply: "+format+"\n", args...)
	os.Exit(1)
}
