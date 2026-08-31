package initcmd

import (
	"fmt"
	"os"
	"path/filepath"
)

const gitignore = `# Host-only artefacts (plugin binaries, bake stamps, MITM CA)
.cache/
# Host proxy / VM runtime state (pid, logs)
run/
*.cageplugin
`

const cageYAML = `version: 1

runtime:
  plugins:
    tart:
      priority: 1
      image: ghcr.io/cirruslabs/ubuntu:latest
      graphics: false
      # bake: [.cage/scripts/bake.sh]  # derived image; see docs/plugins/runtime-image-bake.md
    incus:
      priority: 2
      image: ubuntu/24.04
    hyperv:
      priority: 3
      image: Ubuntu
  workdir: /workspace
  env: {}

fs:
  layout:
    mode: flat
  mount: {}
  copy: {}
  deny:
    - .git
    - .env
    - .ssh
    - credentials
    - .cage
    - .cage/cage.yaml
    - .cage/cage.*.yaml
    - "**/*.pem"
    - "**/*.key"
  plugins:
    mention:
      include:
        - "**/*"
      exclude:
        - "**/.git/**"
        - "**/.cage"
        - "**/.cage/**"
        - "**/cage.*.yaml"

secrets: {}

network:
  plugins: {}
`

const emptyLock = `{
  "plugins": []
}
`

// Options for project init.
type Options struct {
	ProjectRoot string
	Force       bool
}

// Run creates .cage/ with starter config, plugins.lock.json, and .gitignore.
func Run(opts Options) error {
	root := opts.ProjectRoot
	if root == "" {
		root = "."
	}
	dir := filepath.Join(root, ".cage")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "run"), 0o755); err != nil {
		return err
	}

	writes := []struct {
		path string
		data string
	}{
		{filepath.Join(dir, ".gitignore"), gitignore},
		{filepath.Join(dir, "plugins.lock.json"), emptyLock},
		{filepath.Join(dir, "cage.yaml"), cageYAML},
	}
	for _, w := range writes {
		if !opts.Force {
			if _, err := os.Stat(w.path); err == nil {
				continue
			}
		}
		if err := os.WriteFile(w.path, []byte(w.data), 0o644); err != nil {
			return err
		}
		fmt.Println("wrote", w.path)
	}
	return nil
}
