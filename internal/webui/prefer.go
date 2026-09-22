package webui

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	preferServicePath = "/etc/systemd/system/steam302-web-prefer.service"
	preferTimerPath   = "/etc/systemd/system/steam302-web-prefer.timer"
)

// preferTimerStatus 读取 systemd 定时器状态。
func preferTimerStatus() map[string]any {
	enabled := strings.TrimSpace(string(mustRun("systemctl", "is-enabled", "steam302-web-prefer.timer"))) == "enabled"
	active := strings.TrimSpace(string(mustRun("systemctl", "is-active", "steam302-web-prefer.timer"))) == "active"
	minutes := 0
	if data, err := os.ReadFile(preferTimerPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "OnCalendar=*:0/"); ok {
				if n, err := strconv.Atoi(v); err == nil {
					minutes = n
				}
			}
		}
	}
	return map[string]any{"enabled": enabled, "active": active, "minutes": minutes}
}

// writePreferTimer 落盘 oneshot 服务与定时器并重载 systemd。
func writePreferTimer(root string, minutes int) error {
	svc := `[Unit]
Description=steam302-web CDN 优选定时健康检测（oneshot）
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
WorkingDirectory=` + root + `
ExecStart=` + filepath.Join(root, "bin", "prefer") + ` run --quick --timeout 12
ExecStart=` + filepath.Join(root, "bin", "genconfig") + ` --hosts ` + filepath.Join(root, "S302.hosts") + `
ExecStart=/bin/systemctl restart steam302-web-caddy.service
ExecStart=/bin/systemctl restart steam302-web-fwd.service
`
	if err := os.WriteFile(preferServicePath, []byte(svc), 0o644); err != nil {
		return err
	}
	timer := `[Unit]
Description=steam302-web CDN 优选定期健康检测

[Timer]
OnCalendar=*:0/` + strconv.Itoa(minutes) + `
Persistent=true

[Install]
WantedBy=timers.target
`
	if err := os.WriteFile(preferTimerPath, []byte(timer), 0o644); err != nil {
		return err
	}
	if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
		return fmt.Errorf("daemon-reload: %v\n%s", err, out)
	}
	return nil
}

func (s *Server) setPreferTimer(enabled bool, minutes int) (map[string]any, error) {
	if minutes < 1 {
		minutes = 30
	}
	if err := writePreferTimer(s.Root, minutes); err != nil {
		return nil, err
	}
	if enabled {
		if out, err := exec.Command("systemctl", "enable", "--now", "steam302-web-prefer.timer").CombinedOutput(); err != nil {
			return nil, fmt.Errorf("enable --now: %v\n%s", err, out)
		}
	} else {
		_ = exec.Command("systemctl", "disable", "--now", "steam302-web-prefer.timer").Run()
	}
	env, _ := s.loadEnv()
	env.UI.DNSCDNPrefer = enabled && minutes >= 1
	_ = s.saveEnv(env)
	return preferTimerStatus(), nil
}

// registerPrefer 注册 CDN 优选定时健康检测接口。
func (s *Server) registerPrefer(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/prefer/timer", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, preferTimerStatus())
	})
	mux.HandleFunc("POST /api/prefer/timer", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled *bool `json:"enabled"`
			Minutes int   `json:"minutes"`
		}
		if err := jsonDecode(r, &body); err != nil || body.Enabled == nil {
			writeJSON(w, 400, map[string]string{"error": "body 需为 {\"enabled\": true|false, \"minutes\": n}"})
			return
		}
		if body.Minutes < 1 || body.Minutes > 1440 {
			writeJSON(w, 400, map[string]string{"error": "minutes 须在 1~1440"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		done := make(chan map[string]any, 1)
		errc := make(chan error, 1)
		go func() {
			res, err := s.setPreferTimer(*body.Enabled, body.Minutes)
			if err != nil {
				errc <- err
				return
			}
			done <- res
		}()
		select {
		case res := <-done:
			writeJSON(w, 200, map[string]any{"ok": true, "status": res})
		case err := <-errc:
			writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
		case <-ctx.Done():
			writeJSON(w, 200, map[string]any{"ok": false, "error": "操作超时"})
		}
	})
}
