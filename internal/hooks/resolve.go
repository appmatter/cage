package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/appmatter/cage/internal/config"
	"github.com/appmatter/cage/internal/pluginhost"
	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

// Resolve fills r.ResolvedHooks from YAML hooks plus plugin-declared defaults for seated plugins.
func Resolve(projectRoot string, r *config.Runtime) error {
	if r == nil {
		return nil
	}
	out := map[string][]string{}

	// Operator extras from YAML.
	for event, actions := range r.Hooks {
		for _, a := range actions {
			if a.Plugin == "" {
				continue
			}
			out[event] = appendUnique(out[event], a.Plugin)
		}
	}

	// Harness seats: load manifest hooks (build-time declaration).
	for _, hs := range r.HarnessSeats {
		m, err := manifestFor(projectRoot, hs.PluginID)
		if err != nil {
			// Not installed yet — still show seat; hooks unknown until install.
			continue
		}
		for _, event := range m.Hooks {
			out[event] = appendUnique(out[event], hs.PluginID)
		}
	}

	// Stable order per event.
	for event, plugins := range out {
		sort.Strings(plugins)
		out[event] = plugins
	}
	r.ResolvedHooks = out
	return nil
}

func manifestFor(projectRoot, pluginID string) (pluginhost.Manifest, error) {
	_, m, err := pluginhost.ResolveCommand(projectRoot, "runtime", pluginID)
	return m, err
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// CollectBeforeBakeScripts asks before_bake plugins for attachments and writes them under
// projectRoot/.cage/.cache/images/attachments/, returning absolute script paths (operator bake first).
func CollectBeforeBakeScripts(
	projectRoot string,
	r config.Runtime,
	logf func(string, ...any),
) ([]string, error) {
	if err := Resolve(projectRoot, &r); err != nil {
		return nil, err
	}
	scripts := append([]string{}, r.Bake...)

	pluginIDs := r.ResolvedHooks[runtimeplugin.HookBeforeBake]
	if len(pluginIDs) == 0 {
		return scripts, nil
	}

	seatByPlugin := map[string]config.HarnessSeat{}
	for _, hs := range r.HarnessSeats {
		seatByPlugin[hs.PluginID] = hs
	}

	attDir := filepath.Join(projectRoot, ".cage", ".cache", "images", "attachments")
	if err := os.MkdirAll(attDir, 0o755); err != nil {
		return nil, err
	}

	for _, pluginID := range pluginIDs {
		hs, ok := seatByPlugin[pluginID]
		if !ok {
			// YAML-only hook without harness seat — skip seat yaml.
			hs = config.HarnessSeat{Seat: pluginID, PluginID: pluginID}
		}
		cmdPath, _, err := pluginhost.ResolveCommand(projectRoot, "runtime", pluginID)
		if err != nil {
			return nil, fmt.Errorf("before_bake %s: %w", pluginID, err)
		}
		client, err := pluginhost.DispenseRuntimeHooks(cmdPath)
		if err != nil {
			return nil, fmt.Errorf("before_bake %s: %w", pluginID, err)
		}
		atts, err := client.Hooks.BeforeBake(runtimeplugin.HookContext{
			Seat:      hs.Seat,
			Workdir:   r.Workdir,
			BaseImage: r.Image,
			GuestGOOS: "linux",
			SeatYAML:  hs.SeatYAML,
		})
		client.Close()
		if err != nil {
			return nil, fmt.Errorf("before_bake %s: %w", pluginID, err)
		}
		for i, a := range atts {
			name := a.Name
			if name == "" {
				name = fmt.Sprintf("%s-%d", pluginID, i)
			}
			path := filepath.Join(attDir, sanitize(pluginID)+"-"+sanitize(name)+".sh")
			body := a.Body
			if len(body) == 0 || body[len(body)-1] != '\n' {
				body = append(append([]byte{}, body...), '\n')
			}
			if err := os.WriteFile(path, body, 0o755); err != nil {
				return nil, err
			}
			if logf != nil {
				logf("before_bake %s → %s", pluginID, path)
			}
			scripts = append(scripts, path)
		}
	}
	return scripts, nil
}

func sanitize(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			b = append(b, c)
		} else {
			b = append(b, '_')
		}
	}
	if len(b) == 0 {
		return "att"
	}
	return string(b)
}

// RunHookOpts configures host→guest wiring for lifecycle hooks.
type RunHookOpts struct {
	ConfigPath string
	Backend    runtimeplugin.Backend // optional; enables agent dir sync after on_start
	VMID       string
}

// RunHook invokes event on each resolved plugin for that event (harness seats).
func RunHook(projectRoot string, r config.Runtime, event string, logf func(string, ...any), opts RunHookOpts) error {
	if err := Resolve(projectRoot, &r); err != nil {
		return err
	}
	pluginIDs := r.ResolvedHooks[event]
	seatByPlugin := map[string]config.HarnessSeat{}
	for _, hs := range r.HarnessSeats {
		seatByPlugin[hs.PluginID] = hs
	}
	for _, pluginID := range pluginIDs {
		hs := seatByPlugin[pluginID]
		if hs.PluginID == "" {
			hs = config.HarnessSeat{Seat: pluginID, PluginID: pluginID}
		}
		cmdPath, _, err := pluginhost.ResolveCommand(projectRoot, "runtime", pluginID)
		if err != nil {
			return fmt.Errorf("%s %s: %w", event, pluginID, err)
		}
		client, err := pluginhost.DispenseRuntimeHooks(cmdPath)
		if err != nil {
			return fmt.Errorf("%s %s: %w", event, pluginID, err)
		}
		agentDir := ""
		if event == runtimeplugin.HookOnStart || event == runtimeplugin.HookOnAttachShell {
			agentDir = ResolveAgentDir(projectRoot, hs)
		}
		ctx := runtimeplugin.HookContext{
			Seat:        hs.Seat,
			Workdir:     r.Workdir,
			BaseImage:   r.Image,
			GuestGOOS:   "linux",
			ProjectRoot: projectRoot,
			ConfigPath:  opts.ConfigPath,
			AgentDir:    agentDir,
			SeatYAML:    hs.SeatYAML,
		}
		var runErr error
		switch event {
		case runtimeplugin.HookOnStart:
			runErr = client.Hooks.OnStart(ctx)
		case runtimeplugin.HookOnAttachShell:
			runErr = client.Hooks.OnAttachShell(ctx)
		case runtimeplugin.HookBeforeBake:
			client.Close()
			continue
		default:
			client.Close()
			continue
		}
		client.Close()
		if runErr != nil {
			return fmt.Errorf("%s %s: %w", event, pluginID, runErr)
		}
		if (event == runtimeplugin.HookOnStart || event == runtimeplugin.HookOnAttachShell) && opts.Backend != nil && agentDir != "" && pluginID == "pi-agent" {
			if err := SyncAgentDirToGuest(opts.Backend, opts.VMID, agentDir); err != nil {
				return fmt.Errorf("sync agent dir %s: %w", agentDir, err)
			}
			if logf != nil {
				logf("agent dir %s → %s", agentDir, PiGuestAgentDir)
			}
		}
		if logf != nil {
			logf("hook %s %s ok", event, pluginID)
		}
	}
	return nil
}
