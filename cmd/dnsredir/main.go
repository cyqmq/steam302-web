// dnsredir 管理本机的 DNS/局域网重定向落地（需 root，systemd 单元或 sudo）：
//
//	resolv apply  -> 把 /etc/resolv.conf 解析器指向 127.0.0.1（先快照，标记块维护）
//	resolv remove -> 撤除标记块，恢复原状
//	lan install   -> 局域网重定向：DNS(53) 到 dnsd；可选 443/80 转发
//	lan preview   -> 仅打印将要施行的防火墙规则（不执行）
//	lan remove    -> 撤销上述规则
//	status        -> 汇总两种情况
//
// 防火墙后端由 --backend（或 env dns.firewall_backend）决定：
//   iptables / nftables / auto（按 nft 是否存在探测，缺省 iptables）。
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
	lanTable   = "s302"
)

func main() {
	var (
		root      = flag.String("root", "", "项目根目录（默认从 CWD 向上查找）")
		listen    = flag.String("listen", "127.0.0.1:53", "dnsd 监听地址（lan install 用其端口做 REDIRECT 目标）")
		iface     = flag.String("iface", "", "lan install 限定网卡（默认全部 PREROUTING）")
		backup    = flag.String("backup", "", "resolv 快照目录（默认 config/resolv_backup）")
		keep      = flag.Int("keep", 10, "resolv 快照保留份数")
		backend   = flag.String("backend", "auto", "防火墙后端: auto|iptables|nftables")
		httpsFwd  = flag.Int("https-fwd", 0, "局域网 TCP 443 转发目标端口（0=不启用）")
		httpFwd   = flag.Int("http-fwd", 0, "局域网 TCP 80 转发目标端口（0=不启用）")
	)
	flagArgs, posArgs := splitArgs(os.Args[1:])
	if err := flag.CommandLine.Parse(flagArgs); err != nil {
		fatal("%v", err)
	}
	// positionals 来自 reorderArgs（支持 flags 出现在子命令之后的写法）。
	action := ""
	if len(posArgs) > 0 {
		action = posArgs[0]
	}
	sub := func(i int) string {
		if i+1 < len(posArgs) {
			return posArgs[i+1]
		}
		return ""
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

	// env dns.firewall_backend 可作持久化偏好（命令 flag 优先）。
	if b := envFirewallBackend(rootDir); *backend == "auto" && b != "" {
		if b == "iptables" || b == "nftables" {
			*backend = b
		}
	}
	if *backend == "auto" {
		*backend = detectBackend()
	}

	switch action {
	case "resolv":
		if sub(0) == "" {
			fatal("用法: dnsredir resolv <apply|remove>")
		}
		switch sub(0) {
		case "apply":
			resolvApply(backupDir, *keep)
		case "remove":
			resolvRemove()
		default:
			fatal("未知 resolv 操作: %s", sub(0))
		}
	case "lan":
		if sub(0) == "" {
			fatal("用法: dnsredir lan <install|preview|remove>")
		}
		switch sub(0) {
		case "install":
			lanInstall(port, *iface, *backend, *httpsFwd, *httpFwd)
		case "preview":
			for i, cmd := range lanPlan(port, *iface, *backend, *httpsFwd, *httpFwd) {
				fmt.Printf("  %d) %s\n", i+1, strings.Join(cmd, " "))
			}
		case "remove":
			lanRemove(*backend)
		default:
			fatal("未知 lan 操作: %s", sub(0))
		}
	case "status":
		res := map[string]any{
			"resolv_managed": resolvManaged(),
			"resolv_file":    resolvPath,
			"firewall_used":  installedBackend(port),
			"lan_redirect":   lanInstalledAny(port),
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

// ---- 防火墙 LAN 重定向（iptables / nftables 双后端）----

// splitArgs 把 os.Args[1:] 分离为 flag 参数与位置参数；支持
// "lan install --backend iptables" 这种 flags 在子命令之后的常见写法
// （flag 包原生在首个非 flag 参数处停止解析）。
func splitArgs(args []string) (flagArgs, posArgs []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			posArgs = append(posArgs, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			flagArgs = append(flagArgs, a)
			if !strings.Contains(a, "=") && i+1 < len(args) {
				flagArgs = append(flagArgs, args[i+1])
				i++
			}
			continue
		}
		posArgs = append(posArgs, a)
	}
	return flagArgs, posArgs
}

// envFirewallBackend 读取 env.dns.firewall_backend 偏好。
func envFirewallBackend(root string) string {
	env, err := rules.LoadEnv(filepath.Join(root, "config", "env.json"))
	if err != nil {
		return ""
	}
	return env.DNS.FirewallBackend
}

func detectBackend() string {
	if _, err := exec.LookPath("nft"); err == nil {
		return "nftables"
	}
	return "iptables"
}

// lanPlan 构建将要施行的规则（[][]string：每条命令的 argv），便于 install 执行
// 与 preview 打印，保证两种后端行为等效。
func lanPlan(port int, iface, backend string, httpsFwd, httpFwd int) [][]string {
	if backend == "nftables" {
		return nftPlan(port, iface, httpsFwd, httpFwd)
	}
	return iptPlan(port, iface, httpsFwd, httpFwd)
}

// iptPlan 生成 iptables nat 表规则（自建链 lanChain）。
func iptPlan(port int, iface string, httpsFwd, httpFwd int) [][]string {
	var plan [][]string
	nat := func(args ...string) []string {
		return append([]string{"iptables", "-t", "nat"}, args...)
	}
	plan = append(plan, nat("-N", lanChain))
	plan = append(plan, nat("-F", lanChain))
	if iface != "" {
		plan = append(plan, nat("-I", lanChain, "-i", iface, "-j", "RETURN", "-m", "comment", "--comment", lanChain))
	}
	for _, proto := range []string{"udp", "tcp"} {
		plan = append(plan, nat("-A", lanChain, "-p", proto, "--dport", "53", "-j", "REDIRECT", "--to-ports", fmt.Sprint(port), "-m", "comment", "--comment", lanChain))
	}
	if fwd := fwdDefPort("https", httpsFwd); fwd > 0 {
		plan = append(plan, nat("-A", lanChain, "-p", "tcp", "--dport", "443", "-j", "REDIRECT", "--to-ports", fmt.Sprint(fwd), "-m", "comment", "--comment", lanChain))
	}
	if fwd := fwdDefPort("http", httpFwd); fwd > 0 {
		plan = append(plan, nat("-A", lanChain, "-p", "tcp", "--dport", "80", "-j", "REDIRECT", "--to-ports", fmt.Sprint(fwd), "-m", "comment", "--comment", lanChain))
	}
	plan = append(plan, nat("-D", "PREROUTING", "-j", lanChain, "-m", "comment", "--comment", lanChain))
	plan = append(plan, nat("-I", "PREROUTING", "-j", lanChain, "-m", "comment", "--comment", lanChain))
	return plan
}

// nftPlan 生成 nftables inet s302 表规则（等价的 nat hook）。
func nftPlan(port int, iface string, httpsFwd, httpFwd int) [][]string {
	base := []string{"nft", "-a"}
	var plan [][]string
	add := func(args ...string) {
		plan = append(plan, append(append([]string{}, base...), args...))
	}
	add("add", "table", "inet", lanTable)
	add("flush", "table", "inet", lanTable)
	add("add", "chain", "inet", lanTable, "pre", "{", "type", "nat", "hook", "prerouting", "priority", "-100", ";", "}")
	if iface != "" {
		add("add", "rule", "inet", lanTable, "pre", "iifname", fmt.Sprintf("%q", iface), "accept")
	}
	for _, proto := range []string{"udp", "tcp"} {
		add("add", "rule", "inet", lanTable, "pre", proto, "dport", "53", "redirect", "to", ":", fmt.Sprint(port))
	}
	if fwd := fwdDefPort("https", httpsFwd); fwd > 0 {
		add("add", "rule", "inet", lanTable, "pre", "tcp", "dport", "443", "redirect", "to", ":", fmt.Sprint(fwd))
	}
	if fwd := fwdDefPort("http", httpFwd); fwd > 0 {
		add("add", "rule", "inet", lanTable, "pre", "tcp", "dport", "80", "redirect", "to", ":", fmt.Sprint(fwd))
	}
	return plan
}

// fwdDefPort 用显式 flag 优先；0 表示未提供。
func fwdDefPort(_ string, v int) int { return v }

// lanInstall 依据所选后端施行规则。非 root 或缺少后端时返回明确错误。
func lanInstall(port int, iface, backend string, httpsFwd, httpFwd int) {
	plan := lanPlan(port, iface, backend, httpsFwd, httpFwd)
	for _, cmd := range plan {
		if out, err := run(ctxTimeout(), cmd[0], cmd[1:]...); err != nil {
			fatal("执行 %s 失败: %v\n%s", strings.Join(cmd, " "), err, out)
		}
	}
	dns := "127.0.0.1:" + fmt.Sprint(port)
	extra := ""
	if fwdDefPort("https", httpsFwd) > 0 {
		extra += " TCP443→" + fmt.Sprint(httpsFwd)
	}
	if fwdDefPort("http", httpFwd) > 0 {
		extra += " TCP80→" + fmt.Sprint(httpFwd)
	}
	fmt.Printf("已安装局域网重定向（后端 %s，DNS 53→%s%s，%s）\n", backend, dns, extra, ifaceLabel(iface))
}

func lanRemove(backend string) {
	if backend == "nftables" {
		if out, err := run(ctxTimeout(), "nft", "delete", "table", "inet", lanTable); err != nil && !lanStateNotFound(err, out) {
			fatal("删除 nft 表: %v\n%s", err, out)
		}
	} else {
		for _, args := range [][]string{
			{"iptables", "-t", "nat", "-D", "PREROUTING", "-j", lanChain, "-m", "comment", "--comment", lanChain},
			{"iptables", "-t", "nat", "-F", lanChain},
			{"iptables", "-t", "nat", "-X", lanChain},
		} {
			_, _ = run(ctxTimeout(), args[0], args[1:]...)
		}
	}
	fmt.Println("已移除局域网 DNS 重定向规则（" + backend + "）")
}

// lanInstalledAny 检测任一后端是否已装。
func lanInstalledAny(port int) bool {
	if out, err := run(ctxTimeout(), "iptables", "-t", "nat", "-L", "PREROUTING", "-n"); err == nil && strings.Contains(string(out), lanChain) {
		return true
	}
	if out, err := run(ctxTimeout(), "nft", "list", "table", "inet", lanTable); err == nil {
		return strings.Contains(string(out), "redirect") && strings.Contains(string(out), "s302")
	}
	return false
}

// installedBackend 报告当前实际生效的后端（用于 status JSON）。
func installedBackend(port int) string {
	if lanInstalledAny(port) {
		if _, err := run(ctxTimeout(), "nft", "list", "table", "inet", lanTable); err == nil {
			return "nftables"
		}
		return "iptables"
	}
	return "none"
}

func lanStateNotFound(err error, out []byte) bool {
	msg := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(msg, "no such file") || strings.Contains(msg, "no such table")
}

func ifaceLabel(iface string) string {
	if iface == "" {
		return "全部网卡"
	}
	return "网卡 " + iface
}

func ctxTimeout() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	_ = cancel // CommandContext 使用 ctx.Deadline() 超时；返回的 cancel 由调用方运行结束自然释放
	return ctx
}

func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "dnsredir: "+format+"\n", args...)
	os.Exit(1)
}
