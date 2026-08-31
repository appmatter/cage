package hooks

import (
	"fmt"
	"path"
	"strings"

	"github.com/appmatter/cage/internal/config"
	"github.com/appmatter/cage/internal/pluginhost"
)

// WarnMissingEgress compares harness plugin egress_hints to network.plugins.egress allow.
// Soft only — never fails; logf receives warning lines (no "cage:" prefix).
func WarnMissingEgress(projectRoot string, r config.Resolved, logf func(string, ...any)) {
	if logf == nil {
		return
	}
	if err := Resolve(projectRoot, &r.Runtime); err != nil {
		return
	}
	var allowHosts []string
	if r.Network.Plugins.Egress != nil {
		for _, rule := range r.Network.Plugins.Egress.Allow {
			if rule.Host != "" {
				allowHosts = append(allowHosts, rule.Host)
			}
		}
	}
	seen := map[string]bool{}
	for _, hs := range r.Runtime.HarnessSeats {
		m, err := manifestFor(projectRoot, hs.PluginID)
		if err != nil {
			continue
		}
		for _, hint := range m.EgressHints {
			host := strings.TrimSpace(hint.Host)
			if host == "" {
				continue
			}
			key := hs.PluginID + "\x00" + strings.ToLower(host)
			if seen[key] {
				continue
			}
			seen[key] = true
			if hostCovered(allowHosts, host) {
				continue
			}
			msg := fmt.Sprintf("warn egress: plugin %s wants allow host %q", hs.PluginID, host)
			if hint.Reason != "" {
				msg += " (" + hint.Reason + ")"
			}
			msg += " — add under network.plugins.egress.allow"
			logf("%s", msg)
		}
	}
}

func hostCovered(allowPatterns []string, need string) bool {
	need = strings.ToLower(strings.TrimSpace(need))
	if need == "" {
		return true
	}
	for _, p := range allowPatterns {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "*" || p == need {
			return true
		}
		// concrete hint covered by allow glob / exact
		if !strings.Contains(need, "*") && hostMatch(p, need) {
			return true
		}
		// glob hint: need identical allow glob
		if strings.HasPrefix(need, "*.") && strings.HasPrefix(p, "*.") && p[2:] == need[2:] {
			return true
		}
	}
	return false
}

func hostMatch(pattern, host string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	host = strings.ToLower(strings.TrimSpace(host))
	if pattern == "" || host == "" {
		return false
	}
	if pattern == "*" {
		return true
	}
	if ok, err := path.Match(pattern, host); err == nil && ok {
		return true
	}
	if strings.HasPrefix(pattern, "*.") && host == pattern[2:] {
		return true
	}
	return false
}

// CollectEgressHints returns all hints from seated harness plugins (for inspect).
func CollectEgressHints(projectRoot string, r config.Runtime) []pluginhost.EgressHint {
	_ = Resolve(projectRoot, &r)
	var out []pluginhost.EgressHint
	seen := map[string]bool{}
	for _, hs := range r.HarnessSeats {
		m, err := manifestFor(projectRoot, hs.PluginID)
		if err != nil {
			continue
		}
		for _, h := range m.EgressHints {
			k := strings.ToLower(h.Host)
			if h.Host == "" || seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, h)
		}
	}
	return out
}
