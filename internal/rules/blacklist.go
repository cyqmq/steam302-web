package rules

import (
	"encoding/json"
	"os"
	"strings"
)

// LoadBlacklist reads config/blacklist.json of the shape {"domains": [...]}.
// A missing file is not an error and yields an empty list. The domains are used
// to keep certain hosts OUT of the /etc/hosts hijack so they resolve directly.
func LoadBlacklist(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var body struct {
		Domains []string `json:"domains"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}
	return body.Domains, nil
}

// matchBlack reports whether a given host (site) should be excluded by a
// blacklist entry. "*.example.com" matches example.com subdomains as well as a
// literal "*.example.com" line; an exact entry matches only itself.
func matchBlack(host, entry string) bool {
	if host == entry {
		return true
	}
	if strings.HasPrefix(entry, "*.") {
		suffix := strings.TrimPrefix(entry, "*")
		return strings.HasSuffix(host, suffix) && !strings.Contains(strings.TrimSuffix(host, suffix), ".")
	}
	return false
}

// FilterBlacklist returns a copy of rs whose site.Host lists no longer contain
// blacklisted names. Rule/site structs are shallow-copied so original slices in
// file loading remain untouched.
func FilterBlacklist(rs []Rule, names []string) []Rule {
	if len(names) == 0 {
		return rs
	}
	out := make([]Rule, 0, len(rs))
	for _, r := range rs {
		sites := make([]Site, 0, len(r.Sites))
		for _, s := range r.Sites {
			hosts := s.Hosts[:0:0]
			for _, h := range s.Hosts {
				excluded := false
				for _, n := range names {
					if matchBlack(h, n) {
						excluded = true
						break
					}
				}
				if !excluded {
					hosts = append(hosts, h)
				}
			}
			if len(hosts) == 0 {
				continue // 黑名单把该 site 全部域名剔除：整体不渲染，避免无 key server block
			}
			s.Hosts = hosts
			sites = append(sites, s)
		}
		if len(sites) == 0 {
			continue
		}
		r.Sites = sites
		out = append(out, r)
	}
	return out
}
