package main

import (
	"fmt"
	"path"
	"strings"
	"sync"

	"github.com/hashicorp/go-plugin"
	"gopkg.in/yaml.v3"

	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: netplugin.Handshake,
		Plugins:         netplugin.FilterPluginMap(&Egress{}),
	})
}

// Egress implements network filter stage allow/deny lists.
type Egress struct {
	mu    sync.RWMutex
	allow []rule
	deny  []rule
}

type rule struct {
	Host   string
	Port   int
	Method string
	Path   string
}

type configYAML struct {
	Allow []ruleYAML `yaml:"allow"`
	Deny  []ruleYAML `yaml:"deny"`
}

type ruleYAML struct {
	Host   string `yaml:"host"`
	Port   int    `yaml:"port"`
	Method string `yaml:"method"`
	Path   string `yaml:"path"`
}

func (e *Egress) Name() string { return "egress" }

func (e *Egress) Configure(raw []byte) error {
	var cfg configYAML
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("egress configure: %w", err)
	}
	allow, err := parseRules(cfg.Allow, "allow")
	if err != nil {
		return err
	}
	deny, err := parseRules(cfg.Deny, "deny")
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.allow = allow
	e.deny = deny
	e.mu.Unlock()
	return nil
}

func parseRules(in []ruleYAML, kind string) ([]rule, error) {
	out := make([]rule, 0, len(in))
	for _, a := range in {
		host := strings.TrimSpace(a.Host)
		if host == "" {
			return nil, fmt.Errorf("egress %s entry missing host", kind)
		}
		out = append(out, rule{
			Host:   host,
			Port:   a.Port,
			Method: strings.ToUpper(strings.TrimSpace(a.Method)),
			Path:   a.Path,
		})
	}
	return out, nil
}

func (e *Egress) Check(req netplugin.Request) (netplugin.Decision, error) {
	host := strings.TrimSpace(req.Host)
	if host == "" {
		return netplugin.Decision{Allow: false, Reason: "missing host"}, nil
	}
	e.mu.RLock()
	allow := e.allow
	deny := e.deny
	e.mu.RUnlock()

	for _, r := range deny {
		if ruleMatch(r, req, host, false) {
			return netplugin.Decision{Allow: false, Reason: "matched deny rule"}, nil
		}
	}
	for _, r := range allow {
		if ruleMatch(r, req, host, true) {
			return netplugin.Decision{Allow: true, Reason: "matched allow rule"}, nil
		}
	}
	if len(allow) > 0 {
		return netplugin.Decision{Allow: false, Reason: "no allow rule matched"}, nil
	}
	// deny-only (or empty): non-denied traffic allowed
	return netplugin.Decision{Allow: true, Reason: "no deny rule matched"}, nil
}

// softMethodPath: on Partial CONNECT, treat method/path allow rules as host/port-only.
// Deny rules with method/path never soft-match (wait for peek / terminate).
func ruleMatch(r rule, req netplugin.Request, host string, softMethodPath bool) bool {
	if !hostMatch(r.Host, host) {
		return false
	}
	if r.Port != 0 && req.Port != 0 && r.Port != req.Port {
		return false
	}
	if r.Port != 0 && req.Port == 0 {
		return false
	}
	if req.Partial {
		if softMethodPath {
			return true
		}
		if r.Method != "" || r.Path != "" {
			return false
		}
		return true
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if r.Method != "" {
		if method == "" || r.Method != method {
			return false
		}
	}
	if r.Path != "" {
		if req.Path == "" || !pathMatch(r.Path, req.Path) {
			return false
		}
	}
	return true
}

func hostMatch(pattern, host string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	host = strings.ToLower(strings.TrimSpace(host))
	if pattern == "*" {
		return true
	}
	if ok, err := path.Match(pattern, host); err == nil && ok {
		return true
	}
	// "*.example.com" also matches apex "example.com"
	if strings.HasPrefix(pattern, "*.") && host == pattern[2:] {
		return true
	}
	return false
}

func pathMatch(pattern, p string) bool {
	ok, err := path.Match(pattern, p)
	return err == nil && ok
}
