//go:build integration && darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

// TestIntegrationSoftnetHostOnly proves softnet host-only blocks direct internet.
func TestIntegrationSoftnetHostOnly(t *testing.T) {
	if _, err := exec.LookPath("tart"); err != nil {
		t.Skip("tart not on PATH")
	}
	if _, err := exec.LookPath("softnet"); err != nil {
		t.Skip("softnet not on PATH")
	}
	if !softnetPrivileged() {
		t.Skip("softnet needs setuid or passwordless sudo (brew trust / softnet install)")
	}

	image := os.Getenv("CAGE_TART_IMAGE")
	if image == "" {
		image = "ubuntu"
	}
	if !tartHasImage(image) {
		t.Skipf("image %q not local (set CAGE_TART_IMAGE or tart pull/clone first)", image)
	}

	id := fmt.Sprintf("cage-net-it-%d", os.Getpid())
	spec := runtimeplugin.Spec{
		ID:          id,
		ProjectRoot: t.TempDir(),
		Image:       image,
		Workdir:     "/workspace",
		ExtraRunArgs: []string{
			"--net-softnet-block=0.0.0.0/0",
			"--net-softnet-allow=@host",
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

	blockOut, _ := tartExecOut(id, "bash", "-c", "timeout 5 bash -c 'echo >/dev/tcp/1.1.1.1/53' && echo ok || echo fail")
	if strings.Contains(blockOut, "ok") {
		t.Fatalf("direct internet unexpectedly reachable under host-only softnet: out=%q", blockOut)
	}
}

func softnetPrivileged() bool {
	path, err := exec.LookPath("softnet")
	if err != nil {
		return false
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		real = path
	}
	if fi, err := os.Stat(real); err == nil && fi.Mode()&os.ModeSetuid != 0 {
		return true
	}
	return exec.Command("sudo", "-n", "true").Run() == nil
}
