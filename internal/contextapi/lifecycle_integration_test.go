//go:build integration && darwin

package contextapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/appmatter/cage/internal/pluginhost"
	"github.com/appmatter/cage/internal/vminstance"
)

// Context-serve start → running → stop → delete against a real Tart VM
// (conformance L1 over HTTP). Skips if tart, the image, or a build of this
// checkout's tart plugin is unavailable.
func TestIntegrationLifecycleTart(t *testing.T) {
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

	root := t.TempDir()
	configPath := createIntegrationTestProject(t, root, image)
	if err := installTartPlugin(t, root); err != nil {
		t.Fatal(err)
	}

	project, err := LoadProject(root, []string{configPath}, "darwin")
	if err != nil {
		t.Fatal(err)
	}
	s := New(Options{
		Project:   project,
		Authorize: func(*http.Request, string, string) bool { return true },
	})
	defer s.Close()

	instanceID := fmt.Sprintf("ctx-it-%d", os.Getpid())
	_, backendID, err := vminstance.Resolve(root, configPath, instanceID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = exec.Command("tart", "stop", backendID).Run()
		_ = exec.Command("tart", "delete", backendID).Run()
	}()

	base := "/v1/configs/default/vms/" + instanceID
	if w := call(s, http.MethodPost, base+"/start", ""); w.Code != 204 {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	if !listHasState(t, s, instanceID, "running") {
		t.Fatal("expected running")
	}

	if w := call(s, http.MethodPost, base+"/stop", ""); w.Code != 204 {
		t.Fatalf("stop: %d %s", w.Code, w.Body.String())
	}
	if !listHasState(t, s, instanceID, "stopped") {
		t.Fatal("expected stopped")
	}

	if w := call(s, http.MethodPost, base+"/delete", ""); w.Code != 204 {
		t.Fatalf("delete: %d %s", w.Code, w.Body.String())
	}
	body := call(s, http.MethodGet, "/v1/configs/default/vms", "").Body.String()
	if strings.Contains(body, `"`+instanceID+`"`) {
		t.Fatalf("deleted id still listed: %s", body)
	}
	if !strings.Contains(body, `"default"`) {
		t.Fatalf("default missing: %s", body)
	}
}

func listHasState(t *testing.T, s *Server, id, state string) bool {
	t.Helper()
	w := call(s, http.MethodGet, "/v1/configs/default/vms", "")
	if w.Code != 200 {
		t.Fatalf("list: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		VMs []struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"vms"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, vm := range out.VMs {
		if vm.ID == id && vm.State == state {
			return true
		}
	}
	t.Logf("list=%s", w.Body.String())
	return false
}

func createIntegrationTestProject(t *testing.T, root, image string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".cage"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".cage", "cage.yaml")
	body := fmt.Sprintf(`
version: 1
runtime:
  plugins:
    tart:
      priority: 1
      image: %s
  workdir: /workspace
fs:
  layout:
    mode: flat
`, image)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func installTartPlugin(t *testing.T, project string) error {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	src := filepath.Clean(filepath.Join(wd, "../../plugins/runtime/tart"))
	dir := filepath.Join(pluginhost.ProjectDir(project), "runtime", "tart")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	bin := filepath.Join(dir, "cage-runtime-tart"+pluginhost.BinaryExt)
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = src
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build tart plugin: %w\n%s", err, out)
	}
	return pluginhost.WriteManifest(pluginhost.ProjectDir(project), pluginhost.Manifest{
		Kind:    "runtime",
		Name:    "tart",
		Command: bin,
		Source:  "plugins/runtime/tart",
		Stage:   "backend",
	})
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
