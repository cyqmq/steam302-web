package prefer

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"strings"
)

// ResolveCandidates 返回待探测的候选 IP 列表：
//   - mode=node：解析 candidates 的主机名（可含 scheme/端口/路径，仅取 host）为 v4 IP，去重
//   - mode=cf：  从 cidrs（Cloudflare 官方段）中随机采样，每段 samples 个
func (o *Options) ResolveCandidates(mode string, candidates, cidrs []string, samples int) ([]string, error) {
	if mode == "cf" {
		return o.CFSample(cidrs, samples)
	}
	var ips []string
	seen := map[string]bool{}
	for _, c := range candidates {
		host := hostOf(c)
		if host == "" {
			continue
		}
		resolved, err := o.ResolveFn(host)
		if err != nil {
			continue
		}
		for _, ip := range resolved {
			if !seen[ip] {
				seen[ip] = true
				ips = append(ips, ip)
			}
		}
	}
	return ips, nil
}

// CFSample 从 CIDR 段（Cloudflare 官方段）中每段随机采样 samples 个 v4 IP。
func (o *Options) CFSample(cidrs []string, samples int) ([]string, error) {
	if samples <= 0 {
		samples = o.SamplesPerCIDR
	}
	var out []string
	for _, c := range cidrs {
		_, ipnet, err := net.ParseCIDR(c)
		if err != nil {
			return nil, fmt.Errorf("无效 CIDR %q: %w", c, err)
		}
		if ipnet.IP.To4() == nil {
			continue // 暂仅支持 v4
		}
		out = append(out, sampleCIDR(o.Rand, ipnet.IP.To4(), net.IP(ipnet.Mask).To4(), samples)...)
	}
	return out, nil
}

func sampleCIDR(r *rand.Rand, base, mask []byte, samples int) []string {
	baseU := ip4Uint(base)
	maskU := ip4Uint(mask)
	hostBits := ^maskU
	hosts := uint64(hostBits) + 1
	if hosts > 1<<20 {
		hosts = 1 << 20
	}
	seen := map[uint32]bool{}
	var out []string
	for len(out) < samples && hosts > 1 {
		off := r.Uint32() & hostBits
		ipU := baseU | off
		if seen[ipU] {
			continue
		}
		seen[ipU] = true
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, ipU)
		out = append(out, net.IP(b).String())
	}
	return out
}

func ip4Uint(ip net.IP) uint32 {
	return binary.BigEndian.Uint32(ip)
}

// hostOf 从候选串中提取主机名（支持 https://host[:port]/path 或裸 host）。
func hostOf(c string) string {
	if i := strings.Index(c, "://"); i >= 0 {
		c = c[i+3:]
	}
	if i := strings.IndexAny(c, "/?#"); i >= 0 {
		c = c[:i]
	}
	if i := strings.IndexByte(c, ':'); i >= 0 {
		c = c[:i]
	}
	return c
}
