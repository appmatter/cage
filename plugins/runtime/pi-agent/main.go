package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-plugin"
	"gopkg.in/yaml.v3"

	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

func main() {
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: runtimeplugin.Handshake,
		Plugins:         runtimeplugin.HooksPluginMap(&PiAgent{}),
	})
}

// PiAgent is the pi coding-agent harness (bake + lifecycle hooks).
type PiAgent struct{}

func (p *PiAgent) Name() string { return "pi-agent" }

func (p *PiAgent) Hooks() []string {
	return []string{
		runtimeplugin.HookBeforeBake,
		runtimeplugin.HookOnStart,
		runtimeplugin.HookOnAttachShell,
	}
}

type seatConfig struct {
	Version   string   `yaml:"version"`
	Packages  []string `yaml:"packages"`
	NodeMajor int      `yaml:"node_major"`
	AgentDir  string   `yaml:"agent_dir"`
}

func (p *PiAgent) BeforeBake(ctx runtimeplugin.HookContext) ([]runtimeplugin.BakeAttachment, error) {
	cfg, err := parseSeat(ctx.SeatYAML)
	if err != nil {
		return nil, err
	}
	if cfg.Version == "" {
		cfg.Version = "latest"
	}
	if cfg.NodeMajor == 0 {
		cfg.NodeMajor = 22
	}
	if ctx.DryRun {
		return []runtimeplugin.BakeAttachment{{
			Name: "pi-dry",
			Body: []byte(dryBakeScript(cfg)),
		}}, nil
	}
	return []runtimeplugin.BakeAttachment{{
		Name: "install-pi",
		Body: []byte(fullBakeScript(cfg)),
	}}, nil
}

func (p *PiAgent) OnStart(ctx runtimeplugin.HookContext) error {
	return seedAgentDir(ctx.AgentDir)
}

func (p *PiAgent) OnAttachShell(ctx runtimeplugin.HookContext) error {
	return seedAgentDir(ctx.AgentDir)
}

func (p *PiAgent) Init(ctx runtimeplugin.HookContext) error {
	return seedAgentDir(ctx.AgentDir)
}

func seedAgentDir(dir string) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("pi-agent agent_dir: %w", err)
	}
	models := filepath.Join(dir, "models.json")
	if _, err := os.Stat(models); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	const body = `{
  "providers": {}
}
`
	if err := os.WriteFile(models, []byte(body), 0o644); err != nil {
		return err
	}
	readme := filepath.Join(dir, "README.md")
	if _, err := os.Stat(readme); os.IsNotExist(err) {
		_ = os.WriteFile(readme, []byte("# Pi agent config (host)\n\nEdit `models.json` here and commit. Synced to `~/.pi/agent` in the guest on `cage vm start`.\n\nDefault path: `.cage/plugins/runtime/pi-agent/`.\n"), 0o644)
	}
	return nil
}

func parseSeat(raw []byte) (seatConfig, error) {
	var cfg seatConfig
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return cfg, fmt.Errorf("pi-agent seat config: %w", err)
	}
	return cfg, nil
}

func dryBakeScript(cfg seatConfig) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\nset -eu\n")
	b.WriteString("# pi-agent dry-run bake (integration)\n")
	b.WriteString("sudo mkdir -p /var/lib/cage\n")
	fmt.Fprintf(&b, "echo 'pi-agent bake dry version=%s node=%d' | sudo tee /var/lib/cage/pi-agent-baked >/dev/null\n", cfg.Version, cfg.NodeMajor)
	for _, pkg := range cfg.Packages {
		fmt.Fprintf(&b, "echo %q | sudo tee -a /var/lib/cage/pi-agent-baked >/dev/null\n", pkg)
	}
	b.WriteString("sudo cat /var/lib/cage/pi-agent-baked\n")
	return b.String()
}

func fullBakeScript(cfg seatConfig) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\nset -eu\n")
	b.WriteString("# pi-agent before_bake: node, pi, packages\n")
	b.WriteString("export DEBIAN_FRONTEND=noninteractive\n")
	b.WriteString("export NEEDRESTART_MODE=a\n")
	b.WriteString("sudo mkdir -p /var/lib/cage\n")
	fmt.Fprintf(&b, "NODE_MAJOR=%d\n", cfg.NodeMajor)
	b.WriteString(`# Tools pi expects on PATH (avoid runtime GitHub downloads).
if ! command -v rg >/dev/null 2>&1 || { ! command -v fd >/dev/null 2>&1 && ! command -v fdfind >/dev/null 2>&1; }; then
  echo "pi-agent: installing fd + ripgrep"
  sudo apt-get update -y
  sudo apt-get install -y fd-find ripgrep
fi
if command -v fdfind >/dev/null 2>&1 && ! command -v fd >/dev/null 2>&1; then
  sudo ln -sf "$(command -v fdfind)" /usr/local/bin/fd
fi
command -v rg >/dev/null
command -v fd >/dev/null || command -v fdfind >/dev/null
`)
	b.WriteString(`if ! command -v node >/dev/null 2>&1; then
  echo "pi-agent: installing Node.js ${NODE_MAJOR}"
  sudo apt-get update -y
  sudo apt-get install -y ca-certificates curl gnupg
  sudo mkdir -p /etc/apt/keyrings
  curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | sudo gpg --dearmor -o /etc/apt/keyrings/nodesource.gpg
  echo "deb [signed-by=/etc/apt/keyrings/nodesource.gpg] https://deb.nodesource.com/node_${NODE_MAJOR}.x nodistro main" | sudo tee /etc/apt/sources.list.d/nodesource.list
  sudo apt-get update -y
  sudo apt-get install -y nodejs
fi
node -v
npm -v
`)
	fmt.Fprintf(&b, "PI_VERSION=%q\n", cfg.Version)
	// Prefer current package name; fall back for older pins.
	b.WriteString(`echo "pi-agent: installing pi-coding-agent@${PI_VERSION}"
PKG="@earendil-works/pi-coding-agent"
if [ "$PI_VERSION" = "latest" ]; then
  sudo npm install -g "$PKG" || sudo npm install -g @mariozechner/pi-coding-agent
else
  sudo npm install -g "${PKG}@${PI_VERSION}" || sudo npm install -g "@mariozechner/pi-coding-agent@${PI_VERSION}"
fi
hash -r 2>/dev/null || true
export PATH="/usr/bin:/usr/local/bin:$PATH"
if ! command -v pi >/dev/null 2>&1; then
  echo "pi-agent: pi binary missing after npm install" >&2
  npm root -g
  ls "$(npm root -g)/@earendil-works" "$(npm root -g)/@mariozechner" 2>/dev/null || true
  ls -la "$(npm bin -g 2>/dev/null || echo /usr/bin)" 2>/dev/null | head
  exit 1
fi
pi --version
sync
`)
	for _, pkg := range cfg.Packages {
		pkg = strings.TrimSpace(pkg)
		if pkg == "" {
			continue
		}
		fmt.Fprintf(&b, "echo 'pi-agent: pi install %s'\n", pkg)
		fmt.Fprintf(&b, "pi install %q\n", pkg)
	}
	b.WriteString("echo pi-agent-bake-ok | sudo tee /var/lib/cage/pi-agent-baked >/dev/null\nsync\n")
	return b.String()
}
