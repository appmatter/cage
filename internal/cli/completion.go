package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/appmatter/cage/internal/host"
)

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate or install shell completion",
	}
	cmd.AddCommand(newCompletionGenerateCmd(), newCompletionInstallCmd())
	return cmd
}

func newCompletionGenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:                   "generate [bash|zsh|fish|powershell]",
		Short:                 "Print completion script to stdout",
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			return generateCompletion(cmd.Root(), args[0], os.Stdout)
		},
	}
	return cmd
}

func newCompletionInstallCmd() *cobra.Command {
	var shell string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Write completion for the current shell",
		RunE: func(cmd *cobra.Command, args []string) error {
			if shell == "" {
				h := host.FromContext(cmd.Context())
				if h.Shell == "" {
					return fmt.Errorf("could not detect supported shell from $SHELL; pass --shell bash|zsh|fish")
				}
				shell = h.Shell
			}
			path, hint, err := installCompletion(cmd.Root(), shell)
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "wrote %s\n", path)
			fmt.Fprintf(os.Stderr, "reload: %s\n", hint)
			return nil
		},
	}
	cmd.Flags().StringVar(&shell, "shell", "", "bash|zsh|fish (default: detect from $SHELL)")
	return cmd
}

func generateCompletion(root *cobra.Command, shell string, w io.Writer) error {
	switch shell {
	case "bash":
		return root.GenBashCompletion(w)
	case "zsh":
		return root.GenZshCompletion(w)
	case "fish":
		return root.GenFishCompletion(w, true)
	case "powershell":
		return root.GenPowerShellCompletionWithDesc(w)
	default:
		return fmt.Errorf("unsupported shell %q", shell)
	}
}

type completionInstall struct {
	dirUnderHome string
	filename     string
	reloadHint   string // may use %s for installed path
}

var completionInstalls = map[string]completionInstall{
	"zsh": {
		dirUnderHome: filepath.Join(".zsh", "completions"),
		filename:     "_cage",
		reloadHint:   "exec zsh",
	},
	"bash": {
		dirUnderHome: filepath.Join(".local", "share", "bash-completion", "completions"),
		filename:     "cage",
		reloadHint:   "source %s",
	},
	"fish": {
		dirUnderHome: filepath.Join(".config", "fish", "completions"),
		filename:     "cage.fish",
		reloadHint:   "source %s",
	},
}

func installCompletion(root *cobra.Command, shell string) (path, hint string, err error) {
	spec, ok := completionInstalls[shell]
	if !ok {
		return "", "", fmt.Errorf("unsupported shell %q; pass --shell bash|zsh|fish", shell)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(home, spec.dirUnderHome)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", err
	}
	path = filepath.Join(dir, spec.filename)
	f, err := os.Create(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	if err := generateCompletion(root, shell, f); err != nil {
		return "", "", err
	}
	hint = spec.reloadHint
	if spec.reloadHint == "source %s" {
		hint = fmt.Sprintf(spec.reloadHint, path)
	}
	return path, hint, nil
}
