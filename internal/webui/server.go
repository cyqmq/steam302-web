package webui

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"embed"

	"steam302-web/internal/hosts"
	"steam302-web/internal/prefer"
	"steam302-web/internal/rules"
)

//go:embed all:static
var uiFS embed.FS

type Server struct {
	Root  string
	Token string // 非空时启用 HTTP Basic 鉴权（用户固定 steam302）
}

type ruleView struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Group        string   `json:"group"`
	Description  string   `json:"description"`
	Enabled      bool     `json:"enabled"`
	NeedsFiles   []string `json:"needs_files"`
	MissingFiles []string `json:"missing_files"`
	HostCount    int      `json:"host_count"`
}

// settingsView 是「设置」页面的可编辑配置子集（从 config/env.json 派生）。
type settingsView struct {
	BindIP      string             `json:"bind_ip"`
	HTTPSPort   int                `json:"https_port"`
	HTTPPort    int                `json:"http_port"`
	LogMaxBytes int64              `json:"log_max_bytes"`
	BackupKeep  int                `json:"backup_keep"`
	CAYears     int                `json:"ca_years"`
	LeafDays    int                `json:"leaf_days"`
	AutoStart   bool               `json:"autostart"`
	CertExists  bool               `json:"cert_exists"`
	Prefer      rules.PreferConfig `json:"prefer"`
	// 启动行为（原版桌面端的启动/退出行为，Web 版记录偏好）
	AutoStartMode string `json:"autostart_mode"`
	StartService  bool   `json:"start_service"`
	AutoUpdate    bool   `json:"auto_update"`
	ExitSync      bool   `json:"exit_sync"`
	MinimizeTray  bool   `json:"minimize_tray"`
	DevSupport    bool   `json:"dev_support"`
	DevFreq       string `json:"dev_freq"`
	AutoWinProxy  bool   `json:"auto_win_proxy"`
	DNSCDNPrefer  bool   `json:"dns_cdn_prefer"`
	DNSUserRules  bool   `json:"dns_user_rules"`
	DNSLog        bool   `json:"dns_log"`
	// DNS 重定向参数（对应 systemd steam302-web-dnsd / bin/dnsredir）
	DNSListen        string   `json:"dns_listen"`
	DNSUpstream      []string `json:"dns_upstream"`
	DNSTTL           uint32   `json:"dns_ttl"`
	DNSAnswerIP      string   `json:"dns_answer_ip"`
	DNSQueryLog      bool     `json:"dns_query_log"`
	DNSUserRulesFile bool     `json:"dns_user_rules_file"`
	DNSResolvManaged bool     `json:"dns_resolv_managed"`
	DNSLANRedirect   bool     `json:"dns_lan_redirect"`
}

// proxyProfile 是"复制代理参数"面板的数据：给客户端一键配置 hosts /
// curl / PAC / 环境变量的文本片段。
type proxyProfile struct {
	BindIP   string   `json:"bind_ip"`
	HTTPS    int      `json:"https_port"`
	HTTP     int      `json:"http_port"`
	CAExists bool     `json:"ca_exists"`
	CAPath   string   `json:"ca_path"`
	Hosts    []string `json:"hosts"`
	HostsTxt string   `json:"hosts_txt"`
	CurlCmd  string   `json:"curl_cmd"`
	PAC      string   `json:"pac"`
	EnvVars  string   `json:"env"`
}

func (s *Server) profile() (proxyProfile, error) {
	env, err := rules.LoadEnv(s.envPath())
	if err != nil {
		return proxyProfile{}, err
	}
	rs, _, err := s.loadAll()
	if err != nil {
		return proxyProfile{}, err
	}
	bl, err := s.blacklist()
	if err != nil {
		return proxyProfile{}, err
	}
	if len(bl) > 0 {
		rs = rules.FilterBlacklist(rs, bl)
	}
	hostSet := map[string]bool{}
	for _, r := range rs {
		for _, site := range r.Sites {
			for _, h := range site.Hosts {
				hostSet[h] = true
			}
		}
	}
	hosts := make([]string, 0, len(hostSet))
	for h := range hostSet {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	bind := env.Listen.BindIP
	if bind == "" {
		bind = "127.0.0.1"
	}
	if env.Listen.HTTPSPort == 0 {
		env.Listen.HTTPSPort = 25584
	}
	if env.Listen.HTTPPort == 0 {
		env.Listen.HTTPPort = 24196
	}
	caPath := filepath.Join(s.Root, "config", "certs", "ca.pem")
	_, caErr := os.Stat(caPath)
	caExists := caErr == nil

	hostsTxt := ""
	for _, h := range hosts {
		hostsTxt += fmt.Sprintf("%s\t%s\n", bind, h)
	}

	// 白名单式 PAC：仅把被本机劫持/代理的目标域名转发到 caddy（HTTPS 代理），
	// 其余一概 DIRECT。黑名单域名不在 hosts 里，天然被排除，不会 302→502。
	pac := "function FindProxyForURL(url, host) { return \"DIRECT\"; }\n"
	if len(hosts) > 0 {
		conds := make([]string, 0, len(hosts))
		for _, h := range hosts {
			if strings.HasPrefix(h, "*.") {
				conds = append(conds, fmt.Sprintf("host.endsWith(%q)", strings.TrimPrefix(h, "*")))
			} else {
				conds = append(conds, fmt.Sprintf("host === %q || host.endsWith(%q)", h, "."+h))
			}
		}
		pac = fmt.Sprintf(
			"function FindProxyForURL(url, host) {\n"+
				"  /* 白名单式：仅目标会场域名走 HTTPS 代理，其余直连。信任 CA: %s */\n"+
				"  if (%s) return \"HTTPS %s:%d\";\n"+
				"  return \"DIRECT\";\n"+
				"}",
			caPath, strings.Join(conds, " ||\n      "),
			bind, env.Listen.HTTPSPort)
	}

	curlCmd := ""
	curlEnv := ""
	concrete := ""
	for _, h := range hosts {
		if !strings.Contains(h, "*") {
			concrete = h
			break
		}
	}
	if concrete != "" {
		curlCmd = fmt.Sprintf("curl --resolve %s:%d:%s --cacert %s https://%s/",
			concrete, env.Listen.HTTPSPort, bind, caPath, concrete)
		curlEnv = fmt.Sprintf("export HTTPS_PROXY=http://%s:%d\nexport HTTP_PROXY=http://%s:%d\nexport CURL_CA_BUNDLE=%s\n",
			bind, env.Listen.HTTPPort, bind, env.Listen.HTTPPort, caPath)
	}

	return proxyProfile{
		BindIP:   bind,
		HTTPS:    env.Listen.HTTPSPort,
		HTTP:     env.Listen.HTTPPort,
		CAExists: caExists,
		CAPath:   caPath,
		Hosts:    hosts,
		HostsTxt: hostsTxt,
		CurlCmd:  curlCmd,
		PAC:      pac,
		EnvVars:  curlEnv,
	}, nil
}

func (s *Server) overridesPath() string { return filepath.Join(s.Root, "config", "overrides.json") }
func (s *Server) rulesDir() string      { return filepath.Join(s.Root, "config", "rules") }
func (s *Server) envPath() string       { return filepath.Join(s.Root, "config", "env.json") }
func (s *Server) blacklistPath() string { return filepath.Join(s.Root, "config", "blacklist.json") }

func (s *Server) blacklist() ([]string, error) {
	return rules.LoadBlacklist(s.blacklistPath())
}

func (s *Server) loadAll() ([]rules.Rule, map[string]bool, error) {
	ov, err := rules.LoadOverrides(s.overridesPath())
	if err != nil {
		return nil, nil, err
	}
	rs, err := rules.LoadRulesWithOverrides(s.rulesDir(), true, ov)
	if err != nil {
		return nil, nil, err
	}
	return rs, ov, nil
}

func (s *Server) listViews() ([]ruleView, error) {
	rs, _, err := s.loadAll()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Group != rs[j].Group {
			return rs[i].Group < rs[j].Group
		}
		return rs[i].ID < rs[j].ID
	})
	views := make([]ruleView, 0, len(rs))
	for _, r := range rs {
		hosts := 0
		for _, s := range r.Sites {
			hosts += len(s.Hosts)
		}
		views = append(views, ruleView{
			ID:           r.ID,
			Name:         r.Name,
			Group:        r.Group,
			Description:  r.Description,
			Enabled:      r.Enabled,
			NeedsFiles:   r.NeedsFiles,
			MissingFiles: rules.CheckNeedsFiles(s.Root, r),
			HostCount:    hosts,
		})
	}
	return views, nil
}

func (s *Server) saveOverrides(ov map[string]bool) error {
	data, err := json.MarshalIndent(ov, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.overridesPath(), append(data, '\n'), 0o644)
}

type regenResult struct {
	Rules     int    `json:"rules"`
	Hosts     int    `json:"hosts"`
	Caddyfile string `json:"caddyfile"`
	Validate  string `json:"validate"`
}

func (s *Server) regenerate() (regenResult, error) {
	env, err := rules.LoadEnv(s.envPath())
	if err != nil {
		return regenResult{}, err
	}
	ov, err := rules.LoadOverrides(s.overridesPath())
	if err != nil {
		return regenResult{}, err
	}
	rs, err := rules.LoadRulesWithOverrides(s.rulesDir(), false, ov)
	if err != nil {
		return regenResult{}, err
	}
	bl, err := s.blacklist()
	if err != nil {
		return regenResult{}, err
	}
	if len(bl) > 0 {
		rs = rules.FilterBlacklist(rs, bl)
	}
	pref, err := prefer.Load(filepath.Join(s.Root, prefer.Path))
	if err != nil {
		return regenResult{}, err
	}
	cf := rules.GenerateCaddyfile(env, rs, pref)
	cfPath := filepath.Join(s.Root, "Caddyfile")
	if err := os.WriteFile(cfPath, []byte(cf), 0o644); err != nil {
		return regenResult{}, err
	}
	hs := rules.GenerateHosts(env, rs)
	hsPath := filepath.Join(s.Root, "S302.hosts")
	if err := os.WriteFile(hsPath, []byte(hs), 0o644); err != nil {
		return regenResult{}, err
	}
	res := regenResult{
		Rules:     len(rs),
		Hosts:     strings.Count(hs, "\n"),
		Caddyfile: cfPath,
		Validate:  s.validate(cfPath),
	}
	return res, nil
}

func (s *Server) validate(caddyfile string) string {
	if _, err := exec.LookPath("caddy"); err != nil {
		return "skipped: caddy not in PATH"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "caddy", "adapt", "--config", caddyfile, "--validate").CombinedOutput()
	if err != nil {
		return "failed: " + strings.TrimSpace(string(out))
	}
	return "ok"
}

// ---- 设置 ----

const (
	defaultCAYears  = 10
	defaultLeafDays = 365
	defaultLogBytes = 5 << 20
	defaultKeep     = 100
)

func (s *Server) loadEnv() (rules.Env, error) {
	var env rules.Env
	data, err := os.ReadFile(s.envPath())
	if err != nil {
		return env, err
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return env, err
	}
	return env, nil
}

func (s *Server) saveEnv(env rules.Env) error {
	data, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.envPath(), append(data, '\n'), 0o644)
}

func (s *Server) autostartEnabled() bool {
	out, err := exec.Command("systemctl", "is-enabled", "steam302-web-caddy").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "enabled"
}

func (s *Server) settings() settingsView {
	env, err := s.loadEnv()
	if err != nil {
		env = rules.Env{}
	}
	if env.Cert.CAYears == 0 {
		env.Cert.CAYears = defaultCAYears
	}
	if env.Cert.LeafDays == 0 {
		env.Cert.LeafDays = defaultLeafDays
	}
	if env.Fwd.LogMaxBytes == 0 {
		env.Fwd.LogMaxBytes = defaultLogBytes
	}
	if env.Hosts.BackupKeep == 0 {
		env.Hosts.BackupKeep = defaultKeep
	}
	if env.Listen.BindIP == "" {
		env.Listen.BindIP = "127.0.0.1"
	}
	if env.Listen.HTTPSPort == 0 {
		env.Listen.HTTPSPort = 25584
	}
	if env.Listen.HTTPPort == 0 {
		env.Listen.HTTPPort = 24196
	}
	_, statErr := os.Stat(filepath.Join(s.Root, "config", "certs", "ca.pem"))
	sv := settingsView{
		BindIP:           env.Listen.BindIP,
		HTTPSPort:        env.Listen.HTTPSPort,
		HTTPPort:         env.Listen.HTTPPort,
		LogMaxBytes:      env.Fwd.LogMaxBytes,
		BackupKeep:       env.Hosts.BackupKeep,
		CAYears:          env.Cert.CAYears,
		LeafDays:         env.Cert.LeafDays,
		AutoStart:        s.autostartEnabled(),
		CertExists:       statErr == nil,
		Prefer:           env.Prefer,
		ExitSync:         env.UI.ExitSync,
		DevSupport:       env.UI.DevSupport,
		DevFreq:          env.UI.DevFreq,
		AutoWinProxy:     env.UI.AutoWinProxy,
		DNSCDNPrefer:     env.UI.DNSCDNPrefer,
		DNSUserRules:     env.UI.DNSUserRules,
		DNSLog:           env.UI.DNSLog,
		DNSListen:        orStr(env.DNS.Listen, "127.0.0.1:53"),
		DNSUpstream:      env.DNS.Upstream,
		DNSTTL:           env.DNS.TTL,
		DNSAnswerIP:      orStr(env.DNS.AnswerIP, "127.0.0.1"),
		DNSQueryLog:      env.DNS.QueryLog,
		DNSUserRulesFile: env.DNS.UserRules,
		DNSResolvManaged: env.DNS.ResolvManaged,
		DNSLANRedirect:   env.DNS.LANRedirect,
	}
	// 首次（未写偏好）时套用默认：自启服务/自动更新/最小化托盘默认开启；
	// 开机自启模式反映当前 systemd 状态。
	if env.UI.AutoStartMode == "" {
		sv.AutoStartMode = "disabled"
		if sv.AutoStart {
			sv.AutoStartMode = "service"
		}
		sv.StartService = true
		sv.AutoUpdate = true
		sv.MinimizeTray = true
	} else {
		sv.AutoStartMode = env.UI.AutoStartMode
		sv.StartService = env.UI.StartService
		sv.AutoUpdate = env.UI.AutoUpdate
		sv.MinimizeTray = env.UI.MinimizeTray
	}
	if sv.DevFreq == "" {
		sv.DevFreq = "weekly"
	}
	return sv
}

// alignFwdMappings 保持 443→HTTPSPort、80→HTTPPort 两条映射，若端口变化则更新目标。
func alignFwdMappings(in []rules.FwdMap, httpPort, httpsPort int) []rules.FwdMap {
	if httpPort == 0 {
		httpPort = 24196
	}
	if httpsPort == 0 {
		httpsPort = 25584
	}
	out := make([]rules.FwdMap, 0, len(in)+2)
	seen := map[int]bool{}
	for _, m := range in {
		to := m.To
		switch m.From {
		case 443:
			to = httpsPort
		case 80:
			to = httpPort
		}
		if seen[m.From] {
			continue
		}
		seen[m.From] = true
		out = append(out, rules.FwdMap{From: m.From, To: to})
	}
	if !seen[443] {
		out = append(out, rules.FwdMap{From: 443, To: httpsPort})
	}
	if !seen[80] {
		out = append(out, rules.FwdMap{From: 80, To: httpPort})
	}
	return out
}

func (s *Server) applyEnable(units []string, enabled bool) error {
	verb := "disable"
	if enabled {
		verb = "enable"
	}
	args := append([]string{verb}, units...)
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %v\n%s", verb, err, out)
	}
	return nil
}

// ---- 状态汇总 / 服务控制 / 日志 / 版本 ----

// netStatus 是「服务→网络监听/设置信息」面板与侧栏所需的运行时快照。
type netStatus struct {
	BindIP        string            `json:"bind_ip"`
	HTTPSPort     int               `json:"https_port"`
	HTTPPort      int               `json:"http_port"`
	HostsOn       bool              `json:"hosts_on"`
	DNSRedirect   bool              `json:"dns_redirect"`
	AutoProxy     bool              `json:"auto_proxy"`
	SystemProxy   string            `json:"system_proxy"`
	CDNPrefer     bool              `json:"cdn_prefer"`
	CDNPinned     int               `json:"cdn_pinned"`
	HostCount     int               `json:"host_count"`
	Upstream      string            `json:"upstream_domains"`
	AutostartMode string            `json:"autostart_mode"`
	StartService  bool              `json:"start_service"`
	AutoUpdate    bool              `json:"auto_update"`
	ExitSync      bool              `json:"exit_sync"`
	MinimizeTray  bool              `json:"minimize_tray"`
	DevSupport    bool              `json:"dev_support"`
	Services      map[string]string `json:"services"` // caddy/fwd/dnsd/webui: active|inactive|unknown
	Timestamp     string            `json:"timestamp"`
}

func unitActive(name string) string {
	st := strings.TrimSpace(string(mustRun("systemctl", "is-active", name)))
	if st != "active" && st != "activating" && st != "inactive" && st != "failed" {
		return "unknown"
	}
	return st
}

func processRunning(pattern string) bool {
	out, err := exec.Command("pgrep", "-f", pattern).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// svcState 合并 systemd 单元与进程两种视角：单元 active/activating 直接采纳；
// 单元 inactive 但进程在跑（如手工 setsid 启动 caddy）仍报 active，避免“服务在
// 运行却显示已停止”的误报。
func svcState(unit string, patterns ...string) string {
	if st := unitActive(unit); st == "active" || st == "activating" {
		return st
	}
	for _, p := range patterns {
		if processRunning(p) {
			return "active"
		}
	}
	if st := unitActive(unit); st != "" {
		return st
	}
	return "inactive"
}

func mustRun(name string, args ...string) []byte {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return out // systemctl 对 inactive 也返回非零，输出仍有意义
	}
	return out
}

func orStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func (s *Server) hostsOn() bool {
	env, err := s.loadEnv()
	if err != nil {
		return false
	}
	path := env.Hosts.File
	if path == "" {
		path = "/etc/hosts"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	marker := env.Hosts.Marker
	if marker == "" {
		marker = "#S302X"
	}
	return strings.Contains(string(data), marker)
}

func (s *Server) cdnPinnedCount() int {
	c, err := prefer.Load(filepath.Join(s.Root, prefer.Path))
	if err != nil || c == nil {
		return 0
	}
	return len(c.Entries)
}

// upstreamHosts 从 env.json upstream_defaults 汇总上游域名（唯一、去 scheme）。
func upstreamHosts(env rules.Env) string {
	seen := map[string]bool{}
	var hosts []string
	add := func(raw json.RawMessage) {
		var one string
		if json.Unmarshal(raw, &one) == nil {
			if u, err := url.Parse(one); err == nil && u.Host != "" {
				seen[u.Host] = true
			}
			return
		}
		var many []string
		if json.Unmarshal(raw, &many) == nil {
			for _, s := range many {
				if u, err := url.Parse(s); err == nil && u.Host != "" {
					seen[u.Host] = true
				}
			}
		}
	}
	for _, raw := range env.UpstreamDefaults {
		add(raw)
	}
	for h := range seen {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	return strings.Join(hosts, "、")
}

func (s *Server) status() netStatus {
	env, err := s.loadEnv()
	if err != nil {
		env = rules.Env{}
	}
	if env.Listen.HTTPSPort == 0 {
		env.Listen.HTTPSPort = 25584
	}
	if env.Listen.HTTPPort == 0 {
		env.Listen.HTTPPort = 24196
	}
	bind := env.Listen.BindIP
	if bind == "" {
		bind = "127.0.0.1"
	}
	views, err := s.listViews()
	hostCount := 0
	if err == nil {
		for _, v := range views {
			hostCount += v.HostCount
		}
	}
	sv := s.settings()
	up := upstreamHosts(env)
	return netStatus{
		BindIP:        bind,
		HTTPSPort:     env.Listen.HTTPSPort,
		HTTPPort:      env.Listen.HTTPPort,
		HostsOn:       s.hostsOn(),
		DNSRedirect:   unitActive("steam302-web-dnsd.service") == "active",
		AutoProxy:     false, // 我们不做 PAC/显式代理注入，留给客户端自选
		SystemProxy:   "不处理",
		CDNPrefer:     env.Prefer.Enabled,
		CDNPinned:     s.cdnPinnedCount(),
		HostCount:     hostCount,
		Upstream:      up,
		AutostartMode: sv.AutoStartMode,
		StartService:  sv.StartService,
		AutoUpdate:    sv.AutoUpdate,
		ExitSync:      sv.ExitSync,
		MinimizeTray:  sv.MinimizeTray,
		DevSupport:    sv.DevSupport,
		Services: map[string]string{
			"caddy": svcState("steam302-web-caddy.service", "caddy run --config "+filepath.Join(s.Root, "Caddyfile")),
			"fwd":   svcState("steam302-web-fwd.service", "s302fwd run"),
			"dnsd":  svcState("steam302-web-dnsd.service"),
			"webui": svcState("steam302-web-webui.service"),
		},
		Timestamp: time.Now().Format("2006/01/02 15:04:05"),
	}
}

// stopServices 停止本服务栈（systemd 单元 + 手工进程），webui 自身不受影响。
func (s *Server) stopServices() map[string]any {
	var stopped []string
	for _, u := range []string{"steam302-web-caddy.service", "steam302-web-fwd.service", "steam302-web-dnsd.service"} {
		if st := unitActive(u); st == "active" || st == "activating" {
			runOK("systemctl", "stop", u)
			stopped = append(stopped, u)
		}
	}
	// 兜底：非 systemd 启的手工进程按特征结束
	for _, pat := range []string{
		"caddy run --config " + filepath.Join(s.Root, "Caddyfile"),
		"s302fwd run",
	} {
		if runOK("pkill", "-f", pat) {
			stopped = append(stopped, "进程:"+pat)
		}
	}
	return map[string]any{"stopped": stopped, "ts": time.Now().Format("2006/01/02 15:04:05")}
}

func runOK(name string, args ...string) bool {
	return exec.Command(name, args...).Run() == nil
}

type logsView struct {
	File      string   `json:"file"`
	Lines     []string `json:"lines"`
	Total     int      `json:"total"`
	Truncated bool     `json:"truncated"`
}

// tailLogs 取 s302fwd 运行日志的尾部。all=false 固定取最后 maxLines 行。
func (s *Server) tailLogs(all bool, maxLines int) (logsView, error) {
	env, err := s.loadEnv()
	if err != nil {
		return logsView{}, err
	}
	path := env.Fwd.LogFile
	if path == "" {
		path = "config/s302fwd.log"
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(s.Root, path)
	}
	f, err := os.Open(path)
	if err != nil {
		return logsView{}, err
	}
	defer f.Close()
	view := logsView{File: path}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lines := make([]string, 0, 256)
	for sc.Scan() {
		s := sc.Text()
		view.Total++
		if all || len(lines) < maxLines {
			lines = append(lines, s)
			continue
		}
		copy(lines, lines[1:])
		lines[maxLines-1] = s
	}
	view.Truncated = view.Total > len(lines)
	view.Lines = lines
	return view, nil
}

const (
	appName    = "Steamcommunity 302 Web"
	appVersion = "2.0.0"
	appAuthor  = "steam302-web 开发组 · 界面复刻自原版 Steamcommunity 302 (By.羽翼城 | Dogfight360)"
	appHome    = "https://github.com/cyqmq/steam302-web"
)

var (
	latestMu      sync.Mutex
	latestChecked time.Time
	latestVer     string
	latestURL     string
)

// checkLatest 查询上游 GitHub Releases 的最新 tag（30 分钟缓存，失败静默）。
func checkLatest() (ver, page string) {
	latestMu.Lock()
	defer latestMu.Unlock()
	if time.Since(latestChecked) < 30*time.Minute {
		return latestVer, latestURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/cyqmq/steam302-web/releases/latest", nil)
	var gotV, gotU string
	if err == nil {
		req.Header.Set("User-Agent", "steam302-web/webui")
		resp, rerr := http.DefaultClient.Do(req)
		if rerr == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var o struct {
					TagName string `json:"tag_name"`
					HTMLURL string `json:"html_url"`
				}
				if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&o) == nil && o.TagName != "" {
					gotV, gotU = strings.TrimPrefix(o.TagName, "v"), o.HTMLURL
				}
			}
		}
	}
	latestVer, latestURL, latestChecked = gotV, gotU, time.Now()
	return gotV, gotU
}

// newerVersion 按点分段整数比较 cur < latest。
func newerVersion(cur, latest string) bool {
	ck := strings.Split(strings.TrimPrefix(cur, "v"), ".")
	lk := strings.Split(strings.TrimPrefix(latest, "v"), ".")
	n := len(ck)
	if len(lk) > n {
		n = len(lk)
	}
	for i := 0; i < n; i++ {
		a, b := 0, 0
		if i < len(ck) {
			fmt.Sscanf(ck[i], "%d", &a)
		}
		if i < len(lk) {
			fmt.Sscanf(lk[i], "%d", &b)
		}
		if a != b {
			return a < b
		}
	}
	return false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Vite 构建产物：静态资源直出，其余路径回落 index.html（SPA）。
	sub, err := fs.Sub(uiFS, "static")
	if err != nil {
		panic(err)
	}
	indexBytes, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		panic(fmt.Errorf("webui: 读取嵌入式 index.html: %w", err))
	}
	fileHandler := http.FileServer(http.FS(sub))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if f, err := sub.Open(p); err == nil {
			f.Close()
			fileHandler.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexBytes)
	})

	mux.HandleFunc("GET /api/rules", func(w http.ResponseWriter, r *http.Request) {
		views, err := s.listViews()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		enabled := 0
		for _, v := range views {
			if v.Enabled {
				enabled++
			}
		}
		enabledAt, ovAt := "", ""
		if fi, err := os.Stat(s.rulesDir()); err == nil {
			enabledAt = fi.ModTime().Format("2006/01/02 15:04:05")
		}
		if fi, err := os.Stat(s.overridesPath()); err == nil {
			ovAt = fi.ModTime().Format("2006/01/02 15:04:05")
		}
		writeJSON(w, 200, map[string]any{
			"rules": views,
			"meta": map[string]any{
				"count":               len(views),
				"enabled":             enabled,
				"last_edit":           enabledAt,
				"overrides_last_edit": ovAt,
			},
		})
	})

	mux.HandleFunc("POST /api/rules/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Enabled == nil {
			writeJSON(w, 400, map[string]string{"error": "body 需为 {\"enabled\": true|false}"})
			return
		}
		rs, _, err := s.loadAll()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		found := false
		for _, rule := range rs {
			if rule.ID == id {
				found = true
				break
			}
		}
		if !found {
			writeJSON(w, 404, map[string]string{"error": "unknown rule: " + id})
			return
		}
		ov, err := rules.LoadOverrides(s.overridesPath())
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if ov == nil {
			ov = map[string]bool{}
		}
		ov[id] = *body.Enabled
		if err := s.saveOverrides(ov); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		res, err := s.regenerate()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "rule": id, "enabled": *body.Enabled, "regen": res})
	})

	mux.HandleFunc("POST /api/regen", func(w http.ResponseWriter, r *http.Request) {
		res, err := s.regenerate()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "regen": res})
	})

	// 重载服务：重生成配置并重启 caddy/fwd/dnsd（不触碰 /etc/hosts）。
	mux.HandleFunc("POST /api/services/reload", func(w http.ResponseWriter, r *http.Request) {
		res, err := s.regenerate()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		var restarted []string
		var failures []string
		for _, u := range []string{"steam302-web-caddy.service", "steam302-web-fwd.service", "steam302-web-dnsd.service"} {
			if runOK("systemctl", "restart", u) {
				restarted = append(restarted, u)
			} else {
				failures = append(failures, u)
			}
		}
		writeJSON(w, 200, map[string]any{
			"ok": true, "regen": res,
			"restarted": restarted, "failures": failures,
		})
	})

	// 一键应用：sudo -n 调 bin/apply（重生成 → 写 /etc/hosts → 重启 caddy/fwd）。
	// 无免密 sudo 时返回 applied=false，前端提示用命令行手动执行。
	mux.HandleFunc("POST /api/apply", func(w http.ResponseWriter, r *http.Request) {
		applyBin := filepath.Join(s.Root, "bin", "apply")
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "sudo", "-n", applyBin, "--root", s.Root)
		out, err := cmd.CombinedOutput()
		if err != nil {
			code := "sudo -n 不可用或无权限"
			if len(out) > 0 {
				code = strings.TrimSpace(string(out))
			}
			writeJSON(w, 200, map[string]any{
				"applied": false,
				"error":   code,
				"hint":    "请在本机以 root 执行: sudo bin/apply --root " + s.Root,
			})
			return
		}
		writeJSON(w, 200, map[string]any{"applied": true, "out": string(out)})
	})

	mux.HandleFunc("GET /api/profile", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.profile()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, p)
	})

	// PAC 文件导出：浏览器/系统配置自动代理用（白名单式，仅目标域走 HTTPS 代理）。
	mux.HandleFunc("GET /proxy.pac", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.profile()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write([]byte(p.PAC))
	})

	// 同内容以 JSON 返回（供界面预览/复制）。
	mux.HandleFunc("GET /api/pac", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.profile()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"pac": p.PAC, "https_port": p.HTTPS, "http_port": p.HTTP, "bind_ip": p.BindIP})
	})

	// 规则编辑器：GET 原始 JSON 文本；PUT 校验后写盘并重生成。
	// id 仅允许 [a-z0-9_]，杜绝路径穿越。
	validRuleID := func(id string) bool {
		if id == "" || len(id) > 64 {
			return false
		}
		for _, c := range id {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
				return false
			}
		}
		return true
	}
	mux.HandleFunc("GET /api/rule/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !validRuleID(id) {
			writeJSON(w, 400, map[string]string{"error": "非法规则 id"})
			return
		}
		path := filepath.Join(s.rulesDir(), id+".json")
		text, err := os.ReadFile(path)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "规则不存在: " + id})
			return
		}
		writeJSON(w, 200, map[string]any{"id": id, "name": id, "text": string(text)})
	})
	mux.HandleFunc("PUT /api/rule/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !validRuleID(id) {
			writeJSON(w, 400, map[string]string{"error": "非法规则 id"})
			return
		}
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Text) == "" {
			writeJSON(w, 400, map[string]string{"error": "body 需为 {\"text\": \"...\"}"})
			return
		}
		var rl rules.Rule
		if err := json.Unmarshal([]byte(body.Text), &rl); err != nil {
			writeJSON(w, 400, map[string]string{"error": "JSON 无效: " + err.Error()})
			return
		}
		if len(rl.Sites) == 0 {
			writeJSON(w, 400, map[string]string{"error": "规则至少要有一个 sites 条目"})
			return
		}
		if rl.ID != "" && rl.ID != id {
			writeJSON(w, 400, map[string]string{"error": "规则 id 必须与文件名一致（= " + id + "）"})
			return
		}
		rl.ID = id
		// 写完先做一次磁盘级校验：能连同其他规则一起加载才算成功。
		path := filepath.Join(s.rulesDir(), id+".json")
		if err := os.WriteFile(path, []byte(body.Text), 0o644); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if _, _, err := s.loadAll(); err != nil {
			writeJSON(w, 500, map[string]string{"error": "规则集校验失败: " + err.Error()})
			return
		}
		res, err := s.regenerate()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "rule": id, "regen": res})
	})

	// 域名黑名单：GET 返回列表；PUT 用 {"domains": [...]} 整体替换。
	mux.HandleFunc("GET /api/blacklist", func(w http.ResponseWriter, r *http.Request) {
		bl, err := s.blacklist()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"domains": bl})
	})
	mux.HandleFunc("PUT /api/blacklist", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Domains []string `json:"domains"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "body 需为 {\"domains\": [...]}"})
			return
		}
		seen := map[string]bool{}
		var clean []string
		for _, d := range body.Domains {
			d = strings.TrimSpace(d)
			if d == "" || strings.ContainsAny(d, " \t/") {
				writeJSON(w, 400, map[string]string{"error": "非法域名: " + d})
				return
			}
			if !seen[d] {
				seen[d] = true
				clean = append(clean, d)
			}
		}
		data, err := json.MarshalIndent(map[string]any{"domains": clean}, "", "  ")
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if err := os.WriteFile(s.blacklistPath(), append(data, '\n'), 0o644); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		res, err := s.regenerate()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "count": len(clean), "regen": res})
	})

	// 设置：GET 返回可编辑子集；PUT 局部更新（省略字段不变），校验后写 env.json。
	mux.HandleFunc("GET /api/settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.settings())
	})
	mux.HandleFunc("PUT /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			BindIP      *string             `json:"bind_ip"`
			HTTPSPort   *int                `json:"https_port"`
			HTTPPort    *int                `json:"http_port"`
			LogMaxBytes *int64              `json:"log_max_bytes"`
			BackupKeep  *int                `json:"backup_keep"`
			CAYears     *int                `json:"ca_years"`
			LeafDays    *int                `json:"leaf_days"`
			Prefer      *rules.PreferConfig `json:"prefer"`
			// 启动行为偏好
			AutoStartMode *string   `json:"autostart_mode"`
			StartService  *bool     `json:"start_service"`
			AutoUpdate    *bool     `json:"auto_update"`
			ExitSync      *bool     `json:"exit_sync"`
			MinimizeTray  *bool     `json:"minimize_tray"`
			DevSupport    *bool     `json:"dev_support"`
			DevFreq       *string   `json:"dev_freq"`
			AutoWinProxy  *bool     `json:"auto_win_proxy"`
			DNSCDNPrefer  *bool     `json:"dns_cdn_prefer"`
			DNSUserRules  *bool     `json:"dns_user_rules"`
			DNSLog        *bool     `json:"dns_log"`
			DNSListen     *string   `json:"dns_listen"`
			DNSUpstream   *[]string `json:"dns_upstream"`
			DNSTTL        *uint32   `json:"dns_ttl"`
			DNSAnswerIP   *string   `json:"dns_answer_ip"`
			DNSQueryLog   *bool     `json:"dns_query_log"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "body 无效: " + err.Error()})
			return
		}
		if body.BindIP != nil && net.ParseIP(*body.BindIP) == nil {
			writeJSON(w, 400, map[string]string{"error": "bind_ip 不是有效 IP"})
			return
		}
		for _, p := range []struct {
			name string
			v    *int
		}{{"https_port", body.HTTPSPort}, {"http_port", body.HTTPPort}} {
			if p.v != nil && (*p.v < 1 || *p.v > 65535) {
				writeJSON(w, 400, map[string]string{"error": p.name + " 须在 1~65535"})
				return
			}
		}
		if body.LogMaxBytes != nil && *body.LogMaxBytes < 0 {
			writeJSON(w, 400, map[string]string{"error": "log_max_bytes 不能为负"})
			return
		}
		if body.BackupKeep != nil && (*body.BackupKeep < 0 || *body.BackupKeep > 10000) {
			writeJSON(w, 400, map[string]string{"error": "backup_keep 须在 0~10000"})
			return
		}
		if body.CAYears != nil && (*body.CAYears < 1 || *body.CAYears > 100) {
			writeJSON(w, 400, map[string]string{"error": "ca_years 须在 1~100"})
			return
		}
		if body.LeafDays != nil && (*body.LeafDays < 1 || *body.LeafDays > 3650) {
			writeJSON(w, 400, map[string]string{"error": "leaf_days 须在 1~3650"})
			return
		}
		env, err := s.loadEnv()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": "读取 env.json: " + err.Error()})
			return
		}
		if body.BindIP != nil {
			env.Listen.BindIP = *body.BindIP
		}
		if body.HTTPSPort != nil {
			env.Listen.HTTPSPort = *body.HTTPSPort
		}
		if body.HTTPPort != nil {
			env.Listen.HTTPPort = *body.HTTPPort
		}
		if env.Listen.HTTPSPort == env.Listen.HTTPPort {
			writeJSON(w, 400, map[string]string{"error": "http_port 不能与 https_port 相同"})
			return
		}
		// 端口变更时同步重写 fwd 映射的目标端口（443→HTTPS、80→HTTP），
		// 使 s302fwd 重启后转发到新的 caddy 监听。
		env.Fwd.Mappings = alignFwdMappings(env.Fwd.Mappings, env.Listen.HTTPPort, env.Listen.HTTPSPort)
		if body.LogMaxBytes != nil {
			env.Fwd.LogMaxBytes = *body.LogMaxBytes
		}
		if body.BackupKeep != nil {
			env.Hosts.BackupKeep = *body.BackupKeep
		}
		if body.CAYears != nil {
			env.Cert.CAYears = *body.CAYears
		}
		if body.LeafDays != nil {
			env.Cert.LeafDays = *body.LeafDays
		}
		if body.Prefer != nil {
			// 覆盖数值/开关/测速资源字段
			p := env.Prefer
			if body.Prefer.Enabled != p.Enabled {
				p.Enabled = body.Prefer.Enabled
			}
			if body.Prefer.SpeedTest != p.SpeedTest {
				p.SpeedTest = body.Prefer.SpeedTest
			}
			if body.Prefer.LatencyTimeoutMS != 0 {
				p.LatencyTimeoutMS = body.Prefer.LatencyTimeoutMS
			}
			if body.Prefer.LatencyTries != 0 {
				p.LatencyTries = body.Prefer.LatencyTries
			}
			if body.Prefer.Parallel != 0 {
				p.Parallel = body.Prefer.Parallel
			}
			if body.Prefer.MaxMbps >= 0 {
				p.MaxMbps = body.Prefer.MaxMbps
			}
			if body.Prefer.TopN != 0 {
				p.TopN = body.Prefer.TopN
			}
			if body.Prefer.SamplesPerCIDR != 0 {
				p.SamplesPerCIDR = body.Prefer.SamplesPerCIDR
			}
			if body.Prefer.Port != 0 {
				p.Port = body.Prefer.Port
			}
			switch body.Prefer.DownloadURL {
			case "":
			default:
				if u, err := url.Parse(body.Prefer.DownloadURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
					writeJSON(w, 400, map[string]string{"error": "download_url 需为 http(s) 地址"})
					return
				}
				p.DownloadURL = body.Prefer.DownloadURL
			}
			if body.Prefer.DownloadSize != 0 {
				p.DownloadSize = body.Prefer.DownloadSize
			}
			if body.Prefer.DownloadTimeoutS != 0 {
				p.DownloadTimeoutS = body.Prefer.DownloadTimeoutS
			}
			env.Prefer = p
		}
		if body.AutoStartMode != nil {
			env.UI.AutoStartMode = *body.AutoStartMode
		}
		if body.StartService != nil {
			env.UI.StartService = *body.StartService
		}
		if body.AutoUpdate != nil {
			env.UI.AutoUpdate = *body.AutoUpdate
		}
		if body.ExitSync != nil {
			env.UI.ExitSync = *body.ExitSync
		}
		if body.MinimizeTray != nil {
			env.UI.MinimizeTray = *body.MinimizeTray
		}
		if body.DevSupport != nil {
			env.UI.DevSupport = *body.DevSupport
		}
		if body.DevFreq != nil {
			env.UI.DevFreq = *body.DevFreq
		}
		if body.AutoWinProxy != nil {
			env.UI.AutoWinProxy = *body.AutoWinProxy
		}
		if body.DNSCDNPrefer != nil {
			env.UI.DNSCDNPrefer = *body.DNSCDNPrefer
		}
		if body.DNSUserRules != nil {
			env.UI.DNSUserRules = *body.DNSUserRules
		}
		if body.DNSLog != nil {
			env.UI.DNSLog = *body.DNSLog
		}
		if body.DNSListen != nil {
			env.DNS.Listen = *body.DNSListen
		}
		if body.DNSUpstream != nil {
			env.DNS.Upstream = *body.DNSUpstream
		}
		if body.DNSTTL != nil {
			env.DNS.TTL = *body.DNSTTL
		}
		if body.DNSAnswerIP != nil {
			if net.ParseIP(*body.DNSAnswerIP) == nil {
				writeJSON(w, 400, map[string]string{"error": "dns_answer_ip 不是有效 IP"})
				return
			}
			env.DNS.AnswerIP = *body.DNSAnswerIP
		}
		if body.DNSQueryLog != nil {
			env.DNS.QueryLog = *body.DNSQueryLog
		}
		if err := s.saveEnv(env); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, s.settings())
	})

	// 证书重置：root=复位根证书(CA+叶重签)、leaf=仅重签叶。
	mux.HandleFunc("POST /api/settings/certs/{which}", func(w http.ResponseWriter, r *http.Request) {
		which := r.PathValue("which")
		var arg string
		switch which {
		case "root":
			arg = "--reset-root"
		case "leaf":
			arg = "--reset-leaf"
		default:
			writeJSON(w, 400, map[string]string{"error": "which 仅支持 root|leaf"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, filepath.Join(s.Root, "bin", "genpki"), arg, "--root", s.Root).CombinedOutput()
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "output": string(out), "error": err.Error()})
			return
		}
		applyOut, applyErr := s.runApply()
		writeJSON(w, 200, map[string]any{
			"ok": true, "output": string(out),
			"reload": map[string]any{"ok": applyErr == nil, "output": applyOut},
		})
	})

	// 开机自启：enable/disable 三个 systemd 服务。
	mux.HandleFunc("POST /api/settings/autostart", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "body 需为 {\"enabled\": true|false}"})
			return
		}
		units := []string{"steam302-web-caddy.service", "steam302-web-fwd.service", "steam302-web-webui.service"}
		if err := s.applyEnable(units, body.Enabled); err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "enabled": body.Enabled})
	})

	// 恢复出厂：bin/reset（停服、取消自启、撤销 hosts 劫持、清理生成物）。
	mux.HandleFunc("POST /api/settings/reset", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Confirm bool `json:"confirm"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !body.Confirm {
			writeJSON(w, 400, map[string]string{"error": "需 {\"confirm\": true}"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "sudo", "-n", filepath.Join(s.Root, "bin", "reset"), "--root", s.Root).CombinedOutput()
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "output": string(out), "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "output": string(out)})
	})

	// 全选/全不选：一次性写入全部规则覆盖并重生成一次。
	mux.HandleFunc("POST /api/rules/bulk", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Enabled == nil {
			writeJSON(w, 400, map[string]string{"error": "body 需为 {\"enabled\": true|false}"})
			return
		}
		rs, _, err := s.loadAll()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		ov, err := rules.LoadOverrides(s.overridesPath())
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if ov == nil {
			ov = map[string]bool{}
		}
		for _, rule := range rs {
			ov[rule.ID] = *body.Enabled
		}
		if err := s.saveOverrides(ov); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		res, err := s.regenerate()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "enabled": *body.Enabled, "count": len(rs), "regen": res})
	})

	// 运行时状态快照：服务 tab 右侧「网络监听/设置信息」+ 侧栏服务灯。
	mux.HandleFunc("GET /api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.status())
	})

	// 服务运行日志（尾部）。?all=1 返回全部行，否则取最后 1000 行。
	mux.HandleFunc("GET /api/logs", func(w http.ResponseWriter, r *http.Request) {
		all := r.URL.Query().Get("all") == "1"
		view, err := s.tailLogs(all, 1000)
		if err != nil {
			writeJSON(w, 200, map[string]any{"file": "", "lines": []string{}, "total": 0, "error": err.Error()})
			return
		}
		writeJSON(w, 200, view)
	})

	// 停止服务（caddy/fwd/dnsd）。webui 独立存活，可用「重载服务」恢复。
	mux.HandleFunc("POST /api/services/stop", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.stopServices())
	})

	// DNS 重定向模式开关：直接启停 steam302-web-dnsd.service。
	mux.HandleFunc("POST /api/services/dns", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Enabled == nil {
			writeJSON(w, 400, map[string]string{"error": "body 需为 {\"enabled\": true|false}"})
			return
		}
		verb := "start"
		if !*body.Enabled {
			verb = "stop"
		}
		mustRun("systemctl", verb, "steam302-web-dnsd.service")
		writeJSON(w, 200, map[string]any{"ok": true, "enabled": *body.Enabled, "state": unitActive("steam302-web-dnsd.service")})
	})

	// 证书区（CA 下载/状态/系统信任）：
	s.registerCert(mux)

	// DNS 重定向（解析器接管 / 局域网重定向）：
	s.registerDNS(mux)

	// 连接监控（代理到 s302fwd 回环管理接口）：
	s.registerConns(mux)

	// CDN 优选定时健康检测：
	s.registerPrefer(mux)

	// hosts 劫持开关：on=重生成后经 apply 写入 /etc/hosts；off=按 marker 撤回劫持段。
	mux.HandleFunc("POST /api/hosts", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Enabled == nil {
			writeJSON(w, 400, map[string]string{"error": "body 需为 {\"enabled\": true|false}"})
			return
		}
		if *body.Enabled {
			out, err := s.runApply()
			writeJSON(w, 200, map[string]any{
				"ok":      err == nil,
				"enabled": true,
				"out":     string(out),
				"error":   errString(err),
			})
			return
		}
		env, err := s.loadEnv()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		path := env.Hosts.File
		if path == "" {
			path = "/etc/hosts"
		}
		marker := env.Hosts.Marker
		if marker == "" {
			marker = "#S302X"
		}
		_, rerr := hosts.MustRemove(path, marker, false)
		if rerr != nil {
			writeJSON(w, 500, map[string]string{"error": rerr.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "enabled": false})
	})

	// 版本/关于信息：含上游最新版检测与是否有更新。
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		latest, page := checkLatest()
		writeJSON(w, 200, map[string]any{
			"name":       appName,
			"version":    appVersion,
			"author":     appAuthor,
			"homepage":   appHome,
			"latest":     latest,
			"latest_url": page,
			"has_update": newerVersion(appVersion, latest),
		})
	})

	return mux
}

func (s *Server) runApply() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sudo", "-n", filepath.Join(s.Root, "bin", "apply"), "--root", s.Root).CombinedOutput()
	return string(out), err
}

// tokenAuth 用 HTTP Basic（用户固定 steam302）或 ?token= 参数做鉴权。
// 本机（回环来源）免鉴权；Token 为空时一律透传（保持回环裸访问的既有行为）。
func tokenAuth(token string, h http.Handler) http.Handler {
	if token == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if clientIsLoopback(r.RemoteAddr) || r.URL.Query().Get("token") == token || basicTokenOK(r, token) || cookieTokenOK(r, token) {
			h.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="steam302", charset="UTF-8"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	})
}

// indexTokenCookie 把 URL 中的 ?token= 沉淀为 s302token Cookie，
// 使 Vite 产物里 <link>/<script> 的静态资源请求不再需要手动带 token。
func indexTokenCookie(token string, h http.Handler) http.Handler {
	if token == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !clientIsLoopback(r.RemoteAddr) && subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(token)) == 1 {
			http.SetCookie(w, &http.Cookie{
				Name:     "s302token",
				Value:    token,
				Path:     "/",
				MaxAge:   2592000,
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			})
		}
		h.ServeHTTP(w, r)
	})
}

func cookieTokenOK(r *http.Request, token string) bool {
	c, err := r.Cookie("s302token")
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(token)) == 1
}

func clientIsLoopback(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	return isLoopbackHost(host)
}

func basicTokenOK(r *http.Request, token string) bool {
	user, pass, ok := r.BasicAuth()
	if !ok || user != "steam302" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(pass), []byte(token)) == 1
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) Listen(addr string) error {
	if s.Root == "" {
		return errors.New("webui: empty root")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if s.Token == "" && !isLoopbackHost(host) {
		return fmt.Errorf("webui: 监听 %s 需设置 --token（局域网/公网必须鉴权）", addr)
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           tokenAuth(s.Token, indexTokenCookie(s.Token, s.Handler())),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if s.Token == "" {
		fmt.Printf("WebUI: http://%s/ (无鉴权)\n", addr)
	} else {
		fmt.Printf("WebUI: http://%s/ (Basic 鉴权，用户 steam302)\n", addr)
	}
	return srv.ListenAndServe()
}
