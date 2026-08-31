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
  onepassword:
    personal-op:
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
  onepassword:
    personal-op:
      vars:
        OPENAI_API_KEY: op://override
  keychain:
    local-keys:
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
	op := merged.Secrets["onepassword"]["personal-op"]
	if op.Vars["OPENAI_API_KEY"] != "op://override" || op.Vars["KEEP"] != "op://keep" {
		t.Fatalf("secret vars merge: %#v", op.Vars)
	}
	if merged.Secrets["keychain"]["local-keys"].Vars["GROQ_API_KEY"] == "" {
		t.Fatalf("keychain missing: %#v", merged.Secrets)
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
	if f.FS.Plugins.Mention == nil || f.FS.Plugins.SecretsScanner == nil {
		t.Fatalf("fs plugins mention=%v scanner=%v", f.FS.Plugins.Mention != nil, f.FS.Plugins.SecretsScanner != nil)
	}
	if f.Secrets["onepassword"]["personal-op"].Vars["OPENAI_API_KEY"] == "" {
		t.Fatalf("secrets=%#v", f.Secrets)
	}
	http := f.Network.Plugins.HTTPProxy
	pg := f.Network.Plugins.PostgresProxy
	if http == nil || http.Endpoints["openai"].URL == "" || pg == nil || pg.Endpoints["development-postgres"].Listen != 5432 {
		t.Fatalf("proxies=%#v", f.Network.Plugins)
	}
	if http.Priority == nil || *http.Priority != 1 || pg.Priority == nil || *pg.Priority != 2 {
		t.Fatalf("terminate priorities http=%v pg=%v", http.Priority, pg.Priority)
	}
	if f.Network.Plugins.Egress == nil || len(f.Network.Plugins.Egress.Allow) == 0 {
		t.Fatal("egress empty")
	}
}
