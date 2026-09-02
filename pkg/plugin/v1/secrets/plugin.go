package secrets

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

const (
	PluginName = "secrets"
	// MagicCookieValue must match between host and plugin.
	MagicCookieKey   = "CAGE_SECRETS_PLUGIN"
	MagicCookieValue = "cage-secrets-v1"
)

// Handshake is shared by host and secrets plugin binaries.
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   MagicCookieKey,
	MagicCookieValue: MagicCookieValue,
}

// Store is implemented by secrets plugins (e.g. onepassword).
type Store interface {
	Name() string
	Configure(raw []byte) error
	Resolve(refs map[string]string) (map[string]string, error)
}

// PluginMap is the go-plugin map for a secrets store process.
func PluginMap(s Store) map[string]plugin.Plugin {
	return map[string]plugin.Plugin{
		PluginName: &RPCPlugin{Impl: s},
	}
}

// RPCPlugin is the go-plugin net/rpc adapter for Store.
type RPCPlugin struct {
	Impl Store
}

func (p *RPCPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &RPCServer{Impl: p.Impl}, nil
}

func (p *RPCPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &RPCClient{client: c}, nil
}

// RPCServer exposes Store over net/rpc.
type RPCServer struct {
	Impl Store
}

func (s *RPCServer) Name(_ struct{}, resp *string) error {
	*resp = s.Impl.Name()
	return nil
}

func (s *RPCServer) Configure(raw []byte, _ *struct{}) error {
	return s.Impl.Configure(raw)
}

func (s *RPCServer) Resolve(refs map[string]string, resp *map[string]string) error {
	out, err := s.Impl.Resolve(refs)
	if err != nil {
		return err
	}
	*resp = out
	return nil
}

// RPCClient is the host-side Store over net/rpc.
type RPCClient struct {
	client *rpc.Client
}

func (c *RPCClient) Name() string {
	var name string
	if err := c.client.Call("Plugin.Name", struct{}{}, &name); err != nil {
		return ""
	}
	return name
}

func (c *RPCClient) Configure(raw []byte) error {
	return c.client.Call("Plugin.Configure", raw, nil)
}

func (c *RPCClient) Resolve(refs map[string]string) (map[string]string, error) {
	var out map[string]string
	err := c.client.Call("Plugin.Resolve", refs, &out)
	return out, err
}

var _ Store = (*RPCClient)(nil)
