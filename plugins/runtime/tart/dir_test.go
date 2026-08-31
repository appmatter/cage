package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

func TestDirArgs(t *testing.T) {
	got := dirArgs([]runtimeplugin.PathSpec{
		{Host: "/host/src", Guest: "/workspace/src", Permission: "rw"},
		{Host: "/host/tests", Guest: "/workspace/tests", Permission: "ro"},
	})
	want := []string{
		"--dir=" + shareName("/workspace/src") + ":/host/src",
		"--dir=" + shareName("/workspace/tests") + ":/host/tests:ro",
	}
	if len(got) != len(want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v want %#v", got, want)
		}
	}
}

func TestPartitionMountsNestedRO(t *testing.T) {
	top, nestedRO, nestedRW, err := partitionMounts([]runtimeplugin.PathSpec{
		{Host: "/repo", Guest: "/workspace", Permission: "rw"},
		{Host: "/elsewhere/.git", Guest: "/workspace/.git", Permission: "ro"},
		{Host: "/other", Guest: "/mnt/docs", Permission: "rw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 2 || len(nestedRO) != 1 || len(nestedRW) != 0 {
		t.Fatalf("top=%d nestedRO=%d nestedRW=%d", len(top), len(nestedRO), len(nestedRW))
	}
	if nestedRO[0].Guest != "/workspace/.git" || nestedRO[0].Host != "/elsewhere/.git" {
		t.Fatalf("nestedRO=%#v", nestedRO)
	}
	dirs := dirArgs(append(append([]runtimeplugin.PathSpec{}, top...), nestedRO...))
	if len(dirs) != 3 {
		t.Fatalf("expected parent+sibling+nested ro dirs, got %v", dirs)
	}
	joined := strings.Join(dirs, " ")
	if !strings.Contains(joined, shareName("/workspace/.git")+":/elsewhere/.git:ro") {
		t.Fatalf("nested ro must keep its own host share: %v", dirs)
	}
}

func TestPartitionMountsNestedRW(t *testing.T) {
	top, nestedRO, nestedRW, err := partitionMounts([]runtimeplugin.PathSpec{
		{Host: "/repo", Guest: "/workspace", Permission: "ro"},
		{Host: "/repo/scratch", Guest: "/workspace/scratch", Permission: "rw"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(top) != 1 || len(nestedRO) != 0 || len(nestedRW) != 1 {
		t.Fatalf("top=%d nestedRO=%d nestedRW=%d", len(top), len(nestedRO), len(nestedRW))
	}
	dirs := dirArgs(append(append([]runtimeplugin.PathSpec{}, top...), nestedRW...))
	if len(dirs) != 2 {
		t.Fatalf("expected parent+nested rw dirs, got %v", dirs)
	}
	joined := strings.Join(dirs, " ")
	if !strings.Contains(joined, ":ro") {
		t.Fatalf("parent should be ro: %v", dirs)
	}
	if strings.Count(joined, ":ro") != 1 {
		t.Fatalf("nested rw must not get :ro: %v", dirs)
	}
	if !strings.Contains(joined, shareName("/workspace/scratch")+":") {
		t.Fatalf("nested rw share missing: %v", dirs)
	}
}

func TestGuestUnder(t *testing.T) {
	if !guestUnder("/workspace/.git", "/workspace") {
		t.Fatal("expected .git under workspace")
	}
	if guestUnder("/workspace", "/workspace") {
		t.Fatal("equal is not under")
	}
	if guestUnder("/workspace2", "/workspace") {
		t.Fatal("prefix sibling")
	}
}

func TestShareName(t *testing.T) {
	a := shareName("/workspace/a.b")
	b := shareName("/workspace/a/b")
	if a == b {
		t.Fatalf("collision: %q == %q", a, b)
	}
	if !strings.HasPrefix(a, "workspace_a_b_") || !strings.HasPrefix(b, "workspace_a_b_") {
		t.Fatalf("unexpected prefixes: %q %q", a, b)
	}
	if shareName("/workspace/src") != shareName("/workspace/src") {
		t.Fatal("not stable")
	}
	if got := shareName("/"); !strings.HasPrefix(got, "share_") {
		t.Fatalf("root=%q", got)
	}
	if got := shareName(""); !strings.HasPrefix(got, "share_") {
		t.Fatalf("empty=%q", got)
	}
}

func TestCheckMountHosts(t *testing.T) {
	root := t.TempDir()
	ok := filepath.Join(root, "ok")
	if err := os.Mkdir(ok, 0o755); err != nil {
		t.Fatal(err)
	}
	err := checkMountHosts([]runtimeplugin.PathSpec{
		{Host: ok, Guest: "/workspace/ok"},
		{Host: filepath.Join(root, "missing"), Guest: "/workspace/missing"},
	})
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("err=%v", err)
	}
}

func TestExtraRunArgsAppended(t *testing.T) {
	extra := []string{"--net-softnet-block=0.0.0.0/0", "--net-softnet-allow=@host"}
	dirs := dirArgs([]runtimeplugin.PathSpec{{Host: "/h", Guest: "/workspace/x"}})
	got := append(append([]string{"run", "--no-graphics"}, extra...), dirs...)
	got = append(got, "vm")
	want := []string{
		"run", "--no-graphics",
		"--net-softnet-block=0.0.0.0/0", "--net-softnet-allow=@host",
		"--dir=" + shareName("/workspace/x") + ":/h", "vm",
	}
	if len(got) != len(want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %#v want %#v", got, want)
		}
	}
}

func TestCopyMode(t *testing.T) {
	if got := copyMode("rw"); got != "0644" {
		t.Fatalf("rw=%q", got)
	}
	if got := copyMode("ro"); got != "0444" {
		t.Fatalf("ro=%q", got)
	}
	if got := copyMode(""); got != "0644" {
		t.Fatalf("default=%q", got)
	}
}

func TestTartRunHasArgsEmpty(t *testing.T) {
	if !tartRunHasArgs("any", nil) {
		t.Fatal("empty want should match")
	}
}
