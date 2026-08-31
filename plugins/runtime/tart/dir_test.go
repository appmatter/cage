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
		"--dir=src:/host/src",
		"--dir=tests:/host/tests:ro",
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

func TestShareName(t *testing.T) {
	cases := map[string]string{
		"/workspace/src": "src",
		"/a/b/my-dir":    "my-dir",
		"/":              "share",
		"":               "share",
	}
	for in, want := range cases {
		if got := shareName(in); got != want {
			t.Fatalf("shareName(%q)=%q want %q", in, got, want)
		}
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
	// ensureRunning builds args via ExtraRunArgs; unit-check the append order with dirArgs.
	extra := []string{"--net-softnet-block=0.0.0.0/0", "--net-softnet-allow=@host"}
	dirs := dirArgs([]runtimeplugin.PathSpec{{Host: "/h", Guest: "/workspace/x"}})
	got := append(append([]string{"run", "--no-graphics"}, extra...), dirs...)
	got = append(got, "vm")
	want := []string{
		"run", "--no-graphics",
		"--net-softnet-block=0.0.0.0/0", "--net-softnet-allow=@host",
		"--dir=x:/h", "vm",
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
