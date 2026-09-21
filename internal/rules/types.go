package rules

import "encoding/json"

type Env struct {
	SchemaVersion    int                        `json:"schema_version"`
	Listen           Listen                     `json:"listen"`
	Cert             Cert                       `json:"cert"`
	Hosts            Hosts                      `json:"hosts"`
	Fwd              Fwd                        `json:"fwd"`
	Prefer           PreferConfig               `json:"prefer"`
	UpstreamDefaults map[string]json.RawMessage `json:"upstream_defaults"`
	Notes            string                     `json:"notes"`
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
}

type FwdMap struct {
	From int `json:"from"`
	To   int `json:"to"`
}

type Listen struct {
	HTTPSPort int    `json:"https_port"`
	HTTPPort  int    `json:"http_port"`
	BindIP    string `json:"bind_ip"`
	AdminOff  bool   `json:"admin_off"`
	AutoHTTPS string `json:"auto_https"`
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
	Validate     bool    `json:"validate,omitempty"`
	ValidatePath string  `json:"validate_path,omitempty"`
	MaxMbps      float64 `json:"max_mbps"`
	TopN         int     `json:"top_n"`
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
