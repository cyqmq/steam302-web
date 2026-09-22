package webui

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// connectionEntry 是经过规则匹配标注后的转发连接记录。
type connectionEntry struct {
	ID     uint64 `json:"id"`
	Remote string `json:"remote"`
	Host   string `json:"host"`
	Up     int64  `json:"up"`
	Down   int64  `json:"down"`
	Start  string `json:"start"`
	Last   string `json:"last"`
	Closed bool   `json:"closed"`
	Rule   string `json:"rule"`    // 命中的规则名（host 精确/后缀匹配）
	RuleID string `json:"rule_id"` // 命中的规则 id
}

// fwdAdminAddr 返回 fwd 管理接口地址（env.json fwd.admin_addr）。
func (s *Server) fwdAdminAddr() string {
	env, err := s.loadEnv()
	if err != nil {
		return ""
	}
	addr := env.Fwd.AdminAddr
	if addr == "" {
		return "127.0.0.1:28001"
	}
	return addr
}

func (s *Server) fwdRunning() bool {
	return svcState("steam302-web-fwd.service", "s302fwd run") == "active"
}

// fwdAdminFetch 请求 fwd 回环管理接口；fwd 未运行时返回专门错误。
func (s *Server) fwdAdminFetch(method, path string, body io.Reader) ([]byte, error) {
	if !s.fwdRunning() {
		return nil, fmt.Errorf("s302fwd 未运行")
	}
	host := s.fwdAdminAddr()
	if host == "" {
		return nil, fmt.Errorf("env.json 未配置 fwd.admin_addr")
	}
	url := "http://" + host + path
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fwd admin http %d: %s", resp.StatusCode, data)
	}
	return data, nil
}

// annotateConns 把 host 归属到具体规则（规则 hosts 支持精确与 "*.x" 后缀）。
func (s *Server) annotateConns(raw map[string]any) (map[string]any, error) {
	rs, _, err := s.loadAll()
	if err != nil {
		return raw, err
	}
	var hostRules = map[string]string{}
	var hostRuleIDs = map[string]string{}
	var wildRules []struct{ host, name, id string }
	for _, r := range rs {
		for _, site := range r.Sites {
			for _, h := range site.Hosts {
				if strings.HasPrefix(h, "*.") {
					wildRules = append(wildRules, struct{ host, name, id string }{
						strings.TrimPrefix(h, "*."), r.Name, r.ID})
					continue
				}
				if _, ok := hostRules[h]; !ok {
					hostRules[h] = r.Name
					hostRuleIDs[h] = r.ID
				}
			}
		}
	}
	match := func(h string) (string, string) {
		if h == "" {
			return "", ""
		}
		if name, ok := hostRules[h]; ok {
			return name, hostRuleIDs[h]
		}
		for _, w := range wildRules {
			if strings.HasSuffix(h, "."+w.host) {
				return w.name, w.id
			}
		}
		return "", ""
	}
	for _, key := range []string{"active", "recent"} {
		arr, _ := raw[key].([]any)
		for i := range arr {
			m, _ := arr[i].(map[string]any)
			if m == nil {
				continue
			}
			h, _ := m["host"].(string)
			name, id := match(h)
			m["rule"], m["rule_id"] = name, id
		}
	}
	return raw, nil
}

// registerConns 注册连接监控接口（代理到 fwd 回环管理端口）。
func (s *Server) registerConns(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/connections", func(w http.ResponseWriter, r *http.Request) {
		data, err := s.fwdAdminFetch(http.MethodGet, "/conns", nil)
		if err != nil {
			writeJSON(w, 200, map[string]any{
				"ok": false, "running": s.fwdRunning(), "error": err.Error(),
			})
			return
		}
		var raw map[string]any
		if json.Unmarshal(data, &raw) != nil {
			writeJSON(w, 500, map[string]string{"error": "fwd admin 响应无效"})
			return
		}
		if annotated, err := s.annotateConns(raw); err == nil {
			raw = annotated
		}
		raw["running"] = true
		writeJSON(w, 200, raw)
	})

	mux.HandleFunc("POST /api/connections/close", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID uint64 `json:"id"`
		}
		if err := jsonDecode(r, &body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "body 需为 {\"id\": 数字}"})
			return
		}
		data, err := s.fwdAdminFetch(http.MethodPost, "/conns/close", jsonBody(map[string]any{"id": body.ID}))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "result": jsonRaw(data)})
	})

	mux.HandleFunc("POST /api/connections/close-all", func(w http.ResponseWriter, r *http.Request) {
		data, err := s.fwdAdminFetch(http.MethodPost, "/conns/close-all", strings.NewReader("{}"))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "result": jsonRaw(data)})
	})

	mux.HandleFunc("POST /api/connections/clear", func(w http.ResponseWriter, r *http.Request) {
		_, err := s.fwdAdminFetch(http.MethodPost, "/conns/clear", strings.NewReader("{}"))
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true})
	})
}

func jsonBody(v any) *strings.Reader {
	data, _ := json.Marshal(v)
	return strings.NewReader(string(data))
}

func jsonRaw(data []byte) json.RawMessage {
	return json.RawMessage(data)
}
