package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsCageProxyPIDRejectsUnrelated(t *testing.T) {
	if isCageProxyPID(os.Getpid()) {
		t.Fatal("test process should not look like proxy-serve")
	}
	if isCageProxyPID(0) || isCageProxyPID(-1) {
		t.Fatal("invalid pid")
	}
}

func TestStopDetachedProxySkipsStalePID(t *testing.T) {
	root := t.TempDir()
	id := "stale-pid"
	dir := runDir(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	st := State{PID: os.Getpid(), Port: 1, HTTPPort: 2}
	if err := WriteState(root, id, st); err != nil {
		t.Fatal(err)
	}
	if err := Stop(root, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "proxy.json")); !os.IsNotExist(err) {
		t.Fatalf("proxy.json should be removed, err=%v", err)
	}
}
