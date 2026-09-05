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

	clientplugin "github.com/appmatter/cage/pkg/plugin/v1/client"
	fsplugin "github.com/appmatter/cage/pkg/plugin/v1/fs"
	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
	secretsplugin "github.com/appmatter/cage/pkg/plugin/v1/secrets"
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
	Client      bool         `json:"client,omitempty"` // client service is registered in the plugin process
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

// process owns a go-plugin subprocess.
type process struct {
	client *plugin.Client
}

// Close kills the plugin process.
func (p *process) Close() {
	if p != nil && p.client != nil {
		p.client.Kill()
	}
}

// Client is a live runtime plugin connection.
type Client struct {
	process
	Backend runtimeplugin.Backend
}

// HooksClient is a live runtime hooks plugin connection.
type HooksClient struct {
	process
	Hooks runtimeplugin.Hooks
}

// NetworkFilterClient is a live network filter plugin connection.
type NetworkFilterClient struct {
	process
	Filter netplugin.Filter
}

// NetworkTerminateClient is a live network terminate plugin connection.
type NetworkTerminateClient struct {
	process
	Terminate netplugin.Terminate
}

// SecretsClient is a live secrets store plugin connection.
type SecretsClient struct {
	process
	Store secretsplugin.Store
}

// CapabilityClient is a live generic client-capability connection.
type CapabilityClient struct {
	process
	Capability clientplugin.Capability
}

// FSClient is a live fs context plugin connection.
type FSClient struct {
	process
	Plugin fsplugin.Plugin
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
	return &Client{process: process{client: client}, Backend: b}, nil
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
	return &HooksClient{process: process{client: client}, Hooks: h}, nil
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
	return &NetworkFilterClient{process: process{client: client}, Filter: f}, nil
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
	return &NetworkTerminateClient{process: process{client: client}, Terminate: t}, nil
}

// DispenseFS launches an fs context plugin.
func DispenseFS(cmdPath string) (*FSClient, error) {
	client := plugin.NewClient(&plugin.ClientConfig{HandshakeConfig: fsplugin.Handshake, Plugins: map[string]plugin.Plugin{fsplugin.PluginName: &fsplugin.RPCPlugin{}}, Cmd: exec.Command(cmdPath), AllowedProtocols: []plugin.Protocol{plugin.ProtocolNetRPC}, SyncStderr: os.Stderr, Logger: hclog.New(&hclog.LoggerOptions{Name: "plugin", Output: io.Discard, Level: hclog.Error})})
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
	return &FSClient{process: process{client: client}, Plugin: p}, nil
}

// DispenseRuntimeCapability discovers the optional client service on a runtime plugin.
func DispenseRuntimeCapability(cmdPath string) (*CapabilityClient, bool, error) {
	return dispenseCapability(cmdPath, runtimeplugin.Handshake, map[string]plugin.Plugin{
		runtimeplugin.PluginName: &runtimeplugin.RPCPlugin{},
		clientplugin.PluginName:  &clientplugin.RPCPlugin{},
	})
}

// DispenseFSCapability discovers the optional client service on an fs plugin.
func DispenseFSCapability(cmdPath string) (*CapabilityClient, bool, error) {
	return dispenseCapability(cmdPath, fsplugin.Handshake, map[string]plugin.Plugin{
		fsplugin.PluginName:     &fsplugin.RPCPlugin{},
		clientplugin.PluginName: &clientplugin.RPCPlugin{},
	})
}

// DispenseNetworkCapability discovers the optional client service on a network plugin.
func DispenseNetworkCapability(cmdPath string) (*CapabilityClient, bool, error) {
	return dispenseCapability(cmdPath, netplugin.Handshake, map[string]plugin.Plugin{
		netplugin.FilterPluginName:    &netplugin.FilterRPCPlugin{},
		netplugin.TerminatePluginName: &netplugin.TerminateRPCPlugin{},
		clientplugin.PluginName:       &clientplugin.RPCPlugin{},
	})
}

// DispenseSecretsCapability discovers the optional client service on a secrets plugin.
func DispenseSecretsCapability(cmdPath string) (*CapabilityClient, bool, error) {
	return dispenseCapability(cmdPath, secretsplugin.Handshake, map[string]plugin.Plugin{
		secretsplugin.PluginName: &secretsplugin.RPCPlugin{},
		clientplugin.PluginName:  &clientplugin.RPCPlugin{},
	})
}

func dispenseCapability(cmdPath string, handshake plugin.HandshakeConfig, plugins map[string]plugin.Plugin) (*CapabilityClient, bool, error) {
	pc := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: handshake, Plugins: plugins, Cmd: exec.Command(cmdPath),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolNetRPC}, SyncStderr: os.Stderr,
		Logger: hclog.New(&hclog.LoggerOptions{Name: "plugin", Output: io.Discard, Level: hclog.Error}),
	})
	rpcClient, err := pc.Client()
	if err != nil {
		pc.Kill()
		return nil, false, err
	}
	raw, err := rpcClient.Dispense(clientplugin.PluginName)
	if err != nil {
		pc.Kill()
		return nil, false, nil
	}
	capability, ok := raw.(clientplugin.Capability)
	if !ok {
		pc.Kill()
		return nil, false, fmt.Errorf("unexpected client capability type %T", raw)
	}
	return &CapabilityClient{process: process{client: pc}, Capability: capability}, true, nil
}

// DispenseSecrets launches a secrets store plugin binary.
func DispenseSecrets(cmdPath string) (*SecretsClient, error) {
	cmd := exec.Command(cmdPath)
	cmd.Stdin = os.Stdin // so op / desktop-app unlock prompts reach the host terminal
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: secretsplugin.Handshake,
		Plugins: map[string]plugin.Plugin{
			secretsplugin.PluginName: &secretsplugin.RPCPlugin{},
		},
		Cmd:              cmd,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolNetRPC},
		SyncStderr:       os.Stderr,
		Logger:           hclog.New(&hclog.LoggerOptions{Name: "plugin", Output: io.Discard, Level: hclog.Error}),
	})

	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, err
	}
	raw, err := rpcClient.Dispense(secretsplugin.PluginName)
	if err != nil {
		client.Kill()
		return nil, err
	}
	s, ok := raw.(secretsplugin.Store)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("unexpected plugin type %T", raw)
	}
	return &SecretsClient{process: process{client: client}, Store: s}, nil
}
