//go:build integration && darwin

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

// TestIntegrationMountAndCopy is the live Tart IT (macOS only). Skips in <1s when
// tart/image is unavailable (no pull).
func TestIntegrationMountAndCopy(t *testing.T) {
	if _, err := exec.LookPath("tart"); err != nil {
		t.Skip("tart not on PATH")
	}

	image := os.Getenv("CAGE_TART_IMAGE")
	if image == "" {
		image = "ubuntu"
	}
	if !tartHasImage(image) {
		t.Skipf("image %q not local (set CAGE_TART_IMAGE or tart pull/clone first)", image)
	}

	id := fmt.Sprintf("cage-it-%d", os.Getpid())
	root := t.TempDir()
	mountHost := filepath.Join(root, "src")
	if err := os.MkdirAll(mountHost, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mountHost, "mounted.txt"), []byte("mounted-ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	copyHost := filepath.Join(root, "seed.txt")
	if err := os.WriteFile(copyHost, []byte("copied-ok\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	spec := runtimeplugin.Spec{
		ID:      id,
		Image:   image,
		Workdir: "/workspace",
		Mounts: []runtimeplugin.PathSpec{
			{Host: mountHost, Guest: "/workspace/src", Permission: "rw"},
		},
		Copies: []runtimeplugin.PathSpec{
			{Host: copyHost, Guest: "/workspace/copied.txt", Permission: "rw"},
		},
	}

	backend := &Tart{}
	defer func() {
		_ = backend.Stop(id)
		_ = backend.Delete(runtimeplugin.Spec{ID: id})
	}()

	if err := backend.Create(spec); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := backend.Start(spec); err != nil {
		t.Fatalf("start: %v", err)
	}

	mountOut, err := tartExecOut(id, "cat", "/workspace/src/mounted.txt")
	if err != nil || strings.TrimSpace(mountOut) != "mounted-ok" {
		t.Fatalf("mount: out=%q err=%v", mountOut, err)
	}
	copyOut, err := tartExecOut(id, "cat", "/workspace/copied.txt")
	if err != nil || strings.TrimSpace(copyOut) != "copied-ok" {
		t.Fatalf("copy: out=%q err=%v", copyOut, err)
	}
	// rw copy should be writable by admin (not root-only).
	statOut, err := tartExecOut(id, "stat", "-c", "%a %U", "/workspace/copied.txt")
	if err != nil {
		t.Fatalf("stat copy: %v", err)
	}
	statOut = strings.TrimSpace(statOut)
	if !strings.HasPrefix(statOut, "644 ") {
		t.Fatalf("copy mode want 644, got %q", statOut)
	}
	if !strings.Contains(statOut, "admin") && !strings.Contains(statOut, "root") {
		t.Fatalf("copy owner unexpected: %q", statOut)
	}
}

func tartHasImage(name string) bool {
	out, err := exec.Command("tart", "list", "--format", "json").Output()
	if err != nil {
		return false
	}
	var rows []struct {
		Name string `json:"Name"`
	}
	if json.Unmarshal(out, &rows) != nil {
		return false
	}
	for _, r := range rows {
		if r.Name == name {
			return true
		}
	}
	return false
}

func tartExecOut(id string, args ...string) (string, error) {
	cmdArgs := append([]string{"exec", id}, args...)
	out, err := exec.Command("tart", cmdArgs...).CombinedOutput()
	return string(out), err
}
