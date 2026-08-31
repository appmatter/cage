package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/appmatter/cage/internal/climenu"
	"github.com/appmatter/cage/internal/config"
	"github.com/appmatter/cage/internal/hooks"
	"github.com/appmatter/cage/internal/host"
	"github.com/appmatter/cage/internal/pluginhost"
	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

func newPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Install and manage plugins",
	}
	cmd.AddCommand(newPluginInstallCmd(), newPluginListCmd(), newPluginRemoveCmd(), newPluginInitCmd())
	return cmd
}

func newPluginInstallCmd() *cobra.Command {
	var (
		project bool
		kind    string
		name    string
	)
	cmd := &cobra.Command{
		Use:   "install [source]",
		Short: "Install from plugins.lock.json, or add a plugin from git/local source",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return pluginhost.InstallFromLock(".")
			}
			m, err := pluginhost.Install(pluginhost.InstallOptions{
				Source:      args[0],
				Project:     project,
				ProjectRoot: ".",
				Kind:        kind,
				Name:        name,
			})
			if err != nil {
				return err
			}
			fmt.Printf("installed %s/%s pin=%s\n", m.Kind, m.Name, m.Pin)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&project, "project", "l", false, "install under .cage/.cache/plugins and update plugins.lock.json")
	cmd.Flags().StringVar(&kind, "kind", "", "plugin kind/context (e.g. runtime, network)")
	cmd.Flags().StringVar(&name, "name", "", "local install name (alias); required when short name would collide")
	return cmd
}

func newPluginListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List installed plugins",
		RunE: func(cmd *cobra.Command, args []string) error {
			list, err := pluginhost.ListManifests(".")
			if err != nil {
				return err
			}
			if len(list) == 0 {
				fmt.Println("no plugins installed")
				return nil
			}
			for _, m := range list {
				extra := ""
				if len(m.Commands) > 0 {
					extra = "\tcommands=" + strings.Join(m.Commands, ",")
				}
				fmt.Printf("%s/%s\t%s\tpin=%s%s\n", m.Kind, m.Name, m.Source, m.Pin, extra)
			}
			return nil
		},
	}
}

func newPluginRemoveCmd() *cobra.Command {
	var project bool
	cmd := &cobra.Command{
		Use:   "remove <kind>/<name>",
		Short: "Remove an installed plugin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind, name, ok := strings.Cut(args[0], "/")
			if !ok || kind == "" || name == "" {
				return fmt.Errorf("expected kind/name")
			}
			return pluginhost.Remove(project, ".", kind, name)
		},
	}
	cmd.Flags().BoolVarP(&project, "project", "l", false, "remove from .cage/.cache/plugins and plugins.lock.json")
	return cmd
}

func newPluginInitCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "init [kind/name|name]",
		Short: "Seed project artefacts (interactive multi-select, or pass kind/name)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := ""
			if len(args) == 1 {
				arg = args[0]
			}
			return runPluginInit(cmd, ".", configPath, arg)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to cage yaml (for seat agent_dir)")
	return cmd
}

func runPluginInit(cmd *cobra.Command, projectRoot, configPath, arg string) error {
	targets, err := resolveInitTargets(projectRoot, arg)
	if err != nil {
		return err
	}
	goos := host.FromContext(cmd.Context()).GOOS
	for _, m := range targets {
		if err := initOnePlugin(goos, projectRoot, configPath, m); err != nil {
			return err
		}
	}
	return nil
}

func initOnePlugin(goos, projectRoot, configPath string, m pluginhost.Manifest) error {
	if m.Kind != "runtime" {
		return fmt.Errorf("plugin init is only supported for runtime hooks plugins (got %s/%s)", m.Kind, m.Name)
	}

	hs := config.HarnessSeat{Seat: m.Name, PluginID: m.Name}
	cfgPath := ""
	r, path, err := loadOptionalConfig(goos, projectRoot, configPath)
	if err != nil {
		return err
	}
	if path != "" {
		cfgPath = path
		for _, seat := range r.Runtime.HarnessSeats {
			if seat.PluginID == m.Name || seat.Seat == m.Name {
				hs = seat
				break
			}
		}
	}
	agentDir := hooks.ResolveAgentDir(projectRoot, hs)

	cmdPath, _, err := pluginhost.ResolveCommand(projectRoot, m.Kind, m.Name)
	if err != nil {
		return err
	}
	client, err := pluginhost.DispenseRuntimeHooks(cmdPath)
	if err != nil {
		return err
	}
	defer client.Close()

	ctx := runtimeplugin.HookContext{
		Seat:        hs.Seat,
		ProjectRoot: projectRoot,
		ConfigPath:  cfgPath,
		AgentDir:    agentDir,
		SeatYAML:    hs.SeatYAML,
	}
	if err := client.Hooks.Init(ctx); err != nil {
		return err
	}
	fmt.Printf("initialized %s/%s → %s\n", m.Kind, m.Name, agentDir)
	return nil
}

func loadOptionalConfig(goos, projectRoot, configPath string) (config.Resolved, string, error) {
	path := configPath
	if path == "" {
		yamls, err := config.ListConfigs(projectRoot)
		if err != nil || len(yamls) != 1 {
			return config.Resolved{}, "", nil // no config context; use defaults
		}
		path = yamls[0]
	}
	r, err := config.LoadResolved(projectRoot, path, goos)
	if err != nil {
		return config.Resolved{}, "", err
	}
	return r, path, nil
}

func listInitCandidates(projectRoot string) ([]pluginhost.Manifest, error) {
	list, err := pluginhost.ListManifests(projectRoot)
	if err != nil {
		return nil, err
	}
	var candidates []pluginhost.Manifest
	for _, m := range list {
		if m.HasCommand(runtimeplugin.CommandInit) {
			candidates = append(candidates, m)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no installed plugin advertises init (reinstall with plugin.json commands)")
	}
	return candidates, nil
}

func resolveInitTargets(projectRoot, arg string) ([]pluginhost.Manifest, error) {
	candidates, err := listInitCandidates(projectRoot)
	if err != nil {
		return nil, err
	}

	if arg == "" {
		return selectInitPlugins(candidates)
	}

	if kind, name, ok := strings.Cut(arg, "/"); ok {
		for _, m := range candidates {
			if m.Kind == kind && m.Name == name {
				return []pluginhost.Manifest{m}, nil
			}
		}
		return nil, fmt.Errorf("plugin %s does not advertise init (or not installed)", arg)
	}

	var matches []pluginhost.Manifest
	for _, m := range candidates {
		if m.Name == arg {
			matches = append(matches, m)
		}
	}
	switch len(matches) {
	case 1:
		return matches, nil
	case 0:
		return nil, fmt.Errorf("no init plugin named %q", arg)
	default:
		return nil, fmt.Errorf("ambiguous %q; pass kind/name", arg)
	}
}

func selectInitPlugins(candidates []pluginhost.Manifest) ([]pluginhost.Manifest, error) {
	byKey := make(map[string]pluginhost.Manifest, len(candidates))
	items := make([]climenu.Item, 0, len(candidates))
	for _, m := range candidates {
		key := m.Kind + "/" + m.Name
		byKey[key] = m
		items = append(items, climenu.Item{Value: key, Label: key})
	}
	picked, err := climenu.Multi("Plugins to init", items)
	if err != nil {
		if !climenu.IsTTY() {
			return nil, fmt.Errorf("pass kind/name (non-interactive)")
		}
		return nil, err
	}
	out := make([]pluginhost.Manifest, 0, len(picked))
	for _, key := range picked {
		m, ok := byKey[key]
		if !ok {
			return nil, fmt.Errorf("unknown selection %q", key)
		}
		out = append(out, m)
	}
	return out, nil
}
