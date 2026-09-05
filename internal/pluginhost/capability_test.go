package pluginhost

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"

	client "github.com/appmatter/cage/pkg/plugin/v1/client"
)

func TestDispenseFSCapability(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "capability")
	build := exec.Command("go", "build", "-o", binary, "./testdata/capability")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, out)
	}
	loaded, declared, err := DispenseFSCapability(binary)
	if err != nil || !declared || loaded == nil {
		t.Fatalf("dispense: client=%#v declared=%v err=%v", loaded, declared, err)
	}
	defer loaded.Close()
	if err := loaded.Capability.Configure(client.Context{Kind: "fs", Data: json.RawMessage(`{"projectRoot":"/project"}`)}); err != nil {
		t.Fatal(err)
	}
	out, err := loaded.Capability.Call(client.Request{Operation: "echo", Payload: json.RawMessage(`{"unchanged":true}`)})
	if err != nil || string(out.Payload) != `{"unchanged":true}` {
		t.Fatalf("call: payload=%s err=%v", out.Payload, err)
	}
}
