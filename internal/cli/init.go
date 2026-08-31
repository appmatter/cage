package cli

import (
	"github.com/spf13/cobra"

	"github.com/appmatter/cage/internal/initcmd"
)

func newInitCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create .cage/ (config, plugins.lock.json, .gitignore)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return initcmd.Run(initcmd.Options{ProjectRoot: ".", Force: force})
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing files")
	return cmd
}
