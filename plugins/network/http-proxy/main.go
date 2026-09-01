package main

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/hashicorp/go-plugin"
	"gopkg.in/yaml.v3"

	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: netplugin.Handshake,
		Plugins:         netplugin.TerminatePluginMap(&HTTPProxy{}),
	})
}

// HTTPProxy implements network terminate for HTTP(S) reverse proxies.
type HTTPProxy struct {
	mu        sync.RWMutex
	endpoints map[string]endpoint
}

type endpoint struct {
	URL     string
	Headers map[string]string
	Listen  int
}

type endpointYAML struct {
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers"`
	Listen  int               `yaml:"listen"`
}

func (h *HTTPProxy) Name() string { return "http-proxy" }

func (h *HTTPProxy) Configure(raw []byte) error {
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return fmt.Errorf("http-proxy configure: %w", err)
	}
	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		h.mu.Lock()
		h.endpoints = map[string]endpoint{}
		h.mu.Unlock()
		return nil
	}
	m := root.Content[0]
	if m.Kind != yaml.MappingNode {
		return fmt.Errorf("http-proxy configure: expected map")
	}
	eps := map[string]endpoint{}
	for i := 0; i < len(m.Content); i += 2 {
		key := m.Content[i].Value
		val := m.Content[i+1]
		if key == "priority" || key == "package" {
			continue
		}
		var ey endpointYAML
		if err := val.Decode(&ey); err != nil {
			return fmt.Errorf("http-proxy %s: %w", key, err)
		}
		if strings.TrimSpace(ey.URL) == "" {
			return fmt.Errorf("http-proxy %s: url is required", key)
		}
		hdrs := map[string]string{}
		for k, v := range ey.Headers {
			resolved, err := resolveTemplate(v)
			if err != nil {
				return fmt.Errorf("http-proxy %s headers.%s: %w", key, k, err)
			}
			hdrs[k] = resolved
		}
		eps[key] = endpoint{URL: strings.TrimRight(ey.URL, "/"), Headers: hdrs, Listen: ey.Listen}
	}
	h.mu.Lock()
	h.endpoints = eps
	h.mu.Unlock()
	return nil
}

func (h *HTTPProxy) Prepare(in netplugin.PrepareIn) (netplugin.PrepareOut, error) {
	h.mu.RLock()
	ep, ok := h.endpoints[in.Endpoint]
	h.mu.RUnlock()
	if !ok {
		return netplugin.PrepareOut{}, fmt.Errorf("unknown endpoint %q", in.Endpoint)
	}
	base, err := url.Parse(ep.URL)
	if err != nil {
		return netplugin.PrepareOut{}, fmt.Errorf("endpoint url: %w", err)
	}
	guestPath := in.Path
	if guestPath == "" {
		guestPath = "/"
	}
	query := ""
	if i := strings.IndexByte(guestPath, '?'); i >= 0 {
		query = guestPath[i+1:]
		guestPath = guestPath[:i]
	}
	if !strings.HasPrefix(guestPath, "/") {
		guestPath = "/" + guestPath
	}
	up := *base
	up.Path = strings.TrimSuffix(base.Path, "/") + guestPath
	if query != "" {
		up.RawQuery = query
	}
	port := 443
	if base.Scheme == "http" {
		port = 80
	}
	if p := base.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil {
			return netplugin.PrepareOut{}, fmt.Errorf("endpoint port: %w", err)
		}
		port = n
	}
	hdr := http.Header{}
	for k, vals := range in.Header {
		lk := strings.ToLower(k)
		if lk == "host" || lk == "content-length" {
			continue
		}
		for _, v := range vals {
			hdr.Add(k, v)
		}
	}
	for k, v := range ep.Headers {
		hdr.Set(k, v) // case-insensitive replace of guest values
	}
	return netplugin.PrepareOut{
		UpstreamHost: base.Hostname(),
		UpstreamPort: port,
		UpstreamURL:  up.String(),
		Header:       hdr,
	}, nil
}

var (
	envRe     = regexp.MustCompile(`\{\{\s*env\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)
	secretsRe = regexp.MustCompile(`\{\{\s*secrets\.`)
)

func resolveTemplate(s string) (string, error) {
	if secretsRe.MatchString(s) {
		return "", fmt.Errorf("secrets plugins not implemented; use {{ env.VAR }} or a literal")
	}
	var err error
	out := envRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := envRe.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		v, ok := os.LookupEnv(sub[1])
		if !ok {
			err = fmt.Errorf("env %s is not set", sub[1])
			return m
		}
		return v
	})
	if err != nil {
		return "", err
	}
	return out, nil
}
