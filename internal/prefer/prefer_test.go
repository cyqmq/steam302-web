package prefer

import (
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRankTop(t *testing.T) {
	res := []Result{
		{IP: "a", OK: false},
		{IP: "b", OK: true, Delay: 50 * time.Millisecond, SpeedMbps: 100},
		{IP: "c", OK: true, Delay: 10 * time.Millisecond, SpeedMbps: 30},
		{IP: "d", OK: true, Delay: 10 * time.Millisecond, SpeedMbps: 90},
	}
	ranked := RankTop(res, 2)
	if len(ranked) != 2 {
		t.Fatalf("want 2 got %d", len(ranked))
	}
	// 都有测速时按速度降序；速度不同 → b(100) 先于 d(90)
	if ranked[0].IP != "b" || ranked[1].IP != "d" {
		t.Fatalf("order wrong: %s %s", ranked[0].IP, ranked[1].IP)
	}
	if ranked[0].Upstream != "https://b" {
		t.Fatalf("upstream %s", ranked[0].Upstream)
	}
}

func TestRankTopDelayOnly(t *testing.T) {
	res := []Result{
		{IP: "x", OK: true, Delay: 90 * time.Millisecond},
		{IP: "y", OK: true, Delay: 10 * time.Millisecond},
		{IP: "z", OK: true, Delay: 40 * time.Millisecond},
	}
	ranked := RankTop(res, 2)
	if ranked[0].IP != "y" || ranked[1].IP != "z" {
		t.Fatalf("delay order wrong: %s %s", ranked[0].IP, ranked[1].IP)
	}
}

func TestCacheRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefer.json")
	c := &Cache{
		SchemaVersion: 1,
		UpdatedAt:     time.Now(),
		Entries: []Entry{{
			RuleID: "steam_cdn_cloudflare", SiteIndex: 0, Mode: "cf",
			Ranked: []Ranked{{Upstream: "https://104.16.1.1", IP: "104.16.1.1", DelayMS: 12.5}},
		}},
	}
	if _, err := os.Stat(path); err == nil {
		_ = os.Remove(path)
	}
	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got.Entries) != 1 {
		t.Fatalf("load mismatch: %+v", got)
	}
	e := got.Entry("steam_cdn_cloudflare", 0)
	if e == nil || e.Ranked[0].IP != "104.16.1.1" {
		t.Fatalf("entry lookup: %+v", e)
	}
	if got.Entry("nope", 0) != nil {
		t.Fatal("unexpected entry")
	}

	// 目录被删除后 Load 返回 nil
	_ = os.RemoveAll(t.TempDir())
	if _, err := Load(filepath.Join(t.TempDir(), "x.json")); err != nil {
		t.Fatal(err)
	}
}

func TestCFSampleDeterministic(t *testing.T) {
	cidrs := []string{"104.16.0.0/12", "172.64.0.0/17", "1.1.1.0/24"}
	o := &Options{Rand: rand.New(rand.NewSource(42))}
	o.Inflate()
	a, err := o.CFSample(cidrs, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 3*8 {
		t.Fatalf("want 24 got %d", len(a))
	}
	o2 := &Options{Rand: rand.New(rand.NewSource(42))}
	o2.Inflate()
	b, _ := o2.CFSample(cidrs, 8)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("nondeterministic: %v vs %v", a, b)
		}
	}
}

func TestHostOf(t *testing.T) {
	cases := map[string]string{
		"https://str001.steam302.xyz":                  "str001.steam302.xyz",
		"str001.steam302.xyz":                          "str001.steam302.xyz",
		"https://steam.com/path?x=1":                   "steam.com",
		"https://198.41.128.3:443":                     "198.41.128.3",
		"steamuserimages-a.akamaihd.net.edgesuite.net": "steamuserimages-a.akamaihd.net.edgesuite.net",
	}
	for in, want := range cases {
		if got := hostOf(in); got != want {
			t.Errorf("hostOf(%q)=%q want %q", in, got, want)
		}
	}
}

func TestResolveCandidatesNode(t *testing.T) {
	o := &Options{ResolveFn: func(host string) ([]string, error) {
		return []string{"1.1.1.1", "10.2.3.4"}, nil
	}}
	o.Inflate()
	ips, err := o.ResolveCandidates("node", []string{"https://str001.steam302.xyz", "str002.steam302.xyz"}, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 2 {
		t.Fatalf("dedupe failed: %v", ips)
	}
}

func TestAllFakeIP(t *testing.T) {
	cases := []struct {
		ips  []string
		want bool
	}{
		{[]string{"198.18.0.19", "198.18.0.15"}, true},
		{[]string{"198.18.1.200"}, true},
		{[]string{"198.18.0.1", "23.49.104.59"}, false},
		{[]string{"23.49.104.59"}, false},
		{nil, false},
	}
	for _, c := range cases {
		ips := make([]net.IP, 0, len(c.ips))
		for _, s := range c.ips {
			ips = append(ips, net.ParseIP(s))
		}
		if got := allFakeIP(ips); got != c.want {
			t.Fatalf("allFakeIP(%v) = %v, want %v", c.ips, got, c.want)
		}
	}
}
