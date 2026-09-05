// Package client defines the optional generic client capability for Cage plugins.
package client

import (
	"encoding/json"
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

const PluginName = "client"

// Context is resolved by Cage and selected by the server, never by a request.
// Data is context-specific JSON and must not contain secret values.
type Context struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

type Request struct {
	Operation string          `json:"operation"`
	Payload   json.RawMessage `json:"payload"`
}
type Response struct {
	Payload json.RawMessage `json:"payload"`
}

// Capability is implemented by a plugin that wants a client route.
type Capability interface {
	Name() string
	Capabilities() []string
	Configure(Context) error
	Call(Request) (Response, error)
}

// Register adds the client service to an existing multiplexed plugin map.
// Call from the plugin binary only when that plugin intentionally exposes
// client operations. Context PluginMap helpers do not register this.
func Register(plugins map[string]plugin.Plugin, cap Capability) {
	if cap == nil {
		return
	}
	plugins[PluginName] = &RPCPlugin{Impl: cap}
}

type RPCPlugin struct{ Impl Capability }

func (p *RPCPlugin) Server(*plugin.MuxBroker) (interface{}, error) { return &RPCServer{p.Impl}, nil }
func (p *RPCPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &RPCClient{c}, nil
}

type RPCServer struct{ Impl Capability }

func (s *RPCServer) Name(_ struct{}, out *string) error { *out = s.Impl.Name(); return nil }
func (s *RPCServer) Capabilities(_ struct{}, out *[]string) error {
	*out = s.Impl.Capabilities()
	return nil
}
func (s *RPCServer) Configure(in Context, _ *struct{}) error { return s.Impl.Configure(in) }
func (s *RPCServer) Call(in Request, out *Response) error {
	var err error
	*out, err = s.Impl.Call(in)
	return err
}

type RPCClient struct{ client *rpc.Client }

func (c *RPCClient) Name() string {
	var out string
	_ = c.client.Call("Plugin.Name", struct{}{}, &out)
	return out
}
func (c *RPCClient) Capabilities() []string {
	var out []string
	_ = c.client.Call("Plugin.Capabilities", struct{}{}, &out)
	return out
}
func (c *RPCClient) Configure(v Context) error { return c.client.Call("Plugin.Configure", v, nil) }
func (c *RPCClient) Call(v Request) (Response, error) {
	var out Response
	err := c.client.Call("Plugin.Call", v, &out)
	return out, err
}

var _ Capability = (*RPCClient)(nil)
