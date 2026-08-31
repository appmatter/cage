package host

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
)

type ctxKey struct{}

// Info is the host environment for CLI decisions (backend OS, shell).
type Info struct {
	GOOS  string
	Shell string // empty if unknown / unsupported
}

// Current returns GOOS (CAGE_GOOS override) and best-effort shell from $SHELL.
func Current() Info {
	goos := goruntime.GOOS
	if v := os.Getenv("CAGE_GOOS"); v != "" {
		goos = v
	}
	shell, _ := DetectShell()
	return Info{GOOS: goos, Shell: shell}
}

// DetectShell returns bash, zsh, or fish from $SHELL.
func DetectShell() (string, error) {
	raw := os.Getenv("SHELL")
	base := filepath.Base(raw)
	switch base {
	case "bash", "zsh", "fish":
		return base, nil
	default:
		return "", fmt.Errorf("could not detect supported shell from $SHELL=%q; pass --shell bash|zsh|fish", raw)
	}
}

// WithContext stores Info on ctx.
func WithContext(ctx context.Context, info Info) context.Context {
	return context.WithValue(ctx, ctxKey{}, info)
}

// FromContext returns Info, or Current() if missing.
func FromContext(ctx context.Context) Info {
	if ctx == nil {
		return Current()
	}
	if info, ok := ctx.Value(ctxKey{}).(Info); ok {
		return info
	}
	return Current()
}
