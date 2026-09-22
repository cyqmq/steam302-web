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
// 由 rules（含黑名单过滤）收集，支持 "*.example.com" 通配后缀；
// 另支持用户自定义解析（pattern->IP，覆盖默认应答 IP）。
type Server struct {
	bind        string
	upstreams   []string
	timeout     time.Duration
	ttl         uint32
	answerIP    net.IP
	patterns    []string
	userHosts   map[string]string // 规范化 pattern -> 应答 IP
	userPats    []string          // 排序后的用户规则 pattern（供后缀匹配）
	cache       *answerCache
	logger      *log.Logger
	queryLogger *log.Logger // 非 nil 时逐条记录查询
	mu          sync.RWMutex
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

// WithUserHosts 注入用户自定义解析规则（pattern -> 应答 IP）；pattern 支持
// "*.example.com" 通配，优先级高于默认 answer_ip。
func WithUserHosts(m map[string]string) Option {
	return func(s *Server) {
		if len(m) == 0 {
			return
		}
		hosts := map[string]string{}
		var pats []string
		for p, ip := range m {
			n := normalizeName(strings.TrimPrefix(strings.TrimSpace(p), "*."))
			if n == "" || net.ParseIP(strings.TrimSpace(ip)) == nil {
				continue
			}
			if _, ok := hosts[n]; !ok {
				hosts[n] = strings.TrimSpace(ip)
				pats = append(pats, n)
			}
		}
		sort.Strings(pats)
		s.userHosts = hosts
		s.userPats = pats
	}
}

// WithQueryLogger 逐条记录查询日志（qname、类型、动作、应答 IP）。
func WithQueryLogger(l *log.Logger) Option {
	return func(s *Server) {
		s.queryLogger = l
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

// Hijacked 报告域名是否命中劫持集合（含用户自定义规则）。
func (s *Server) Hijacked(qname string) bool {
	return matchAny(normalizeName(qname), s.patterns) || matchAny(normalizeName(qname), s.userPats)
}

// answerIPFor 优先返回用户自定义规则命中的 IP，否则默认应答 IP。
func (s *Server) answerIPFor(name string) net.IP {
	if ipStr, ok := lookupUser(s.userHosts, s.userPats, name); ok {
		if ip := net.ParseIP(ipStr); ip != nil {
			return ip
		}
	}
	return s.answerIP
}

func lookupUser(hosts map[string]string, pats []string, name string) (string, bool) {
	if hosts == nil {
		return "", false
	}
	if v, ok := hosts[name]; ok {
		return v, true
	}
	for _, p := range pats {
		if p != name && strings.HasSuffix(name, "."+p) {
			return hosts[p], true
		}
	}
	return "", false
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
		s.logQuery(req, "format-error", "", "")
		return
	}
	start := time.Now()
	name := normalizeName(q[0].Name)
	if s.Hijacked(name) {
		s.replyHijacked(w, req, name)
		s.logQuery(req, "hijack", s.answerIPFor(name).String(), time.Since(start).String())
		return
	}
	action := s.forward(w, req)
	s.logQuery(req, action, "", time.Since(start).String())
}

func (s *Server) logQuery(req *dns.Msg, action, detail, elapsed string) {
	if s.queryLogger == nil || len(req.Question) == 0 {
		return
	}
	s.queryLogger.Printf("%s %s %s %s detail=%s", time.Now().Format("2006/01/02 15:04:05"),
		normalizeName(req.Question[0].Name), dns.TypeToString[req.Question[0].Qtype], action, detail)
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
			A: s.answerIPFor(name),
		}
		m.Answer = []dns.RR{rr}
	}
	_ = w.WriteMsg(m)
}

// forward 将非劫持域名转发上游，带 TTL 缓存；UDP 截断时回退 TCP。
// 返回值记录动作供查询日志使用。
func (s *Server) forward(w dns.ResponseWriter, req *dns.Msg) string {
	key := cacheKey(&req.Question[0])
	if resp, ok := s.cache.Get(key); ok && !resp.Expired() {
		cm := resp.Msg.Copy()
		cm.Id = req.Id
		_ = w.WriteMsg(cm)
		return "cache"
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
		return "forward"
	}
	m := new(dns.Msg)
	m.SetRcode(req, dns.RcodeServerFailure)
	_ = w.WriteMsg(m)
	return "servfail"
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
