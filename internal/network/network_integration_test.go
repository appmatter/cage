//go:build network && darwin

package network_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestNetworkHarness runs scripts/test-network.sh (headless proxy + softnet smoke).
// Skip is exit 0 from the script when tart/softnet/image/privileges are missing.
func TestNetworkHarness(t *testing.T) {
	root := repoRoot(t)
	script := filepath.Join(root, "scripts", "test-network.sh")
	cmd := exec.Command("bash", script)
	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "CAGE_NET_TEST_ID=cage-net-goit")
	if err := cmd.Run(); err != nil {
		t.Fatalf("scripts/test-network.sh: %v", err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// internal/network → repo root
	root := filepath.Clean(filepath.Join(wd, "../.."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("go.mod not found from %s (wd=%s)", root, wd)
	}
	return root
}
