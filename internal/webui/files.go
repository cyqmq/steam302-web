package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxSlotSize = 1 << 20 // 1MB

// textSlot 是"文件槽"编辑器的受控文件（对应原版 dns_hosts / pac_user /
// dns_blacklist 三个文本槽）。仅允许操作此白名单，杜绝路径穿越。
type textSlot struct {
	Name string
	Rel  string // 相对于项目根
	Hint string
	Seed string // 文件不存在时的默认内容
}

var textSlots = []textSlot{
	{
		Name: "dns_hosts",
		Rel:  "config/dns_hosts.txt",
		Hint: "用户自定义 DNS 解析规则：每行「<域名或 *.域名> <IP>」，命中则返回该 IP（覆盖默认应答）。# 开头为注释。",
		Seed: "# 用户自定义 DNS 解析规则（config/dns_hosts.txt）\n# 每行: <域名或 *.域名> <IP>，例如:\n# github.api.example 127.0.0.1\n",
	},
	{
		Name: "pac_user",
		Rel:  "config/pac_user.txt",
		Hint: "PAC 用户补充域名：每行一个域名或 *.域名，将额外加入生成的 proxy.pac 走本机 HTTPS 代理。",
		Seed: "# PAC 用户补充域名（config/pac_user.txt）\n# 每行一个域名或 *.域名，例如:\n# example-cdn.example.com\n",
	},
	{
		Name: "dns_blacklist",
		Rel:  "config/dns_blacklist.txt",
		Hint: "DNS CDN 黑名单：每行一个域名或 *.域名，命中则不劫持、直接转发上游 DNS 返回真实解析结果。",
		Seed: "# DNS CDN 黑名单（config/dns_blacklist.txt）\n# 每行一个域名或 *.域名，例如:\n# *.example.com\n",
	},
}

func (s *Server) slotPath(slot textSlot) string { return filepath.Join(s.Root, slot.Rel) }

func findSlot(name string) (textSlot, bool) {
	for _, sl := range textSlots {
		if sl.Name == name {
			return sl, true
		}
	}
	return textSlot{}, false
}

// loadPACUser 读取 config/pac_user.txt，返回每行一个的域名模式（含 *. 通配）。
func (s *Server) loadPACUser() []string {
	data, err := os.ReadFile(filepath.Join(s.Root, "config", "pac_user.txt"))
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		if !strings.Contains(line, "*") && !isHostLike(line) {
			continue
		}
		out = append(out, line)
	}
	return out
}

func isHostLike(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r == '.' || r == '-' || r == ':' || r == '_' ||
			(r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return strings.Contains(s, ".")
}

func (s *Server) handleFileGet(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	slot, ok := findSlot(name)
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "unknown file slot: " + name})
		return
	}
	path := s.slotPath(slot)
	content := slot.Seed
	exists := false
	if data, err := os.ReadFile(path); err == nil {
		content = string(data)
		exists = true
	}
	rel, _ := filepath.Rel(s.Root, path)
	writeJSON(w, 200, map[string]any{
		"name":    slot.Name,
		"path":    rel,
		"hint":    slot.Hint,
		"exists":  exists,
		"content": content,
	})
}

func (s *Server) handleFileSave(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, 400, map[string]string{"error": "body 需为 {name, content}"})
		return
	}
	slot, ok := findSlot(body.Name)
	if !ok {
		writeJSON(w, 400, map[string]string{"error": "unknown file slot: " + body.Name})
		return
	}
	if len(body.Content) > maxSlotSize {
		writeJSON(w, 413, map[string]string{"error": "内容过大（上限 1MB）"})
		return
	}
	path := s.slotPath(slot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	content := strings.ReplaceAll(body.Content, "\r\n", "\n")
	tmp := fmt.Sprintf("%s.tmp", path)
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "name": slot.Name, "saved_at": time.Now().Format(time.RFC3339)})
}