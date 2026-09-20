package rules

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
			got := GenerateCaddyfile(env, rules)
			if want := mustGolden(t, tc.goldenCaddy); got != want {
				t.Errorf("Caddyfile mismatch with golden %s", tc.goldenCaddy)
			}
			hosts := GenerateHosts(env, rules)
			if want := mustGolden(t, tc.goldenHosts); hosts != want {
				t.Errorf("hosts mismatch with golden %s", tc.goldenHosts)
			}
		})
	}
}
