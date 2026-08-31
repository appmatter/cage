package hooks_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/appmatter/cage/internal/config"
	"github.com/appmatter/cage/internal/hooks"
	"github.com/appmatter/cage/internal/pluginhost"
	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

func TestResolveMergesPluginDeclaredHooks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".cage", ".cache", "plugins", "runtime", "pi-agent")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Fake binary so ResolveCommand succeeds.
	bin := filepath.Join(dir, "cage-runtime-pi-agent.cageplugin")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := pluginhost.Manifest{
		Kind:    "runtime",
		Name:    "pi-agent",
		Command: "cage-runtime-pi-agent.cageplugin",
		Source:  "local",
		Stage:   "harness",
		Hooks: []string{
			runtimeplugin.HookBeforeBake,
			runtimeplugin.HookOnStart,
			runtimeplugin.HookOnAttachShell,
		},
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	rt := config.Runtime{
		Hooks: map[string][]config.HookAction{
			runtimeplugin.HookOnStart: {{Plugin: "extra-hook"}},
		},
		HarnessSeats: []config.HarnessSeat{
			{Seat: "pi-agent", PluginID: "pi-agent"},
		},
	}
	if err := hooks.Resolve(root, &rt); err != nil {
		t.Fatal(err)
	}
	got := rt.ResolvedHooks
	if len(got[runtimeplugin.HookBeforeBake]) != 1 || got[runtimeplugin.HookBeforeBake][0] != "pi-agent" {
		t.Fatalf("before_bake=%v", got[runtimeplugin.HookBeforeBake])
	}
	// YAML extra + plugin default on on_start
	if len(got[runtimeplugin.HookOnStart]) != 2 {
		t.Fatalf("on_start=%v", got[runtimeplugin.HookOnStart])
	}
	if len(got[runtimeplugin.HookOnAttachShell]) != 1 {
		t.Fatalf("on_attach_shell=%v", got[runtimeplugin.HookOnAttachShell])
	}
}
