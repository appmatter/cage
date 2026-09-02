//go:build integration && darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/go-plugin"

	runtimeplugin "github.com/appmatter/cage/pkg/plugin/v1/runtime"
)

// TestIntegrationBeforeBakeOnTart: go-plugin BeforeBake (dry) → Tart bake → guest marker.
func TestIntegrationBeforeBakeOnTart(t *testing.T) {
	if _, err := exec.LookPath("tart"); err != nil {
		t.Skip("tart not on PATH")
	}
	image := os.Getenv("CAGE_TART_IMAGE")
	if image == "" {
		image = "ubuntu"
	}
	if !localTartImage(image) {
		t.Skipf("image %q not local", image)
	}

	bin := filepath.Join(t.TempDir(), "pi-agent.cageplugin")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: runtimeplugin.Handshake,
		Plugins: map[string]plugin.Plugin{
			runtimeplugin.HooksPluginName: &runtimeplugin.HooksRPCPlugin{},
		},
		Cmd:              exec.Command(bin),
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolNetRPC},
	})
	defer client.Kill()
	rpcClient, err := client.Client()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := rpcClient.Dispense(runtimeplugin.HooksPluginName)
	if err != nil {
		t.Fatal(err)
	}
	hooks := raw.(runtimeplugin.Hooks)
	if len(hooks.Hooks()) < 3 {
		t.Fatalf("declared hooks=%v", hooks.Hooks())
	}

	atts, err := hooks.BeforeBake(runtimeplugin.HookContext{
		DryRun:   true,
		SeatYAML: []byte("version: \"0.1.0\"\npackages:\n  - npm:@acme/it@1\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "bake.sh")
	if err := os.WriteFile(script, atts[0].Body, 0o755); err != nil {
		t.Fatal(err)
	}

	derived := fmt.Sprintf("cage-pi-it-%d", os.Getpid())
	defer func() {
		_ = exec.Command("tart", "stop", derived).Run()
		_ = exec.Command("tart", "delete", derived).Run()
	}()
	_ = exec.Command("tart", "delete", derived).Run()

	if err := exec.Command("tart", "clone", image, derived).Run(); err != nil {
		t.Fatalf("clone: %v", err)
	}
	run := exec.Command("tart", "run", "--no-graphics", derived)
	if err := run.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { _ = run.Wait() }()
	if err := waitGuest(derived, 60*time.Second); err != nil {
		t.Fatal(err)
	}

	body, _ := os.ReadFile(script)
	bake := exec.Command("tart", "exec", "-i", derived, "sudo", "sh", "-s")
	bake.Stdin = strings.NewReader(string(body))
	if out, err := bake.CombinedOutput(); err != nil {
		t.Fatalf("bake script: %v\n%s", err, out)
	}

	out, err := exec.Command("tart", "exec", derived, "cat", "/var/lib/cage/pi-agent-baked").CombinedOutput()
	if err != nil {
		t.Fatalf("marker: %v\n%s", err, out)
	}
	got := string(out)
	if !strings.Contains(got, "version=0.1.0") || !strings.Contains(got, "npm:@acme/it@1") {
		t.Fatalf("marker=%q", got)
	}
}

func localTartImage(name string) bool {
	out, err := exec.Command("tart", "list", "--format", "json").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), name)
}

func waitGuest(id string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := exec.Command("tart", "exec", id, "true").Run(); err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("guest %s not ready", id)
}
