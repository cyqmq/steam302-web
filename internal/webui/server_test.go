package webui

import (
	"encoding/json"
	"io/fs"
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

// TestStaticIndexAndAssets 锁定 Vite 构建产物托管：/ 返回 SPA 入口，/assets 静态直出。
func TestStaticIndexAndAssets(t *testing.T) {
	srv := serve(t, newTestRoot(t))

	rr := do(t, srv, "GET", "/", nil)
	if rr.Code != 200 {
		t.Fatalf("GET / => %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `<div id="app"></div>`) {
		t.Fatalf("index.html 缺少 #app 挂载点")
	}

	// 任意已嵌入的 assets 资源应可直出
	entries, err := fs.ReadDir(uiFS, "static/assets")
	if err != nil {
		t.Fatalf("读取嵌入 assets 失败: %v", err)
	}
	if len(entries) == 0 {
		t.Skip("无静态资源")
	}
	name := entries[0].Name()
	rr = do(t, srv, "GET", "/assets/"+name, nil)
	if rr.Code != 200 {
		t.Fatalf("GET /assets/%s => %d, want 200", name, rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct == "" {
		t.Fatalf("静态资源缺少 Content-Type")
	}

	// 未知路径回落 index.html（SPA 路由）
	rr = do(t, srv, "GET", "/settings", nil)
	if rr.Code != 200 {
		t.Fatalf("GET /settings => %d, want 200（SPA 回落）", rr.Code)
	}
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

func TestStatusEndpoint(t *testing.T) {
	h := serve(t, newTestRoot(t))
	rr := do(t, h, "GET", "/api/status", nil)
	if rr.Code != 200 {
		t.Fatalf("status code=%d", rr.Code)
	}
	var st netStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.BindIP != "127.0.0.1" || st.HTTPSPort != 25584 || st.HTTPPort != 24196 {
		t.Fatalf("status wrong: %+v", st)
	}
	if st.Upstream != "" {
		t.Fatalf("upstream should be empty: %q", st.Upstream)
	}
	if len(st.Services) != 4 {
		t.Fatalf("services map should have 4 entries: %+v", st.Services)
	}
}

func TestLogsEndpoint(t *testing.T) {
	root := newTestRoot(t)
	h := serve(t, root)

	// 文件不存在：200 + error 字段
	rr := do(t, h, "GET", "/api/logs", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "error") {
		t.Fatalf("missing log expected 200+error, got %d: %s", rr.Code, rr.Body.String())
	}

	// 写入日志后：all=1 返回全部，默认截断到最后 1 行体量的 1000 行
	logPath := filepath.Join(root, "config", "s302fwd.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{"[00:00:01] hello", "[00:00:02] world", "[00:00:03] ok"}
	mustWrite(t, logPath, strings.Join(lines, "\n")+"\n")

	rr = do(t, h, "GET", "/api/logs?all=1", nil)
	if rr.Code != 200 {
		t.Fatalf("logs all code=%d", rr.Code)
	}
	var lv logsView
	if err := json.Unmarshal(rr.Body.Bytes(), &lv); err != nil {
		t.Fatal(err)
	}
	if len(lv.Lines) != 3 || lv.Total != 3 || lv.Truncated {
		t.Fatalf("logs wrong: %+v", lv)
	}
	rr = do(t, h, "GET", "/api/logs", nil)
	_ = json.Unmarshal(rr.Body.Bytes(), &lv)
	if len(lv.Lines) != 3 || lv.Lines[0] != lines[0] {
		t.Fatalf("default logs wrong: %+v", lv)
	}
}

func TestBulkEndpoint(t *testing.T) {
	h := serve(t, newTestRoot(t))
	rr := do(t, h, "POST", "/api/rules/bulk", map[string]any{"enabled": false})
	if rr.Code != 200 {
		t.Fatalf("bulk code=%d: %s", rr.Code, rr.Body.String())
	}
	var out struct {
		OK    bool `json:"ok"`
		Count int  `json:"count"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Count != 1 {
		t.Fatalf("bulk wrong: %+v", out)
	}
	// 缺 body
	if rr := do(t, h, "POST", "/api/rules/bulk", nil); rr.Code != 400 {
		t.Fatalf("bulk without body expected 400, got %d", rr.Code)
	}
}

func TestVersionEndpoint(t *testing.T) {
	h := serve(t, newTestRoot(t))
	rr := do(t, h, "GET", "/api/version", nil)
	if rr.Code != 200 {
		t.Fatalf("version code=%d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"name":"`) || !strings.Contains(rr.Body.String(), `"version":"`) {
		t.Fatalf("version body missing fields: %s", rr.Body.String())
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
		{false, `return "HTTPS` + `"`}, // 白名单式 PAC 不应再无条件全代理

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
