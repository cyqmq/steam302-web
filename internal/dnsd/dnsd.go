// Package dnsd 提供轻量 DNS 重定向服务：对 S302 劫持域名返回 127.0.0.1（或
// 配置地址），其余域名转发上游 DNS。仅面向本机（默认绑定 127.0.0.1:53），
// 仅依赖 github.com/miekg/dns。
package dnsd

import (
	"context"
	"log"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

// Server 是本机 DNS 重定向服务器。域名单与 hosts 劫持同源：
// 由 rules（含黑名单过滤）收集，支持 "*.example.com" 通配后缀。
type Server struct {
	bind      string
	upstreams []string
	timeout   time.Duration
	ttl       uint32
	answerIP  net.IP
	patterns  []string
	cache     *answerCache
	logger    *log.Logger
	mu        sync.RWMutex
}

type Option func(*Server)

// WithUpstreams 覆盖上游 DNS（默认从 /etc/resolv.conf 读取 nameserver）。
func WithUpstreams(ups []string) Option {
	return func(s *Server) {
		s.upstreams = ups
	}
}

// WithTTL 设置劫持应答 TTL（秒），默认 600。
func WithTTL(ttl uint32) Option {
	return func(s *Server) {
		if ttl > 0 {
			s.ttl = ttl
		}
	}
}

// WithAnswerIP 设置劫持域应答 IP（默认 127.0.0.1）。
func WithAnswerIP(ip string) Option {
	return func(s *Server) {
		if v := net.ParseIP(ip); v != nil {
			s.answerIP = v
		}
	}
}

// WithTimeout 设置上游查询超时，默认 3s。
func WithTimeout(d time.Duration) Option {
	return func(s *Server) {
		if d > 0 {
			s.timeout = d
		}
	}
}

// WithLogger 指定日志（默认丢弃）。
func WithLogger(l *log.Logger) Option {
	return func(s *Server) {
		s.logger = l
	}
}

// New 创建 DNS 重定向服务器。domains 为劫持域名模式列表，支持
// "*.example.com" 通配后缀。
func New(bind string, domains []string, opts ...Option) *Server {
	s := &Server{
		bind:     bind,
		timeout:  3 * time.Second,
		ttl:      600,
		answerIP: net.ParseIP("127.0.0.1"),
		logger:   log.New(nil, "", 0),
	}
	for _, o := range opts {
		o(s)
	}
	s.patterns = normalizePatterns(domains)
	if len(s.upstreams) == 0 {
		s.upstreams = readResolvConf()
	}
	s.cache = newAnswerCache(256)
	return s
}

// Hijacked 报告域名是否命中劫持集合。
func (s *Server) Hijacked(qname string) bool {
	return matchAny(normalizeName(qname), s.patterns)
}

// Upstreams 返回当前上游 DNS 列表（New 时已解析 resolv.conf）。
func (s *Server) Upstreams() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.upstreams...)
}

func (s *Server) logf(format string, args ...any) {
	s.logger.Printf(format, args...)
}

// Serve 启动 UDP 与 TCP listener 并阻塞直到 ctx 取消。
func (s *Server) Serve(ctx context.Context) error {
	var servers []*dns.Server
	for _, proto := range []string{"udp", "tcp"} {
		addr := s.bind
		if proto == "tcp" && strings.HasPrefix(addr, ":") {
			addr = "127.0.0.1" + addr
		}
		srv := &dns.Server{
			Addr:    addr,
			Net:     proto,
			Handler: s,
		}
		servers = append(servers, srv)
		go func(srv *dns.Server) {
			if err := srv.ListenAndServe(); err != nil {
				s.logf("dns %s %s: %v", proto, addr, err)
			}
		}(srv)
	}
	<-ctx.Done()
	for _, srv := range servers {
		_ = srv.Shutdown()
	}
	return nil
}

// ServeDNS 处理单个查询（dns.Handler 接口）。
func (s *Server) ServeDNS(w dns.ResponseWriter, req *dns.Msg) {
	q := req.Question
	if len(q) != 1 {
		m := new(dns.Msg)
		m.SetReply(req)
		m.Rcode = dns.RcodeFormatError
		_ = w.WriteMsg(m)
		return
	}
	name := normalizeName(q[0].Name)
	if s.Hijacked(name) {
		s.replyHijacked(w, req, name)
		return
	}
	s.forward(w, req)
}

// replyHijacked 对劫持域名应答：A → 应答 IP；其余类型（含 AAAA/HTTPS/SVCB）
// 返回 NoError 空应答，促使客户端回到 A 查询并落到本机地址。
func (s *Server) replyHijacked(w dns.ResponseWriter, req *dns.Msg, name string) {
	m := new(dns.Msg)
	m.SetReply(req)
	m.RecursionAvailable = true
	if len(req.Question) == 1 && req.Question[0].Qtype == dns.TypeA {
		rr := &dns.A{
			Hdr: dns.RR_Header{
				Name:   dns.Fqdn(name),
				Rrtype: dns.TypeA,
				Class:  dns.ClassINET,
				Ttl:    s.ttl,
			},
			A: s.answerIP,
		}
		m.Answer = []dns.RR{rr}
	}
	_ = w.WriteMsg(m)
}

// forward 将非劫持域名转发上游，带 TTL 缓存；UDP 截断时回退 TCP。
func (s *Server) forward(w dns.ResponseWriter, req *dns.Msg) {
	key := cacheKey(&req.Question[0])
	if resp, ok := s.cache.Get(key); ok && !resp.Expired() {
		cm := resp.Msg.Copy()
		cm.Id = req.Id
		_ = w.WriteMsg(cm)
		return
	}
	for _, up := range s.upstreams {
		m, err := s.exchange(up, req)
		if err != nil {
			s.logf("upstream %s: %v", up, err)
			continue
		}
		s.cache.Put(key, m, s.ttl)
		m.Id = req.Id
		_ = w.WriteMsg(m)
		return
	}
	m := new(dns.Msg)
	m.SetRcode(req, dns.RcodeServerFailure)
	_ = w.WriteMsg(m)
}

func (s *Server) exchange(up string, req *dns.Msg) (*dns.Msg, error) {
	client := &dns.Client{Timeout: s.timeout}
	m, _, err := client.Exchange(req, up)
	if err != nil {
		return nil, err
	}
	if !m.Truncated {
		return m, nil
	}
	tcp := &dns.Client{Net: "tcp", Timeout: s.timeout}
	m, _, err = tcp.Exchange(req, up)
	if err != nil {
		return nil, err
	}
	return m, nil
}

// readResolvConf 从 /etc/resolv.conf 收集 nameserver。失败时回退公共 DNS。
func readResolvConf() []string {
	cfg, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil || len(cfg.Servers) == 0 {
		return []string{"223.5.5.5:53", "119.29.29.29:53"}
	}
	ups := make([]string, 0, len(cfg.Servers))
	for _, sv := range cfg.Servers {
		ups = append(ups, net.JoinHostPort(sv, cfg.Port))
	}
	return ups
}

// normalizeName 小写并保证有一层 FQDN 结尾点，便于匹配。
func normalizeName(name string) string {
	name = strings.ToLower(strings.TrimSuffix(name, "."))
	if name == "" {
		name = "."
	}
	return name
}

func normalizePatterns(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range in {
		n := normalizeName(strings.TrimPrefix(p, "*."))
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// matchAny 判断 qname 是否命中任一模式（模式已去掉 "*." 前缀，故通配语义为
// "域名自身或其任意子域名/任意深度子域均命中"，与 hosts 劫持集合一致——
// hosts 仅能精确写一行，DNS 模式对子域名也能兜住）。
func matchAny(qname string, patterns []string) bool {
	i := sort.SearchStrings(patterns, qname)
	if i < len(patterns) && patterns[i] == qname {
		return true
	}
	for _, p := range patterns {
		if strings.HasSuffix(qname, "."+p) {
			return true
		}
	}
	return false
}
