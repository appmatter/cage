package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/hashicorp/go-plugin"
	"gopkg.in/yaml.v3"

	secretsplugin "github.com/appmatter/cage/pkg/plugin/v1/secrets"
)

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: secretsplugin.Handshake,
		Plugins:         secretsplugin.PluginMap(&OnePassword{}),
	})
}

// OnePassword resolves op:// refs via the 1Password CLI (`op read`).
type OnePassword struct {
	mu      sync.Mutex
	account string
	app     bool // desktop CLI integration; default true
}

type configYAML struct {
	Account string `yaml:"account,omitempty"`
	App     *bool  `yaml:"app,omitempty"` // omit/true = desktop app CLI integration
}

func (o *OnePassword) Name() string { return "onepassword" }

func (o *OnePassword) Configure(raw []byte) error {
	var cfg configYAML
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return fmt.Errorf("onepassword configure: %w", err)
		}
	}
	app := true
	if cfg.App != nil {
		app = *cfg.App
	}
	o.mu.Lock()
	o.account = strings.TrimSpace(cfg.Account)
	o.app = app
	o.mu.Unlock()
	return nil
}

func (o *OnePassword) Resolve(refs map[string]string) (map[string]string, error) {
	o.mu.Lock()
	account := o.account
	app := o.app
	o.mu.Unlock()

	if _, err := exec.LookPath("op"); err != nil {
		return nil, fmt.Errorf("onepassword: op CLI not on PATH")
	}

	out := make(map[string]string, len(refs))
	for name, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return nil, fmt.Errorf("onepassword: %s: empty ref", name)
		}
		val, err := opRead(ref, account, app)
		if err != nil {
			return nil, fmt.Errorf("onepassword: %s (%s): %w", name, ref, err)
		}
		out[name] = val
	}
	return out, nil
}

func opRead(ref, account string, app bool) (string, error) {
	args := []string{"read"}
	if account != "" {
		args = append(args, "--account", account)
	}
	args = append(args, ref)
	cmd := exec.Command("op", args...)
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Env = opEnv(os.Environ(), app)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stdout.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return strings.TrimRight(stdout.String(), "\r\n"), nil
}

// opEnv applies seat app preference from cage.yaml.
func opEnv(base []string, app bool) []string {
	out := make([]string, 0, len(base)+1)
	for _, e := range base {
		switch {
		case strings.HasPrefix(e, "OP_BIOMETRIC_UNLOCK_ENABLED="):
			continue
		case !app && strings.HasPrefix(e, "OP_LOAD_DESKTOP_APP_SETTINGS="):
			continue
		default:
			out = append(out, e)
		}
	}
	if app {
		out = append(out, "OP_BIOMETRIC_UNLOCK_ENABLED=true")
	} else {
		out = append(out, "OP_BIOMETRIC_UNLOCK_ENABLED=false")
		out = append(out, "OP_LOAD_DESKTOP_APP_SETTINGS=false")
	}
	return out
}
