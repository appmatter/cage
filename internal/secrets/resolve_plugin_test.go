package secrets_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/appmatter/cage/internal/config"
	"github.com/appmatter/cage/internal/pluginhost"
	"github.com/appmatter/cage/internal/secrets"
)

func TestResolveViaInstalledPlugin(t *testing.T) {
	root := t.TempDir()
	fakeOp := filepath.Join(root, "bin")
	if err := os.MkdirAll(fakeOp, 0o755); err != nil {
		t.Fatal(err)
	}
	op := filepath.Join(fakeOp, "op")
	if err := os.WriteFile(op, []byte("#!/bin/sh\nprintf 'secret-value\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeOp+string(os.PathListSeparator)+os.Getenv("PATH"))

	pluginSrc, err := filepath.Abs("../../plugins/secrets/onepassword")
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(root, ".cage", ".cache", "plugins", "secrets", "onepassword")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(cache, "cage-secrets-onepassword"+pluginhost.BinaryExt)
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = pluginSrc
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build plugin: %v\n%s", err, out)
	}
	manifest := `{"kind":"secrets","name":"onepassword","command":"cage-secrets-onepassword` + pluginhost.BinaryExt + `","source":"plugins/secrets/onepassword","pin":"local"}`
	if err := os.WriteFile(filepath.Join(cache, "manifest.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	lockDir := filepath.Join(root, ".cage")
	if err := os.WriteFile(filepath.Join(lockDir, "plugins.lock.json"), []byte(`{"plugins":[`+manifest+`]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	vals, err := secrets.Resolve(root, map[string]config.SecretStore{
		"onepassword": {
			Vars: map[string]string{"OPENAI_API_KEY": "op://Vault/item/field"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := vals["onepassword"]["OPENAI_API_KEY"]; got != "secret-value" {
		t.Fatalf("got %q", got)
	}
	applied, err := secrets.Apply(`Bearer {{ secrets.onepassword.OPENAI_API_KEY }}`, vals)
	if err != nil || applied != "Bearer secret-value" {
		t.Fatalf("apply %q %v", applied, err)
	}
}
