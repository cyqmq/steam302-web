package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const srcURL = "https://raw.githubusercontent.com/mansourjabin/cdn-ip-database/main/data/resolved_ips.json"

func main() {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(srcURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetch-cdn-ips: 下载失败: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "fetch-cdn-ips: HTTP %d\n", resp.StatusCode)
		os.Exit(1)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetch-cdn-ips: 读取失败: %v\n", err)
		os.Exit(1)
	}
	var src map[string]struct {
		ASNs []string `json:"asns"`
		IPs  []string `json:"ips"`
	}
	if err := json.Unmarshal(body, &src); err != nil {
		fmt.Fprintf(os.Stderr, "fetch-cdn-ips: 解析失败: %v\n", err)
		os.Exit(1)
	}
	want := []string{"Akamai", "Cloudflare", "Fastly", "CloudFront"}
	out := map[string]any{
		"source":         "https://github.com/mansourjabin/cdn-ip-database",
		"snapshotted_at": time.Now().UTC().Format("2006-01-02"),
		"providers":      map[string]any{},
	}
	pm := out["providers"].(map[string]any)
	for _, p := range want {
		v, ok := src[p]
		if !ok {
			continue
		}
		pm[p] = v
	}
	dir := "config"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	f, err := os.Create(dir + "/cdn_ips.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "fetch-cdn-ips: 写入失败: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintf(os.Stderr, "fetch-cdn-ips: encode: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("cdn_ips.json 已更新")
}
