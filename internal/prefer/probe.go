package prefer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"
)

// Result 是一次单 IP 探测的结果。
type Result struct {
	IP        string
	OK        bool
	Delay     time.Duration
	SpeedMbps float64
}

// Probe 分两阶段：先并行对所有候选做 TCP 延迟探测；若启用测速，再对延迟最优的
// SpeedCandidates 个 IP 做 HTTP 下载测速。返回顺序与传入 ips 一致。
func Probe(ips []string, o *Options) []Result {
	o.Inflate()
	results := make([]Result, len(ips))
	latency(ips, results, o)
	if o.ValidateHost != "" && !(o.SkipValidateFakeIP && allFakeIPStrings(ips)) {
		validateRelease(ips, results, o)
	}
	if o.SpeedTest {
		speedPhase(ips, results, o)
	}
	return results
}

// validateRelease 用 ValidateHost 作为 SNI 向候选 IP 发起一次忽略证书的 HTTPS
// 请求；仅当服务端返回 2xx（200/304 等）才视为该 IP 真能服务该域名。
// 403/5xx/连接失败等一律视为不可用，避免 CF 边缘对大陆 IP 的 403 被当作有效。
func validateRelease(ips []string, results []Result, o *Options) {
	var cand []int
	for i, r := range results {
		if r.OK {
			cand = append(cand, i)
		}
	}
	sem := make(chan struct{}, o.Parallel)
	var wg sync.WaitGroup
	for _, i := range cand {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			host := o.ValidateHost
			u := fmt.Sprintf("https://%s%s", host, o.ValidatePath)
			tr := &http.Transport{
				DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "tcp", net.JoinHostPort(results[i].IP, fmt.Sprintf("%d", o.Port)))
				},
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: true, ServerName: o.ValidateSNI},
				DisableKeepAlives:   true,
				MaxIdleConnsPerHost: 1,
			}
			client := &http.Client{
				Transport: tr,
				Timeout:   2 * time.Second,
				// 跟随重定向时保持 Host 为校验域名：跨域 301（如 raw → github.com）
				// 若被替换为 Location 主机名，会对源 IP 发到其他虚拟主机端口，误伤 5xx，
				// 而 GitHub 边缘 Host=github.com 落在 raw IP 上大概率返回 500。
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					if len(via) >= 5 {
						return http.ErrUseLastResponse
					}
					if o.ValidateAnyStatus {
						return http.ErrUseLastResponse
					}
					req.Host = o.ValidateHost
					return nil
				},
			}
			defer client.CloseIdleConnections()
			req, err := http.NewRequest(http.MethodGet, u, nil)
			if err != nil {
				results[i].OK = false
				return
			}
			req.Host = host
			req.Header.Set("User-Agent", "Mozilla/5.0 steam302-web/prefer")
			resp, err := client.Do(req)
			if err != nil {
				results[i].OK = false
				return
			}
			resp.Body.Close()
			if o.ValidateAnyStatus {
				// node 模式：放行 2xx/3xx/4xx（301/404 都算可达），仅拒 5xx/连接失败
				if resp.StatusCode >= 500 {
					results[i].OK = false
				}
			} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
				results[i].OK = false
			}
		}(i)
	}
	wg.Wait()
}

func latency(ips []string, results []Result, o *Options) {
	sem := make(chan struct{}, o.Parallel)
	var wg sync.WaitGroup
	for i, ip := range ips {
		wg.Add(1)
		go func(i int, ip string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = probeLatency(ip, o)
		}(i, ip)
	}
	wg.Wait()
}

func probeLatency(ip string, o *Options) Result {
	var r Result
	r.IP = ip
	addr := net.JoinHostPort(ip, fmt.Sprintf("%d", o.Port))
	recv := 0
	var total time.Duration
	for i := 0; i < o.LatencyTries; i++ {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", addr, o.LatencyTimeout)
		if err != nil {
			continue
		}
		_ = conn.Close()
		recv++
		total += time.Since(start)
	}
	if recv == 0 {
		return r
	}
	r.OK = true
	r.Delay = total / time.Duration(recv)
	return r
}

func speedPhase(ips []string, results []Result, o *Options) {
	var cand []int
	for i, r := range results {
		if r.OK {
			cand = append(cand, i)
		}
	}
	sort.SliceStable(cand, func(a, b int) bool { return results[cand[a]].Delay < results[cand[b]].Delay })
	if len(cand) > o.SpeedCandidates {
		cand = cand[:o.SpeedCandidates]
	}
	sem := make(chan struct{}, o.Parallel)
	var wg sync.WaitGroup
	for _, i := range cand {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i].SpeedMbps = downloadSpeed(results[i].IP, net.JoinHostPort(results[i].IP, fmt.Sprintf("%d", o.Port)), o)
		}(i)
	}
	wg.Wait()
}

// downloadSpeed 经目标 IP 拉取 DownloadURL 测速，可选 MaxMbps 限速。
func downloadSpeed(ip, addr string, o *Options) float64 {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", addr)
		},
		DisableKeepAlives: true,
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   o.DownloadTimeout + 3*time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	defer client.CloseIdleConnections()

	req, err := http.NewRequest(http.MethodGet, o.DownloadURL, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) steam302-web/prefer")
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return 0
	}

	start := time.Now()
	limit := o.DownloadSize
	thr := &throttle{maxBps: o.MaxMbps * 1e6 / 8}
	buf := make([]byte, 32<<10)
	for thr.done < limit {
		if time.Since(start) > o.DownloadTimeout {
			break
		}
		thr.waitForBudget(start)
		n, err := resp.Body.Read(buf)
		if n > 0 {
			thr.done += int64(n)
		}
		if err != nil {
			break
		}
	}
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 || thr.done <= 0 {
		return 0
	}
	return float64(thr.done) * 8 / 1e6 / elapsed
}

// throttle 用令牌桶思想把平均下载速率压到 maxBps（字节/秒）以内，<=0 不限速。
type throttle struct {
	mu     sync.Mutex
	maxBps float64
	done   int64
}

func (t *throttle) waitForBudget(start time.Time) {
	if t.maxBps <= 0 {
		return
	}
	for {
		t.mu.Lock()
		done := t.done
		t.mu.Unlock()
		elapsed := time.Since(start).Seconds()
		if elapsed <= 0 {
			return
		}
		allowed := t.maxBps * elapsed
		if float64(done) <= allowed {
			return
		}
		need := time.Duration(float64(done)/t.maxBps - elapsed)
		time.Sleep(need * time.Second)
	}
}
