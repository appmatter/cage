package main

import (
	"os"
	"path/filepath"
	"testing"

	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

func TestSeedAgentDir(t *testing.T) {
	dir := t.TempDir()
	p := &PiAgent{}
	if err := p.Init(runtimeplugin.HookContext{AgentDir: dir}); err != nil {
		t.Fatal(err)
	}
	models := filepath.Join(dir, "models.json")
	b, err := os.ReadFile(models)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("empty models.json")
	}
	// second call must not clobber
	if err := os.WriteFile(models, []byte(`{"providers":{"x":{}}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := p.Init(runtimeplugin.HookContext{AgentDir: dir}); err != nil {
		t.Fatal(err)
	}
	b2, _ := os.ReadFile(models)
	if string(b2) == string(b) {
		t.Fatal("should keep user edit")
	}
}
