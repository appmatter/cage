package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/appmatter/cage/internal/host"
)

const testRuntime = `
runtime:
  plugins:
    tart:
      priority: 1
      image: img:tart
    incus:
      priority: 2
      image: img:incus
    hyperv:
      priority: 3
      image: img:hyperv
  workdir: /workspace
`

func TestConfigInspect(t *testing.T) {
	root := t.TempDir()
	cageDir := filepath.Join(root, ".cage")
	if err := os.MkdirAll(cageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(cageDir, "cage.yaml")
	if err := os.WriteFile(base, []byte(`
version: 1
`+testRuntime+`
fs:
  layout:
    mode: flat
  mount:
    src: ./src
    tests:
      host: ./tests
      permission: ro
  copy:
    .env: .env.example
  deny:
    - .git
    - .env
secrets:
  plugins:
    onepassword:
      vars:
        OPENAI_API_KEY: op://x
    organization-op:
      plugin: onepassword
      account: company.1password.com
      vars:
        ANTHROPIC_API_KEY: op://y
`), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := host.WithContext(context.Background(), host.Info{GOOS: "darwin"})
	var buf bytes.Buffer
	if err := runConfigInspect(ctx, root, base, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"goos:\tdarwin",
		"backend:\ttart",
		"image:\timg:tart",
		"layout:\tflat",
		filepath.Join(root, "src") + " → /workspace/src",
		filepath.Join(root, "tests") + " → /workspace/tests\t(ro)",
		filepath.Join(root, ".env.example") + " → /workspace/.env",
		".git",
		"onepassword\tplugin=onepassword\tvars=OPENAI_API_KEY",
		"organization-op\tplugin=onepassword\taccount=company.1password.com\tvars=ANTHROPIC_API_KEY",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestConfigInspectProfileMerge(t *testing.T) {
	root := t.TempDir()
	cageDir := filepath.Join(root, ".cage")
	if err := os.MkdirAll(cageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cageDir, "cage.yaml"), []byte(`
version: 1
`+testRuntime+`
fs:
  layout:
    mode: flat
  mount:
    src: ./src
    tests: ./tests
  deny:
    - .git
`), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := filepath.Join(cageDir, "cage.docs.yaml")
	if err := os.WriteFile(profile, []byte(`
extends: cage.yaml
fs:
  mount:
    docs: ./docs
    tests:
      remove: true
  deny:
    - node_modules
`), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := host.WithContext(context.Background(), host.Info{GOOS: "darwin"})
	var buf bytes.Buffer
	if err := runConfigInspect(ctx, root, profile, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "/workspace/tests") {
		t.Fatalf("tests should be removed:\n%s", out)
	}
	if !strings.Contains(out, filepath.Join(root, "docs")+" → /workspace/docs") {
		t.Fatalf("docs missing:\n%s", out)
	}
	if !strings.Contains(out, "node_modules") {
		t.Fatalf("deny merge missing:\n%s", out)
	}
}
