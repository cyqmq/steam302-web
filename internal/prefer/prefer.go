// Package prefer 实现 CDN 优选：对候选 IP 做 TCP 延迟探测与 HTTP 下载测速，
// 选出 Top-N 写入缓存，Caddyfile 渲染时将其前置为上游（IP 直连 + SNI 伪装）。
// 方法论参考 XIU2/CloudflareSpeedTest（TCP 握手测延迟 → 下载测速 → 排序）。
package prefer

import (
	"math/rand"
	"net"
	"time"
)

// Ranked 是单个优选结果。
type Ranked struct {
	Upstream  string  `json:"upstream"` // https://<ip>
	IP        string  `json:"ip"`
	DelayMS   float64 `json:"delay_ms"`
	SpeedMbps float64 `json:"speed_mbps,omitempty"`
}

// Entry 是一个 site（按 ruleID+siteIndex 定位）的优选结果。
type Entry struct {
	RuleID    string   `json:"rule_id"`
	SiteIndex int      `json:"site_index"`
	Mode      string   `json:"mode"`
	Ranked    []Ranked `json:"ranked"`
}

// Cache 是磁盘上 config/prefer.json 的结构。
type Cache struct {
	SchemaVersion int       `json:"schema_version"`
	UpdatedAt     time.Time `json:"updated_at"`
	Entries       []Entry   `json:"entries"`
}

// Options 控制一次优选探测。零值将按缺省值补齐，便于命令行覆盖全局配置。
type Options struct {
	Port            int
	LatencyTimeout  time.Duration
	LatencyTries    int
	Parallel        int
	SpeedTest       bool
	DownloadURL     string
	DownloadSize    int64
	DownloadTimeout time.Duration
	MaxMbps         float64
	TopN            int
	SamplesPerCIDR  int
	SpeedCandidates int
	// ValidateHost 非空时，探测出延迟可用的 IP 还会做一次 HTTPS 握手/请求校验
	// （以该域名为 SNI 打到该 IP），校验失败视为不可用。典型用于 cf 模式：
	// 仅保留真正能服务目标站点的 Anycast IP，避免 caddy 502。
	ValidateHost string
	Rand         *rand.Rand
	// ResolveFn 解析主机名 → IP 列表（默认 net.LookupIP），测试可注入。
	ResolveFn func(host string) ([]string, error)
}

// Inflate 用缺省值补齐零/负值字段。
func (o *Options) Inflate() {
	if o.Port <= 0 || o.Port >= 65535 {
		o.Port = 443
	}
	if o.LatencyTimeout <= 0 {
		o.LatencyTimeout = 1200 * time.Millisecond
	}
	if o.LatencyTries <= 0 {
		o.LatencyTries = 3
	}
	if o.Parallel <= 0 {
		o.Parallel = 32
	}
	if o.DownloadURL == "" {
		o.DownloadURL = "https://speed.cloudflare.com/__down?bytes=8388608"
	}
	if o.DownloadSize <= 0 {
		o.DownloadSize = 8 << 20 // 8 MiB
	}
	if o.DownloadTimeout <= 0 {
		o.DownloadTimeout = 5 * time.Second
	}
	// MaxMbps<=0 表示不限速
	if o.TopN <= 0 {
		o.TopN = 2
	}
	if o.SamplesPerCIDR <= 0 {
		o.SamplesPerCIDR = 16
	}
	if o.SpeedCandidates <= 0 {
		o.SpeedCandidates = 6
	}
	if o.ResolveFn == nil {
		o.ResolveFn = lookupIPv4
	}
	if o.Rand == nil {
		o.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
}

func lookupIPv4(host string) ([]string, error) {
	ips, err := net.LookupIP(host)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			out = append(out, v4.String())
		}
	}
	if len(out) == 0 { // 无 v4 记录时退回 v6，避免误判可达性
		for _, ip := range ips {
			out = append(out, ip.String())
		}
	}
	return out, nil
}
