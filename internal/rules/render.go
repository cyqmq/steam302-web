package rules

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"steam302-web/internal/prefer"
)

func caddyQuote(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, `"`, `\"`)
	if strings.Contains(s, " ") || s == "" {
		return `"` + s + `"`
	}
	return s
}

func renderMatch(idx int, m *Match) (name, block string) {
	if m == nil {
		return "", ""
	}
	name = fmt.Sprintf("m%d", idx)
	var b strings.Builder
	b.WriteString("@" + name + " {\n")
	for _, p := range m.Path {
		b.WriteString("    path " + p + "\n")
	}
	for _, mm := range m.Method {
		b.WriteString("    method " + mm + "\n")
	}
	for _, ex := range m.Expression {
		b.WriteString("    expression " + ex + "\n")
	}
	for _, n := range m.Not {
		for _, p := range n.Path {
			b.WriteString("    not path " + p + "\n")
		}
		for _, mm := range n.Method {
			b.WriteString("    not method " + mm + "\n")
		}
		for _, ex := range n.Expression {
			b.WriteString("    not expression " + ex + "\n")
		}
	}
	b.WriteString("}")
	return name, b.String()
}

func strOr(msg json.RawMessage) string {
	var s string
	_ = json.Unmarshal(msg, &s)
	return s
}

func renderHeaderUp(entries [][]json.RawMessage) string {
	var lines []string
	for _, e := range entries {
		switch len(e) {
		case 2:
			name := strOr(e[0])
			if string(e[1]) == "null" {
				lines = append(lines, "    header_up -"+name)
			} else {
				lines = append(lines, "    header_up "+name+" "+caddyQuote(strOr(e[1])))
			}
		case 3:
			lines = append(lines, "    header_up "+strOr(e[0])+" "+caddyQuote(strOr(e[1]))+" "+caddyQuote(strOr(e[2])))
		}
	}
	return strings.Join(lines, "\n")
}

func renderHeaderDown(entries [][]json.RawMessage) string {
	var lines []string
	for _, e := range entries {
		if len(e) == 0 {
			continue
		}
		name := strOr(e[0])
		if len(e) == 1 || string(e[1]) == "null" {
			lines = append(lines, "    header_down -"+name)
		} else {
			lines = append(lines, "    header_down "+name+" "+caddyQuote(strOr(e[len(e)-1])))
		}
	}
	return strings.Join(lines, "\n")
}

func renderTransport(t *Transport) string {
	if t == nil {
		return ""
	}
	var lines []string
	lines = append(lines, "    transport http {")
	if t.TLS {
		lines = append(lines, "        tls")
	}
	if t.TLSServerName != "" {
		lines = append(lines, "        tls_server_name "+caddyQuote(t.TLSServerName))
	}
	if t.InsecureSkipVerify {
		lines = append(lines, "        tls_insecure_skip_verify")
	}
	lines = append(lines, "    }")
	return strings.Join(lines, "\n")
}

func renderReverseProxy(prefix, matcher string, h Handler) string {
	if len(h.Upstreams) == 0 && len(h.DynamicUpstreams) == 0 {
		return ""
	}
	line := strings.TrimLeft(prefix+"reverse_proxy "+matcher, " ")
	line = strings.TrimRight(line, " ")
	for _, u := range h.Upstreams {
		if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
			u = "https://" + u
		}
		line += " " + u
	}
	line += " {"

	var block []string
	for _, d := range h.DynamicUpstreams {
		dns := d.DNS
		if dns == "" {
			dns = "a"
		}
		port := d.Port
		if port == 0 {
			port = 443
		}
		block = append(block, fmt.Sprintf("    dynamic %s %s %d", dns, d.Host, port))
	}
	if lb := h.LB; lb != nil {
		if lb.Policy != "" {
			p := "    lb_policy " + lb.Policy
			if lb.Policy == "random_choose" && lb.Choose > 0 {
				p += fmt.Sprintf(" %d", lb.Choose)
			}
			block = append(block, p)
		}
		if lb.TryDuration != "" {
			block = append(block, "    lb_try_duration "+lb.TryDuration)
		}
		if lb.FailDuration != "" {
			block = append(block, "    fail_duration "+lb.FailDuration)
		}
		if lb.MaxFails != 0 {
			block = append(block, fmt.Sprintf("    max_fails %d", lb.MaxFails))
		}
		if lb.UnhealthyLatency != "" {
			block = append(block, "    unhealthy_latency "+lb.UnhealthyLatency)
		}
		if lb.UnhealthyStatus != "" {
			block = append(block, "    unhealthy_status "+lb.UnhealthyStatus)
		}
	}
	if hup := renderHeaderUp(h.HeaderUp); hup != "" {
		block = append(block, hup)
	}
	if hdown := renderHeaderDown(h.HeaderDown); hdown != "" {
		block = append(block, hdown)
	}
	if tr := renderTransport(h.Transport); tr != "" {
		block = append(block, tr)
	}
	block = append(block, "}")
	return line + "\n" + strings.Join(block, "\n")
}

func renderHandler(prefix string, h Handler, idx int) string {
	name, mblock := renderMatch(idx, h.Match)
	matcher := ""
	if name != "" {
		matcher = "@" + name + " "
	}
	var seg []string
	if mblock != "" {
		seg = append(seg, mblock)
	}
	switch h.Type {
	case "reverse_proxy":
		if rp := renderReverseProxy(prefix, strings.TrimSpace(matcher), h); rp != "" {
			seg = append(seg, rp)
		}
	case "file_server":
		fs := h.FileServer
		if fs == nil {
			fs = &FileServer{}
		}
		root := fs.Root
		if root == "" {
			root = "."
		}
		var b strings.Builder
		base := strings.TrimLeft(prefix+"file_server "+matcher, " ")
		b.WriteString(strings.TrimRight(base, " "))
		b.WriteString(" {\n")
		b.WriteString(prefix + "    root " + root + "\n")
		if fs.Browse {
			b.WriteString(prefix + "    browse\n")
		}
		b.WriteString(prefix + "}")
		seg = append(seg, b.String())
	case "respond":
		r := h.Respond
		if r == nil {
			return ""
		}
		status := r.Status
		if status == 0 {
			status = 200
		}
		line := strings.TrimLeft(prefix+"respond "+matcher, " ")
		line = strings.TrimRight(line, " ") + fmt.Sprintf(" %d", status)
		if r.Body != "" {
			line += " {\n" + prefix + "    body " + caddyQuote(r.Body) + "\n" + prefix + "}"
		} else if r.Close {
			line += " {\n" + prefix + "    close\n" + prefix + "}"
		}
		seg = append(seg, line)
	case "redir":
		to := "{uri}"
		if h.Redir != nil && h.Redir.To != "" {
			to = h.Redir.To
		}
		base := strings.TrimLeft(prefix+"redir "+matcher, " ")
		line := strings.TrimRight(base, " ") + " " + to
		if h.Redir != nil && h.Redir.Permanent {
			line += " permanent"
		}
		seg = append(seg, strings.TrimLeft(line, " "))
	case "rewrite":
		to := ""
		if h.Rewrite != nil {
			to = h.Rewrite.To
		}
		base := strings.TrimLeft(prefix+"rewrite "+matcher, " ")
		seg = append(seg, strings.TrimRight(strings.TrimRight(base, " ")+" "+to, " "))
	}
	return strings.Join(seg, "\n")
}

func renderCORS(prefix string) string {
	return prefix + "@preflight {\n" +
		prefix + "    method OPTIONS\n" +
		prefix + "}\n" +
		prefix + "respond @preflight 204\n" +
		prefix + "header {\n" +
		prefix + "    defer\n" +
		prefix + "    Access-Control-Allow-Origin {http.request.header.Origin}\n" +
		prefix + "    Access-Control-Allow-Credentials true\n" +
		prefix + "    Access-Control-Allow-Methods *\n" +
		prefix + "    Access-Control-Allow-Headers *\n" +
		prefix + "    Access-Control-Allow-Private-Network true\n" +
		prefix + "    Access-Control-Request-Private-Network true\n" +
		prefix + "}\n"
}

func renderExtraHeaders(prefix string, headers []Header) string {
	if len(headers) == 0 {
		return ""
	}
	lines := []string{prefix + "header {"}
	for _, h := range headers {
		if h.Defer {
			lines = append(lines, prefix+"    defer")
		}
		lines = append(lines, prefix+"    "+h.Name+" "+caddyQuote(h.Value))
	}
	lines = append(lines, prefix+"}")
	return strings.Join(lines, "\n")
}

func renderSite(site Site, env *Env, pref *prefer.Cache, ruleID string, siteIdx int) string {
	if len(site.Hosts) == 0 {
		return ""
	}
	if pref != nil {
		if e := pref.Entry(ruleID, siteIdx); e != nil && len(e.Ranked) > 0 {
			site = applyPreferred(site, e)
		}
	}
	port := env.Listen.HTTPSPort
	hosts := make([]string, 0, len(site.Hosts))
	for _, h := range site.Hosts {
		hosts = append(hosts, fmt.Sprintf("https://%s:%d", h, port))
	}
	cert := site.TLSCert
	if cert == "" {
		cert = env.Cert.CertFile
	}
	key := site.TLSKey
	if key == "" {
		key = env.Cert.KeyFile
	}

	var out []string
	out = append(out, strings.Join(hosts, " ")+" {")
	out = append(out, "    tls "+cert+" "+key)
	if site.PNACors {
		out = append(out, renderCORS("    "))
	}
	if ex := renderExtraHeaders("    ", site.ExtraHeaders); ex != "" {
		out = append(out, ex)
	}
	for i, h := range site.Handlers {
		if r := renderHandler("    ", h, i); r != "" {
			out = append(out, r)
		}
	}
	out = append(out, "}")
	return strings.Join(out, "\n")
}

// applyPreferred 把优选结果对应的 IP 前置到该 site 的 reverse_proxy 上游，
// 原上游保留作 fallback（node/cf-cidr 模式下除外：cf/cidr 只保留校验过的 pins，
// 因为 akamai/动态上游对 fastly/cloudflare 主机名会 400 Invalid URL）。
// cf/cidr 模式下若 handler 未配置 TLS/SNI，补上 {host} 伪装。
func applyPreferred(site Site, e *prefer.Entry) Site {
	ups := make([]string, 0, len(e.Ranked))
	for _, r := range e.Ranked {
		ups = append(ups, r.Upstream)
	}
	handlers := make([]Handler, len(site.Handlers))
	for i, h := range site.Handlers {
		h2 := h
		if h2.Type == "reverse_proxy" && (len(h2.Upstreams) > 0 || len(h2.DynamicUpstreams) > 0) {
			combined := make([]string, 0, len(ups)+len(h2.Upstreams))
			combined = append(combined, ups...)
			combined = append(combined, h2.Upstreams...)
			h2.Upstreams = combined
			// cidr/cf 模式下 pins 是严格按该域名校验过的边缘；原 akamai/动态上游
			// 对 fastly/cloudflare 主机名会 400（Invalid URL），故只保留 pins。
			if e.Mode == "cf" || e.Mode == "cidr" {
				h2.Upstreams = ups
				h2.DynamicUpstreams = nil
			}
			if (e.Mode == "cf" || e.Mode == "cidr") && (h2.Transport == nil || !h2.Transport.TLS) {
				h2.Transport = &Transport{TLS: true, TLSServerName: "{host}"}
			}
			// 边缘上游证书与域名不匹配时跳过上游 TLS 校验保可用
			if e.Mode == "cf" || e.Mode == "cidr" {
				if h2.Transport == nil {
					h2.Transport = &Transport{}
				}
				t2 := *h2.Transport
				t2.TLS = true
				if t2.TLSServerName == "" {
					t2.TLSServerName = "{host}"
				}
				t2.InsecureSkipVerify = true
				h2.Transport = &t2
			}
		}
		handlers[i] = h2
	}
	site.Handlers = handlers
	return site
}

func renderGlobal(env *Env) string {
	aut := env.Listen.AutoHTTPS
	if aut == "" {
		aut = "off"
	}
	lines := []string{"{"}
	if env.Listen.AdminOff {
		lines = append(lines, "    admin off")
	}
	lines = append(lines, "    auto_https "+aut)
	if b := env.Listen.BindIP; b != "" && b != "0.0.0.0" {
		lines = append(lines, "    default_bind "+b)
	}
	lines = append(lines, "}")
	return strings.Join(lines, "\n")
}

func renderHTTPRedirect(env *Env) string {
	if env.Listen.HTTPPort <= 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("http://:%d {\n", env.Listen.HTTPPort))
	b.WriteString("    redir https://{host}{uri} permanent\n")
	b.WriteString("}")
	return b.String()
}

func GenerateCaddyfile(env *Env, rules []Rule, pref *prefer.Cache) string {
	parts := []string{renderGlobal(env), ""}
	for _, rule := range rules {
		for i, site := range rule.Sites {
			parts = append(parts, renderSite(site, env, pref, rule.ID, i), "")
		}
	}
	if http := renderHTTPRedirect(env); http != "" {
		parts = append(parts, http, "")
	}
	return strings.Join(parts, "\n")
}

func GenerateHosts(env *Env, rules []Rule) string {
	marker := env.Hosts.Marker
	seen := map[string]bool{}
	var hosts []string
	for _, rule := range rules {
		for _, site := range rule.Sites {
			for _, h := range site.Hosts {
				if seen[h] {
					continue
				}
				seen[h] = true
				hosts = append(hosts, h)
			}
		}
	}
	sort.Strings(hosts)
	var lines []string
	for _, h := range hosts {
		lines = append(lines, "127.0.0.1 "+h+" "+marker)
	}
	return strings.Join(lines, "\n") + "\n"
}
