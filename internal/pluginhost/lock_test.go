package pluginhost

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertLockEntrySameSourceUpdates(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".cage"), 0o755); err != nil {
		t.Fatal(err)
	}
	m1 := Manifest{Kind: "network", Name: "egress", Source: "git:github.com/acme/egress", Pin: "aaa", Command: "cage-network-egress.cageplugin"}
	if err := UpsertLockEntry(root, m1); err != nil {
		t.Fatal(err)
	}
	m2 := m1
	m2.Pin = "bbb"
	if err := UpsertLockEntry(root, m2); err != nil {
		t.Fatal(err)
	}
	lock, err := LoadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Plugins) != 1 || lock.Plugins[0].Pin != "bbb" {
		t.Fatalf("%#v", lock.Plugins)
	}
}

func TestUpsertLockEntryDifferentSourceFails(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".cage"), 0o755); err != nil {
		t.Fatal(err)
	}
	m1 := Manifest{Kind: "network", Name: "egress", Source: "git:github.com/acme/egress", Pin: "aaa", Command: "c"}
	if err := UpsertLockEntry(root, m1); err != nil {
		t.Fatal(err)
	}
	m2 := Manifest{Kind: "network", Name: "egress", Source: "git:github.com/other/egress", Pin: "ccc", Command: "c"}
	err := UpsertLockEntry(root, m2)
	if err == nil {
		t.Fatal("expected conflict")
	}
	if !strings.Contains(err.Error(), "--name") {
		t.Fatalf("err=%v", err)
	}
}
