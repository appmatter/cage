package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/appmatter/cage/internal/proxy"
)

func TestConsumeHTTPProxyResolvedYAML(t *testing.T) {
	root := t.TempDir()
	id := "vm-consume"
	path := proxy.ResolvedYAMLPath(root, id)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	want := []byte("token: super-secret-token-xyz\n")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := consumeHTTPProxyResolvedYAML(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("resolved file should be deleted after read, err=%v", err)
	}
}

func TestConsumeHTTPProxyResolvedYAMLMissing(t *testing.T) {
	got, err := consumeHTTPProxyResolvedYAML(t.TempDir(), "missing")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("missing file should yield nil, got %q", got)
	}
}

func TestProxyServeHasNoResolvedFlag(t *testing.T) {
	cmd := newProxyServeCmd()
	if cmd.Flags().Lookup("http-proxy-resolved") != nil {
		t.Fatal("--http-proxy-resolved must be removed")
	}
	if cmd.Flags().Lookup("http-proxy") == nil {
		t.Fatal("--http-proxy must remain")
	}
}

func TestLoadHTTPProxyRequiresConfig(t *testing.T) {
	_, _, err := loadHTTPProxy(".", "", []byte("token: {{ secrets.foo }}\n"))
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if strings.Contains(msg, "--http-proxy-resolved") {
		t.Fatalf("error must not mention removed flag: %s", msg)
	}
	if !strings.Contains(msg, "--config") {
		t.Fatalf("error should mention --config: %s", msg)
	}
}
