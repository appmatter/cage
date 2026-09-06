package contextapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const projectRuntime = `
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

func writeProjectConfig(t *testing.T, root, name, body string) string {
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

func TestLoadProjectOK(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "cage.yaml", projectRuntime)
	writeProjectConfig(t, root, "cage.dogfood.yaml", projectRuntime)

	project, err := LoadProject(root, []string{".cage/cage.yaml", ".cage/cage.dogfood.yaml"}, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	if project.Root != mustAbs(t, root) {
		t.Fatalf("root: %s", project.Root)
	}
	def, ok := project.Get("default")
	if !ok || def.ProjectRoot != mustAbs(t, root) || def.Resolved.Runtime.Workdir == "" {
		t.Fatalf("default: %#v ok=%v", def, ok)
	}
	dog, ok := project.Get("dogfood")
	if !ok || dog.ConfigPath == def.ConfigPath {
		t.Fatalf("dogfood: %#v ok=%v", dog, ok)
	}
	if _, ok := project.Get("missing"); ok {
		t.Fatal("missing should be absent")
	}
}

func TestLoadProjectDefaultResolve(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "cage.yaml", projectRuntime)
	project, err := LoadProject(root, nil, runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := project.Get("default"); !ok {
		t.Fatal("default missing")
	}
}

func TestLoadProjectRejects(t *testing.T) {
	root := t.TempDir()
	writeProjectConfig(t, root, "cage.yaml", projectRuntime)

	outsideDir := t.TempDir()
	outside := filepath.Join(outsideDir, "escape.yaml")
	if err := os.WriteFile(outside, []byte(projectRuntime), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name    string
		root    string
		configs []string
		want    string
	}{
		{"missing-config", root, []string{".cage/missing.yaml"}, "config"},
		{"escape", root, []string{outside}, "escapes"},
		{"invalid-resolved", root, []string{
			writeProjectConfig(t, root, "cage.bad.yaml", "version: 1\nextends: missing-base.yaml\n"),
		}, "config"},
		{"duplicate", root, []string{".cage/cage.yaml", ".cage/cage.yaml"}, "duplicate"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadProject(tc.root, tc.configs, runtime.GOOS)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want substring %q", err, tc.want)
			}
		})
	}
}

func TestNewToken(t *testing.T) {
	a, err := NewToken()
	if err != nil || len(a) != 64 {
		t.Fatalf("token=%q err=%v", a, err)
	}
	b, err := NewToken()
	if err != nil || a == b {
		t.Fatalf("tokens collided: %q %q err=%v", a, b, err)
	}
}

func TestWriteServeState(t *testing.T) {
	root := t.TempDir()
	if err := WriteServeState(root, ServeState{Addr: "127.0.0.1:9", Token: "secret", PID: 1}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(ServeStatePath(root))
	if err != nil {
		t.Fatal(err)
	}
	var got ServeState
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Addr != "127.0.0.1:9" || got.Token != "secret" || got.PID != 1 {
		t.Fatalf("%+v", got)
	}
}

func TestConfigAlias(t *testing.T) {
	for _, tt := range []struct {
		path, want string
	}{
		{"/p/.cage/cage.yaml", "default"},
		{"/p/.cage/cage.yml", "default"},
		{"/p/.cage/cage.dogfood.yaml", "dogfood"},
		{"/p/.cage/other.yaml", "other"},
	} {
		if got := configAlias(tt.path); got != tt.want {
			t.Fatalf("%s: got %q want %q", tt.path, got, tt.want)
		}
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
