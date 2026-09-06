package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/appmatter/cage/internal/bake"
	"github.com/appmatter/cage/internal/climenu"
	"github.com/appmatter/cage/internal/termlog"
	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

func newBakeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bake",
		Short: "Manage derived bake images (hash cache + backend image)",
	}
	addProjectFlag(cmd)
	cmd.AddCommand(newBakeListCmd(), newBakeDeleteCmd())
	return cmd
}

func newBakeListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List derived bake images and host hash stamps",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := absProjectRoot(cmd)
			if err != nil {
				return err
			}
			ents, err := bake.List(root)
			if err != nil {
				return err
			}
			if len(ents) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "(none)")
				return nil
			}
			for _, e := range ents {
				ok := "incomplete"
				if e.OK {
					ok = "ok"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\tbase=%s\n", e.Short, e.Image, ok, e.Base)
			}
			return nil
		},
	}
}

func newBakeDeleteCmd() *cobra.Command {
	var (
		all        bool
		configPath string
	)
	cmd := &cobra.Command{
		Use:   "delete [hash|cage-bake-…]…",
		Short: "Delete derived bake image(s) and .cage/.cache/images hash stamps",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if all && len(args) > 0 {
				return fmt.Errorf("pass either --all or id(s), not both")
			}

			_, backendName, projectRoot, err := loadRuntimeScope(cmd, configPath)
			if err != nil {
				return err
			}

			return withRuntime(projectRoot, backendName, func(b runtimeplugin.Backend) error {
				deleteAll := func() error {
					ents, err := bake.List(projectRoot)
					if err != nil {
						return err
					}
					if len(ents) == 0 {
						termlog.CLI("no bake images")
						return nil
					}
					for _, e := range ents {
						termlog.CLI("delete bake %s (%s)", e.Short, e.Image)
						if err := bake.RemoveDerived(projectRoot, e, b); err != nil {
							return err
						}
					}
					termlog.CLI("deleted %d bake image(s)", len(ents))
					return nil
				}

				if all {
					return deleteAll()
				}

				var targets []bake.Entry
				if len(args) > 0 {
					for _, a := range args {
						e, err := bake.ResolveEntry(projectRoot, a)
						if err != nil {
							return err
						}
						targets = append(targets, e)
					}
				} else {
					targets, err = selectBakeEntries(projectRoot)
					if err != nil {
						return err
					}
				}
				for _, e := range targets {
					termlog.CLI("delete bake %s (%s)", e.Short, e.Image)
					if err := bake.RemoveDerived(projectRoot, e, b); err != nil {
						return err
					}
					termlog.CLI("deleted %s", e.Image)
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "delete every bake image and hash stamp")
	cmd.Flags().StringVar(&configPath, "config", "", "path to cage yaml (for backend plugin)")
	return cmd
}

func selectBakeEntries(projectRoot string) ([]bake.Entry, error) {
	ents, err := bake.List(projectRoot)
	if err != nil {
		return nil, err
	}
	if len(ents) == 0 {
		return nil, fmt.Errorf("no bake images (cage bake list)")
	}
	byKey := make(map[string]bake.Entry, len(ents))
	items := make([]climenu.Item, 0, len(ents))
	for _, e := range ents {
		ok := "incomplete"
		if e.OK {
			ok = "ok"
		}
		label := fmt.Sprintf("%s  %s  %s  base=%s", e.Short, e.Image, ok, e.Base)
		byKey[e.Short] = e
		items = append(items, climenu.Item{Value: e.Short, Label: label})
	}
	picked, err := climenu.Multi("Bake images to delete", items)
	if err != nil {
		if !climenu.IsTTY() {
			return nil, fmt.Errorf("pass an id or --all (non-interactive)")
		}
		return nil, err
	}
	out := make([]bake.Entry, 0, len(picked))
	for _, key := range picked {
		e, ok := byKey[key]
		if !ok {
			return nil, fmt.Errorf("unknown selection %q", key)
		}
		out = append(out, e)
	}
	return out, nil
}
