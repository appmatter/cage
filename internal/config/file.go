package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// File is a full cage config document (keys follow contexts — see docs/project-structure.md).
type File struct {
	Version int     `yaml:"version"`
	Extends string  `yaml:"extends"`
	Runtime Runtime `yaml:"runtime"`
	FS      FS      `yaml:"fs"`
	Secrets Secrets `yaml:"secrets"`
	Network Network `yaml:"network"`
}

// Secrets is the secrets context: installable store seats under plugins.
type Secrets struct {
	Plugins map[string]SecretStore `yaml:"plugins"` // seat → store
}

// Runtime is the runtime context: plugins (backend seats), workdir, guest env, hooks.
type Runtime struct {
	Plugins map[string]RuntimePlugin `yaml:"plugins"` // seat → backend plugin config
	Workdir string                   `yaml:"workdir"`
	Env     map[string]string        `yaml:"env"`
	Hooks   map[string][]HookAction  `yaml:"hooks"`

	// Set by LoadResolved for this host (not from yaml).
	Backend   string   `yaml:"-"` // selected plugin id
	Seat      string   `yaml:"-"` // selected seat name under plugins
	Image     string   `yaml:"-"` // base image from selected seat
	Graphics  bool     `yaml:"-"` // from selected seat; tart: show UI when true
	OnCreate  []string `yaml:"-"` // abs host script paths from selected seat
	OnStart   []string `yaml:"-"`
	OnDestroy []string `yaml:"-"`
	Bake      []string `yaml:"-"` // abs host bake scripts (derived image); empty = no bake
	GOOS      string   `yaml:"-"`
	// HarnessSeats are image-less runtime.plugins seats (sorted by seat name).
	HarnessSeats []HarnessSeat `yaml:"-"`
	// ResolvedHooks is event → plugin ids that run (YAML hooks + plugin-declared defaults).
	ResolvedHooks map[string][]string `yaml:"-"`
}

// HarnessSeat is a resolved image-less runtime plugin seat.
type HarnessSeat struct {
	Seat      string
	PluginID  string
	Version   string
	Packages  []string
	NodeMajor int
	AgentDir  string // host dir for agent config (models.json, …); empty = .cage/plugins/runtime/<plugin>
	SeatYAML  []byte // marshaled seat config for hook plugins
}

// RuntimePlugin is one seat under runtime.plugins (backend has image; harness seats omit image).
type RuntimePlugin struct {
	Priority  *int     `yaml:"priority,omitempty"`
	Plugin    string   `yaml:"plugin,omitempty"`  // short install name; omit = seat name
	Package   string   `yaml:"package,omitempty"` // optional source override (git:… or path)
	Image     string   `yaml:"image"`
	Graphics  *bool    `yaml:"graphics,omitempty"`  // tart: open UI; omit/false = --no-graphics
	GOOS      []string `yaml:"goos,omitempty"`      // host GOOS list; omit = plugin default / any
	OnCreate  []string `yaml:"on-create,omitempty"` // host paths; plugin runs (bash/pwsh/…)
	OnStart   []string `yaml:"on-start,omitempty"`
	OnDestroy []string `yaml:"on-destroy,omitempty"`
	Bake      []string `yaml:"bake,omitempty"` // host scripts → derived image (hashed, cached)
	// Harness / plugin-owned fields (opaque to backend selection).
	Version   string   `yaml:"version,omitempty"`
	Packages  []string `yaml:"packages,omitempty"`
	NodeMajor int      `yaml:"node_major,omitempty"`
	AgentDir  string   `yaml:"agent_dir,omitempty"` // host dir synced to guest ~/.pi/agent; omit = .cage/plugins/runtime/<plugin>
}

// PluginID returns the installable short name for this seat.
func (p RuntimePlugin) PluginID(seat string) string {
	if p.Plugin != "" {
		return p.Plugin
	}
	return seat
}

// Layout controls guest path placement (fs.layout).
type Layout struct {
	Mode string `yaml:"mode"` // flat | host
}

// FS is the fs context: core layout/mount/copy/deny + plugins.
type FS struct {
	Layout  Layout                  `yaml:"layout"`
	Mount   PathMap                 `yaml:"mount"`
	Copy    PathMap                 `yaml:"copy"`
	Deny    []DenyEntry             `yaml:"deny"`
	Plugins FSPlugins               `yaml:"plugins"`
	Hooks   map[string][]HookAction `yaml:"hooks"`
}

// DenyEntry is one fs.deny path. Scalar path, or map with path + optional active.
type DenyEntry struct {
	Path   string
	Active bool // false = drop matching paths on merge (default true)
}

// UnmarshalYAML accepts a string path or {path, active}.
func (d *DenyEntry) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		*d = DenyEntry{Path: value.Value, Active: true}
		return nil
	}
	var raw struct {
		Path   string `yaml:"path"`
		Active *bool  `yaml:"active"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	if strings.TrimSpace(raw.Path) == "" {
		return fmt.Errorf("fs.deny entry: path is required")
	}
	active := true
	if raw.Active != nil {
		active = *raw.Active
	}
	*d = DenyEntry{Path: raw.Path, Active: active}
	return nil
}

// FSPlugins are installable seats under fs.plugins.
type FSPlugins struct {
	Mention        *Mention        `yaml:"mention,omitempty"`
	SecretsScanner *SecretsScanner `yaml:"secrets_scanner,omitempty"`
}

// PathMap is guest-target → host path spec.
type PathMap map[string]PathSpec

// PathSpec is a mount or copy entry.
type PathSpec struct {
	Host       string
	Permission string
	Remove     bool
}

// UnmarshalYAML accepts a string host or a map.
func (p *PathSpec) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		p.Host = value.Value
		return nil
	}
	var raw struct {
		Host       string `yaml:"host"`
		Permission string `yaml:"permission"`
		Remove     bool   `yaml:"remove"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*p = PathSpec{Host: raw.Host, Permission: raw.Permission, Remove: raw.Remove}
	return nil
}

// Mention is the fs.plugins.mention plugin — host-side @mention / search globs (not agent FS).
type Mention struct {
	Package string   `yaml:"package,omitempty"`
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

// SecretStore is one secrets.plugins.<seat> entry.
// No priority — secrets resolve by dependency DAG (uses + template refs).
type SecretStore struct {
	Plugin  string            `yaml:"plugin,omitempty"`  // short install name; omit = seat name
	Package string            `yaml:"package,omitempty"` // optional source override
	Uses    []string          `yaml:"uses,omitempty"`
	Account string            `yaml:"account,omitempty"` // onepassword: op --account
	Region  string            `yaml:"region,omitempty"`  // aws_sm
	Vars    map[string]string `yaml:"vars"`
}

// PluginID returns the installable short name for this seat.
func (s SecretStore) PluginID(seat string) string {
	if s.Plugin != "" {
		return s.Plugin
	}
	return seat
}

// Network is the network context: optional proxy settings, installable plugins, hooks.
type Network struct {
	Proxy   NetworkProxy            `yaml:"proxy,omitempty"`
	Plugins NetworkPlugins          `yaml:"plugins"`
	Hooks   map[string][]HookAction `yaml:"hooks"`
}

// NetworkProxy is host SOCKS5 / HTTP CONNECT / softnet lock settings under network.proxy.
type NetworkProxy struct {
	Disabled *bool `yaml:"disabled,omitempty"` // omit/false = proxy ON
	Logging  *bool `yaml:"logging,omitempty"`  // CONNECT allow/deny on stderr
	MITM     *bool `yaml:"mitm,omitempty"`     // omit/true = HTTPS MITM when proxy on; false = tunnel only
}

// ProxyEnabled is true unless network.proxy.disabled is explicitly true.
func (n Network) ProxyEnabled() bool {
	if n.Proxy.Disabled == nil {
		return true
	}
	return !*n.Proxy.Disabled
}

// LoggingEnabled is true when network.proxy.logging is explicitly true.
func (n Network) LoggingEnabled() bool {
	return n.Proxy.Logging != nil && *n.Proxy.Logging
}

// MITMEnabled is true when proxy is on and mitm is not explicitly false.
func (n Network) MITMEnabled() bool {
	if !n.ProxyEnabled() {
		return false
	}
	if n.Proxy.MITM == nil {
		return true
	}
	return *n.Proxy.MITM
}

// NetworkPlugins are installable seats under network.plugins.
type NetworkPlugins struct {
	Egress        *Egress          `yaml:"egress,omitempty"`
	HTTPProxy     *ProtocolProxies `yaml:"http-proxy,omitempty"`
	PostgresProxy *ProtocolProxies `yaml:"postgres-proxy,omitempty"`
}

// ProtocolProxies is one terminate-stage protocol plugin (http-proxy, postgres-proxy, …).
// priority and package are reserved; remaining keys are named endpoints.
type ProtocolProxies struct {
	Priority  *int
	Package   string
	Endpoints map[string]Proxy
}

// UnmarshalYAML accepts priority/package plus named proxy endpoints.
func (p *ProtocolProxies) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("protocol proxy plugin: expected map")
	}
	p.Endpoints = map[string]Proxy{}
	p.Priority = nil
	p.Package = ""
	for i := 0; i < len(value.Content); i += 2 {
		key := value.Content[i].Value
		val := value.Content[i+1]
		switch key {
		case "priority":
			var n int
			if err := val.Decode(&n); err != nil {
				return fmt.Errorf("priority: %w", err)
			}
			p.Priority = &n
		case "package":
			if err := val.Decode(&p.Package); err != nil {
				return fmt.Errorf("package: %w", err)
			}
		default:
			var proxy Proxy
			if err := val.Decode(&proxy); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			p.Endpoints[key] = proxy
		}
	}
	return nil
}

// MarshalYAML writes priority/package (if set) then endpoints.
func (p ProtocolProxies) MarshalYAML() (any, error) {
	var keys []string
	for k := range p.Endpoints {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	node := &yaml.Node{Kind: yaml.MappingNode}
	if p.Priority != nil {
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "priority"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprintf("%d", *p.Priority), Tag: "!!int"},
		)
	}
	if p.Package != "" {
		node.Content = append(node.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "package"},
			&yaml.Node{Kind: yaml.ScalarNode, Value: p.Package},
		)
	}
	for _, k := range keys {
		var vn yaml.Node
		if err := vn.Encode(p.Endpoints[k]); err != nil {
			return nil, err
		}
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &vn)
	}
	return node, nil
}

// Egress is network.plugins.egress (filter stage).
type Egress struct {
	Priority     *int          `yaml:"priority,omitempty"`
	Package      string        `yaml:"package,omitempty"`
	DenyResponse *DenyResponse `yaml:"deny_response,omitempty"`
	Allow        []EgressRule  `yaml:"allow"`
	Deny         []EgressRule  `yaml:"deny,omitempty"`
}

// DenyResponse controls optional HTTP bodies when egress denies after a peeked request.
type DenyResponse struct {
	HTTP    bool   `yaml:"http"`              // default false — inject 403 for plain HTTP DENY
	Message string `yaml:"message,omitempty"` // omit → built-in default
}

// DenyHTTPResponse is true when egress.deny_response.http is set.
func (e *Egress) DenyHTTPResponse() bool {
	return e != nil && e.DenyResponse != nil && e.DenyResponse.HTTP
}

// DenyHTTPMessage returns the configured deny body, or empty for the built-in default.
func (e *Egress) DenyHTTPMessage() string {
	if e == nil || e.DenyResponse == nil {
		return ""
	}
	return strings.TrimSpace(e.DenyResponse.Message)
}

// EgressRule is one allow or deny destination rule.
type EgressRule struct {
	Host   string `yaml:"host"`
	Port   int    `yaml:"port,omitempty"` // omit or 0 = any
	Method string `yaml:"method,omitempty"`
	Path   string `yaml:"path,omitempty"`
}

// Proxy is one named endpoint under network.plugins.<protocol-proxy>.
type Proxy struct {
	URL      string            `yaml:"url,omitempty"`
	Headers  map[string]string `yaml:"headers,omitempty"`
	Listen   int               `yaml:"listen,omitempty"`
	Host     string            `yaml:"host,omitempty"`
	Port     int               `yaml:"port,omitempty"`
	Database string            `yaml:"database,omitempty"`
	Username string            `yaml:"username,omitempty"`
	Password string            `yaml:"password,omitempty"`
	SSL      string            `yaml:"ssl,omitempty"`
	AWS      map[string]string `yaml:"aws,omitempty"`
}

// SecretsScanner is the fs.plugins.secrets_scanner plugin config.
type SecretsScanner struct {
	Package string                `yaml:"package,omitempty"`
	OnFind  string                `yaml:"on_find,omitempty"` // warn | fail
	Allow   []SecretsScannerAllow `yaml:"allow,omitempty"`
}

// SecretsScannerAllow is a string name/pattern or {path|pattern|name}.
type SecretsScannerAllow struct {
	Name    string
	Path    string
	Pattern string
}

// UnmarshalYAML accepts "OPENAI_API_KEY" or {path|pattern|name}.
func (a *SecretsScannerAllow) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		a.Name = value.Value
		return nil
	}
	var raw struct {
		Name    string `yaml:"name"`
		Path    string `yaml:"path"`
		Pattern string `yaml:"pattern"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	*a = SecretsScannerAllow{Name: raw.Name, Path: raw.Path, Pattern: raw.Pattern}
	return nil
}

// HookAction names a plugin (or built-in) to run at a context hook point.
type HookAction struct {
	Plugin string `yaml:"plugin"`
	OnFind string `yaml:"on_find,omitempty"` // optional override for secrets_scanner
}

// UnmarshalYAML accepts "secrets_scanner" or {plugin, on_find}.
func (h *HookAction) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		h.Plugin = value.Value
		return nil
	}
	var raw struct {
		Plugin string `yaml:"plugin"`
		Action string `yaml:"action"` // alias
		OnFind string `yaml:"on_find"`
	}
	if err := value.Decode(&raw); err != nil {
		return err
	}
	name := raw.Plugin
	if name == "" {
		name = raw.Action
	}
	*h = HookAction{Plugin: name, OnFind: raw.OnFind}
	return nil
}

// Resolved is the post-merge view used by inspect / start.
type Resolved struct {
	Path      string
	Runtime   Runtime
	Network   Network
	Secrets   Secrets
	Layout    Layout
	Mounts    []ResolvedPath
	Copies    []ResolvedPath
	Deny      []string
	DenyMasks []string // guest paths to obscure under allowed mounts (exist on host at resolve)
}

// ResolvedPath is one mount or copy after merge.
type ResolvedPath struct {
	Target     string
	Host       string
	Guest      string
	Permission string
}

// LoadFile reads one yaml without merge.
func LoadFile(path string) (File, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	var f File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return File{}, err
	}
	return f, nil
}

// LoadResolved loads path, walks extends (multi-level), returns resolved FS for goos.
func LoadResolved(projectRoot, path, goos string) (Resolved, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return Resolved{}, err
	}
	root, err := filepath.Abs(projectRoot)
	if err != nil {
		return Resolved{}, err
	}
	merged, err := loadChain(abs, nil)
	if err != nil {
		return Resolved{}, err
	}

	if merged.Runtime.Workdir == "" {
		merged.Runtime.Workdir = "/workspace"
	}
	if merged.FS.Layout.Mode == "" {
		merged.FS.Layout.Mode = "flat"
	}
	merged.Runtime, err = selectRuntime(merged.Runtime, goos)
	if err != nil {
		return Resolved{}, fmt.Errorf("%s: %w", abs, err)
	}
	if err := resolveLifecycleScripts(root, &merged.Runtime); err != nil {
		return Resolved{}, fmt.Errorf("%s: %w", abs, err)
	}

	r := Resolved{
		Path:    abs,
		Runtime: merged.Runtime,
		Network: merged.Network,
		Secrets: Secrets{Plugins: mergeSecretPlugins(merged.Secrets.Plugins, nil)},
		Layout:  merged.FS.Layout,
		Deny:    uniqueStrings(denyPaths(merged.FS.Deny)),
	}
	r.Mounts, err = resolveMap(root, merged.Runtime.Workdir, merged.FS.Layout.Mode, merged.FS.Mount)
	if err != nil {
		return Resolved{}, err
	}
	r.Copies, err = resolveMap(root, merged.Runtime.Workdir, merged.FS.Layout.Mode, merged.FS.Copy)
	if err != nil {
		return Resolved{}, err
	}
	if err := checkDeny(r); err != nil {
		return Resolved{}, err
	}
	r.DenyMasks = computeDenyMasks(r)
	if err := ValidateFile(merged); err != nil {
		return Resolved{}, err
	}
	return r, nil
}

// ChainPaths returns abs paths in the extends chain (leaf first, then parents).
func ChainPaths(path string) ([]string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	for {
		if seen[abs] {
			return nil, fmt.Errorf("circular extends: %s", abs)
		}
		seen[abs] = true
		out = append(out, abs)
		f, err := LoadFile(abs)
		if err != nil {
			return nil, err
		}
		parent, ok := parentPath(abs, f)
		if !ok {
			return out, nil
		}
		abs, err = filepath.Abs(parent)
		if err != nil {
			return nil, err
		}
	}
}

// loadChain loads path and merges parents first (deepest base → leaf).
func loadChain(path string, stack []string) (File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return File{}, err
	}
	for _, seen := range stack {
		if seen == abs {
			return File{}, fmt.Errorf("circular extends: %s", strings.Join(append(stack, abs), " -> "))
		}
	}
	f, err := LoadFile(abs)
	if err != nil {
		return File{}, err
	}
	parent, ok := parentPath(abs, f)
	if !ok {
		return f, nil
	}
	base, err := loadChain(parent, append(stack, abs))
	if err != nil {
		if strings.HasPrefix(err.Error(), "circular extends:") {
			return File{}, err
		}
		return File{}, fmt.Errorf("extends %s: %w", parent, err)
	}
	return mergeFiles(base, f), nil
}

// parentPath returns the file this config extends, if any.
// Explicit extends wins. Otherwise non-cage.yaml defaults to cage.yaml; cage.yaml/yml is the root.
func parentPath(abs string, f File) (string, bool) {
	if f.Extends != "" {
		return filepath.Join(filepath.Dir(abs), f.Extends), true
	}
	base := filepath.Base(abs)
	if base == "cage.yaml" || base == "cage.yml" {
		return "", false
	}
	return filepath.Join(filepath.Dir(abs), "cage.yaml"), true
}

func mergeFiles(base, over File) File {
	out := base
	if over.Version != 0 {
		out.Version = over.Version
	}
	out.Runtime.Plugins = mergeRuntimePlugins(base.Runtime.Plugins, over.Runtime.Plugins)
	if over.Runtime.Workdir != "" {
		out.Runtime.Workdir = over.Runtime.Workdir
	}
	if len(over.Runtime.Env) > 0 {
		env := map[string]string{}
		for k, v := range out.Runtime.Env {
			env[k] = v
		}
		for k, v := range over.Runtime.Env {
			env[k] = v
		}
		out.Runtime.Env = env
	}
	if over.FS.Layout.Mode != "" {
		out.FS.Layout.Mode = over.FS.Layout.Mode
	}
	out.FS.Mount = mergePathMap(base.FS.Mount, over.FS.Mount)
	out.FS.Copy = mergePathMap(base.FS.Copy, over.FS.Copy)
	out.FS.Deny = mergeDeny(base.FS.Deny, over.FS.Deny)
	if over.FS.Plugins.Mention != nil {
		m := Mention{}
		if out.FS.Plugins.Mention != nil {
			m = *out.FS.Plugins.Mention
		}
		if over.FS.Plugins.Mention.Package != "" {
			m.Package = over.FS.Plugins.Mention.Package
		}
		if over.FS.Plugins.Mention.Include != nil {
			m.Include = append([]string{}, over.FS.Plugins.Mention.Include...)
		}
		if over.FS.Plugins.Mention.Exclude != nil {
			m.Exclude = append([]string{}, over.FS.Plugins.Mention.Exclude...)
		}
		out.FS.Plugins.Mention = &m
	}
	if over.FS.Plugins.SecretsScanner != nil {
		s := SecretsScanner{}
		if out.FS.Plugins.SecretsScanner != nil {
			s = *out.FS.Plugins.SecretsScanner
		}
		if over.FS.Plugins.SecretsScanner.Package != "" {
			s.Package = over.FS.Plugins.SecretsScanner.Package
		}
		if over.FS.Plugins.SecretsScanner.OnFind != "" {
			s.OnFind = over.FS.Plugins.SecretsScanner.OnFind
		}
		if over.FS.Plugins.SecretsScanner.Allow != nil {
			s.Allow = append([]SecretsScannerAllow{}, over.FS.Plugins.SecretsScanner.Allow...)
		}
		out.FS.Plugins.SecretsScanner = &s
	}
	out.FS.Hooks = mergeHookEvents(base.FS.Hooks, over.FS.Hooks)
	out.Runtime.Hooks = mergeHookEvents(base.Runtime.Hooks, over.Runtime.Hooks)
	out.Secrets.Plugins = mergeSecretPlugins(base.Secrets.Plugins, over.Secrets.Plugins)
	if over.Network.Proxy.Disabled != nil {
		v := *over.Network.Proxy.Disabled
		out.Network.Proxy.Disabled = &v
	}
	if over.Network.Proxy.Logging != nil {
		v := *over.Network.Proxy.Logging
		out.Network.Proxy.Logging = &v
	}
	if over.Network.Proxy.MITM != nil {
		v := *over.Network.Proxy.MITM
		out.Network.Proxy.MITM = &v
	}
	if over.Network.Plugins.Egress != nil {
		e := Egress{}
		if out.Network.Plugins.Egress != nil {
			e = *out.Network.Plugins.Egress
		}
		if over.Network.Plugins.Egress.Priority != nil {
			p := *over.Network.Plugins.Egress.Priority
			e.Priority = &p
		}
		if over.Network.Plugins.Egress.Package != "" {
			e.Package = over.Network.Plugins.Egress.Package
		}
		if over.Network.Plugins.Egress.DenyResponse != nil {
			d := *over.Network.Plugins.Egress.DenyResponse
			if e.DenyResponse != nil && over.Network.Plugins.Egress.DenyResponse.Message == "" {
				d.Message = e.DenyResponse.Message
			}
			e.DenyResponse = &d
		}
		if over.Network.Plugins.Egress.Allow != nil {
			e.Allow = append([]EgressRule{}, over.Network.Plugins.Egress.Allow...)
		}
		if over.Network.Plugins.Egress.Deny != nil {
			e.Deny = append([]EgressRule{}, over.Network.Plugins.Egress.Deny...)
		}
		out.Network.Plugins.Egress = &e
	}
	out.Network.Plugins.HTTPProxy = mergeProtocolProxies(base.Network.Plugins.HTTPProxy, over.Network.Plugins.HTTPProxy)
	out.Network.Plugins.PostgresProxy = mergeProtocolProxies(base.Network.Plugins.PostgresProxy, over.Network.Plugins.PostgresProxy)
	out.Network.Hooks = mergeHookEvents(base.Network.Hooks, over.Network.Hooks)
	return out
}

func mergeSecretPlugins(base, over map[string]SecretStore) map[string]SecretStore {
	if len(base) == 0 && len(over) == 0 {
		return nil
	}
	out := map[string]SecretStore{}
	for seat, store := range base {
		out[seat] = cloneSecretStore(store)
	}
	for seat, store := range over {
		cur := out[seat]
		if store.Plugin != "" {
			cur.Plugin = store.Plugin
		}
		if store.Package != "" {
			cur.Package = store.Package
		}
		if store.Uses != nil {
			cur.Uses = append([]string{}, store.Uses...)
		}
		if store.Account != "" {
			cur.Account = store.Account
		}
		if store.Region != "" {
			cur.Region = store.Region
		}
		if store.Vars != nil {
			if cur.Vars == nil {
				cur.Vars = map[string]string{}
			}
			for k, v := range store.Vars {
				cur.Vars[k] = v
			}
		}
		out[seat] = cur
	}
	return out
}

func cloneSecretStore(s SecretStore) SecretStore {
	out := s
	if s.Uses != nil {
		out.Uses = append([]string{}, s.Uses...)
	}
	if s.Vars != nil {
		out.Vars = map[string]string{}
		for k, v := range s.Vars {
			out.Vars[k] = v
		}
	}
	return out
}

func mergeProtocolProxies(base, over *ProtocolProxies) *ProtocolProxies {
	if base == nil && over == nil {
		return nil
	}
	out := &ProtocolProxies{Endpoints: map[string]Proxy{}}
	if base != nil {
		if base.Priority != nil {
			p := *base.Priority
			out.Priority = &p
		}
		out.Package = base.Package
		for k, v := range base.Endpoints {
			out.Endpoints[k] = v
		}
	}
	if over != nil {
		if over.Priority != nil {
			p := *over.Priority
			out.Priority = &p
		}
		if over.Package != "" {
			out.Package = over.Package
		}
		for k, v := range over.Endpoints {
			out.Endpoints[k] = v
		}
	}
	if len(out.Endpoints) == 0 && out.Priority == nil && out.Package == "" {
		return nil
	}
	return out
}

func mergeHookEvents(base, over map[string][]HookAction) map[string][]HookAction {
	if len(over) == 0 {
		return base
	}
	if len(base) == 0 {
		return over
	}
	out := map[string][]HookAction{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v // replace per event
	}
	return out
}

func mergeRuntimePlugins(base, over map[string]RuntimePlugin) map[string]RuntimePlugin {
	if len(base) == 0 && len(over) == 0 {
		return nil
	}
	out := map[string]RuntimePlugin{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		cur := out[k]
		if v.Priority != nil {
			p := *v.Priority
			cur.Priority = &p
		}
		if v.Plugin != "" {
			cur.Plugin = v.Plugin
		}
		if v.Package != "" {
			cur.Package = v.Package
		}
		if v.Image != "" {
			cur.Image = v.Image
		}
		if v.Graphics != nil {
			g := *v.Graphics
			cur.Graphics = &g
		}
		if v.GOOS != nil {
			cur.GOOS = append([]string{}, v.GOOS...)
		}
		if v.OnCreate != nil {
			cur.OnCreate = append([]string{}, v.OnCreate...)
		}
		if v.OnStart != nil {
			cur.OnStart = append([]string{}, v.OnStart...)
		}
		if v.OnDestroy != nil {
			cur.OnDestroy = append([]string{}, v.OnDestroy...)
		}
		if v.Bake != nil {
			cur.Bake = append([]string{}, v.Bake...)
		}
		if v.Version != "" {
			cur.Version = v.Version
		}
		if v.Packages != nil {
			cur.Packages = append([]string{}, v.Packages...)
		}
		if v.NodeMajor != 0 {
			cur.NodeMajor = v.NodeMajor
		}
		out[k] = cur
	}
	return out
}

func selectRuntime(r Runtime, goos string) (Runtime, error) {
	if len(r.Plugins) == 0 {
		return r, fmt.Errorf("runtime.plugins is required")
	}
	type cand struct {
		seat string
		spec RuntimePlugin
		prio int
	}
	var matched []cand
	for seat, spec := range r.Plugins {
		if spec.Image == "" {
			continue // harness / bake-only seat
		}
		if !runtimeSeatMatchesGOOS(seat, spec, goos) {
			continue
		}
		matched = append(matched, cand{seat: seat, spec: spec, prio: EffectivePriority(spec.Priority)})
	}
	if len(matched) == 0 {
		return r, fmt.Errorf("runtime.plugins: no backend seat with image supports goos %q", goos)
	}
	sort.Slice(matched, func(i, j int) bool {
		if matched[i].prio != matched[j].prio {
			return matched[i].prio < matched[j].prio
		}
		return matched[i].seat < matched[j].seat
	})
	pick := matched[0]
	pluginID := pick.spec.PluginID(pick.seat)
	if pluginID == "" {
		return r, fmt.Errorf("runtime.plugins.%s: plugin is required", pick.seat)
	}
	r.GOOS = goos
	r.Seat = pick.seat
	r.Backend = pluginID
	r.Image = pick.spec.Image
	if pick.spec.Graphics != nil {
		r.Graphics = *pick.spec.Graphics
	}
	r.OnCreate = append([]string{}, pick.spec.OnCreate...)
	r.OnStart = append([]string{}, pick.spec.OnStart...)
	r.OnDestroy = append([]string{}, pick.spec.OnDestroy...)
	r.Bake = collectBakeScripts(r.Plugins, pick.seat, pick.spec)
	r.HarnessSeats = collectHarnessSeats(r.Plugins)
	return r, nil
}

// collectBakeScripts: selected backend bake + other seats without image that list bake (stable seat order).
func collectBakeScripts(plugins map[string]RuntimePlugin, backendSeat string, backend RuntimePlugin) []string {
	var out []string
	out = append(out, backend.Bake...)
	var seats []string
	for seat, spec := range plugins {
		if seat == backendSeat || spec.Image != "" || len(spec.Bake) == 0 {
			continue
		}
		seats = append(seats, seat)
	}
	sort.Strings(seats)
	for _, seat := range seats {
		out = append(out, plugins[seat].Bake...)
	}
	return out
}

func collectHarnessSeats(plugins map[string]RuntimePlugin) []HarnessSeat {
	var names []string
	for seat, spec := range plugins {
		if spec.Image != "" {
			continue
		}
		names = append(names, seat)
	}
	sort.Strings(names)
	out := make([]HarnessSeat, 0, len(names))
	for _, seat := range names {
		spec := plugins[seat]
		raw, _ := yaml.Marshal(map[string]RuntimePlugin{seat: spec})
		// Marshal just the seat value for the plugin.
		seatRaw, err := yaml.Marshal(spec)
		if err != nil {
			seatRaw = raw
		}
		out = append(out, HarnessSeat{
			Seat:      seat,
			PluginID:  spec.PluginID(seat),
			Version:   spec.Version,
			Packages:  append([]string{}, spec.Packages...),
			NodeMajor: spec.NodeMajor,
			AgentDir:  spec.AgentDir,
			SeatYAML:  seatRaw,
		})
	}
	return out
}

// resolveLifecycleScripts turns seat script paths into absolute host paths; missing files fail.
func resolveLifecycleScripts(projectRoot string, r *Runtime) error {
	var err error
	if r.OnCreate, err = absScriptPaths(projectRoot, r.OnCreate, "on-create"); err != nil {
		return err
	}
	if r.OnStart, err = absScriptPaths(projectRoot, r.OnStart, "on-start"); err != nil {
		return err
	}
	if r.OnDestroy, err = absScriptPaths(projectRoot, r.OnDestroy, "on-destroy"); err != nil {
		return err
	}
	if r.Bake, err = absScriptPaths(projectRoot, r.Bake, "bake"); err != nil {
		return err
	}
	return nil
}

func absScriptPaths(projectRoot string, paths []string, label string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			return nil, fmt.Errorf("runtime.plugins: empty %s script path", label)
		}
		abs := p
		if !filepath.IsAbs(p) {
			abs = filepath.Join(projectRoot, p)
		}
		abs, err := filepath.Abs(abs)
		if err != nil {
			return nil, fmt.Errorf("runtime.plugins %s %q: %w", label, p, err)
		}
		if _, err := os.Stat(abs); err != nil {
			return nil, fmt.Errorf("runtime.plugins %s %q: %w", label, p, err)
		}
		out = append(out, abs)
	}
	return out, nil
}

// defaultRuntimePluginGOOS is used when a seat omits goos (until plugin manifests supply this).
var defaultRuntimePluginGOOS = map[string][]string{
	"tart":   {"darwin"},
	"incus":  {"linux"},
	"hyperv": {"windows"},
	// docker omitted → any GOOS
}

func runtimeSeatMatchesGOOS(seat string, spec RuntimePlugin, goos string) bool {
	list := spec.GOOS
	if len(list) == 0 {
		list = defaultRuntimePluginGOOS[spec.PluginID(seat)]
	}
	if len(list) == 0 {
		return true // any
	}
	for _, g := range list {
		if g == goos {
			return true
		}
	}
	return false
}

func mergePathMap(base, over PathMap) PathMap {
	out := PathMap{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		if v.Remove {
			delete(out, k)
			continue
		}
		out[k] = v
	}
	return out
}

// mergeDeny unions path entries; over entries with active: false drop matching paths.
func mergeDeny(base, over []DenyEntry) []DenyEntry {
	out := append([]DenyEntry{}, base...)
	for _, e := range over {
		path := strings.TrimSpace(e.Path)
		if path == "" {
			continue
		}
		if !e.Active {
			kept := out[:0]
			for _, x := range out {
				if x.Path != path {
					kept = append(kept, x)
				}
			}
			out = kept
			continue
		}
		out = append(out, DenyEntry{Path: path, Active: true})
	}
	return out
}

func denyPaths(in []DenyEntry) []string {
	out := make([]string, 0, len(in))
	for _, e := range in {
		if !e.Active || e.Path == "" {
			continue
		}
		out = append(out, e.Path)
	}
	return out
}

func resolveMap(projectRoot, workdir, mode string, m PathMap) ([]ResolvedPath, error) {
	var out []ResolvedPath
	for target, spec := range m {
		if spec.Remove {
			continue
		}
		if spec.Host == "" {
			return nil, fmt.Errorf("fs entry %q: host is required", target)
		}
		perm := spec.Permission
		if perm == "" {
			perm = "rw"
		}
		host := spec.Host
		if !filepath.IsAbs(host) {
			host = filepath.Join(projectRoot, host)
		}
		host = filepath.Clean(host)
		guest := guestPath(workdir, mode, target)
		out = append(out, ResolvedPath{Target: target, Host: host, Guest: guest, Permission: perm})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Target < out[j].Target })
	return out, nil
}

func guestPath(workdir, mode, target string) string {
	target = strings.TrimPrefix(target, "/")
	if mode == "host" {
		return "/" + target
	}
	return filepath.ToSlash(filepath.Join(workdir, target))
}

func checkDeny(r Resolved) error {
	var bad []string
	for _, p := range append(append([]ResolvedPath{}, r.Mounts...), r.Copies...) {
		if matchDeny(p.Host, r.Deny) {
			bad = append(bad, fmt.Sprintf("%s (%s)", p.Target, p.Host))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("fs.deny blocked: %s", strings.Join(bad, ", "))
	}
	return nil
}

// computeDenyMasks lists guest paths under allowed mounts that match fs.deny and exist on the host.
// Explicit mount guest roots are skipped (checkDeny already blocked denied mount hosts).
func computeDenyMasks(r Resolved) []string {
	if len(r.Deny) == 0 || len(r.Mounts) == 0 {
		return nil
	}
	mounted := make(map[string]bool, len(r.Mounts))
	for _, m := range r.Mounts {
		mounted[filepath.Clean(m.Guest)] = true
	}
	var masks []string
	seen := map[string]bool{}
	add := func(guest string) {
		guest = filepath.ToSlash(filepath.Clean(guest))
		if guest == "" || guest == "." || mounted[guest] || seen[guest] {
			return
		}
		for _, m := range masks {
			if guest == m || strings.HasPrefix(guest, m+"/") {
				return
			}
		}
		seen[guest] = true
		masks = append(masks, guest)
	}
	for _, m := range r.Mounts {
		st, err := os.Stat(m.Host)
		if err != nil || !st.IsDir() {
			continue
		}
		hostRoot := m.Host
		for _, d := range r.Deny {
			d = filepath.ToSlash(strings.TrimPrefix(strings.TrimSpace(d), "./"))
			if d == "" {
				continue
			}
			if strings.ContainsAny(d, "*?[") {
				_ = filepath.Walk(hostRoot, func(path string, info os.FileInfo, err error) error {
					if err != nil || path == hostRoot {
						return nil
					}
					rel, err := filepath.Rel(hostRoot, path)
					if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
						return nil
					}
					relSlash := filepath.ToSlash(rel)
					if !matchDeny(path, []string{d}) && !matchDeny(relSlash, []string{d}) {
						return nil
					}
					add(filepath.ToSlash(filepath.Join(m.Guest, relSlash)))
					if info.IsDir() {
						return filepath.SkipDir
					}
					return nil
				})
				continue
			}
			cand := filepath.Join(hostRoot, filepath.FromSlash(d))
			if _, err := os.Stat(cand); err != nil {
				continue
			}
			rel, err := filepath.Rel(hostRoot, cand)
			if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
				continue
			}
			add(filepath.ToSlash(filepath.Join(m.Guest, filepath.ToSlash(rel))))
		}
	}
	sort.Strings(masks)
	return masks
}

func matchDeny(host string, denies []string) bool {
	base := filepath.Base(host)
	hostSlash := filepath.ToSlash(host)
	for _, d := range denies {
		d = filepath.ToSlash(strings.TrimPrefix(strings.TrimSpace(d), "./"))
		if d == "" {
			continue
		}
		if d == base || d == hostSlash || strings.HasSuffix(hostSlash, "/"+d) {
			return true
		}
		if !strings.ContainsAny(d, "*?[") {
			continue
		}
		if ok, _ := filepath.Match(d, base); ok {
			return true
		}
		if ok, _ := filepath.Match(d, hostSlash); ok {
			return true
		}
		if strings.HasPrefix(d, "**/") {
			rest := strings.TrimPrefix(d, "**/")
			if ok, _ := filepath.Match(rest, base); ok {
				return true
			}
			if ok, _ := filepath.Match(rest, hostSlash); ok {
				return true
			}
		}
	}
	return false
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
