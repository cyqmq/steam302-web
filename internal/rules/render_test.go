package rules

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"steam302-web/internal/prefer"
)

func projectRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..")
}

func mustGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(projectRoot(t), "internal", "rules", "testdata", name))
	if err != nil {
		t.Fatalf("read golden %s: %v", name, err)
	}
	return string(b)
}

func TestGenerate(t *testing.T) {
	root := projectRoot(t)
	env, err := LoadEnv(filepath.Join(root, "config", "env.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		includeAll  bool
		goldenCaddy string
		goldenHosts string
	}{
		{"default", false, "golden_caddyfile.txt", "golden_hosts.txt"},
		{"all", true, "golden_caddyfile_all.txt", "golden_hosts_all.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rules, err := LoadRules(filepath.Join(root, "config", "rules"), tc.includeAll)
			if err != nil {
				t.Fatal(err)
			}
			if len(rules) == 0 {
				t.Fatal("no rules loaded")
			}
			got := GenerateCaddyfile(env, rules, nil)
			hosts := GenerateHosts(env, rules)
			if os.Getenv("RUN_UPDATE_GOLDEN") == "1" {
				dir := filepath.Join(root, "internal", "rules", "testdata")
				for _, f := range []struct{ name, data string }{
					{tc.goldenCaddy, got}, {tc.goldenHosts, hosts},
				} {
					if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.data), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				return
			}
			if want := mustGolden(t, tc.goldenCaddy); got != want {
				t.Errorf("Caddyfile mismatch with golden %s", tc.goldenCaddy)
			}
			if want := mustGolden(t, tc.goldenHosts); hosts != want {
				t.Errorf("hosts mismatch with golden %s", tc.goldenHosts)
			}
		})
	}
}

// TestGenerateWithPrefer 验证有 CDN 优选缓存时：优选 IP 前置进上游、原上游保留。
func TestGenerateWithPrefer(t *testing.T) {
	root := projectRoot(t)
	env, err := LoadEnv(filepath.Join(root, "config", "env.json"))
	if err != nil {
		t.Fatal(err)
	}
	rules, err := LoadRules(filepath.Join(root, "config", "rules"), false)
	if err != nil {
		t.Fatal(err)
	}
	pref := &prefer.Cache{
		SchemaVersion: 1,
		Entries: []prefer.Entry{{
			RuleID: "steam_cdn_akamai", SiteIndex: 0, Mode: "node",
			Ranked: []prefer.Ranked{
				{Upstream: "https://1.2.3.4", IP: "1.2.3.4", DelayMS: 5},
				{Upstream: "https://5.6.7.8", IP: "5.6.7.8", DelayMS: 8},
			},
		}},
	}
	cf := GenerateCaddyfile(env, rules, pref)
	if !strings.Contains(cf, "https://1.2.3.4") || !strings.Contains(cf, "https://5.6.7.8") {
		t.Fatal("prefer IPs missing in Caddyfile")
	}
	if !strings.Contains(cf, "steamuserimages-a.akamaihd.net.edgesuite.net") {
		t.Fatal("original upstreams lost")
	}
}
