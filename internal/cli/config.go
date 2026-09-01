package cli

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"

	"github.com/appmatter/cage/internal/bake"
	"github.com/appmatter/cage/internal/config"
	"github.com/appmatter/cage/internal/host"
	"github.com/appmatter/cage/internal/hooks"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect and work with cage configs",
	}
	cmd.AddCommand(newConfigInspectCmd())
	return cmd
}

func newConfigInspectCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Show resolved runtime and fs (mounts, copies, denies)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigInspect(cmd.Context(), ".", configPath, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to cage yaml")
	return cmd
}

func runConfigInspect(ctx context.Context, projectRoot, configPath string, w io.Writer) error {
	path, err := config.Resolve(projectRoot, configPath)
	if err != nil {
		return err
	}
	h := host.FromContext(ctx)
	r, err := config.LoadResolved(projectRoot, path, h.GOOS)
	if err != nil {
		return err
	}
	_ = hooks.Resolve(projectRoot, &r.Runtime)
	hooks.WarnMissingEgress(projectRoot, r, func(format string, args ...any) {
		fmt.Fprintf(w, format+"\n", args...)
	})
	formatInspect(w, projectRoot, r)
	return nil
}

func formatInspect(w io.Writer, projectRoot string, r config.Resolved) {
	fmt.Fprintf(w, "config:\t%s\n", r.Path)
	fmt.Fprintf(w, "goos:\t%s\n", r.Runtime.GOOS)
	fmt.Fprintf(w, "backend:\t%s\n", r.Runtime.Backend)
	fmt.Fprintf(w, "image:\t%s\n", r.Runtime.Image)
	fmt.Fprintf(w, "graphics:\t%v\n", r.Runtime.Graphics)
	fmt.Fprintf(w, "workdir:\t%s\n", r.Runtime.Workdir)
	fmt.Fprintln(w, "on-create:")
	writeScriptList(w, r.Runtime.OnCreate)
	fmt.Fprintln(w, "on-start:")
	writeScriptList(w, r.Runtime.OnStart)
	fmt.Fprintln(w, "on-destroy:")
	writeScriptList(w, r.Runtime.OnDestroy)
	fmt.Fprintln(w, "bake:")
	writeScriptList(w, r.Runtime.Bake)
	if len(r.Runtime.Bake) > 0 {
		h, err := bake.Hash(bake.HashInputs{
			BaseImage: r.Runtime.Image,
			Backend:   r.Runtime.Backend,
			Scripts:   r.Runtime.Bake,
			GuestGOOS: "linux",
		})
		if err != nil {
			fmt.Fprintf(w, "bake-derived:\t(error: %v)\n", err)
		} else {
			fmt.Fprintf(w, "bake-derived:\t%s\n", bake.DerivedName(h))
		}
	}
	fmt.Fprintln(w, "hooks:")
	writeResolvedHooks(w, r.Runtime)
	fmt.Fprintln(w, "harness:")
	if len(r.Runtime.HarnessSeats) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, hs := range r.Runtime.HarnessSeats {
		fmt.Fprintf(w, "  %s\tplugin=%s", hs.Seat, hs.PluginID)
		if hs.Version != "" {
			fmt.Fprintf(w, "\tversion=%s", hs.Version)
		}
		if len(hs.Packages) > 0 {
			fmt.Fprintf(w, "\tpackages=%v", hs.Packages)
		}
		agentDir := hooks.ResolveAgentDir(projectRoot, hs)
		fmt.Fprintf(w, "\tagent_dir=%s", agentDir)
		fmt.Fprintln(w)
	}
	if hints := hooks.CollectEgressHints(projectRoot, r.Runtime); len(hints) > 0 {
		fmt.Fprintln(w, "egress-hints:")
		for _, h := range hints {
			if h.Reason != "" {
				fmt.Fprintf(w, "  %s\t(%s)\n", h.Host, h.Reason)
			} else {
				fmt.Fprintf(w, "  %s\n", h.Host)
			}
		}
	}
	fmt.Fprintf(w, "layout:\t%s\n", r.Layout.Mode)
	fmt.Fprintln(w, "\nmounts:")
	if len(r.Mounts) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, m := range r.Mounts {
		fmt.Fprintf(w, "  %s\t%s → %s\t(%s)\n", m.Target, m.Host, m.Guest, m.Permission)
	}
	fmt.Fprintln(w, "\ncopies:")
	if len(r.Copies) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, c := range r.Copies {
		fmt.Fprintf(w, "  %s\t%s → %s\t(%s)\n", c.Target, c.Host, c.Guest, c.Permission)
	}
	fmt.Fprintln(w, "\ndeny:")
	if len(r.Deny) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, d := range r.Deny {
		fmt.Fprintf(w, "  %s\n", d)
	}
	if len(r.DenyMasks) > 0 {
		fmt.Fprintln(w, "\ndeny masks (guest):")
		for _, g := range r.DenyMasks {
			fmt.Fprintf(w, "  %s\n", g)
		}
	}
}

func writeResolvedHooks(w io.Writer, rt config.Runtime) {
	if len(rt.ResolvedHooks) == 0 {
		fmt.Fprintln(w, "  (none)")
		return
	}
	events := make([]string, 0, len(rt.ResolvedHooks))
	for e := range rt.ResolvedHooks {
		events = append(events, e)
	}
	sort.Strings(events)
	for _, e := range events {
		fmt.Fprintf(w, "  %s:\t%s\n", e, stringsJoin(rt.ResolvedHooks[e]))
	}
}

func stringsJoin(ss []string) string {
	if len(ss) == 0 {
		return "(none)"
	}
	out := ss[0]
	for i := 1; i < len(ss); i++ {
		out += ", " + ss[i]
	}
	return out
}

func writeScriptList(w io.Writer, paths []string) {
	if len(paths) == 0 {
		fmt.Fprintln(w, "  (none)")
		return
	}
	for _, p := range paths {
		fmt.Fprintf(w, "  %s\n", p)
	}
}
