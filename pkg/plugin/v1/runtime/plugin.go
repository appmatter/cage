package runtime

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

const (
	PluginName = "runtime"
	// MagicCookieValue must match between host and plugin.
	MagicCookieKey   = "CAGE_RUNTIME_PLUGIN"
	MagicCookieValue = "cage-runtime-v1"
)

// Handshake is shared by host and plugin binaries.
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   MagicCookieKey,
	MagicCookieValue: MagicCookieValue,
}

// PathSpec is a host↔guest path mapping for mount or copy.
type PathSpec struct {
	Host       string
	Guest      string
	Permission string // rw | ro
}

// Spec describes a sandbox instance.
type Spec struct {
	ID           string
	Image        string
	Workdir      string
	Graphics     bool // when true, runtime may show a UI (tart omits --no-graphics)
	Env          map[string]string // runtime.env only — never host os.Environ
	Mounts       []PathSpec
	Copies       []PathSpec
	DenyMasks    []string // guest paths to obscure under mounts (fs.deny descendants)
	OnCreate     []string // abs host script paths; plugin chooses interpreter
	OnStart      []string
	OnDestroy    []string
	ExtraRunArgs []string // appended to runtime run (e.g. softnet flags); empty = none
}

// Status is the lifecycle state of a sandbox.
type Status struct {
	ID    string
	State string // created | running | stopped | unknown
}

// ExecOpts is a guest command.
type ExecOpts struct {
	Argv  []string
	Stdin []byte // optional; ignored when TTY (uses process stdin)
	TTY   bool   // allocate a guest PTY and attach host stdin/stdout
}

// BakeSpec materializes a derived image once per content hash (see docs/plugins/runtime-image-bake.md).
type BakeSpec struct {
	BaseImage string   // seat base image
	DerivedID string   // e.g. cage-bake-<hash>
	Scripts   []string // abs host script paths; run once during bake
	Workdir   string
	Force     bool // delete DerivedID first (incomplete / corrupt cache)
}

// Backend is implemented by runtime plugins (e.g. tart).
type Backend interface {
	Name() string
	Create(spec Spec) error
	Start(spec Spec) error
	Stop(id string) error
	Status(id string) (Status, error)
	Delete(spec Spec) error
	Exec(id string, opts ExecOpts) error
	// Bake builds DerivedID from BaseImage+Scripts if missing; no-op when DerivedID already exists.
	Bake(spec BakeSpec) error
}

// PluginMap is the go-plugin map for a runtime plugin process.
func PluginMap(b Backend) map[string]plugin.Plugin {
	return map[string]plugin.Plugin{
		PluginName: &RPCPlugin{Impl: b},
	}
}

// RPCPlugin is the go-plugin net/rpc adapter for Backend.
type RPCPlugin struct {
	Impl Backend
}

func (p *RPCPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &RPCServer{Impl: p.Impl}, nil
}

func (p *RPCPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &RPCClient{client: c}, nil
}

// RPCServer exposes Backend over net/rpc.
type RPCServer struct {
	Impl Backend
}

func (s *RPCServer) Name(_ struct{}, resp *string) error {
	*resp = s.Impl.Name()
	return nil
}

func (s *RPCServer) Create(spec Spec, _ *struct{}) error {
	return s.Impl.Create(spec)
}

func (s *RPCServer) Start(spec Spec, _ *struct{}) error {
	return s.Impl.Start(spec)
}

func (s *RPCServer) Stop(id string, _ *struct{}) error {
	return s.Impl.Stop(id)
}

func (s *RPCServer) Status(id string, resp *Status) error {
	st, err := s.Impl.Status(id)
	if err != nil {
		return err
	}
	*resp = st
	return nil
}

func (s *RPCServer) Delete(spec Spec, _ *struct{}) error {
	return s.Impl.Delete(spec)
}

type ExecArgs struct {
	ID   string
	Opts ExecOpts
}

func (s *RPCServer) Exec(args ExecArgs, _ *struct{}) error {
	return s.Impl.Exec(args.ID, args.Opts)
}

func (s *RPCServer) Bake(spec BakeSpec, _ *struct{}) error {
	return s.Impl.Bake(spec)
}

// RPCClient is the host-side Backend over net/rpc.
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

func (c *RPCClient) Create(spec Spec) error {
	return c.client.Call("Plugin.Create", spec, nil)
}

func (c *RPCClient) Start(spec Spec) error {
	return c.client.Call("Plugin.Start", spec, nil)
}

func (c *RPCClient) Stop(id string) error {
	return c.client.Call("Plugin.Stop", id, nil)
}

func (c *RPCClient) Status(id string) (Status, error) {
	var st Status
	err := c.client.Call("Plugin.Status", id, &st)
	return st, err
}

func (c *RPCClient) Delete(spec Spec) error {
	return c.client.Call("Plugin.Delete", spec, nil)
}

func (c *RPCClient) Exec(id string, opts ExecOpts) error {
	return c.client.Call("Plugin.Exec", ExecArgs{ID: id, Opts: opts}, nil)
}

func (c *RPCClient) Bake(spec BakeSpec) error {
	return c.client.Call("Plugin.Bake", spec, nil)
}

var _ Backend = (*RPCClient)(nil)
