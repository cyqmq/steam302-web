package dnsd

import (
	"errors"
	"net"
	"reflect"
	"testing"

	"github.com/miekg/dns"
)

func TestNormalizeName(t *testing.T) {
	cases := map[string]string{
		"store.steampowered.com":  "store.steampowered.com",
		"Store.SteamPowered.Com.": "store.steampowered.com",
		"foo..bar":                "foo..bar",
		".":                       ".",
		"":                        ".",
		"api.github.com.":         "api.github.com",
	}
	for in, want := range cases {
		if got := normalizeName(in); got != want {
			t.Errorf("normalizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizePatterns(t *testing.T) {
	in := []string{"store.steampowered.com", "*.mod.io", "*.mod.io", "MOD.IO", "cdn.steamstatic.com."}
	got := normalizePatterns(in)
	want := []string{"cdn.steamstatic.com", "mod.io", "store.steampowered.com"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("normalizePatterns = %v, want %v", got, want)
	}
}

func TestMatchAny(t *testing.T) {
	patterns := []string{"cdn.steamstatic.com", "mod.io", "steamstatic.com", "store.steampowered.com"}
	cases := []struct {
		qname string
		want  bool
	}{
		{"cdn.steamstatic.com", true},
		{"avatars.akamai.steamstatic.com", true},
		{"mod.io", true},
		{"www.mod.io", true},
		{"a.b.c.mod.io", true},
		{"cdn.mod.io.evil.example", false},
		{"store.steampowered.com", true},
		{"api.github.com", false},
		{"github.com", false},
	}
	for _, c := range cases {
		if got := matchAny(c.qname, patterns); got != c.want {
			t.Errorf("matchAny(%q) = %v, want %v", c.qname, got, c.want)
		}
	}
}

// startUpstream 在随机端口起一个最小上游 DNS，返回 addr 与关闭函数。
func startUpstream(t *testing.T) string {
	t.Helper()
	srv := &dns.Server{
		Addr: "127.0.0.1:0",
		Net:  "udp",
		Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
			m := new(dns.Msg)
			m.SetReply(r)
			if len(r.Question) == 1 {
				hdr := dns.RR_Header{Name: r.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 30}
				m.Answer = []dns.RR{&dns.A{Hdr: hdr, A: net.ParseIP("192.0.2.1")}}
			}
			_ = w.WriteMsg(m)
		}),
	}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.PacketConn = pc
	go func() { _ = srv.ActivateAndServe() }()
	return pc.LocalAddr().String()
}

// startServer 用随机端口起被测 Server，返回可用 addr 与关闭函数。
func startServer(t *testing.T, s *Server) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// 新起一个 server，仅替换监听地址为动态端口。
	s2 := &dns.Server{Addr: pc.LocalAddr().String(), Net: "udp", PacketConn: pc, Handler: s}
	go func() { _ = s2.ActivateAndServe() }()
	t.Cleanup(func() { _ = s2.Shutdown() })
	return pc.LocalAddr().String()
}

func queryA(t *testing.T, addr, name string) *dns.Msg {
	t.Helper()
	c := new(dns.Client)
	m := new(dns.Msg)
	m.SetQuestion(dns.Fqdn(name), dns.TypeA)
	r, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatalf("query %s: %v", name, err)
	}
	return r
}

func TestHijackedAnswer(t *testing.T) {
	up := startUpstream(t)
	s := New("127.0.0.1:0", []string{"store.steampowered.com", "*.steamstatic.com"},
		WithUpstreams([]string{up}), WithTTL(60))
	addr := startServer(t, s)

	r := queryA(t, addr, "cdn.steamstatic.com")
	if len(r.Answer) != 1 {
		t.Fatalf("hijacked answer empty: %v", r.Answer)
	}
	a, ok := r.Answer[0].(*dns.A)
	if !ok || a.A.String() != "127.0.0.1" {
		t.Fatalf("want 127.0.0.1, got %v", r.Answer[0])
	}
}

func TestHijackedAAAAEmpty(t *testing.T) {
	up := startUpstream(t)
	s := New("127.0.0.1:0", []string{"store.steampowered.com"}, WithUpstreams([]string{up}))
	addr := startServer(t, s)

	c := new(dns.Client)
	m := new(dns.Msg)
	m.SetQuestion("store.steampowered.com.", dns.TypeAAAA)
	r, _, err := c.Exchange(m, addr)
	if err != nil {
		t.Fatal(err)
	}
	if r.Rcode != dns.RcodeSuccess || len(r.Answer) != 0 {
		t.Fatalf("AAAA want empty NoError, got rcode=%d answers=%v", r.Rcode, r.Answer)
	}
}

func TestForwardNonHijacked(t *testing.T) {
	up := startUpstream(t)
	s := New("127.0.0.1:0", []string{"store.steampowered.com"}, WithUpstreams([]string{up}))
	addr := startServer(t, s)

	r := queryA(t, addr, "www.example.com")
	if len(r.Answer) != 1 {
		t.Fatalf("forward answer empty: %v", r.Answer)
	}
	a, ok := r.Answer[0].(*dns.A)
	if !ok || a.A.String() != "192.0.2.1" {
		t.Fatalf("want 192.0.2.1, got %v", r.Answer[0])
	}
}

func TestHijackWildcardDeep(t *testing.T) {
	_ = startUpstream(t)
	s := New("127.0.0.1:0", []string{"*.mod.io"})
	if !s.Hijacked("www.mod.io") || !s.Hijacked("a.b.mod.io") || s.Hijacked("mod.io.evil.com") {
		t.Fatalf("wildcard matching wrong")
	}
}

func TestAnswerCacheTTL(t *testing.T) {
	c := newAnswerCache(4)
	q := &dns.Question{Name: "x.example.com.", Qtype: dns.TypeA}
	key := cacheKey(q)
	m := new(dns.Msg)
	m.Answer = []dns.RR{&dns.A{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 10}}}
	c.Put(key, m, 5)
	if _, ok := c.Get(key); !ok {
		t.Fatal("cache miss after put")
	}
	// TTL 已覆盖（minTTL>=ttl 取较大者），超时后再取应 miss。
	if err := c.forceExpire(key); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get(key); ok {
		t.Fatal("cache hit after expire")
	}
}

// forceExpire 直接标记某 key 过期用于测试。
func (c *answerCache) forceExpire(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[key]
	if !ok {
		return errors.New("key not found")
	}
	it.ExpireAt = it.ExpireAt.Add(-1 << 40)
	return nil
}
