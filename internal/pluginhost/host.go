package pluginhost

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"

	fsplugin "github.com/appmatter/cage/pkg/plugin/v1/fs"
	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

// BinaryExt is the installed plugin binary suffix (gitignore: *.cageplugin).
const BinaryExt = ".cageplugin"

// Manifest describes an installed plugin.
type Manifest struct {
	Kind        string       `json:"kind"`
	Name        string       `json:"name"`
	Command     string       `json:"command"`
	Source      string       `json:"source"`
	Pin         string       `json:"pin,omitempty"`
	Stage       string       `json:"stage,omitempty"`    // backend | harness | …
	Hooks       []string     `json:"hooks,omitempty"`    // build-time hook attachments
	Commands    []string     `json:"commands,omitempty"` // CLI verbs (e.g. init)
	EgressHints []EgressHint `json:"egress_hints,omitempty"`
}

// HasCommand reports whether the manifest advertises cmd (e.g. "init").
func (m Manifest) HasCommand(cmd string) bool {
	for _, c := range m.Commands {
		if c == cmd {
			return true
		}
	}
	return false
}

// EgressHint is a soft allowlist suggestion declared in plugin.json.
type EgressHint struct {
	Host   string `json:"host"`
	Reason string `json:"reason,omitempty"`
}

// CacheDir is .cage/.cache under the project (binaries, bake stamps, CA).
func CacheDir(projectRoot string) string {
	if projectRoot == "" {
		projectRoot = "."
	}
	return filepath.Join(projectRoot, ".cage", ".cache")
}

// GlobalDir is ~/.cage/.cache/plugins.
func GlobalDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cage", ".cache", "plugins"), nil
}

// ProjectDir is .cage/.cache/plugins under the project (installed binaries).
func ProjectDir(projectRoot string) string {
	return filepath.Join(CacheDir(projectRoot), "plugins")
}

// ManifestPath returns the manifest path for kind/name under root.
func ManifestPath(root, kind, name string) string {
	return filepath.Join(root, kind, name, "manifest.json")
}

// LoadManifest reads a plugin manifest.
func LoadManifest(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// WriteManifest writes a plugin manifest.
func WriteManifest(root string, m Manifest) error {
	dir := filepath.Join(root, m.Kind, m.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), append(b, '\n'), 0o644)
}

// ResolveCommand finds the plugin binary for kind/name (project then global).
func ResolveCommand(projectRoot, kind, name string) (string, Manifest, error) {
	roots := []string{ProjectDir(projectRoot)}
	if g, err := GlobalDir(); err == nil {
		roots = append(roots, g)
	}
	for _, root := range roots {
		mp := ManifestPath(root, kind, name)
		m, err := LoadManifest(mp)
		if err != nil {
			continue
		}
		cmdPath := m.Command
		if !filepath.IsAbs(cmdPath) {
			cmdPath = filepath.Join(root, kind, name, filepath.Base(cmdPath))
		}
		if _, err := os.Stat(cmdPath); err != nil {
			return "", Manifest{}, fmt.Errorf("plugin binary missing: %s", cmdPath)
		}
		return cmdPath, m, nil
	}
	return "", Manifest{}, fmt.Errorf("plugin %s/%s not installed", kind, name)
}

// ListManifests returns manifests under project and global plugin dirs.
func ListManifests(projectRoot string) ([]Manifest, error) {
	var out []Manifest
	seen := map[string]bool{}
	roots := []string{ProjectDir(projectRoot)}
	if g, err := GlobalDir(); err == nil {
		roots = append(roots, g)
	}
	for _, root := range roots {
		kindDirs, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, kd := range kindDirs {
			if !kd.IsDir() {
				continue
			}
			nameDirs, err := os.ReadDir(filepath.Join(root, kd.Name()))
			if err != nil {
				continue
			}
			for _, nd := range nameDirs {
				if !nd.IsDir() {
					continue
				}
				key := kd.Name() + "/" + nd.Name()
				if seen[key] {
					continue
				}
				m, err := LoadManifest(ManifestPath(root, kd.Name(), nd.Name()))
				if err != nil {
					continue
				}
				seen[key] = true
				out = append(out, m)
			}
		}
	}
	return out, nil
}

// Client is a live runtime plugin connection.
type Client struct {
	client  *plugin.Client
	Backend runtimeplugin.Backend
}

// Close kills the plugin process.
func (c *Client) Close() {
	if c != nil && c.client != nil {
		c.client.Kill()
	}
}

// HooksClient is a live runtime hooks plugin connection.
type HooksClient struct {
	client *plugin.Client
	Hooks  runtimeplugin.Hooks
}

// Close kills the plugin process.
func (c *HooksClient) Close() {
	if c != nil && c.client != nil {
		c.client.Kill()
	}
}

// FSClient is a live generic fs plugin connection.
type FSClient struct {
	client *plugin.Client
	Plugin fsplugin.Plugin
}

// Close kills the plugin process.
func (c *FSClient) Close() {
	if c != nil && c.client != nil {
		c.client.Kill()
	}
}

// NetworkFilterClient is a live network filter plugin connection.
type NetworkFilterClient struct {
	client *plugin.Client
	Filter netplugin.Filter
}

// Close kills the plugin process.
func (c *NetworkFilterClient) Close() {
	if c != nil && c.client != nil {
		c.client.Kill()
	}
}

// NetworkTerminateClient is a live network terminate plugin connection.
type NetworkTerminateClient struct {
	client    *plugin.Client
	Terminate netplugin.Terminate
}

// Close kills the plugin process.
func (c *NetworkTerminateClient) Close() {
	if c != nil && c.client != nil {
		c.client.Kill()
	}
}

// DispenseRuntime launches a runtime plugin binary and returns Backend.
func DispenseRuntime(cmdPath string) (*Client, error) {
	cmd := exec.Command(cmdPath)
	cmd.Stdin = os.Stdin // so interactive Exec (TTY) can attach the host terminal
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: runtimeplugin.Handshake,
		Plugins: map[string]plugin.Plugin{
			runtimeplugin.PluginName: &runtimeplugin.RPCPlugin{},
		},
		Cmd:              cmd,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolNetRPC},
		SyncStdout:       os.Stdout,
		SyncStderr:       os.Stderr,
		Logger:           hclog.New(&hclog.LoggerOptions{Name: "plugin", Output: io.Discard, Level: hclog.Error}),
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, err
	}
	raw, err := rpcClient.Dispense(runtimeplugin.PluginName)
	if err != nil {
		client.Kill()
		return nil, err
	}
	b, ok := raw.(runtimeplugin.Backend)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("unexpected plugin type %T", raw)
	}
	return &Client{client: client, Backend: b}, nil
}

// DispenseRuntimeHooks launches a runtime hooks plugin binary.
func DispenseRuntimeHooks(cmdPath string) (*HooksClient, error) {
	cmd := exec.Command(cmdPath)
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: runtimeplugin.Handshake,
		Plugins: map[string]plugin.Plugin{
			runtimeplugin.HooksPluginName: &runtimeplugin.HooksRPCPlugin{},
		},
		Cmd:              cmd,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolNetRPC},
		SyncStdout:       os.Stdout,
		SyncStderr:       os.Stderr,
		Logger:           hclog.New(&hclog.LoggerOptions{Name: "plugin", Output: io.Discard, Level: hclog.Error}),
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, err
	}
	raw, err := rpcClient.Dispense(runtimeplugin.HooksPluginName)
	if err != nil {
		client.Kill()
		return nil, err
	}
	h, ok := raw.(runtimeplugin.Hooks)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("unexpected plugin type %T", raw)
	}
	return &HooksClient{client: client, Hooks: h}, nil
}

// DispenseFS launches a generic fs plugin binary.
func DispenseFS(cmdPath string) (*FSClient, error) {
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: fsplugin.Handshake,
		Plugins: map[string]plugin.Plugin{
			fsplugin.PluginName: &fsplugin.RPCPlugin{},
		},
		Cmd:              exec.Command(cmdPath),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolNetRPC},
		SyncStderr:       os.Stderr,
		Logger:           hclog.New(&hclog.LoggerOptions{Name: "plugin", Output: io.Discard, Level: hclog.Error}),
	})
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, err
	}
	raw, err := rpcClient.Dispense(fsplugin.PluginName)
	if err != nil {
		client.Kill()
		return nil, err
	}
	p, ok := raw.(fsplugin.Plugin)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("unexpected plugin type %T", raw)
	}
	return &FSClient{client: client, Plugin: p}, nil
}

// DispenseNetworkFilter launches a network filter plugin binary.
func DispenseNetworkFilter(cmdPath string) (*NetworkFilterClient, error) {
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: netplugin.Handshake,
		Plugins: map[string]plugin.Plugin{
			netplugin.FilterPluginName: &netplugin.FilterRPCPlugin{},
		},
		Cmd:              exec.Command(cmdPath),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolNetRPC},
		SyncStderr:       os.Stderr,
		Logger:           hclog.New(&hclog.LoggerOptions{Name: "plugin", Output: io.Discard, Level: hclog.Error}),
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, err
	}
	raw, err := rpcClient.Dispense(netplugin.FilterPluginName)
	if err != nil {
		client.Kill()
		return nil, err
	}
	f, ok := raw.(netplugin.Filter)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("unexpected plugin type %T", raw)
	}
	return &NetworkFilterClient{client: client, Filter: f}, nil
}

// DispenseNetworkTerminate launches a network terminate plugin binary.
func DispenseNetworkTerminate(cmdPath string) (*NetworkTerminateClient, error) {
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: netplugin.Handshake,
		Plugins: map[string]plugin.Plugin{
			netplugin.TerminatePluginName: &netplugin.TerminateRPCPlugin{},
		},
		Cmd:              exec.Command(cmdPath),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolNetRPC},
		SyncStderr:       os.Stderr,
		Logger:           hclog.New(&hclog.LoggerOptions{Name: "plugin", Output: io.Discard, Level: hclog.Error}),
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, err
	}
	raw, err := rpcClient.Dispense(netplugin.TerminatePluginName)
	if err != nil {
		client.Kill()
		return nil, err
	}
	t, ok := raw.(netplugin.Terminate)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("unexpected plugin type %T", raw)
	}
	return &NetworkTerminateClient{client: client, Terminate: t}, nil
}
