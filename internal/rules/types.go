package rules

import "encoding/json"

type Env struct {
	SchemaVersion    int                        `json:"schema_version"`
	Listen           Listen                     `json:"listen"`
	Cert             Cert                       `json:"cert"`
	Hosts            Hosts                      `json:"hosts"`
	Fwd              Fwd                        `json:"fwd"`
	Prefer           PreferConfig               `json:"prefer"`
	DNS              DNS                        `json:"dns"`
	Update           Update                     `json:"update,omitempty"`
	UpstreamDefaults map[string]json.RawMessage `json:"upstream_defaults"`
	UI               UI                         `json:"ui,omitempty"`
	Notes            string                     `json:"notes"`
}

// DNS 是本机 DNS 重定向（systemd steam302-web-dnsd）的可配置参数，
// 由 cmd/dnsd 读取；Listen 为空时保持默认 127.0.0.1:53。
type DNS struct {
	Listen       string   `json:"listen,omitempty"`
	Upstream     []string `json:"upstream,omitempty"`
	TTL          uint32   `json:"ttl,omitempty"`
	AnswerIP     string   `json:"answer_ip,omitempty"`
	QueryLog     bool     `json:"query_log,omitempty"`
	QueryLogFile string   `json:"query_log_file,omitempty"`
UserRules    bool   `json:"user_rules,omitempty"`
	UserRulesDir string `json:"user_rules_file,omitempty"`
	// BlacklistFile 是"DNS CDN 黑名单"文件（每行一个域名）；命中则不劫持、
	// 直接转发上游（对应原版 dns_blacklist.txt）。
	BlacklistFile string `json:"blacklist_file,omitempty"`
	// FirewallBackend 是局域网重定向的防火墙后端偏好（dnsredir 读取）：
	// "" | iptables | nftables，缺省 auto 探测。
	FirewallBackend string `json:"firewall_backend,omitempty"`
	// ResolvManaged / LANRedirect 是 bin/dnsredir 施加的系统级重定向，
	// 记录状态供 UI 展示（实际规则由 dnsredir 管理）。
	ResolvManaged bool `json:"resolv_managed,omitempty"`
	LANRedirect   bool `json:"lan_redirect,omitempty"`
}

// Update 是自动更新的发布源配置（cmd/update 与 webui 更新端点读取）。
type Update struct {
	// Repo 是 GitHub 仓库（owner/name），发布物来自其 releases/latest。
	Repo string `json:"repo,omitempty"`
	// AssetPrefix 是发布资产名称前缀，实际下载 "<prefix>.tar.gz" 与
	// "<prefix>.sha256"。默认 steam302-web-linux-amd64。
	AssetPrefix string `json:"asset_prefix,omitempty"`
	// URLOverride 直接指定下载地址（tar.gz），并约定同路径 +".sha256" 为
	// 校验文件；设置后跳过 GitHub API（用于镜像/私有分发）。
	URLOverride string `json:"url_override,omitempty"`
	// SkipVerify 为 true 时跳过 sha256 校验（仅调试用，默认 false）。
	SkipVerify bool `json:"skip_verify,omitempty"`
}

// UI 是 Web 控制台「设置→启动行为」的偏好（对应原版桌面端的启动/退出行为；Web
// 版仅记录偏好，系统级自启仍由 systemd 单元管理）。
type UI struct {
	AutoStartMode string `json:"autostart_mode,omitempty"` // foreground|service|disabled
	StartService  bool   `json:"start_service,omitempty"`
	AutoUpdate    bool   `json:"auto_update,omitempty"`
	ExitSync      bool   `json:"exit_sync,omitempty"`
	MinimizeTray  bool   `json:"minimize_tray,omitempty"`
	DevSupport    bool   `json:"dev_support,omitempty"`
	DevFreq       string `json:"dev_freq,omitempty"` // weekly|daily|none
	// 以下为 hosts/DNS/代理方案的偏好记录（对应能力由 systemd/运维层提供）：
	AutoWinProxy bool `json:"auto_win_proxy,omitempty"`
	DNSCDNPrefer bool `json:"dns_cdn_prefer,omitempty"`
	DNSUserRules bool `json:"dns_user_rules,omitempty"`
	DNSLog       bool `json:"dns_log,omitempty"`
}

// PreferConfig 是 CDN 优选(测速)的全局默认值，规则内的 site.prefer 可覆盖。
type PreferConfig struct {
	Enabled          bool    `json:"enabled"`
	LatencyTimeoutMS int     `json:"latency_timeout_ms"`
	LatencyTries     int     `json:"latency_tries"`
	Parallel         int     `json:"parallel"`
	SpeedTest        bool    `json:"speed_test"`
	DownloadURL      string  `json:"download_url"`
	DownloadSize     int64   `json:"download_size"`
	DownloadTimeoutS int     `json:"download_timeout_s"`
	MaxMbps          float64 `json:"max_mbps"`
	TopN             int     `json:"top_n"`
	SamplesPerCIDR   int     `json:"samples_per_cidr"`
	Port             int     `json:"port"`
}

type Fwd struct {
	Bind        string   `json:"bind"`
	PidFile     string   `json:"pid_file"`
	LogFile     string   `json:"log_file"`
	LogMaxBytes int64    `json:"log_max_bytes"` // 达到该字节数时轮转（0=默认 5MB）
	Mappings    []FwdMap `json:"mappings"`
	// AdminAddr 是转发进程暴露的回环管理端口（JSON），供 webui 读取连接监控。
	AdminAddr string `json:"admin_addr,omitempty"`
}

type FwdMap struct {
	From int  `json:"from"`
	To   int  `json:"to"`
	UDP  bool `json:"udp,omitempty"` // 同时做 UDP 中继（HTTP/3，443 默认开启）
}

type Listen struct {
	HTTPSPort int    `json:"https_port"`
	HTTPPort  int    `json:"http_port"`
	BindIP    string `json:"bind_ip"`
	AdminOff  bool   `json:"admin_off"`
	AutoHTTPS string `json:"auto_https"`
	// HTTP3 开启对外 HTTP/3（QUIC）：caddy 监听 https_port 的 UDP 并协商 h3，
	// 浏览器 QUIC 流量经 fwd 的 UDP 中继到达 caddy。
	HTTP3 bool `json:"http3,omitempty"`
}

type Cert struct {
	CertFile string `json:"cert_file"`
	KeyFile  string `json:"key_file"`
	CAFile   string `json:"ca_file"`
	// CAYears/LeafDays 是 genpki 未显式传 flag 时的默认有效期（0=用内置默认 10年/365天）。
	CAYears  int `json:"ca_years,omitempty"`
	LeafDays int `json:"leaf_days,omitempty"`
}

type Hosts struct {
	Marker    string `json:"marker"`
	BackupDir string `json:"backup_dir"`
	File      string `json:"file"`
	// BackupKeep 是 hosts 快照保留数量；0 表示关闭快照清理。
	BackupKeep int `json:"backup_keep,omitempty"`
}

type Rule struct {
	SchemaVersion int      `json:"schema_version"`
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Group         string   `json:"group"`
	Description   string   `json:"description"`
	Enabled       bool     `json:"enabled"`
	NeedsFiles    []string `json:"needs_files"`
	Sites         []Site   `json:"sites"`
}

type Site struct {
	Hosts        []string  `json:"hosts"`
	TLSCert      string    `json:"tls_cert"`
	TLSKey       string    `json:"tls_key"`
	PNACors      bool      `json:"pna_cors"`
	ExtraHeaders []Header  `json:"extra_headers"`
	Prefer       *Prefer   `json:"prefer"`
	Handlers     []Handler `json:"handlers"`
}

// Prefer 为单个 site 开启 CDN 优选。mode：
//   - "node": 候选为接入节点/上游主机，解析成 IP 后测速，Top-N 前置为 pins（tls_server_name 沿用 handler 的 SNI 伪装）
//   - "cf" / "cidr": 候选为 cidrs（Anycast 官方 IP 段，如 Cloudflare/Fastly），随机采样后测速，
//     仅保留能对该域名返回 2xx 的边缘作为 Top-N pins（SNI={host}）
type Prefer struct {
	Mode           string   `json:"mode"`
	Port           int      `json:"port"`
	Candidates     []string `json:"candidates"`
	CIDRs          []string `json:"cidrs"`
	SamplesPerCIDR int      `json:"samples_per_cidr"`
	SpeedTest      *bool    `json:"speed_test"`
	DownloadURL    string   `json:"download_url"`
	// Validate 为 true 时，node 模式候选也用与真实链路一致的 SNI 做严格 2xx 校验，
	// 淘汰对该域名返回 403/404 的边缘（如部分 Akamai 边缘对 cloudflare.steamstatic 拒绝）。
	Validate     bool   `json:"validate,omitempty"`
	ValidatePath string `json:"validate_path,omitempty"`
	// ValidateAnyStatus 为 true 时放宽校验：2xx/3xx/4xx 均算可达（仅拒 5xx/连接失败）。
	// 用于根路径会返回 404 但确实能服务该域名的 GitHub 等任何站点。
	ValidateAnyStatus bool    `json:"validate_any_status,omitempty"`
	MaxMbps           float64 `json:"max_mbps"`
	TopN              int     `json:"top_n"`
}

type Header struct {
	Defer bool   `json:"defer"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Handler struct {
	Type             string              `json:"type"`
	Match            *Match              `json:"match"`
	Upstreams        []string            `json:"upstreams"`
	DynamicUpstreams []DynamicUpstream   `json:"dynamic_upstreams"`
	LB               *LB                 `json:"lb"`
	HeaderUp         [][]json.RawMessage `json:"header_up"`
	HeaderDown       [][]json.RawMessage `json:"header_down"`
	Transport        *Transport          `json:"transport"`
	FileServer       *FileServer         `json:"file_server"`
	Respond          *Respond            `json:"respond"`
	Redir            *Redir              `json:"redir"`
	Rewrite          *Rewrite            `json:"rewrite"`
}

type Match struct {
	Path       []string   `json:"path"`
	Method     []string   `json:"method"`
	Expression []string   `json:"expression"`
	Not        []NotMatch `json:"not"`
}

type NotMatch struct {
	Path       []string `json:"path"`
	Method     []string `json:"method"`
	Expression []string `json:"expression"`
}

type DynamicUpstream struct {
	DNS  string `json:"dns"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

type LB struct {
	Policy           string `json:"policy"`
	Choose           int    `json:"choose"`
	TryDuration      string `json:"try_duration"`
	FailDuration     string `json:"fail_duration"`
	MaxFails         int    `json:"max_fails"`
	UnhealthyLatency string `json:"unhealthy_latency"`
	UnhealthyStatus  string `json:"unhealthy_status"`
}

type Transport struct {
	TLS                bool   `json:"tls"`
	TLSServerName      string `json:"tls_server_name"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
	DialTimeout        string `json:"dial_timeout,omitempty"`
}

type FileServer struct {
	Root   string `json:"root"`
	Browse bool   `json:"browse"`
}

type Respond struct {
	Status int    `json:"status"`
	Body   string `json:"body"`
	Close  bool   `json:"close"`
}

type Redir struct {
	To        string `json:"to"`
	Permanent bool   `json:"permanent"`
}

type Rewrite struct {
	To string `json:"to"`
}
