package webui

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// updateStatePath 是 cmd/update 写入状态的 JSON 文件（config/.update/state.json）。
func (s *Server) updateStatePath() string {
	return filepath.Join(s.Root, "config", ".update", "state.json")
}

// handleUpdateStatus 返回当前更新状态（无进行中的更新时为 idle）。
func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	version, page := checkLatest()
	st := map[string]any{
		"phase":      "idle",
		"done":       false,
		"ok":         false,
		"current":    appVersion,
		"latest":     version,
		"has_update": newerVersion(appVersion, version),
		"latest_url": page,
	}
	if data, err := os.ReadFile(s.updateStatePath()); err == nil {
		var i map[string]any
		if json.Unmarshal(data, &i) == nil {
			for k, v := range i {
				st[k] = v
			}
			st["current"] = appVersion
		}
	}
	writeJSON(w, 200, st)
}

// handleUpdateStart 后台拉起 bin/update 执行自更新（仅 loopback/持 token 可调）。
func (s *Server) handleUpdateStart(w http.ResponseWriter, r *http.Request) {
	if _, err := os.Stat(filepath.Join(s.Root, "bin", "update")); err != nil {
		writeJSON(w, 200, map[string]any{"started": false, "error": "bin/update 不存在，请先构建"})
		return
	}
	logPath := filepath.Join(s.Root, "config", ".update", "run.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	// setsid 脱离会话，父进程退出后继续运行；sudo -n 无交互（需免密）。
	sh := "cd " + shellQuote(s.Root) + " && setsid " + shellQuote(filepath.Join(s.Root, "bin", "update")) +
		" --root " + shellQuote(s.Root) + " --current " + shellQuote(appVersion) +
		" > " + shellQuote(logPath) + " 2>&1 < /dev/null &"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "sudo", "-n", "setsid", "bash", "-c", sh).CombinedOutput()
	if err != nil {
		writeJSON(w, 200, map[string]any{
			"started": false,
			"error":   "无法以 root 启动更新进程: " + err.Error(),
			"hint":    "请在本机执行: sudo bash -c \"" + sh + "\"",
		})
		return
	}
	_ = out
	writeJSON(w, 200, map[string]any{"started": true})
}

func shellQuote(s string) string {
	return "'" + s + "'"
}
