package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/appmatter/cage/internal/config"
)

const runtimeBlock = `
runtime:
  plugins:
    tart:
      priority: 1
      image: ghcr.io/cirruslabs/ubuntu:latest
    incus:
      priority: 2
      image: ubuntu/24.04
    hyperv:
      priority: 3
      image: Ubuntu
  workdir: /workspace
`

func TestLoadResolvedBase(t *testing.T) {
	root := t.TempDir()
	cageDir := filepath.Join(root, ".cage")
	if err := os.MkdirAll(cageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cageDir, "cage.yaml"), `
version: 1
`+runtimeBlock+`
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
`)
	r, err := config.LoadResolved(root, filepath.Join(cageDir, "cage.yaml"), "darwin")
	if err != nil {
		t.Fatal(err)
	}
	if r.Runtime.Backend != "tart" || r.Runtime.Image != "ghcr.io/cirruslabs/ubuntu:latest" {
		t.Fatalf("backend=%q image=%q", r.Runtime.Backend, r.Runtime.Image)
	}
	if len(r.Mounts) != 2 {
		t.Fatalf("mounts=%d %#v", len(r.Mounts), r.Mounts)
	}
	byTarget := map[string]config.ResolvedPath{}
	for _, m := range r.Mounts {
		byTarget[m.Target] = m
	}
	if byTarget["tests"].Permission != "ro" {
		t.Fatalf("tests perm=%q", byTarget["tests"].Permission)
	}
	if byTarget["src"].Guest != "/workspace/src" {
		t.Fatalf("src guest=%q", byTarget["src"].Guest)
	}
	if len(r.Copies) != 1 || r.Copies[0].Target != ".env" {
		t.Fatalf("copies=%#v", r.Copies)
	}
}

func TestLoadResolvedLinuxBackend(t *testing.T) {
	root := t.TempDir()
	cageDir := filepath.Join(root, ".cage")
	if err := os.MkdirAll(cageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cageDir, "cage.yaml"), "version: 1\n"+runtimeBlock)
	r, err := config.LoadResolved(root, filepath.Join(cageDir, "cage.yaml"), "linux")
	if err != nil {
		t.Fatal(err)
	}
	if r.Runtime.Backend != "incus" || r.Runtime.Image != "ubuntu/24.04" {
		t.Fatalf("backend=%q image=%q", r.Runtime.Backend, r.Runtime.Image)
	}
}

func TestLoadResolvedPreferLowerPriority(t *testing.T) {
	root := t.TempDir()
	cageDir := filepath.Join(root, ".cage")
	if err := os.MkdirAll(cageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cageDir, "cage.yaml"), `
version: 1
runtime:
  plugins:
    docker:
      priority: 5
      image: ubuntu:24.04
    darwin-tart:
      priority: 1
      plugin: tart
      image: ghcr.io/cirruslabs/ubuntu:latest
  workdir: /workspace
`)
	r, err := config.LoadResolved(root, filepath.Join(cageDir, "cage.yaml"), "darwin")
	if err != nil {
		t.Fatal(err)
	}
	if r.Runtime.Backend != "tart" || r.Runtime.Seat != "darwin-tart" {
		t.Fatalf("backend=%q seat=%q", r.Runtime.Backend, r.Runtime.Seat)
	}
}

func TestLoadResolvedMissingGOOS(t *testing.T) {
	root := t.TempDir()
	cageDir := filepath.Join(root, ".cage")
	if err := os.MkdirAll(cageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cageDir, "cage.yaml"), "version: 1\n"+runtimeBlock)
	_, err := config.LoadResolved(root, filepath.Join(cageDir, "cage.yaml"), "plan9")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadResolvedProfileMerge(t *testing.T) {
	root := t.TempDir()
	cageDir := filepath.Join(root, ".cage")
	if err := os.MkdirAll(cageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cageDir, "cage.yaml"), `
version: 1
`+runtimeBlock+`
fs:
  layout:
    mode: flat
  mount:
    src: ./src
    tests: ./tests
  deny:
    - .git
`)
	write(t, filepath.Join(cageDir, "cage.docs-agent.yaml"), `
extends: cage.yaml
fs:
  mount:
    docs: ./docs
    tests:
      remove: true
  deny:
    - node_modules
`)
	r, err := config.LoadResolved(root, filepath.Join(cageDir, "cage.docs-agent.yaml"), "darwin")
	if err != nil {
		t.Fatal(err)
	}
	byTarget := map[string]config.ResolvedPath{}
	for _, m := range r.Mounts {
		byTarget[m.Target] = m
	}
	if _, ok := byTarget["tests"]; ok {
		t.Fatal("tests should be removed")
	}
	if _, ok := byTarget["docs"]; !ok {
		t.Fatal("docs missing")
	}
	if _, ok := byTarget["src"]; !ok {
		t.Fatal("src missing")
	}
	foundNode := false
	for _, d := range r.Deny {
		if d == "node_modules" {
			foundNode = true
		}
	}
	if !foundNode {
		t.Fatalf("deny=%v", r.Deny)
	}
}

func TestLoadResolvedDenyActive(t *testing.T) {
	root := t.TempDir()
	cageDir := filepath.Join(root, ".cage")
	if err := os.MkdirAll(cageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cageDir, "cage.yaml"), `
version: 1
`+runtimeBlock+`
fs:
  layout:
    mode: flat
  mount:
    ".": .
  deny:
    - .git
    - .cage
    - .cage/cage.yaml
`)
	write(t, filepath.Join(cageDir, "cage.dogfood.yaml"), `
extends: cage.yaml
fs:
  mount:
    .cage:
      host: .cage
      permission: ro
  deny:
    - path: .cage
      active: false
    - path: .cage/cage.yaml
      active: false
`)
	r, err := config.LoadResolved(root, filepath.Join(cageDir, "cage.dogfood.yaml"), "darwin")
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range r.Deny {
		if d == ".cage" || d == ".cage/cage.yaml" {
			t.Fatalf("deny should drop .cage entries: %v", r.Deny)
		}
	}
	foundGit := false
	for _, d := range r.Deny {
		if d == ".git" {
			foundGit = true
		}
	}
	if !foundGit {
		t.Fatalf("deny should keep .git: %v", r.Deny)
	}
	byTarget := map[string]config.ResolvedPath{}
	for _, m := range r.Mounts {
		byTarget[m.Target] = m
	}
	cageMount, ok := byTarget[".cage"]
	if !ok {
		t.Fatal(".cage mount missing")
	}
	if cageMount.Permission != "ro" {
		t.Fatalf(".cage permission=%q", cageMount.Permission)
	}
}

func TestLoadResolvedMultiLevel(t *testing.T) {
	root := t.TempDir()
	cageDir := filepath.Join(root, ".cage")
	if err := os.MkdirAll(cageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cageDir, "cage.yaml"), `
version: 1
`+runtimeBlock+`
fs:
  mount:
    src: ./src
  deny:
    - .git
`)
	write(t, filepath.Join(cageDir, "cage.team.yaml"), `
extends: cage.yaml
fs:
  mount:
    shared: ./shared
  deny:
    - node_modules
`)
	write(t, filepath.Join(cageDir, "cage.docs-agent.yaml"), `
extends: cage.team.yaml
fs:
  mount:
    docs: ./docs
    shared:
      remove: true
`)
	r, err := config.LoadResolved(root, filepath.Join(cageDir, "cage.docs-agent.yaml"), "darwin")
	if err != nil {
		t.Fatal(err)
	}
	byTarget := map[string]config.ResolvedPath{}
	for _, m := range r.Mounts {
		byTarget[m.Target] = m
	}
	if _, ok := byTarget["src"]; !ok {
		t.Fatal("src missing from base")
	}
	if _, ok := byTarget["docs"]; !ok {
		t.Fatal("docs missing")
	}
	if _, ok := byTarget["shared"]; ok {
		t.Fatal("shared should be removed")
	}
	foundNode := false
	for _, d := range r.Deny {
		if d == "node_modules" {
			foundNode = true
		}
	}
	if !foundNode {
		t.Fatalf("deny should union team: %v", r.Deny)
	}
}

func TestLoadResolvedCircularExtends(t *testing.T) {
	root := t.TempDir()
	cageDir := filepath.Join(root, ".cage")
	if err := os.MkdirAll(cageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cageDir, "cage.a.yaml"), "extends: cage.b.yaml\n"+runtimeBlock)
	write(t, filepath.Join(cageDir, "cage.b.yaml"), "extends: cage.a.yaml\n")
	_, err := config.LoadResolved(root, filepath.Join(cageDir, "cage.a.yaml"), "darwin")
	if err == nil {
		t.Fatal("expected circular extends error")
	}
	if !strings.Contains(err.Error(), "circular extends") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadResolvedDenyBlocks(t *testing.T) {
	root := t.TempDir()
	cageDir := filepath.Join(root, ".cage")
	if err := os.MkdirAll(cageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cageDir, "cage.yaml"), `
version: 1
`+runtimeBlock+`
fs:
  mount:
    env: ./.env
  deny:
    - .env
`)
	_, err := config.LoadResolved(root, filepath.Join(cageDir, "cage.yaml"), "darwin")
	if err == nil {
		t.Fatal("expected deny error — denied mounts must not reach the runtime plugin")
	}
	if !strings.Contains(err.Error(), "fs.deny") {
		t.Fatalf("want fs.deny in error, got %v", err)
	}
}

func TestLoadResolvedDenyBlocksCopy(t *testing.T) {
	root := t.TempDir()
	cageDir := filepath.Join(root, ".cage")
	if err := os.MkdirAll(cageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cageDir, "cage.yaml"), `
version: 1
`+runtimeBlock+`
fs:
  copy:
    env: ./.env
  deny:
    - .env
`)
	_, err := config.LoadResolved(root, filepath.Join(cageDir, "cage.yaml"), "darwin")
	if err == nil {
		t.Fatal("expected deny error for copy")
	}
	if !strings.Contains(err.Error(), "fs.deny") {
		t.Fatalf("want fs.deny in error, got %v", err)
	}
}

func TestLoadResolvedDenyMasksUnderParent(t *testing.T) {
	root := t.TempDir()
	cageDir := filepath.Join(root, ".cage")
	if err := os.MkdirAll(cageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte("SECRET=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "leak.pem"), []byte("key\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(cageDir, "cage.yaml"), `
version: 1
`+runtimeBlock+`
fs:
  mount:
    ".": .
    .git:
      host: .git
      permission: ro
  deny:
    - .env
    - .cage
    - "**/*.pem"
`)
	r, err := config.LoadResolved(root, filepath.Join(cageDir, "cage.yaml"), "darwin")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/workspace/.env":     true,
		"/workspace/.cage":    true,
		"/workspace/leak.pem": true,
	}
	got := map[string]bool{}
	for _, g := range r.DenyMasks {
		got[g] = true
	}
	for g := range want {
		if !got[g] {
			t.Fatalf("missing mask %q in %#v", g, r.DenyMasks)
		}
	}
	if got["/workspace/.git"] {
		t.Fatalf("explicit .git mount must not be masked: %#v", r.DenyMasks)
	}
	if got["/workspace/ok.txt"] {
		t.Fatalf("ok.txt must not be masked: %#v", r.DenyMasks)
	}
}

func TestLoadFileNestedPlugins(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cage.yaml")
	write(t, path, `
version: 1
`+runtimeBlock+`
fs:
  plugins:
    mention:
      include: ["docs/**"]
      exclude: ["**/.git/**"]
    secrets_scanner:
      on_find: warn
      allow:
        - OPENAI_API_KEY
        - path: .env.example
secrets:
  plugins:
    personal-op:
      plugin: onepassword
      vars:
        OPENAI_API_KEY: op://x
    dev-sm:
      plugin: aws_sm
      region: eu-west-2
      vars:
        DB_PASSWORD: arn:aws:sm:x
network:
  plugins:
    egress:
      allow:
        - host: api.openai.com
          port: 443
          method: POST
    http-proxy:
      openai:
        url: https://api.openai.com/v1
        headers:
          Authorization: "Bearer {{ secrets.personal-op.OPENAI_API_KEY }}"
    postgres-proxy:
      priority: 1
      db:
        listen: 5432
        host: db.example.com
`)
	f, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.FS.Plugins.Mention == nil || len(f.FS.Plugins.Mention.Include) != 1 {
		t.Fatalf("mention=%#v", f.FS.Plugins.Mention)
	}
	sc := f.FS.Plugins.SecretsScanner
	if sc == nil || sc.OnFind != "warn" || len(sc.Allow) != 2 {
		t.Fatalf("scanner=%#v", sc)
	}
	if sc.Allow[0].Name != "OPENAI_API_KEY" || sc.Allow[1].Path != ".env.example" {
		t.Fatalf("allow=%#v", sc.Allow)
	}
	op := f.Secrets.Plugins["personal-op"]
	if op.Plugin != "onepassword" || op.Vars["OPENAI_API_KEY"] == "" {
		t.Fatalf("secrets=%#v", f.Secrets.Plugins)
	}
	if f.Secrets.Plugins["dev-sm"].Region != "eu-west-2" {
		t.Fatalf("aws_sm=%#v", f.Secrets.Plugins["dev-sm"])
	}
	if f.Network.Plugins.HTTPProxy == nil || f.Network.Plugins.HTTPProxy.Endpoints["openai"].URL == "" {
		t.Fatalf("http-proxy=%#v", f.Network.Plugins.HTTPProxy)
	}
	if f.Network.Plugins.PostgresProxy == nil || f.Network.Plugins.PostgresProxy.Endpoints["db"].Listen != 5432 {
		t.Fatalf("postgres-proxy=%#v", f.Network.Plugins.PostgresProxy)
	}
	eg := f.Network.Plugins.Egress
	if eg == nil || len(eg.Allow) != 1 || eg.Allow[0].Method != "POST" || eg.Allow[0].Port != 443 {
		t.Fatalf("egress=%#v", eg)
	}
	if err := config.ValidateFile(f); err == nil {
		t.Fatal("expected priority validation error (two terminate plugins, http-proxy missing priority)")
	}
}

func TestLoadFilePackageOverride(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "cage.yaml")
	write(t, path, `
version: 1
`+runtimeBlock+`
network:
  plugins:
    egress:
      package: git:github.com/acme/egress
      allow:
        - host: example.com
    http-proxy:
      priority: 1
      package: git:github.com/acme/http-proxy
      openai:
        url: https://api.openai.com/v1
`)
	f, err := config.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Network.Plugins.Egress.Package != "git:github.com/acme/egress" {
		t.Fatalf("egress package=%q", f.Network.Plugins.Egress.Package)
	}
	if f.Network.Plugins.HTTPProxy.Package != "git:github.com/acme/http-proxy" {
		t.Fatalf("http-proxy package=%q", f.Network.Plugins.HTTPProxy.Package)
	}
}

func TestLoadResolvedLifecycleScripts(t *testing.T) {
	root := t.TempDir()
	cageDir := filepath.Join(root, ".cage")
	if err := os.MkdirAll(cageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scripts := filepath.Join(root, "scripts")
	if err := os.MkdirAll(scripts, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(scripts, "create.sh"), "#!/bin/sh\ntrue\n")
	write(t, filepath.Join(scripts, "start.sh"), "#!/bin/sh\ntrue\n")
	write(t, filepath.Join(scripts, "destroy.sh"), "#!/bin/sh\ntrue\n")
	write(t, filepath.Join(scripts, "start2.sh"), "#!/bin/sh\ntrue\n")
	write(t, filepath.Join(scripts, "bake.sh"), "#!/bin/sh\ntrue\n")
	write(t, filepath.Join(cageDir, "cage.yaml"), `
version: 1
runtime:
  plugins:
    tart:
      priority: 1
      image: ghcr.io/cirruslabs/ubuntu:latest
      on-create:
        - ./scripts/create.sh
      on-start:
        - ./scripts/start.sh
      on-destroy:
        - ./scripts/destroy.sh
      bake:
        - ./scripts/bake.sh
    pi-agent:
      bake:
        - ./scripts/bake.sh
    incus:
      priority: 2
      image: ubuntu/24.04
    hyperv:
      priority: 3
      image: Ubuntu
  workdir: /workspace
fs:
  layout: { mode: flat }
  mount: {}
  copy: {}
  deny: []
`)
	write(t, filepath.Join(cageDir, "cage.docs-agent.yaml"), `
extends: cage.yaml
runtime:
  plugins:
    tart:
      on-start:
        - ./scripts/start2.sh
`)
	r, err := config.LoadResolved(root, filepath.Join(cageDir, "cage.docs-agent.yaml"), "darwin")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Runtime.OnCreate) != 1 || !strings.HasSuffix(r.Runtime.OnCreate[0], "create.sh") {
		t.Fatalf("on-create=%v", r.Runtime.OnCreate)
	}
	if len(r.Runtime.OnStart) != 1 || !strings.HasSuffix(r.Runtime.OnStart[0], "start2.sh") {
		t.Fatalf("on-start replaced=%v", r.Runtime.OnStart)
	}
	if len(r.Runtime.OnDestroy) != 1 || !strings.HasSuffix(r.Runtime.OnDestroy[0], "destroy.sh") {
		t.Fatalf("on-destroy=%v", r.Runtime.OnDestroy)
	}
	// backend bake + harness seat bake (same script twice in list is ok; both hashed)
	if len(r.Runtime.Bake) != 2 {
		t.Fatalf("bake=%v", r.Runtime.Bake)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
