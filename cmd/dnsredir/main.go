// dnsredir 管理本机的 DNS 重定向落地（需 root，systemd 单元或 sudo）：
//
//	resolv apply  -> 把 /etc/resolv.conf 解析器指向 127.0.0.1（先快照，标记块维护）
//	resolv remove -> 撤除标记块，恢复原状
//	lan install   -> iptables PREROUTING 重定向局域网 DNS(53) 到 dnsd 监听端口
//	lan remove    -> 撤销上述 iptables 规则
//	status        -> 汇总两种情况
//
// 注意：resolv apply 会接管 /etc/resolv.conf 的 nameserver；若系统用
// NetworkManager/systemd-resolved 托管该文件，网络变更后可能被覆写，需再次 apply。
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"steam302-web/internal/hosts"
	"steam302-web/internal/rules"
)

const (
	resolvPath = "/etc/resolv.conf"
	resolvMk   = "#S302X-DNS"
	lanChain   = "steam302-web-dns"
)

func main() {
	var (
		root   = flag.String("root", "", "项目根目录（默认从 CWD 向上查找）")
		listen = flag.String("listen", "127.0.0.1:53", "dnsd 监听地址（lan install 用其端口做 REDIRECT 目标）")
		iface  = flag.String("iface", "", "lan install 限定网卡（默认全部 PREROUTING）")
		backup = flag.String("backup", "", "resolv 快照目录（默认 config/resolv_backup）")
		keep   = flag.Int("keep", 10, "resolv 快照保留份数")
	)
	flag.Parse()
	action := flag.Arg(0)

	rootDir := *root
	if rootDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fatal("%v", err)
		}
		rootDir = rules.FindRoot(cwd)
	}
	if rootDir == "" {
		fatal("未找到 config/rules（请用 --root 指定）")
	}
	backupDir := *backup
	if backupDir == "" {
		backupDir = filepath.Join(rootDir, "config", "resolv_backup")
	}

	port, err := parsePort(*listen)
	if err != nil {
		fatal("%v", err)
	}

	switch action {
	case "resolv":
		if flag.Arg(1) == "" {
			fatal("用法: dnsredir resolv <apply|remove>")
		}
		switch flag.Arg(1) {
		case "apply":
			resolvApply(backupDir, *keep)
		case "remove":
			resolvRemove()
		default:
			fatal("未知 resolv 操作: %s", flag.Arg(1))
		}
	case "lan":
		if flag.Arg(1) == "" {
			fatal("用法: dnsredir lan <install|remove>")
		}
		switch flag.Arg(1) {
		case "install":
			lanInstall(port, *iface)
		case "remove":
			lanRemove()
		default:
			fatal("未知 lan 操作: %s", flag.Arg(1))
		}
	case "status":
		res := map[string]any{
			"resolv_managed": resolvManaged(),
			"resolv_file":    resolvPath,
			"lan_redirect":   lanInstalled(),
			"lan_target":     fmt.Sprintf("127.0.0.1:%d", port),
		}
		data, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(data))
	case "":
		fatal("用法: dnsredir <resolv apply|remove> | <lan install|remove> | status [--root DIR] [--listen ADDR]")
	default:
		fatal("未知操作: %s", action)
	}
}

func parsePort(listen string) (int, error) {
	_, pStr, err := net.SplitHostPort(listen)
	if err != nil {
		return 0, fmt.Errorf("listen 需为 host:port: %w", err)
	}
	if pStr == "" {
		return 53, nil
	}
	v, err := net.LookupPort("tcp", pStr)
	if err != nil {
		return 0, fmt.Errorf("无效端口 %s: %w", pStr, err)
	}
	return v, nil
}

// ---- resolv.conf 管理 ----

func resolvApply(backupDir string, keep int) {
	if err := hosts.Snapshot(resolvPath, backupDir, keep); err != nil {
		fatal("快照 %s: %v", resolvPath, err)
	}
	block := "# 由 steam302-web dnsredir 管理（解析先走本机 dnsd，未命中再转发上游）\n" +
		"nameserver 127.0.0.1\n"
	if _, err := hosts.MustApply(resolvPath, resolvMk, block, false); err != nil {
		fatal("写入 %s: %v", resolvPath, err)
	}
	fmt.Printf("已把 %s 解析器指向 127.0.0.1（快照保留 %d 份到 %s）\n", resolvPath, keep, backupDir)
}

func resolvRemove() {
	_, err := hosts.MustRemove(resolvPath, resolvMk, false)
	if err != nil {
		fatal("撤除 %s 标记块: %v", resolvPath, err)
	}
	fmt.Println("已恢复", resolvPath)
}

func resolvManaged() bool {
	data, err := os.ReadFile(resolvPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), resolvMk)
}

// ---- iptables LAN 重定向 ----

// lanInstall 建立自建链 steamp302-web-dns，仅把 53 端口流量 REDIRECT 到 dnsd 端口；
// 其余流量 RETURN。非 root 或缺少 iptables 时返回明确的失败信息。
func lanInstall(port int, iface string) {
	// 创建/清空自建链
	_, _ = iptablesOut("iptables", "-t", "nat", "-N", lanChain)
	if out, err := iptablesOut("iptables", "-t", "nat", "-F", lanChain); err != nil {
		fatal("清空 %s 链: %v\n%s", lanChain, err, out)
	}
	if iface != "" {
		if out, err := iptablesOut("iptables", "-t", "nat", "-I", lanChain,
			"-i", iface, "-j", "RETURN", "-m", "comment", "--comment", lanChain); err != nil {
			fatal("添加网卡例外: %v\n%s", err, out)
		}
	}
	for _, proto := range []string{"udp", "tcp"} {
		if out, err := iptablesOut("iptables", "-t", "nat", "-A", lanChain,
			"-p", proto, "--dport", "53", "-j", "REDIRECT", "--to-ports", fmt.Sprint(port),
			"-m", "comment", "--comment", lanChain); err != nil {
			fatal("添加 %s:53 重定向: %v\n%s", proto, err, out)
		}
	}
	// 挂到 PREROUTING（幂等：先 -D 再 -I）。
	if iface == "" {
		_, _ = iptablesOut("iptables", "-t", "nat", "-D", "PREROUTING", "-j", lanChain, "-m", "comment", "--comment", lanChain)
		if out, err := iptablesOut("iptables", "-t", "nat", "-I", "PREROUTING", "-j", lanChain, "-m", "comment", "--comment", lanChain); err != nil {
			fatal("挂载 PREROUTING: %v\n%s", err, out)
		}
	}
	fmt.Printf("已安装局域网 53→127.0.0.1:%d 重定向（udp+tcp，%s）\n", port, ifaceLabel(iface))
}

func lanRemove() {
	_, _ = iptablesOut("iptables", "-t", "nat", "-D", "PREROUTING", "-j", lanChain, "-m", "comment", "--comment", lanChain)
	_, _ = iptablesOut("iptables", "-t", "nat", "-F", lanChain)
	_, _ = iptablesOut("iptables", "-t", "nat", "-X", lanChain)
	fmt.Println("已移除局域网 DNS 重定向规则")
}

func lanInstalled() bool {
	out, err := iptablesOut("iptables", "-t", "nat", "-L", "PREROUTING", "-n")
	if err != nil {
		return false
	}
	return strings.Contains(string(out), lanChain)
}

func ifaceLabel(iface string) string {
	if iface == "" {
		return "全部网卡"
	}
	return "网卡 " + iface
}

func iptablesOut(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "dnsredir: "+format+"\n", args...)
	os.Exit(1)
}
