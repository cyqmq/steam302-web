package webui

import (
	"context"
	"net/http"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"steam302-web/internal/rules"
)

// dnsSnapshot 供「服务」页展示当前 DNS 重定向的运行时信息。
type dnsSnapshot struct {
	Listen        string   `json:"listen"`
	Upstream      []string `json:"upstream"`
	TTL           uint32   `json:"ttl"`
	AnswerIP      string   `json:"answer_ip"`
	QueryLog      bool     `json:"query_log"`
	UserRules     bool     `json:"user_rules"`
	ResolvManaged bool     `json:"resolv_managed"`
	LANRedirect   bool     `json:"lan_redirect"`
	Active        bool     `json:"active"`
	Forwarded     int64    `json:"queries_forwarded"`
}

var dnsQueryMu sync.Mutex

// dnsConfigView 从 env.json 组装 DNS 快照。
func (s *Server) dnsConfigView() dnsSnapshot {
	env, err := s.loadEnv()
	if err != nil {
		env = rules.Env{}
	}
	return dnsSnapshot{
		Listen:        orStr(env.DNS.Listen, "127.0.0.1:53"),
		Upstream:      env.DNS.Upstream,
		TTL:           env.DNS.TTL,
		AnswerIP:      orStr(env.DNS.AnswerIP, "127.0.0.1"),
		QueryLog:      env.DNS.QueryLog,
		UserRules:     env.DNS.UserRules,
		ResolvManaged: env.DNS.ResolvManaged,
		LANRedirect:   env.DNS.LANRedirect,
		Active:        unitActive("steam302-web-dnsd.service") == "active",
	}
}

// runDnsredir 以 sudo 调 bin/dnsredir；失败返回输出供前端展示。
func (s *Server) runDnsredir(args ...string) (string, error) {
	argv := append([]string{"-n", filepath.Join(s.Root, "bin", "dnsredir")}, args...)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sudo", argv...).CombinedOutput()
	return string(out), err
}

// registerDNS 注册 DNS 重定向相关接口。
func (s *Server) registerDNS(mux *http.ServeMux) {
	// DNS 配置快照。
	mux.HandleFunc("GET /api/dns", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.dnsConfigView())
	})

	// 系统解析器接管/释放：POST /api/dns/resolv {"enabled":true|false}
	mux.HandleFunc("POST /api/dns/resolv", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if err := jsonDecode(r, &body); err != nil || body.Enabled == nil {
			writeJSON(w, 400, map[string]string{"error": "body 需为 {\"enabled\": true|false}"})
			return
		}
		verb := "apply"
		if !*body.Enabled {
			verb = "remove"
		}
		out, err := s.runDnsredir("resolv", verb)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "output": out, "error": errString(err)})
			return
		}
		env, _ := s.loadEnv()
		env.DNS.ResolvManaged = *body.Enabled
		_ = s.saveEnv(env)
		writeJSON(w, 200, map[string]any{"ok": true, "enabled": *body.Enabled, "output": out})
	})

	// 局域网 53 重定向：POST /api/dns/lan {"enabled":true|false}
	mux.HandleFunc("POST /api/dns/lan", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if err := jsonDecode(r, &body); err != nil || body.Enabled == nil {
			writeJSON(w, 400, map[string]string{"error": "body 需为 {\"enabled\": true|false}"})
			return
		}
		env, _ := s.loadEnv()
		listen := orStr(env.DNS.Listen, "127.0.0.1:53")
		verb := "install"
		if !*body.Enabled {
			verb = "remove"
		}
		args := []string{"lan", verb, "--listen", listen}
		out, err := s.runDnsredir(args...)
		if err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "output": out, "error": errString(err)})
			return
		}
		env.DNS.LANRedirect = *body.Enabled
		_ = s.saveEnv(env)
		writeJSON(w, 200, map[string]any{"ok": true, "enabled": *body.Enabled, "output": out})
	})
}
