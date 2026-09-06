//go:build integration && darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/appmatter/cage/internal/config"
	"github.com/appmatter/cage/internal/guestenv"
	"github.com/appmatter/cage/internal/pluginhost"
	"github.com/appmatter/cage/internal/secrets"
	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

// TestIntegrationRuntimeEnvSecrets resolves {{ secrets.* }} on the host (fake op),
// installs runtime.env into a live Tart guest, and checks the value is present.
func TestIntegrationRuntimeEnvSecrets(t *testing.T) {
	if _, err := exec.LookPath("tart"); err != nil {
		t.Skip("tart not on PATH")
	}
	image := os.Getenv("CAGE_TART_IMAGE")
	if image == "" {
		image = "ubuntu"
	}
	if !tartHasImage(image) {
		t.Skipf("image %q not local (set CAGE_TART_IMAGE or tart pull/clone first)", image)
	}

	project := t.TempDir()
	installFakeOnepassword(t, project)
	t.Setenv("PATH", filepath.Join(project, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"))

	vals, err := secrets.Resolve(project, map[string]config.SecretStore{
		"onepassword": {
			Vars: map[string]string{"DEMO_SECRET": "op://Vault/item/field"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	env, err := secrets.ApplyMap(map[string]string{
		"DEMO_SECRET": "{{ secrets.onepassword.DEMO_SECRET }}",
		"APP_MODE":    "it",
	}, vals)
	if err != nil {
		t.Fatal(err)
	}
	if env["DEMO_SECRET"] != "secret-value" || env["APP_MODE"] != "it" {
		t.Fatalf("apply map: %#v", env)
	}

	id := fmt.Sprintf("cage-env-it-%d", os.Getpid())
	backend := &Tart{}
	defer func() {
		_ = backend.Stop(id)
		_ = backend.Delete(runtimeplugin.Spec{ID: id})
	}()
	spec := runtimeplugin.Spec{ID: id, ProjectRoot: project, Image: image, Workdir: "/workspace"}
	if err := backend.Create(spec); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := backend.Start(spec); err != nil {
		t.Fatalf("start: %v", err)
	}

	if err := backend.Exec(id, runtimeplugin.ExecOpts{
		Argv:  []string{"sudo", "sh", "-s"},
		Stdin: []byte(guestenv.InstallScript(env)),
	}); err != nil {
		t.Fatalf("install runtime.env: %v", err)
	}

	got, err := tartExecOut(id, "bash", "-lc", "set -a; . /var/lib/cage/runtime.env; set +a; printf '%s' \"$DEMO_SECRET\"")
	if err != nil {
		t.Fatalf("read env: %v\n%s", err, got)
	}
	if strings.TrimSpace(got) != "secret-value" {
		t.Fatalf("guest DEMO_SECRET=%q", got)
	}
	raw, err := tartExecOut(id, "cat", "/var/lib/cage/runtime.env")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "{{ secrets.") {
		t.Fatalf("template leaked into guest:\n%s", raw)
	}
}

func installFakeOnepassword(t *testing.T, project string) {
	t.Helper()
	binDir := filepath.Join(project, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	op := filepath.Join(binDir, "op")
	if err := os.WriteFile(op, []byte("#!/bin/sh\nprintf 'secret-value\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	pluginSrc := filepath.Join(root, "plugins/secrets/onepassword")

	cache := filepath.Join(project, ".cage", ".cache", "plugins", "secrets", "onepassword")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(cache, "cage-secrets-onepassword"+pluginhost.BinaryExt)
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = pluginSrc
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build onepassword plugin: %v\n%s", err, out)
	}
	manifest := `{"kind":"secrets","name":"onepassword","command":"cage-secrets-onepassword` + pluginhost.BinaryExt + `","source":"plugins/secrets/onepassword","pin":"local"}`
	if err := os.WriteFile(filepath.Join(cache, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".cage", "plugins.lock.json"), []byte(`{"plugins":[`+manifest+`]}`), 0o644); err != nil {
		t.Fatal(err)
	}
}
