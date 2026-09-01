// Package fs defines generic host-side filesystem plugin contracts.
package fs

import (
	"encoding/json"
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
	Host       string
	Guest      string
	Path       string
	Permission string
}

// Context is the resolved filesystem context supplied to every fs plugin.
type Context struct {
	ProjectRoot string
	Workdir     string
	Layout      string
	Mounts      []Path
	Copies      []Path
	Deny        []string
	SeatYAML    []byte
}

// Request and Response carry plugin-owned operation payloads.
type Request struct {
	Operation string
	Payload   json.RawMessage
}
type Response struct{ Payload json.RawMessage }

// Plugin is an fs plugin. Cage routes operations without interpreting them.
type Plugin interface {
	Name() string
	Configure(Context) error
	Call(Request) (Response, error)
}

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

func (s *RPCServer) Name(_ struct{}, out *string) error      { *out = s.Impl.Name(); return nil }
func (s *RPCServer) Configure(in Context, _ *struct{}) error { return s.Impl.Configure(in) }
func (s *RPCServer) Call(in Request, out *Response) error {
	result, err := s.Impl.Call(in)
	*out = result
	return err
}

type RPCClient struct{ client *rpc.Client }

func (c *RPCClient) Name() string {
	var out string
	if c.client.Call("Plugin.Name", struct{}{}, &out) != nil {
		return ""
	}
	return out
}
func (c *RPCClient) Configure(in Context) error { return c.client.Call("Plugin.Configure", in, nil) }
func (c *RPCClient) Call(in Request) (Response, error) {
	var out Response
	err := c.client.Call("Plugin.Call", in, &out)
	return out, err
}

var _ Plugin = (*RPCClient)(nil)
