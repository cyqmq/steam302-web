package rules

import "encoding/json"

type Env struct {
	SchemaVersion    int                        `json:"schema_version"`
	Listen           Listen                     `json:"listen"`
	Cert             Cert                       `json:"cert"`
	Hosts            Hosts                      `json:"hosts"`
	Fwd              Fwd                        `json:"fwd"`
	UpstreamDefaults map[string]json.RawMessage `json:"upstream_defaults"`
	Notes            string                     `json:"notes"`
}

type Fwd struct {
	Bind     string    `json:"bind"`
	PidFile  string    `json:"pid_file"`
	LogFile  string    `json:"log_file"`
	Mappings []FwdMap  `json:"mappings"`
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
}

type Hosts struct {
	Marker    string `json:"marker"`
	BackupDir string `json:"backup_dir"`
	File      string `json:"file"`
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
	Handlers     []Handler `json:"handlers"`
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
	Path   []string   `json:"path"`
	Method []string   `json:"method"`
	Not    []NotMatch `json:"not"`
}

type NotMatch struct {
	Path   []string `json:"path"`
	Method []string `json:"method"`
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
