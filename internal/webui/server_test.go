package webui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestRoot builds a minimal project root with one rule so endpoints that
// read/validate/produce config can run without touching the real repo.
func newTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "config", "env.json"), `{
  "schema_version": 1,
  "listen": {"https_port": 25584, "http_port": 24196, "bind_ip": "127.0.0.1"},
  "cert": {"cert_file": "config/certs/leaf.pem", "key_file": "config/certs/leaf.key", "ca_file": "config/certs/ca.pem"},
  "hosts": {"marker": "#S302X"},
  "fwd": {"bind": "127.0.0.1", "mappings": [{"from": 443, "to": 25584}]},
  "prefer": {"enabled": true},
  "upstream_defaults": {}
}`)
	mustWrite(t, filepath.Join(root, "config", "rules", "test_rule.json"), `{
  "schema_version": 1,
  "name": "测试服务",
  "group": "steam",
  "enabled": true,
  "sites": [
    {
      "hosts": ["a.example.com", "b.example.com"],
      "handlers": [
        {"type": "reverse_proxy", "upstreams": ["https://n1.example.com"]}
      ]
    }
  ]
}`)
	return root
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func do(t *testing.T, h http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, rd)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func serve(t *testing.T, root string) http.Handler {
	t.Helper()
	return (&Server{Root: root}).Handler()
}

func TestRuleEditorEndpoints(t *testing.T) {
	h := serve(t, newTestRoot(t))

	rr := do(t, h, "GET", "/api/rule/test_rule", nil)
	if rr.Code != 200 {
		t.Fatalf("GET rule code=%d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"id":"test_rule"`) && !strings.Contains(rr.Body.String(), `"id": "test_rule"`) {
		t.Fatalf("GET rule unexpected body: %s", rr.Body.String())
	}

	// 非法 JSON
	rr = do(t, h, "PUT", "/api/rule/test_rule", map[string]string{"text": "{bad"})
	if rr.Code != 400 {
		t.Fatalf("invalid json expected 400, got %d", rr.Code)
	}
	// 无 sites
	rr = do(t, h, "PUT", "/api/rule/test_rule", map[string]string{"text": "{}"})
	if rr.Code != 400 {
		t.Fatalf("empty rule expected 400, got %d", rr.Code)
	}
	// id 不匹配
	rr = do(t, h, "PUT", "/api/rule/test_rule", map[string]string{"text": `{"id":"other","name":"x","group":"steam","enabled":true,"sites":[{"hosts":["x.com"],"handlers":[]}]}`})
	if rr.Code != 400 {
		t.Fatalf("id mismatch expected 400, got %d (%s)", rr.Code, rr.Body.String())
	}
	// 路径穿越
	rr = do(t, h, "GET", "/api/rule/..%2F..%2Fenv", nil)
	if rr.Code != 400 {
		t.Fatalf("path traversal expected 400, got %d", rr.Code)
	}
	// 合法保存
	rr = do(t, h, "PUT", "/api/rule/test_rule", map[string]string{"text": `{"schema_version":1,"name":"改","group":"steam","enabled":true,"sites":[{"hosts":["c.example.com"],"handlers":[{"type":"reverse_proxy","upstreams":["https://n2.example.com"]}]}]}`})
	if rr.Code != 200 {
		t.Fatalf("valid save expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestBlacklistEndpoints(t *testing.T) {
	root := newTestRoot(t)
	h := serve(t, root)

	rr := do(t, h, "GET", "/api/blacklist", nil)
	if rr.Code != 200 {
		t.Fatalf("GET blacklist code=%d", rr.Code)
	}

	// 非法域名
	rr = do(t, h, "PUT", "/api/blacklist", map[string]any{"domains": []string{"has space.com"}})
	if rr.Code != 400 {
		t.Fatalf("bad domain expected 400, got %d", rr.Code)
	}

	// 黑名单 b.example.com → 再生成 hosts 不应含它
	rr = do(t, h, "PUT", "/api/blacklist", map[string]any{"domains": []string{"b.example.com"}})
	if rr.Code != 200 {
		t.Fatalf("set blacklist expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(root, "S302.hosts")); err != nil {
		t.Fatalf("S302.hosts not written: %v", err)
	}
	hosts, err := os.ReadFile(filepath.Join(root, "S302.hosts"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(hosts), "b.example.com") {
		t.Fatalf("blacklisted host still present:\n%s", hosts)
	}
	if !strings.Contains(string(hosts), "a.example.com") {
		t.Fatalf("non-blacklisted host missing:\n%s", hosts)
	}

	// 清空
	rr = do(t, h, "PUT", "/api/blacklist", map[string]any{"domains": []string{}})
	if rr.Code != 200 {
		t.Fatalf("clear blacklist expected 200, got %d", rr.Code)
	}
}

func TestProfileEndpoint(t *testing.T) {
	h := serve(t, newTestRoot(t))
	rr := do(t, h, "GET", "/api/profile", nil)
	if rr.Code != 200 {
		t.Fatalf("profile code=%d", rr.Code)
	}
	var p proxyProfile
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if p.BindIP != "127.0.0.1" || !strings.Contains(p.HostsTxt, "a.example.com") {
		t.Fatalf("profile wrong: %+v", p)
	}
	if p.HostsTxt == "" {
		t.Fatal("hosts_txt empty")
	}
	for _, assert := range []struct {
		wantOK   bool
		contains string
	}{
		{true, `host === "a.example.com" || host.endsWith(".a.example.com")`},
		{true, `return "DIRECT";`},
		{false, `return "HTTPS` + `"`, // 白名单式 PAC 不应再无条件全代理
		},
	} {
		got := strings.Contains(p.PAC, assert.contains)
		if got != assert.wantOK {
			t.Fatalf("PAC assert(contains=%q want=%v) mismatch", assert.contains, assert.wantOK)
		}
	}
	// 黑名单后 PAC 不再包含该域名条件
	if rr := do(t, h, "PUT", "/api/blacklist", map[string][]string{"domains": {"b.example.com"}}); rr.Code != 200 {
		t.Fatalf("blacklist put code=%d", rr.Code)
	}
	if rr := do(t, h, "GET", "/api/profile", nil); rr.Code == 200 {
		var p2 proxyProfile
		_ = json.Unmarshal(rr.Body.Bytes(), &p2)
		if strings.Contains(p2.PAC, `"b.example.com"`) {
			t.Fatal("blacklisted host still whitelisted in PAC")
		}
	} else {
		t.Fatalf("profile after blacklist code=%d", rr.Code)
	}
}
