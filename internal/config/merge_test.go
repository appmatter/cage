package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMergeNestedSecretsProxiesMention(t *testing.T) {
	root := t.TempDir()
	cageDir := filepath.Join(root, ".cage")
	if err := os.MkdirAll(cageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	basePath := filepath.Join(cageDir, "cage.yaml")
	profilePath := filepath.Join(cageDir, "cage.docs.yaml")
	if err := os.WriteFile(basePath, []byte(`
version: 1
runtime:
  plugins:
    tart:
      priority: 1
      image: img
fs:
  plugins:
    mention:
      include: ["**/*"]
      exclude: ["**/.git/**"]
    secrets_scanner:
      on_find: warn
      allow:
        - OPENAI_API_KEY
secrets:
  plugins:
    personal-op:
      plugin: onepassword
      vars:
        OPENAI_API_KEY: op://base
        KEEP: op://keep
network:
  plugins:
    http-proxy:
      openai:
        url: https://api.openai.com/v1
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, []byte(`
extends: cage.yaml
fs:
  plugins:
    mention:
      include: ["docs/**"]
    secrets_scanner:
      on_find: fail
secrets:
  plugins:
    personal-op:
      vars:
        OPENAI_API_KEY: op://override
    local-keys:
      plugin: keychain
      vars:
        GROQ_API_KEY: GROQ_API_KEY
network:
  plugins:
    http-proxy:
      anthropic:
        url: https://api.anthropic.com/v1
`), 0o644); err != nil {
		t.Fatal(err)
	}

	merged, err := loadChain(profilePath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := merged.FS.Plugins.Mention.Include; len(got) != 1 || got[0] != "docs/**" {
		t.Fatalf("mention include replaced: %v", got)
	}
	if got := merged.FS.Plugins.Mention.Exclude; len(got) != 1 || got[0] != "**/.git/**" {
		t.Fatalf("mention exclude kept: %v", got)
	}
	if merged.FS.Plugins.SecretsScanner.OnFind != "fail" {
		t.Fatalf("scanner on_find=%q", merged.FS.Plugins.SecretsScanner.OnFind)
	}
	if len(merged.FS.Plugins.SecretsScanner.Allow) != 1 || merged.FS.Plugins.SecretsScanner.Allow[0].Name != "OPENAI_API_KEY" {
		t.Fatalf("scanner allow kept: %#v", merged.FS.Plugins.SecretsScanner.Allow)
	}
	op := merged.Secrets.Plugins["personal-op"]
	if op.Plugin != "onepassword" {
		t.Fatalf("plugin id kept: %#v", op)
	}
	if op.Vars["OPENAI_API_KEY"] != "op://override" || op.Vars["KEEP"] != "op://keep" {
		t.Fatalf("secret vars merge: %#v", op.Vars)
	}
	if merged.Secrets.Plugins["local-keys"].Vars["GROQ_API_KEY"] == "" {
		t.Fatalf("keychain missing: %#v", merged.Secrets.Plugins)
	}
	if merged.Network.Plugins.HTTPProxy == nil || merged.Network.Plugins.HTTPProxy.Endpoints["openai"].URL == "" {
		t.Fatal("openai proxy dropped")
	}
	if merged.Network.Plugins.HTTPProxy.Endpoints["anthropic"].URL == "" {
		t.Fatal("anthropic proxy missing")
	}
}

func TestLoadProjectExample(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	path := filepath.Join(filepath.Dir(file), "../../.cage/cage.yaml")
	f, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateFile(f); err != nil {
		t.Fatal(err)
	}
	if f.Runtime.Plugins["tart"].Image == "" || f.Runtime.Plugins["tart"].Priority == nil || *f.Runtime.Plugins["tart"].Priority != 1 {
		t.Fatalf("runtime=%#v", f.Runtime.Plugins)
	}
	if len(f.Secrets.Plugins) != 0 {
		t.Fatalf("project secrets should be empty stubs; got %#v", f.Secrets.Plugins)
	}
	http := f.Network.Plugins.HTTPProxy
	if http == nil || http.Endpoints["openai"].URL == "" {
		t.Fatalf("http-proxy=%#v", f.Network.Plugins.HTTPProxy)
	}
	if f.Network.Plugins.Egress == nil || len(f.Network.Plugins.Egress.Allow) == 0 {
		t.Fatal("egress empty")
	}
}

func TestLoadSecretsPluginsFixtures(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	dir := filepath.Join(filepath.Dir(file), "testdata/secrets")
	base := filepath.Join(dir, "cage.yaml")
	f, err := LoadFile(base)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateFile(f); err != nil {
		t.Fatal(err)
	}
	op := f.Secrets.Plugins["onepassword"]
	if op.Plugin != "" || op.Vars["OPENAI_API_KEY"] == "" {
		t.Fatalf("onepassword seat=%#v", op)
	}
	http := f.Network.Plugins.HTTPProxy
	if http == nil || http.Endpoints["openai"].Headers["Authorization"] == "" {
		t.Fatalf("http-proxy=%#v", http)
	}

	merged, err := loadChain(filepath.Join(dir, "cage.multi-account.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateFile(merged); err != nil {
		t.Fatal(err)
	}
	if merged.Secrets.Plugins["onepassword"].Account != "my.1password.com" {
		t.Fatalf("account merge: %#v", merged.Secrets.Plugins["onepassword"])
	}
	org := merged.Secrets.Plugins["organization-op"]
	if org.Plugin != "onepassword" || org.Vars["ANTHROPIC_API_KEY"] == "" {
		t.Fatalf("organization-op=%#v", org)
	}
	if merged.Network.Plugins.HTTPProxy.Endpoints["anthropic"].URL == "" {
		t.Fatal("anthropic proxy missing")
	}
}
