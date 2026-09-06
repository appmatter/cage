// Package fs defines generic host-side filesystem plugin contracts.
package fs

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

const (
	PluginName       = "fs"
	MagicCookieKey   = "CAGE_FS_PLUGIN"
	MagicCookieValue = "cage-fs-v1"
)

var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   MagicCookieKey,
	MagicCookieValue: MagicCookieValue,
}

// Path maps a host path to its resolved guest path.
type Path struct {
	Host       string `json:"host"`
	Guest      string `json:"guest"`
	Path       string `json:"path"`
	Permission string `json:"permission"`
}

// Context is the resolved filesystem context supplied to a client provider.
type Context struct {
	ProjectRoot string   `json:"projectRoot"`
	Workdir     string   `json:"workdir"`
	Layout      string   `json:"layout"`
	Mounts      []Path   `json:"mounts"`
	Copies      []Path   `json:"copies"`
	Deny        []string `json:"deny"`
	SeatYAML    []byte   `json:"seatYAML"`
	InstanceID  string   `json:"instanceID,omitempty"`
	BackendVMID string   `json:"backendVMID,omitempty"`
}

// Plugin is the base fs service. Client operations are registered by the
// plugin binary via client.Register when needed.
type Plugin interface {
	Name() string
}

// PluginMap is the go-plugin map for an fs plugin process.
func PluginMap(p Plugin) map[string]plugin.Plugin {
	return map[string]plugin.Plugin{PluginName: &RPCPlugin{Impl: p}}
}

type RPCPlugin struct{ Impl Plugin }

func (p *RPCPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &RPCServer{Impl: p.Impl}, nil
}
func (p *RPCPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &RPCClient{client: c}, nil
}

type RPCServer struct{ Impl Plugin }

func (s *RPCServer) Name(_ struct{}, out *string) error { *out = s.Impl.Name(); return nil }

type RPCClient struct{ client *rpc.Client }

func (c *RPCClient) Name() string {
	var out string
	if c.client.Call("Plugin.Name", struct{}{}, &out) != nil {
		return ""
	}
	return out
}

var _ Plugin = (*RPCClient)(nil)
