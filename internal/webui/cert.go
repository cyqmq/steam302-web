package webui

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type certInfo struct {
	Name     string `json:"name"`
	Exists   bool   `json:"exists"`
	NotAfter string `json:"not_after"`
	DaysLeft int    `json:"days_left"`
	Error    string `json:"error,omitempty"`
}

type certStatus struct {
	CA      *certInfo `json:"ca"`
	Leaf    *certInfo `json:"leaf"`
	Trusted bool      `json:"trusted"`
	Store   string    `json:"trust_store"`
}

// inspectCert 读取 PEM 证书并汇总有效期信息。
func inspectCert(path string) *certInfo {
	info := &certInfo{Name: filepath.Base(path)}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			info.Error = err.Error()
		}
		return info
	}
	block, _ := pem.Decode(data)
	if block == nil {
		info.Exists = true
		info.Error = "不是合法 PEM"
		return info
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		info.Exists = true
		info.Error = "解析失败: " + err.Error()
		return info
	}
	info.Exists = true
	info.NotAfter = cert.NotAfter.Format("2006/01/02 15:04:05")
	info.DaysLeft = int(time.Until(cert.NotAfter).Hours() / 24)
	return info
}

// certStatusView 汇总根/叶证书与系统信任状态。
func (s *Server) certStatusView() certStatus {
	base := filepath.Join(s.Root, "config", "certs")
	st := certStatus{
		CA:    inspectCert(filepath.Join(base, "ca.pem")),
		Leaf:  inspectCert(filepath.Join(base, "leaf.pem")),
		Store: "/usr/local/share/ca-certificates",
	}
	trusted := s.caTrustStatus()
	st.Trusted = trusted == "已信任"
	return st
}

func (s *Server) caTrustStatus() string {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, filepath.Join(s.Root, "bin", "catrust"), "status", "--root", s.Root).CombinedOutput()
	if err != nil {
		return "未知"
	}
	switch t := strings.TrimSpace(string(out)); t {
	case "已信任":
		return "已信任"
	case "未信任":
		return "未信任"
	}
	return "未知"
}

// registerCert 注册证书区相关接口。
func (s *Server) registerCert(mux *http.ServeMux) {
	// 导出根证书（ca.pem）为可下载 PEM。
	mux.HandleFunc("GET /api/cert/ca", func(w http.ResponseWriter, r *http.Request) {
		caPath := filepath.Join(s.Root, "config", "certs", "ca.pem")
		data, err := os.ReadFile(caPath)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "ca.pem 不存在: " + err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Header().Set("Content-Disposition", `attachment; filename="steam302-web-ca.pem"`)
		_, _ = w.Write(data)
	})

	// 证书状态：CA/叶 有效期 + 系统信任。
	mux.HandleFunc("GET /api/cert/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.certStatusView())
	})

	// 系统信任安装/移除。动作 install|remove，经 sudo 调 bin/catrust。
	mux.HandleFunc("POST /api/cert/trust", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Action string `json:"action"`
		}
		if err := jsonDecode(r, &body); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		switch body.Action {
		case "install", "remove", "status":
		default:
			writeJSON(w, 400, map[string]string{"error": "action 仅支持 install|remove|status"})
			return
		}
		argv := []string{"-n", filepath.Join(s.Root, "bin", "catrust"), body.Action, "--root", s.Root}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "sudo", argv...).CombinedOutput()
		writeJSON(w, 200, map[string]any{
			"ok":     err == nil,
			"action": body.Action,
			"output": string(out),
			"error":  errString(err),
			"status": s.certStatusView(),
		})
	})
}

func jsonDecode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
