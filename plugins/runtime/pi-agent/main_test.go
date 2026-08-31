package main

import (
	"strings"
	"testing"

	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

func TestHooksDeclaration(t *testing.T) {
	p := &PiAgent{}
	hooks := p.Hooks()
	want := map[string]bool{
		runtimeplugin.HookBeforeBake:    true,
		runtimeplugin.HookOnStart:       true,
		runtimeplugin.HookOnAttachShell: true,
	}
	if len(hooks) != len(want) {
		t.Fatalf("hooks=%v", hooks)
	}
	for _, h := range hooks {
		if !want[h] {
			t.Fatalf("unexpected hook %q", h)
		}
	}
}

func TestBeforeBakeFullScript(t *testing.T) {
	p := &PiAgent{}
	atts, err := p.BeforeBake(runtimeplugin.HookContext{
		SeatYAML: []byte("version: \"0.84.4\"\nnode_major: 22\npackages:\n  - npm:@acme/tools@1.0.0\n  - git:github.com/acme/ext@abc\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 1 {
		t.Fatalf("atts=%d", len(atts))
	}
	body := string(atts[0].Body)
	for _, want := range []string{
		"NODE_MAJOR=22",
		"PI_VERSION=\"0.84.4\"",
		"@earendil-works/pi-coding-agent",
		"pi install \"npm:@acme/tools@1.0.0\"",
		"pi install \"git:github.com/acme/ext@abc\"",
		"deb.nodesource.com",
		"pi binary missing after npm install",
		"fd-find",
		"ripgrep",
		"/usr/local/bin/fd",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "--ignore-scripts") {
		t.Fatalf("should not ignore-scripts")
	}
}

func TestBeforeBakeDryRun(t *testing.T) {
	p := &PiAgent{}
	atts, err := p.BeforeBake(runtimeplugin.HookContext{
		DryRun:   true,
		SeatYAML: []byte("version: \"1.2.3\"\npackages:\n  - npm:@foo/bar@9\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	body := string(atts[0].Body)
	if !strings.Contains(body, "/var/lib/cage/pi-agent-baked") {
		t.Fatalf("dry body:\n%s", body)
	}
	if strings.Contains(body, "apt-get") {
		t.Fatalf("dry run should not apt-get")
	}
	if !strings.Contains(body, "npm:@foo/bar@9") {
		t.Fatalf("missing package in dry:\n%s", body)
	}
}

func TestBeforeBakeDefaults(t *testing.T) {
	p := &PiAgent{}
	atts, err := p.BeforeBake(runtimeplugin.HookContext{})
	if err != nil {
		t.Fatal(err)
	}
	body := string(atts[0].Body)
	if !strings.Contains(body, `PI_VERSION="latest"`) {
		t.Fatalf("default version:\n%s", body)
	}
	if !strings.Contains(body, "NODE_MAJOR=22") {
		t.Fatalf("default node:\n%s", body)
	}
}
