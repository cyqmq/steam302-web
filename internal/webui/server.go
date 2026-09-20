package webui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	Root string
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

func (s *Server) overridesPath() string { return filepath.Join(s.Root, "config", "overrides.json") }
func (s *Server) rulesDir() string      { return filepath.Join(s.Root, "config", "rules") }
func (s *Server) envPath() string       { return filepath.Join(s.Root, "config", "env.json") }

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

	return mux
}

func (s *Server) Listen(addr string) error {
	if s.Root == "" {
		return errors.New("webui: empty root")
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	fmt.Printf("WebUI: http://%s/\n", addr)
	return srv.ListenAndServe()
}
