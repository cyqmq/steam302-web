package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"steam302-web/internal/hosts"
	"steam302-web/internal/rules"
)

// reset 恢复出厂：停服并取消开机自启、撤销 hosts 劫持、清理生成物
// （Caddyfile / S302.hosts / 证书 / prefer 缓存 / 黑名单 / overrides / 日志）。
// 规则文件与 env.json 保留，便于重新启用。
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

	fmt.Println("[1/4] 停止服务并取消开机自启…")
	units := []string{"steam302-web-caddy.service", "steam302-web-fwd.service", "steam302-web-webui.service"}
	if out, err := exec.Command("systemctl", append([]string{"disable", "--now"}, units...)...).CombinedOutput(); err != nil {
		fmt.Printf("  systemctl disable 提示: %s（继续）\n", strings.TrimSpace(string(out)))
	}

	fmt.Println("[2/4] 撤销 hosts 劫持（移除 marker 行）…")
	env, err := rules.LoadEnv(filepath.Join(rootDir, "config", "env.json"))
	if err == nil {
		hsPath := env.Hosts.File
		if hsPath == "" {
			hsPath = "/etc/hosts"
		}
		marker := env.Hosts.Marker
		if marker == "" {
			marker = "#S302X"
		}
		if _, err := hosts.MustRemove(hsPath, marker, false); err != nil {
			fmt.Printf("  撤销 hosts 失败: %v（继续）\n", err)
		}
	}

	fmt.Println("[3/4] 清理生成物…")
	victims := []string{
		"Caddyfile",
		"S302.hosts",
		filepath.Join("config", "prefer.json"),
		filepath.Join("config", "blacklist.json"),
		filepath.Join("config", "overrides.json"),
		filepath.Join("config", "s302fwd.log"),
		filepath.Join("config", "s302fwd.pid"),
	}
	for _, v := range victims {
		p := filepath.Join(rootDir, v)
		if _, err := os.Stat(p); err == nil {
			if err := os.Remove(p); err != nil {
				fmt.Printf("  删除 %s 失败: %v\n", p, err)
			} else {
				fmt.Printf("  已删除 %s\n", v)
			}
		}
	}
	certsDir := filepath.Join(rootDir, "config", "certs")
	if _, err := os.Stat(certsDir); err == nil {
		if err := os.RemoveAll(certsDir); err != nil {
			fmt.Printf("  删除证书目录失败: %v\n", err)
		} else {
			fmt.Println("  已删除 config/certs/")
		}
	}

	fmt.Println("[4/4] 完成。规则文件与 env.json 保留；重新启用：systemctl enable --now steam302-web-caddy steam302-web-fwd steam302-web-webui")
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "reset: "+format+"\n", args...)
	os.Exit(1)
}
