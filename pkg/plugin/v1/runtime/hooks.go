package runtime

import (
	"net/rpc"

	"github.com/hashicorp/go-plugin"
)

// Hook event names (plugin declares these at build time in manifest / Hooks()).
const (
	HookBeforeBake    = "before_bake"
	HookOnStart       = "on_start"
	HookOnAttachShell = "on_attach_shell"
	HookUp            = "up"
	HookDown          = "down"
)

// Plugin CLI commands advertised in plugin.json / manifest.
const CommandInit = "init"

const HooksPluginName = "runtime-hooks"

// BakeAttachment is a script contributed by a before_bake hook.
type BakeAttachment struct {
	Name string // label (hashed + logged)
	Body []byte // script contents
}

// HookContext is passed to hook handlers.
type HookContext struct {
	Seat        string
	Workdir     string
	BaseImage   string
	GuestGOOS   string
	ProjectRoot string
	ConfigPath  string // active cage yaml
	AgentDir    string // abs host dir for agent config (models.json, …); may be empty
	// SeatYAML is the seat config under runtime.plugins.<seat> (plugin parses its fields).
	SeatYAML []byte
	// DryRun asks the plugin for marker-only bake scripts (tests / CI).
	DryRun bool
}

// Hooks is implemented by harness (and other) runtime plugins that attach to context hooks.
type Hooks interface {
	Name() string
	// Hooks returns events this plugin attaches to (build-time declaration).
	Hooks() []string
	BeforeBake(ctx HookContext) ([]BakeAttachment, error)
	OnStart(ctx HookContext) error
	OnAttachShell(ctx HookContext) error
	// Init seeds project-local plugin artefacts (cage plugin init). No-op if unused.
	Init(ctx HookContext) error
}

// HooksPluginMap is the go-plugin map for a hooks plugin process.
func HooksPluginMap(h Hooks) map[string]plugin.Plugin {
	return map[string]plugin.Plugin{
		HooksPluginName: &HooksRPCPlugin{Impl: h},
	}
}

// HooksRPCPlugin is the go-plugin net/rpc adapter for Hooks.
type HooksRPCPlugin struct {
	Impl Hooks
}

func (p *HooksRPCPlugin) Server(*plugin.MuxBroker) (interface{}, error) {
	return &HooksRPCServer{Impl: p.Impl}, nil
}

func (p *HooksRPCPlugin) Client(_ *plugin.MuxBroker, c *rpc.Client) (interface{}, error) {
	return &HooksRPCClient{client: c}, nil
}

// HooksRPCServer exposes Hooks over net/rpc.
type HooksRPCServer struct {
	Impl Hooks
}

func (s *HooksRPCServer) Name(_ struct{}, resp *string) error {
	*resp = s.Impl.Name()
	return nil
}

func (s *HooksRPCServer) Hooks(_ struct{}, resp *[]string) error {
	*resp = append([]string{}, s.Impl.Hooks()...)
	return nil
}

func (s *HooksRPCServer) BeforeBake(ctx HookContext, resp *[]BakeAttachment) error {
	out, err := s.Impl.BeforeBake(ctx)
	if err != nil {
		return err
	}
	*resp = out
	return nil
}

func (s *HooksRPCServer) OnStart(ctx HookContext, _ *struct{}) error {
	return s.Impl.OnStart(ctx)
}

func (s *HooksRPCServer) OnAttachShell(ctx HookContext, _ *struct{}) error {
	return s.Impl.OnAttachShell(ctx)
}

func (s *HooksRPCServer) Init(ctx HookContext, _ *struct{}) error {
	return s.Impl.Init(ctx)
}

// HooksRPCClient is the host-side Hooks over net/rpc.
type HooksRPCClient struct {
	client *rpc.Client
}

func (c *HooksRPCClient) Name() string {
	var name string
	if err := c.client.Call("Plugin.Name", struct{}{}, &name); err != nil {
		return ""
	}
	return name
}

func (c *HooksRPCClient) Hooks() []string {
	var hooks []string
	if err := c.client.Call("Plugin.Hooks", struct{}{}, &hooks); err != nil {
		return nil
	}
	return hooks
}

func (c *HooksRPCClient) BeforeBake(ctx HookContext) ([]BakeAttachment, error) {
	var out []BakeAttachment
	err := c.client.Call("Plugin.BeforeBake", ctx, &out)
	return out, err
}

func (c *HooksRPCClient) OnStart(ctx HookContext) error {
	return c.client.Call("Plugin.OnStart", ctx, nil)
}

func (c *HooksRPCClient) OnAttachShell(ctx HookContext) error {
	return c.client.Call("Plugin.OnAttachShell", ctx, nil)
}

func (c *HooksRPCClient) Init(ctx HookContext) error {
	return c.client.Call("Plugin.Init", ctx, nil)
}

var _ Hooks = (*HooksRPCClient)(nil)
