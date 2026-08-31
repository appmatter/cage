package hooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/appmatter/cage/internal/config"
)

func TestResolveAgentDir(t *testing.T) {
	root := t.TempDir()
	def := ResolveAgentDir(root, config.HarnessSeat{PluginID: "pi-agent"})
	want := filepath.Join(root, ".cage", "plugins", "runtime", "pi-agent")
	if def != want {
		t.Fatalf("default=%q want %q", def, want)
	}
	over := ResolveAgentDir(root, config.HarnessSeat{AgentDir: "agents/pi"})
	if over != filepath.Join(root, "agents/pi") {
		t.Fatalf("override=%q", over)
	}
	abs := ResolveAgentDir(root, config.HarnessSeat{AgentDir: "/tmp/pi-agent"})
	if abs != "/tmp/pi-agent" {
		t.Fatalf("abs=%q", abs)
	}
}

func TestTarDirSkipsAuth(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "models.json"), []byte("{}\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "auth.json"), []byte("secret\n"), 0o644)
	b, err := tarDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) == 0 {
		t.Fatal("empty tar")
	}
	if bytesContains(b, []byte("auth.json")) {
		t.Fatal("auth.json should be skipped")
	}
	if !bytesContains(b, []byte("models.json")) {
		t.Fatal("models.json missing")
	}
}

func bytesContains(b, sub []byte) bool {
	return len(b) >= len(sub) && (string(b) != "" && contains(b, sub))
}

func contains(b, sub []byte) bool {
	for i := 0; i+len(sub) <= len(b); i++ {
		ok := true
		for j := range sub {
			if b[i+j] != sub[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
