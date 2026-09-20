package webui

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "embed"

	"steam302-web/internal/prefer"
	"steam302-web/internal/rules"
)

//go:embed static/index.html
var indexHTML []byte

type Server struct {
	Root  string
	Token string // 非空时启用 HTTP Basic 鉴权（用户固定 steam302）
}

type ruleView struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Group        string   `json:"group"`
	Description  string   `json:"description"`
	Enabled      bool     `json:"enabled"`
	NeedsFiles   []string `json:"needs_files"`
	MissingFiles []string `json:"missing_files"`
	HostCount    int      `json:"host_count"`
}

// proxyProfile 是"复制代理参数"面板的数据：给客户端一键配置 hosts /
// curl / PAC / 环境变量的文本片段。
type proxyProfile struct {
	BindIP   string   `json:"bind_ip"`
	HTTPS    int      `json:"https_port"`
	HTTP     int      `json:"http_port"`
	CAExists bool     `json:"ca_exists"`
	CAPath   string   `json:"ca_path"`
	Hosts    []string `json:"hosts"`
	HostsTxt string   `json:"hosts_txt"`
	CurlCmd  string   `json:"curl_cmd"`
	PAC      string   `json:"pac"`
	EnvVars  string   `json:"env"`
}

func (s *Server) profile() (proxyProfile, error) {
	env, err := rules.LoadEnv(s.envPath())
	if err != nil {
		return proxyProfile{}, err
	}
	rs, _, err := s.loadAll()
	if err != nil {
		return proxyProfile{}, err
	}
	bl, err := s.blacklist()
	if err != nil {
		return proxyProfile{}, err
	}
	if len(bl) > 0 {
		rs = rules.FilterBlacklist(rs, bl)
	}
	hostSet := map[string]bool{}
	for _, r := range rs {
		for _, site := range r.Sites {
			for _, h := range site.Hosts {
				hostSet[h] = true
			}
		}
	}
	hosts := make([]string, 0, len(hostSet))
	for h := range hostSet {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	bind := env.Listen.BindIP
	if bind == "" {
		bind = "127.0.0.1"
	}
	if env.Listen.HTTPSPort == 0 {
		env.Listen.HTTPSPort = 25584
	}
	if env.Listen.HTTPPort == 0 {
		env.Listen.HTTPPort = 24196
	}
	caPath := filepath.Join(s.Root, "config", "certs", "ca.pem")
	_, caErr := os.Stat(caPath)
	caExists := caErr == nil

	hostsTxt := ""
	for _, h := range hosts {
		hostsTxt += fmt.Sprintf("%s\t%s\n", bind, h)
	}

	curlCmd := ""
	curlEnv := ""
	concrete := ""
	for _, h := range hosts {
		if !strings.Contains(h, "*") {
			concrete = h
			break
		}
	}
	if concrete != "" {
		curlCmd = fmt.Sprintf("curl --resolve %s:%d:%s --cacert %s https://%s/",
			concrete, env.Listen.HTTPSPort, bind, caPath, concrete)
		curlEnv = fmt.Sprintf("export HTTPS_PROXY=http://%s:%d\nexport HTTP_PROXY=http://%s:%d\nexport CURL_CA_BUNDLE=%s\n",
			bind, env.Listen.HTTPPort, bind, env.Listen.HTTPPort, caPath)
	}
	pac := fmt.Sprintf("function FindProxyForURL(url, host) { /* 需先信任 %s 的 CA */\n  return \"HTTPS %s:%d\";\n}\n",
		caPath, bind, env.Listen.HTTPSPort)

	return proxyProfile{
		BindIP:   bind,
		HTTPS:    env.Listen.HTTPSPort,
		HTTP:     env.Listen.HTTPPort,
		CAExists: caExists,
		CAPath:   caPath,
		Hosts:    hosts,
		HostsTxt: hostsTxt,
		CurlCmd:  curlCmd,
		PAC:      pac,
		EnvVars:  curlEnv,
	}, nil
}

func (s *Server) overridesPath() string { return filepath.Join(s.Root, "config", "overrides.json") }
func (s *Server) rulesDir() string      { return filepath.Join(s.Root, "config", "rules") }
func (s *Server) envPath() string       { return filepath.Join(s.Root, "config", "env.json") }
func (s *Server) blacklistPath() string { return filepath.Join(s.Root, "config", "blacklist.json") }

func (s *Server) blacklist() ([]string, error) {
	return rules.LoadBlacklist(s.blacklistPath())
}

func (s *Server) loadAll() ([]rules.Rule, map[string]bool, error) {
	ov, err := rules.LoadOverrides(s.overridesPath())
	if err != nil {
		return nil, nil, err
	}
	rs, err := rules.LoadRulesWithOverrides(s.rulesDir(), true, ov)
	if err != nil {
		return nil, nil, err
	}
	return rs, ov, nil
}

func (s *Server) listViews() ([]ruleView, error) {
	rs, _, err := s.loadAll()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].Group != rs[j].Group {
			return rs[i].Group < rs[j].Group
		}
		return rs[i].ID < rs[j].ID
	})
	views := make([]ruleView, 0, len(rs))
	for _, r := range rs {
		hosts := 0
		for _, s := range r.Sites {
			hosts += len(s.Hosts)
		}
		views = append(views, ruleView{
			ID:           r.ID,
			Name:         r.Name,
			Group:        r.Group,
			Description:  r.Description,
			Enabled:      r.Enabled,
			NeedsFiles:   r.NeedsFiles,
			MissingFiles: rules.CheckNeedsFiles(s.Root, r),
			HostCount:    hosts,
		})
	}
	return views, nil
}

func (s *Server) saveOverrides(ov map[string]bool) error {
	data, err := json.MarshalIndent(ov, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.overridesPath(), append(data, '\n'), 0o644)
}

type regenResult struct {
	Rules     int    `json:"rules"`
	Hosts     int    `json:"hosts"`
	Caddyfile string `json:"caddyfile"`
	Validate  string `json:"validate"`
}

func (s *Server) regenerate() (regenResult, error) {
	env, err := rules.LoadEnv(s.envPath())
	if err != nil {
		return regenResult{}, err
	}
	ov, err := rules.LoadOverrides(s.overridesPath())
	if err != nil {
		return regenResult{}, err
	}
	rs, err := rules.LoadRulesWithOverrides(s.rulesDir(), false, ov)
	if err != nil {
		return regenResult{}, err
	}
	bl, err := s.blacklist()
	if err != nil {
		return regenResult{}, err
	}
	if len(bl) > 0 {
		rs = rules.FilterBlacklist(rs, bl)
	}
	pref, err := prefer.Load(filepath.Join(s.Root, prefer.Path))
	if err != nil {
		return regenResult{}, err
	}
	cf := rules.GenerateCaddyfile(env, rs, pref)
	cfPath := filepath.Join(s.Root, "Caddyfile")
	if err := os.WriteFile(cfPath, []byte(cf), 0o644); err != nil {
		return regenResult{}, err
	}
	hs := rules.GenerateHosts(env, rs)
	hsPath := filepath.Join(s.Root, "S302.hosts")
	if err := os.WriteFile(hsPath, []byte(hs), 0o644); err != nil {
		return regenResult{}, err
	}
	res := regenResult{
		Rules:     len(rs),
		Hosts:     strings.Count(hs, "\n"),
		Caddyfile: cfPath,
		Validate:  s.validate(cfPath),
	}
	return res, nil
}

func (s *Server) validate(caddyfile string) string {
	if _, err := exec.LookPath("caddy"); err != nil {
		return "skipped: caddy not in PATH"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "caddy", "adapt", "--config", caddyfile, "--validate").CombinedOutput()
	if err != nil {
		return "failed: " + strings.TrimSpace(string(out))
	}
	return "ok"
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})

	mux.HandleFunc("GET /api/rules", func(w http.ResponseWriter, r *http.Request) {
		views, err := s.listViews()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"rules": views})
	})

	mux.HandleFunc("POST /api/rules/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var body struct {
			Enabled *bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Enabled == nil {
			writeJSON(w, 400, map[string]string{"error": "body 需为 {\"enabled\": true|false}"})
			return
		}
		rs, _, err := s.loadAll()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		found := false
		for _, rule := range rs {
			if rule.ID == id {
				found = true
				break
			}
		}
		if !found {
			writeJSON(w, 404, map[string]string{"error": "unknown rule: " + id})
			return
		}
		ov, err := rules.LoadOverrides(s.overridesPath())
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if ov == nil {
			ov = map[string]bool{}
		}
		ov[id] = *body.Enabled
		if err := s.saveOverrides(ov); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		res, err := s.regenerate()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "rule": id, "enabled": *body.Enabled, "regen": res})
	})

	mux.HandleFunc("POST /api/regen", func(w http.ResponseWriter, r *http.Request) {
		res, err := s.regenerate()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "regen": res})
	})

	mux.HandleFunc("GET /api/profile", func(w http.ResponseWriter, r *http.Request) {
		p, err := s.profile()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, p)
	})

	// 规则编辑器：GET 原始 JSON 文本；PUT 校验后写盘并重生成。
	// id 仅允许 [a-z0-9_]，杜绝路径穿越。
	validRuleID := func(id string) bool {
		if id == "" || len(id) > 64 {
			return false
		}
		for _, c := range id {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
				return false
			}
		}
		return true
	}
	mux.HandleFunc("GET /api/rule/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !validRuleID(id) {
			writeJSON(w, 400, map[string]string{"error": "非法规则 id"})
			return
		}
		path := filepath.Join(s.rulesDir(), id+".json")
		text, err := os.ReadFile(path)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "规则不存在: " + id})
			return
		}
		writeJSON(w, 200, map[string]any{"id": id, "name": id, "text": string(text)})
	})
	mux.HandleFunc("PUT /api/rule/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !validRuleID(id) {
			writeJSON(w, 400, map[string]string{"error": "非法规则 id"})
			return
		}
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Text) == "" {
			writeJSON(w, 400, map[string]string{"error": "body 需为 {\"text\": \"...\"}"})
			return
		}
		var rl rules.Rule
		if err := json.Unmarshal([]byte(body.Text), &rl); err != nil {
			writeJSON(w, 400, map[string]string{"error": "JSON 无效: " + err.Error()})
			return
		}
		if len(rl.Sites) == 0 {
			writeJSON(w, 400, map[string]string{"error": "规则至少要有一个 sites 条目"})
			return
		}
		if rl.ID != "" && rl.ID != id {
			writeJSON(w, 400, map[string]string{"error": "规则 id 必须与文件名一致（= " + id + "）"})
			return
		}
		rl.ID = id
		// 写完先做一次磁盘级校验：能连同其他规则一起加载才算成功。
		path := filepath.Join(s.rulesDir(), id+".json")
		if err := os.WriteFile(path, []byte(body.Text), 0o644); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if _, _, err := s.loadAll(); err != nil {
			writeJSON(w, 500, map[string]string{"error": "规则集校验失败: " + err.Error()})
			return
		}
		res, err := s.regenerate()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "rule": id, "regen": res})
	})

	// 域名黑名单：GET 返回列表；PUT 用 {"domains": [...]} 整体替换。
	mux.HandleFunc("GET /api/blacklist", func(w http.ResponseWriter, r *http.Request) {
		bl, err := s.blacklist()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"domains": bl})
	})
	mux.HandleFunc("PUT /api/blacklist", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Domains []string `json:"domains"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "body 需为 {\"domains\": [...]}"})
			return
		}
		seen := map[string]bool{}
		var clean []string
		for _, d := range body.Domains {
			d = strings.TrimSpace(d)
			if d == "" || strings.ContainsAny(d, " \t/") {
				writeJSON(w, 400, map[string]string{"error": "非法域名: " + d})
				return
			}
			if !seen[d] {
				seen[d] = true
				clean = append(clean, d)
			}
		}
		data, err := json.MarshalIndent(map[string]any{"domains": clean}, "", "  ")
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if err := os.WriteFile(s.blacklistPath(), append(data, '\n'), 0o644); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		res, err := s.regenerate()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "count": len(clean), "regen": res})
	})

	return mux
}

// tokenAuth 用 HTTP Basic（用户固定 steam302）或 ?token= 参数做鉴权。
// 本机（回环来源）免鉴权；Token 为空时一律透传（保持回环裸访问的既有行为）。
func tokenAuth(token string, h http.Handler) http.Handler {
	if token == "" {
		return h
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if clientIsLoopback(r.RemoteAddr) || r.URL.Query().Get("token") == token || basicTokenOK(r, token) {
			h.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="steam302", charset="UTF-8"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
	})
}

func clientIsLoopback(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	return isLoopbackHost(host)
}

func basicTokenOK(r *http.Request, token string) bool {
	user, pass, ok := r.BasicAuth()
	if !ok || user != "steam302" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(pass), []byte(token)) == 1
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) Listen(addr string) error {
	if s.Root == "" {
		return errors.New("webui: empty root")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if s.Token == "" && !isLoopbackHost(host) {
		return fmt.Errorf("webui: 监听 %s 需设置 --token（局域网/公网必须鉴权）", addr)
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           tokenAuth(s.Token, s.Handler()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	if s.Token == "" {
		fmt.Printf("WebUI: http://%s/ (无鉴权)\n", addr)
	} else {
		fmt.Printf("WebUI: http://%s/ (Basic 鉴权，用户 steam302)\n", addr)
	}
	return srv.ListenAndServe()
}
