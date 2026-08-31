package guestenv

import (
	"os"
	"strings"
	"testing"
)

func TestInstallScriptOnlyExplicitKeys(t *testing.T) {
	const hostSecret = "SUPER_SECRET_HOST_VALUE_XYZ"
	t.Setenv("HOST_SECRET", hostSecret)
	t.Setenv("AWS_SECRET_ACCESS_KEY", "should-never-appear")
	t.Setenv("OPENAI_API_KEY", "host-openai-should-never-appear")

	// Prove we do not pull from the process environment.
	script := InstallScript(map[string]string{
		"APP_MODE": "dev",
		"FEATURE":  "on",
	})

	if strings.Contains(script, hostSecret) {
		t.Fatal("HOST_SECRET value leaked into guest install script")
	}
	if strings.Contains(script, "should-never-appear") {
		t.Fatal("host AWS/OpenAI env leaked into guest install script")
	}
	if strings.Contains(script, "HOST_SECRET") {
		t.Fatal("HOST_SECRET key must not appear — only operator runtime.env keys")
	}
	if strings.Contains(script, "AWS_SECRET_ACCESS_KEY") {
		t.Fatal("AWS_SECRET_ACCESS_KEY must not appear")
	}
	if !strings.Contains(script, "export APP_MODE='dev'") {
		t.Fatalf("missing APP_MODE export:\n%s", script)
	}
	if !strings.Contains(script, "export FEATURE='on'") {
		t.Fatalf("missing FEATURE export:\n%s", script)
	}
	if !strings.Contains(script, "APP_MODE=dev") || !strings.Contains(script, "FEATURE=on") {
		t.Fatalf("missing /etc/environment lines:\n%s", script)
	}

	// os.Environ still has the secrets on the host — guest script must stay clean.
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "HOST_SECRET=") && strings.Contains(script, strings.TrimPrefix(kv, "HOST_SECRET=")) {
			t.Fatal("host environ value present in script")
		}
	}
}

func TestInstallScriptEmpty(t *testing.T) {
	if got := InstallScript(nil); strings.TrimSpace(got) != "true" {
		t.Fatalf("nil: %q", got)
	}
	if got := InstallScript(map[string]string{}); strings.TrimSpace(got) != "true" {
		t.Fatalf("empty: %q", got)
	}
}

func TestInstallScriptQuotes(t *testing.T) {
	script := InstallScript(map[string]string{"MSG": "it's fine"})
	if !strings.Contains(script, `export MSG='it'\''s fine'`) {
		t.Fatalf("quote escape:\n%s", script)
	}
}
