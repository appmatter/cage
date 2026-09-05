package network

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

const (
	PluginName          = "network"
	FilterPluginName    = "filter"
	TerminatePluginName = "terminate"
	// MagicCookieValue must match between host and plugin.
	MagicCookieKey   = "CAGE_NETWORK_PLUGIN"
	MagicCookieValue = "cage-network-v1"
)

// Handshake is shared by host and network plugin binaries.
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   MagicCookieKey,
	MagicCookieValue: MagicCookieValue,
}

// Request is one outbound destination check.
type Request struct {
	Host   string
	Port   int
	Method string
	Path   string
	// Partial is true on SOCKS CONNECT before application data is known.
	// Filters ignore method/path constraints when Partial is set.
	Partial bool
}

// Decision is the filter result.
type Decision struct {
	Allow  bool
	Reason string
}

// Filter is implemented by network filter-stage plugins (e.g. egress).
type Filter interface {
	Name() string
	Configure(raw []byte) error
	Check(req Request) (Decision, error)
}

// PrepareIn is a guest HTTP request hitting a terminate endpoint.
type PrepareIn struct {
	Endpoint string
	Method   string
	Path     string
	Header   map[string][]string
}

// PrepareOut is the upstream request the host should dial.
type PrepareOut struct {
	UpstreamHost string
	UpstreamPort int
	UpstreamURL  string
	Header       map[string][]string
}

// Terminate is implemented by network terminate-stage plugins (e.g. http-proxy).
type Terminate interface {
	Name() string
	Configure(raw []byte) error
	Prepare(in PrepareIn) (PrepareOut, error)
}

// FilterPluginMap is the go-plugin map for a filter plugin process.
// Client operations are registered by the plugin binary via client.Register.
func FilterPluginMap(f Filter) map[string]plugin.Plugin {
	return map[string]plugin.Plugin{FilterPluginName: &FilterRPCPlugin{Impl: f}}
}

// TerminatePluginMap is the go-plugin map for a terminate plugin process.
// Client operations are registered by the plugin binary via client.Register.
func TerminatePluginMap(t Terminate) map[string]plugin.Plugin {
	return map[string]plugin.Plugin{TerminatePluginName: &TerminateRPCPlugin{Impl: t}}
}

// FilterRPCPlugin is the go-plugin net/rpc adapter for Filter.
type FilterRPCPlugin struct {
	Impl Filter
}

func (p *FilterRPCPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &FilterRPCServer{Impl: p.Impl}, nil
}

func (p *FilterRPCPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &FilterRPCClient{client: c}, nil
}

// FilterRPCServer exposes Filter over net/rpc.
type FilterRPCServer struct {
	Impl Filter
}

func (s *FilterRPCServer) Name(_ struct{}, resp *string) error {
	*resp = s.Impl.Name()
	return nil
}

func (s *FilterRPCServer) Configure(raw []byte, _ *struct{}) error {
	return s.Impl.Configure(raw)
}

func (s *FilterRPCServer) Check(req Request, resp *Decision) error {
	d, err := s.Impl.Check(req)
	if err != nil {
		return err
	}
	*resp = d
	return nil
}

// FilterRPCClient is the host-side Filter over net/rpc.
type FilterRPCClient struct {
	client *rpc.Client
}

func (c *FilterRPCClient) Name() string {
	var name string
	if err := c.client.Call("Plugin.Name", struct{}{}, &name); err != nil {
		return ""
	}
	return name
}

func (c *FilterRPCClient) Configure(raw []byte) error {
	return c.client.Call("Plugin.Configure", raw, nil)
}

func (c *FilterRPCClient) Check(req Request) (Decision, error) {
	var d Decision
	err := c.client.Call("Plugin.Check", req, &d)
	return d, err
}

var _ Filter = (*FilterRPCClient)(nil)

// TerminateRPCPlugin is the go-plugin net/rpc adapter for Terminate.
type TerminateRPCPlugin struct {
	Impl Terminate
}

func (p *TerminateRPCPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &TerminateRPCServer{Impl: p.Impl}, nil
}

func (p *TerminateRPCPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &TerminateRPCClient{client: c}, nil
}

// TerminateRPCServer exposes Terminate over net/rpc.
type TerminateRPCServer struct {
	Impl Terminate
}

func (s *TerminateRPCServer) Name(_ struct{}, resp *string) error {
	*resp = s.Impl.Name()
	return nil
}

func (s *TerminateRPCServer) Configure(raw []byte, _ *struct{}) error {
	return s.Impl.Configure(raw)
}

func (s *TerminateRPCServer) Prepare(in PrepareIn, resp *PrepareOut) error {
	out, err := s.Impl.Prepare(in)
	if err != nil {
		return err
	}
	*resp = out
	return nil
}

// TerminateRPCClient is the host-side Terminate over net/rpc.
type TerminateRPCClient struct {
	client *rpc.Client
}

func (c *TerminateRPCClient) Name() string {
	var name string
	if err := c.client.Call("Plugin.Name", struct{}{}, &name); err != nil {
		return ""
	}
	return name
}

func (c *TerminateRPCClient) Configure(raw []byte) error {
	return c.client.Call("Plugin.Configure", raw, nil)
}

func (c *TerminateRPCClient) Prepare(in PrepareIn) (PrepareOut, error) {
	var out PrepareOut
	err := c.client.Call("Plugin.Prepare", in, &out)
	return out, err
}

var _ Terminate = (*TerminateRPCClient)(nil)
