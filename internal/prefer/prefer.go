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

	// ValidatePath 是 2xx 校验用的请求路径（默认 "/"）。
	// 对没有根路由的 CDN 主机（如 Fastly 的 cdn.fastly.steamstatic.com），
	// 需用真实资源路径，否则抽样边缘会在根路径上返回 404 被误判不可用。
	ValidatePath string

	// ValidateSNI 为校验连接使用的 TLS SNI（默认与 ValidateHost 相同）。
	// node 模式会沿用 handler 的 SNI 伪装（如 img-s-msn），以复现真实链路。
	ValidateSNI string

	// ValidateAnyStatus 为 true 时，服务端返回 2xx/3xx/4xx 均视为"能服务该域名"
	// （仅拒 5xx/连接失败）。用于 node 模式：放行 301/404 等可达边缘，仅淘汰 403/挂死。
	ValidateAnyStatus bool

	// SkipValidateFakeIP 为 true 时，若候选 IP 全部落在 198.18.0.0/15（本机
	// clash fake-ip 段），跳过 validateRelease——这类地址经 TUN 走节点，无法做
	// 真实链路校验，保持原有的"TCP 延迟即存活"判定。
	SkipValidateFakeIP bool
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
	if o.ValidatePath == "" {
		o.ValidatePath = "/"
	}
	if o.ValidateSNI == "" {
		o.ValidateSNI = o.ValidateHost
	}
	if o.Rand == nil {
		o.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
}

func lookupIPv4(host string) ([]string, error) {
	ips, err := net.LookupIP(host)
	if err == nil && !allFakeIP(ips) {
		return v4OrFallback(ips), nil
	}
	// 本机 resolv.conf 若指向 clash/mihomo 且开启 fake-ip，解析结果全是 198.18.0.0/15
	// 虚拟段（延迟无意义、开机无 clash 时不可达）。此时改用公共 DoH 取真实 CDN 边缘。
	if real, derr := dohLookupIPv4(host); derr == nil && len(real) > 0 {
		return real, nil
	}
	if err != nil {
		return nil, err
	}
	return v4OrFallback(ips), nil
}

func v4OrFallback(ips []net.IP) []string {
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
	return out
}

// allFakeIP 判断全部 IP 位于 clash fake-ip 段 198.18.0.0/15。
func allFakeIP(ips []net.IP) bool {
	if len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		v4 := ip.To4()
		if v4 == nil {
			return false
		}
		if v4[0] != 198 || v4[1] != 18 {
			return false
		}
	}
	return true
}
