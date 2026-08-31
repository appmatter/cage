package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/appmatter/cage/internal/bake"
	"github.com/appmatter/cage/internal/config"
	"github.com/appmatter/cage/internal/host"
	"github.com/appmatter/cage/internal/hooks"
	"github.com/appmatter/cage/internal/network"
	"github.com/appmatter/cage/internal/pluginhost"
	"github.com/appmatter/cage/internal/termlog"
	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

func newVMCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vm",
		Short: "Manage sandbox VMs via the runtime plugin",
	}
	cmd.AddCommand(newVMCreateCmd(), newVMStartCmd(), newVMStopCmd(), newVMStatusCmd(), newVMDeleteCmd(), newVMExecCmd(), newVMLogsCmd())
	return cmd
}

func newVMCreateCmd() *cobra.Command {
	var (
		configPath string
		id         string
	)
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a VM from config image",
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, backendName, r, err := loadSpec(cmd, configPath, id)
			if err != nil {
				return err
			}
			return withRuntime(backendName, func(b runtimeplugin.Backend) error {
				if err := applyBake(&spec, r, b); err != nil {
					return err
				}
				termlog.CLI("create %s (plugin=%s image=%s)", spec.ID, backendName, spec.Image)
				if err := b.Create(spec); err != nil {
					return err
				}
				termlog.CLI("created %s", spec.ID)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to cage yaml")
	cmd.Flags().StringVar(&id, "id", "", "VM name (default: cage-vm)")
	return cmd
}

func newVMStartCmd() *cobra.Command {
	var (
		configPath string
		id         string
		follow     bool
	)
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a VM (applies mounts, copies, host proxy when enabled)",
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, backendName, r, err := loadSpec(cmd, configPath, id)
			if err != nil {
				return err
			}
			if follow && !r.Network.LoggingEnabled() {
				return fmt.Errorf("start -f requires network.proxy.logging: true")
			}
			proxyOn := r.Network.ProxyEnabled()
			var proxyState network.ProxyState
			if proxyOn {
				st, err := startHostProxy(".", spec.ID, r)
				if err != nil {
					return err
				}
				proxyState = st
				argsSoft, err := network.SoftnetHostOnlyArgs()
				if err != nil {
					_ = network.StopDetachedProxy(".", spec.ID)
					return err
				}
				spec.ExtraRunArgs = argsSoft
			}
			termlog.CLI("start %s (plugin=%s mounts=%d copies=%d proxy=%v)",
				spec.ID, backendName, len(spec.Mounts), len(spec.Copies), proxyOn)
			hooks.WarnMissingEgress(".", r, termlog.CLI)
			err = withRuntime(backendName, func(b runtimeplugin.Backend) error {
				if err := applyBake(&spec, r, b); err != nil {
					return err
				}
				// Create from derived/base if the sandbox VM is missing.
				if err := b.Create(spec); err != nil {
					return err
				}
				if err := b.Start(spec); err != nil {
					return err
				}
				if proxyOn && (proxyState.HTTPPort > 0 || proxyState.Port > 0) {
					if err := injectGuestProxyEnv(b, spec.ID, proxyState, r.Network.MITMEnabled()); err != nil {
						return err
					}
					if r.Network.LoggingEnabled() {
						probeSoftnetDrop(".", spec.ID, b)
					}
				}
				if err := hooks.RunHook(".", r.Runtime, runtimeplugin.HookOnStart, termlog.CLI, hooks.RunHookOpts{
					ConfigPath: r.Path,
					Backend:    b,
					VMID:       spec.ID,
				}); err != nil {
					return err
				}
				termlog.CLI("started %s", spec.ID)
				return nil
			})
			if err != nil && proxyOn {
				_ = network.StopDetachedProxy(".", spec.ID)
			}
			if err != nil || !follow {
				return err
			}
			termlog.CLI("following proxy log (ctrl-c to stop; VM keeps running)")
			return network.WriteTrafficFollow(network.ProxyLogPath(".", spec.ID), true, func(line string) {
				termlog.CLI("%s", line)
			})
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to cage yaml")
	cmd.Flags().StringVar(&id, "id", "", "VM name (default: cage-vm)")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "after start, follow proxy CONNECT log (needs network.proxy.logging)")
	return cmd
}

func newVMStopCmd() *cobra.Command {
	var (
		configPath string
		id         string
	)
	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop a VM",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, backendName, err := loadRuntime(cmd, configPath)
			if err != nil {
				return err
			}
			if id == "" {
				id = defaultVMID()
			}
			termlog.CLI("stop %s", id)
			_ = network.StopDetachedProxy(".", id)
			return withRuntime(backendName, func(b runtimeplugin.Backend) error {
				if err := b.Stop(id); err != nil {
					return err
				}
				termlog.CLI("stopped %s", id)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to cage yaml")
	cmd.Flags().StringVar(&id, "id", "", "VM name (default: cage-vm)")
	return cmd
}

func newVMStatusCmd() *cobra.Command {
	var (
		configPath string
		id         string
	)
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show VM status",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, backendName, err := loadRuntime(cmd, configPath)
			if err != nil {
				return err
			}
			if id == "" {
				id = defaultVMID()
			}
			return withRuntime(backendName, func(b runtimeplugin.Backend) error {
				st, err := b.Status(id)
				if err != nil {
					return err
				}
				fmt.Printf("%s\t%s\n", st.ID, st.State)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to cage yaml")
	cmd.Flags().StringVar(&id, "id", "", "VM name (default: cage-vm)")
	return cmd
}

func newVMDeleteCmd() *cobra.Command {
	var (
		configPath string
		id         string
	)
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a VM",
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, backendName, _, err := loadSpec(cmd, configPath, id)
			if err != nil {
				return err
			}
			termlog.CLI("delete %s", spec.ID)
			_ = network.StopDetachedProxy(".", spec.ID)
			return withRuntime(backendName, func(b runtimeplugin.Backend) error {
				if err := b.Delete(spec); err != nil {
					return err
				}
				termlog.CLI("deleted %s", spec.ID)
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to cage yaml")
	cmd.Flags().StringVar(&id, "id", "", "VM name (default: cage-vm)")
	return cmd
}

func newVMExecCmd() *cobra.Command {
	var (
		configPath string
		id         string
		tty        bool
	)
	cmd := &cobra.Command{
		Use:   "exec [-- argv...]",
		Short: "Run a command in the guest (no args = interactive login shell)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				// /var/lib/cage/shell always sources proxy.env (tart exec ≠ PAM login).
				args = []string{"/var/lib/cage/shell"}
				tty = true
			}
			r, backendName, err := loadRuntime(cmd, configPath)
			if err != nil {
				return err
			}
			if id == "" {
				id = defaultVMID()
			}
			_ = withRuntime(backendName, func(b runtimeplugin.Backend) error {
				return hooks.RunHook(".", r.Runtime, runtimeplugin.HookOnAttachShell, termlog.CLI, hooks.RunHookOpts{
					ConfigPath: r.Path,
					Backend:    b,
					VMID:       id,
				})
			})
			// TTY must use the host terminal FDs. go-plugin SyncStdout is a pipe,
			// which makes tart exec -t crash (ioctl TIOCGWINSZ).
			if tty {
				return execTTY(backendName, id, args)
			}
			return withRuntime(backendName, func(b runtimeplugin.Backend) error {
				return b.Exec(id, runtimeplugin.ExecOpts{Argv: args})
			})
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to cage yaml")
	cmd.Flags().StringVar(&id, "id", "", "VM name (default: cage-vm)")
	cmd.Flags().BoolVarP(&tty, "tty", "t", false, "allocate a pseudo-TTY (implied when no args)")
	return cmd
}

// execTTY runs an interactive guest command with the real host terminal attached.
func execTTY(backendName, id string, argv []string) error {
	if backendName != "tart" {
		return fmt.Errorf("interactive exec (-t) is only implemented for tart (got %s)", backendName)
	}
	args := append([]string{"exec", "-i", "-t", id}, argv...)
	c := exec.Command("tart", args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("tart exec: %w", err)
	}
	return nil
}

func newVMLogsCmd() *cobra.Command {
	var (
		configPath string
		id         string
		follow     bool
	)
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show host proxy CONNECT log (JSONL under .cage/run/<id>/proxy.log)",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = configPath
			if id == "" {
				id = defaultVMID()
			}
			path := network.ProxyLogPath(".", id)
			return network.WriteTrafficFollow(path, follow, func(line string) {
				termlog.CLI("%s", line)
			})
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "unused; kept for consistency")
	cmd.Flags().StringVar(&id, "id", "", "VM name (default: cage-vm)")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow new CONNECT events")
	return cmd
}

func loadSpec(cmd *cobra.Command, configPath, id string) (runtimeplugin.Spec, string, config.Resolved, error) {
	r, backendName, err := loadRuntime(cmd, configPath)
	if err != nil {
		return runtimeplugin.Spec{}, "", config.Resolved{}, err
	}
	if id == "" {
		id = defaultVMID()
	}
	spec := runtimeplugin.Spec{
		ID:        id,
		Image:     r.Runtime.Image,
		Workdir:   r.Runtime.Workdir,
		Graphics:  r.Runtime.Graphics,
		OnCreate:  append([]string{}, r.Runtime.OnCreate...),
		OnStart:   append([]string{}, r.Runtime.OnStart...),
		OnDestroy: append([]string{}, r.Runtime.OnDestroy...),
	}
	for _, m := range r.Mounts {
		spec.Mounts = append(spec.Mounts, runtimeplugin.PathSpec{
			Host: m.Host, Guest: m.Guest, Permission: m.Permission,
		})
	}
	for _, c := range r.Copies {
		spec.Copies = append(spec.Copies, runtimeplugin.PathSpec{
			Host: c.Host, Guest: c.Guest, Permission: c.Permission,
		})
	}
	return spec, backendName, r, nil
}

// applyBake collects before_bake attachments + seat bake scripts, then ensures derived image.
func applyBake(spec *runtimeplugin.Spec, r config.Resolved, b runtimeplugin.Backend) error {
	scripts, err := hooks.CollectBeforeBakeScripts(".", r.Runtime, termlog.CLI)
	if err != nil {
		return err
	}
	img, hash, err := bake.EnsureDerived(".", r.Runtime.Image, scripts, "linux", r.Runtime.Backend, b, r.Runtime.Workdir, termlog.CLI)
	if err != nil {
		return err
	}
	spec.Image = img
	if hash != "" {
		termlog.CLI("using derived image %s", img)
	}
	return nil
}

func startHostProxy(projectRoot, vmID string, r config.Resolved) (network.ProxyState, error) {
	bin, err := os.Executable()
	if err != nil {
		return network.ProxyState{}, err
	}
	var egressYAML []byte
	if r.Network.Plugins.Egress != nil {
		egressYAML, err = yaml.Marshal(r.Network.Plugins.Egress)
		if err != nil {
			return network.ProxyState{}, err
		}
		// Ensure plugin is installed before detaching.
		if _, _, err := pluginhost.ResolveCommand(projectRoot, "network", "egress"); err != nil {
			return network.ProxyState{}, fmt.Errorf("network/egress: %w (install with: cage plugin install -l ./plugins/network/egress)", err)
		}
	} else {
		egressYAML = []byte("{}\n")
	}
	denyHTTP, denyMsg := false, ""
	if r.Network.Plugins.Egress != nil {
		denyHTTP = r.Network.Plugins.Egress.DenyHTTPResponse()
		denyMsg = r.Network.Plugins.Egress.DenyHTTPMessage()
	}
	var httpProxyYAML []byte
	if r.Network.Plugins.HTTPProxy != nil && len(r.Network.Plugins.HTTPProxy.Endpoints) > 0 {
		httpProxyYAML, err = yaml.Marshal(r.Network.Plugins.HTTPProxy)
		if err != nil {
			return network.ProxyState{}, err
		}
		if _, _, err := pluginhost.ResolveCommand(projectRoot, "network", "http-proxy"); err != nil {
			return network.ProxyState{}, fmt.Errorf("network/http-proxy: %w (install with: cage plugin install -l ./plugins/network/http-proxy)", err)
		}
	}
	st, err := network.StartDetachedProxy(projectRoot, vmID, bin, network.StartDetachedProxyOpts{
		EgressYAML:    egressYAML,
		HTTPProxyYAML: httpProxyYAML,
		Logging:       r.Network.LoggingEnabled(),
		ConfigPath:    r.Path,
		DenyHTTP:      denyHTTP,
		DenyMessage:   denyMsg,
		Softnet:       true,
		MITM:          r.Network.MITMEnabled(),
	})
	if err != nil {
		return network.ProxyState{}, err
	}
	termlog.CLI("host HTTP proxy on port %d (socks %d, pid %d, mitm=%v)", st.HTTPPort, st.Port, st.PID, r.Network.MITMEnabled())
	if ports, err := network.ReadHTTPProxyState(projectRoot, vmID); err == nil && len(ports) > 0 {
		for name, p := range ports {
			termlog.CLI("http-proxy %s on port %d", name, p)
		}
	}
	if r.Network.LoggingEnabled() {
		termlog.CLI("proxy log %s (cage vm logs -f)", network.ProxyLogPath(projectRoot, vmID))
	}
	return st, nil
}

func injectGuestProxyEnv(b runtimeplugin.Backend, vmID string, st network.ProxyState, mitm bool) error {
	httpPort := st.HTTPPort
	if httpPort <= 0 {
		httpPort = st.Port
	}
	if mitm {
		mitmCA, err := network.LoadOrCreateCA(".")
		if err != nil {
			return fmt.Errorf("mitm ca: %w", err)
		}
		if err := b.Exec(vmID, runtimeplugin.ExecOpts{
			Argv:  []string{"sudo", "sh", "-s"},
			Stdin: []byte(network.GuestMITMCAInstallScript(mitmCA.CAPEM())),
		}); err != nil {
			return fmt.Errorf("guest MITM CA: %w", err)
		}
	}
	if err := b.Exec(vmID, runtimeplugin.ExecOpts{
		Argv:  []string{"sudo", "sh", "-s"},
		Stdin: []byte(network.GuestProxyInstallScript(httpPort)),
	}); err != nil {
		return fmt.Errorf("guest proxy env: %w", err)
	}
	ports, err := network.ReadHTTPProxyState(".", vmID)
	if err != nil || len(ports) == 0 {
		return nil
	}
	if err := b.Exec(vmID, runtimeplugin.ExecOpts{
		Argv:  []string{"sudo", "sh", "-s"},
		Stdin: []byte(network.GuestHTTPProxyInstallScript(ports)),
	}); err != nil {
		return fmt.Errorf("guest http-proxy env: %w", err)
	}
	return nil
}

// probeSoftnetDrop tries direct guest egress; under host-only softnet it should fail.
// Softnet does not emit per-packet events — this is a start-time visibility sample for proxy.log.
func probeSoftnetDrop(projectRoot, vmID string, b runtimeplugin.Backend) {
	err := b.Exec(vmID, runtimeplugin.ExecOpts{Argv: network.SoftnetProbeArgv()})
	ev := network.SoftnetProbeEvent(err == nil)
	_ = network.AppendProxyTraffic(projectRoot, vmID, ev)
	termlog.CLI("%s", network.FormatTrafficHuman(ev))
}

func loadRuntime(cmd *cobra.Command, configPath string) (config.Resolved, string, error) {
	path, err := config.Resolve(".", configPath)
	if err != nil {
		return config.Resolved{}, "", err
	}
	h := host.FromContext(cmd.Context())
	r, err := config.LoadResolved(".", path, h.GOOS)
	if err != nil {
		return config.Resolved{}, "", err
	}
	return r, r.Runtime.Backend, nil
}

func withRuntime(backendName string, fn func(runtimeplugin.Backend) error) error {
	cmdPath, _, err := pluginhost.ResolveCommand(".", "runtime", backendName)
	if err != nil {
		return err
	}
	client, err := pluginhost.DispenseRuntime(cmdPath)
	if err != nil {
		return err
	}
	defer client.Close()
	return fn(client.Backend)
}

func defaultVMID() string {
	if v := os.Getenv("CAGE_VM_ID"); v != "" {
		return v
	}
	return "cage-vm"
}
