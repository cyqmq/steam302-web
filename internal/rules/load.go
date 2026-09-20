package rules

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func FindRoot(start string) string {
	p := start
	for {
		if fi, err := os.Stat(filepath.Join(p, "config", "rules")); err == nil && fi.IsDir() {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return ""
		}
		p = parent
	}
}

func LoadEnv(path string) (*Env, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var env Env
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	return &env, nil
}

func LoadRules(dir string, includeDisabled bool) ([]Rule, error) {
	return LoadRulesWithOverrides(dir, includeDisabled, nil)
}

// LoadRulesWithOverrides loads rules and applies on top of each rule file's
// `enabled` value any override from the given map (used by the WebUI to flip
// switches without rewriting the rule files, preserving key order).
func LoadRulesWithOverrides(dir string, includeDisabled bool, overrides map[string]bool) ([]Rule, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var rules []Rule
	for _, n := range names {
		data, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			return nil, err
		}
		var r Rule
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		if r.ID == "" {
			r.ID = strings.TrimSuffix(n, ".json")
		}
		if v, ok := overrides[r.ID]; ok {
			r.Enabled = v
		}
		if !r.Enabled && !includeDisabled {
			continue
		}
		rules = append(rules, r)
	}
	return rules, nil
}

// LoadOverrides reads the WebUI switch overrides JSON (map[string]bool). Its
// absence is not an error.
func LoadOverrides(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := map[string]bool{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func CheckNeedsFiles(root string, rule Rule) []string {
	var missing []string
	for _, p := range rule.NeedsFiles {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			missing = append(missing, p)
		}
	}
	return missing
}
