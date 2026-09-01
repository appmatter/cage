//go:build example

package config

import (
	"path/filepath"
	"runtime"
	"testing"
)

// Run via lefthook: go test -tags example ./internal/config/ -run TestLoadProjectExample
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
