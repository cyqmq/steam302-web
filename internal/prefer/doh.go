package prefer

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// dohEndpoints 是国内可达且不投毒的 DoH 源（AliDNS/DNSPod/Cloudflare）。
var dohEndpoints = []string{
	"https://dns.alidns.com/resolve?name=%s&type=A",
	"https://doh.pub/resolve?name=%s&type=A",
}

type dohAnswer struct {
	Status int `json:"Status"`
	Answer []struct {
		Type int    `json:"type"`
		Data string `json:"data"`
	} `json:"Answer"`
}

// dohLookupIPv4 用 DoH 解析主机名，返回 A 记录 IPv4。
func dohLookupIPv4(host string) ([]string, error) {
	client := &http.Client{Timeout: 4 * time.Second}
	for _, ep := range dohEndpoints {
		url := fmt.Sprintf(ep, host)
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		var ans dohAnswer
		err = json.NewDecoder(resp.Body).Decode(&ans)
		resp.Body.Close()
		if err != nil || ans.Status != 0 {
			continue
		}
		var out []string
		for _, a := range ans.Answer {
			if a.Type != 1 {
				continue
			}
			if ip := net.ParseIP(a.Data); ip != nil && ip.To4() != nil {
				out = append(out, ip.To4().String())
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	return nil, fmt.Errorf("doh 解析失败: %s", host)
}

func allFakeIPStrings(ips []string) bool {
	if len(ips) == 0 {
		return false
	}
	for _, s := range ips {
		ip := net.ParseIP(s)
		if ip == nil || ip.To4() == nil {
			return false
		}
		if ip.To4()[0] != 198 || ip.To4()[1] != 18 {
			return false
		}
	}
	return true
}
