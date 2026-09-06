package cli

import (
	"github.com/spf13/cobra"

	"github.com/appmatter/cage/internal/host"
)

// NewRoot builds the cage command tree.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "cage",
		Short:         "Host-side agent sandbox",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			cmd.SetContext(host.WithContext(cmd.Context(), host.Current()))
		},
	}
	root.AddCommand(newInitCmd(), newConfigCmd(), newPluginCmd(), newVMCmd(), newBakeCmd(), newCompletionCmd(), newProxyServeCmd(), newContextServeCmd())
	return root
}
