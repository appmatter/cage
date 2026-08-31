package hooks

import (
	"testing"

	"github.com/appmatter/cage/internal/config"
	"github.com/appmatter/cage/internal/pluginhost"
)

func TestHostCovered(t *testing.T) {
	allow := []string{"api.github.com", "*.githubusercontent.com", "pi.dev"}
	if !hostCovered(allow, "api.github.com") {
		t.Fatal("exact")
	}
	if !hostCovered(allow, "*.githubusercontent.com") {
		t.Fatal("glob equal")
	}
	if hostCovered(allow, "evil.com") {
		t.Fatal("should miss")
	}
	if !hostCovered([]string{"*.pi.dev"}, "api.pi.dev") {
		t.Fatal("glob covers subdomain")
	}
	if !hostCovered([]string{"*"}, "anything") {
		t.Fatal("star")
	}
}

func TestWarnMissingEgress(t *testing.T) {
	// Unit without installed plugin: empty harness → no panic.
	var lines []string
	WarnMissingEgress(".", config.Resolved{}, func(f string, a ...any) {
		lines = append(lines, f)
	})
	_ = pluginhost.EgressHint{}
}
