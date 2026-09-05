package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	clientplugin "github.com/appmatter/cage/pkg/plugin/v1/client"
	secretsplugin "github.com/appmatter/cage/pkg/plugin/v1/secrets"
)

func TestOnePasswordResolveFakeOp(t *testing.T) {
	dir := t.TempDir()
	op := filepath.Join(dir, "op")
	script := `#!/bin/sh
account=
ref=
while [ $# -gt 0 ]; do
  case "$1" in
    --account) account=$2; shift 2 ;;
    read) shift ;;
    *) ref=$1; shift ;;
  esac
done
if [ -n "$account" ]; then
  printf '%s|%s\n' "$account" "$ref"
else
  printf '%s\n' "$ref"
fi
`
	if err := os.WriteFile(op, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	p := &OnePassword{}
	if err := p.Configure([]byte("account: my.1password.com\n")); err != nil {
		t.Fatal(err)
	}
	got, err := p.Resolve(map[string]string{"OPENAI_API_KEY": "op://Vault/item/field"})
	if err != nil {
		t.Fatal(err)
	}
	want := "my.1password.com|op://Vault/item/field"
	if got["OPENAI_API_KEY"] != want {
		t.Fatalf("got %#v want %q", got, want)
	}
}

func TestOnePasswordMissingOp(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	p := &OnePassword{}
	_ = p.Configure(nil)
	if _, err := p.Resolve(map[string]string{"X": "op://a/b/c"}); err == nil {
		t.Fatal("expected missing op")
	}
}

func TestOpEnvAppFromConfig(t *testing.T) {
	got := opEnv([]string{"PATH=/bin", "OP_BIOMETRIC_UNLOCK_ENABLED=false"}, true)
	if !containsExact(got, "OP_BIOMETRIC_UNLOCK_ENABLED=true") {
		t.Fatalf("app on: %v", got)
	}
	got = opEnv([]string{"PATH=/bin", "OP_BIOMETRIC_UNLOCK_ENABLED=true"}, false)
	if !containsExact(got, "OP_BIOMETRIC_UNLOCK_ENABLED=false") {
		t.Fatalf("app off: %v", got)
	}
	if !containsExact(got, "OP_LOAD_DESKTOP_APP_SETTINGS=false") {
		t.Fatalf("desktop settings should disable when app off: %v", got)
	}
}

func TestConfigureAppDefault(t *testing.T) {
	p := &OnePassword{}
	if err := p.Configure([]byte("account: my.1password.com\n")); err != nil {
		t.Fatal(err)
	}
	if !p.app || p.account != "my.1password.com" {
		t.Fatalf("%#v", p)
	}
	if err := p.Configure([]byte("app: false\n")); err != nil {
		t.Fatal(err)
	}
	if p.app {
		t.Fatal("expected app false")
	}
}

func TestOnePasswordHasNoClientService(t *testing.T) {
	if _, ok := secretsplugin.PluginMap(&OnePassword{})[clientplugin.PluginName]; ok {
		t.Fatal("default secrets plugin exposes client service")
	}
}

func TestOnePasswordBuild(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skip build check on windows")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "onepassword.cageplugin")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, b)
	}
}

func containsExact(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
