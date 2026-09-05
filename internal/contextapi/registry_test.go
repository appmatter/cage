package contextapi

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const registryRuntime = `
version: 1
runtime:
  plugins:
    tart:
      priority: 1
      image: ubuntu
    incus:
      priority: 2
      image: ubuntu/24.04
    hyperv:
      priority: 3
      image: Ubuntu
  workdir: /workspace
fs:
  layout:
    mode: flat
`

func writeRegistryProject(t *testing.T, root, name, body string) string {
	t.Helper()
	cage := filepath.Join(root, ".cage")
	if err := os.MkdirAll(cage, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cage, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadRegistryOK(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	writeRegistryProject(t, a, "cage.yaml", registryRuntime)
	writeRegistryProject(t, b, "cage.yaml", registryRuntime)

	reg, err := LoadRegistry(RegistryFile{Instances: []RegistryFileEntry{
		{ID: "alpha", Project: a, Config: ".cage/cage.yaml"},
		{ID: "beta", Project: b, Config: ".cage/cage.yaml"},
	}}, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	alpha, ok := reg.Get("alpha")
	if !ok || alpha.ProjectRoot != mustAbs(t, a) || alpha.Resolved.Runtime.Workdir == "" {
		t.Fatalf("alpha: %#v ok=%v", alpha, ok)
	}
	if _, ok := reg.Get("missing"); ok {
		t.Fatal("missing should be absent")
	}
}

func TestLoadRegistryFile(t *testing.T) {
	project := t.TempDir()
	writeRegistryProject(t, project, "cage.yaml", registryRuntime)
	regPath := filepath.Join(t.TempDir(), "registry.yaml")
	body := "instances:\n  - id: one\n    project: " + project + "\n    config: .cage/cage.yaml\n"
	if err := os.WriteFile(regPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := LoadRegistryFile(regPath, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("one"); !ok {
		t.Fatal("one missing")
	}
}

func TestLoadRegistryRejects(t *testing.T) {
	project := t.TempDir()
	writeRegistryProject(t, project, "cage.yaml", registryRuntime)

	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "escape.yaml")
	if err := os.WriteFile(outside, []byte(registryRuntime), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		file RegistryFile
		want string
	}{
		{"empty", RegistryFile{}, "no instances"},
		{"dup", RegistryFile{Instances: []RegistryFileEntry{
			{ID: "a", Project: project, Config: ".cage/cage.yaml"},
			{ID: "a", Project: project, Config: ".cage/cage.yaml"},
		}}, "duplicate"},
		{"bad-id", RegistryFile{Instances: []RegistryFileEntry{
			{ID: "../x", Project: project, Config: ".cage/cage.yaml"},
		}}, "invalid id"},
		{"missing-config", RegistryFile{Instances: []RegistryFileEntry{
			{ID: "a", Project: project, Config: ".cage/missing.yaml"},
		}}, "config"},
		{"escape", RegistryFile{Instances: []RegistryFileEntry{
			{ID: "a", Project: project, Config: outside},
		}}, "escapes"},
		{"invalid-resolved", RegistryFile{Instances: []RegistryFileEntry{
			{ID: "a", Project: project, Config: writeRegistryProject(t, project, "cage.bad.yaml", "version: 1\nextends: missing-base.yaml\n")},
		}}, "config"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadRegistry(tc.file, runtime.GOOS)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want substring %q", err, tc.want)
			}
		})
	}
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}
