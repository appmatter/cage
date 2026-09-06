package contextapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/appmatter/cage/internal/config"
	"github.com/appmatter/cage/internal/pluginhost"
	clientplugin "github.com/appmatter/cage/pkg/plugin/v1/client"
)

// YAML seat names are the {seat} path segment. Missing client plugins error
// for runtime/fs/network; secrets stay unstarted (no plugin, no error).
func TestLoadSeatsUsesConfiguredNamesForEveryContext(t *testing.T) {
	var file config.File
	if err := yaml.Unmarshal([]byte(`
runtime:
  plugins:
    runtime-seat: {plugin: absent-runtime, image: image}
fs:
  plugins:
    fs-seat: {plugin: absent-fs}
network:
  plugins:
    network-seat: {plugin: absent-network}
secrets:
  plugins:
    secrets-seat: {plugin: absent-secrets}
`), &file); err != nil {
		t.Fatal(err)
	}
	seats := LoadSeats(t.TempDir(), config.Resolved{Runtime: file.Runtime, FS: file.FS, Network: file.Network, Secrets: file.Secrets}, VMMeta{})
	for context, seat := range map[string]string{"runtime": "runtime-seat", "fs": "fs-seat", "network": "network-seat"} {
		got, ok := seats.Seats[context][seat]
		if !ok || got.Err == nil {
			t.Fatalf("%s/%s: %#v", context, seat, got)
		}
	}
	if got := seats.Seats["secrets"]["secrets-seat"]; got.Plugin != nil || got.Err != nil {
		t.Fatalf("undeclared secret seat: %#v", got)
	}
}

type loadedFSPlugin struct{}

func (loadedFSPlugin) Name() string                         { return "fake" }
func (loadedFSPlugin) Capabilities() []string               { return []string{"echo"} }
func (loadedFSPlugin) Configure(clientplugin.Context) error { return nil }
func (loadedFSPlugin) Call(in clientplugin.Request) (clientplugin.Response, error) {
	return clientplugin.Response{Payload: in.Payload}, nil
}

// Secrets seats are not client capabilities unless the plugin opts in.
// A default secrets binary must not start.
func TestLoadSeatsDoesNotStartDefaultSecretsPlugin(t *testing.T) {
	project := t.TempDir()
	root := filepath.Join(pluginhost.ProjectDir(project), "secrets", "default")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(project, "started")
	command := filepath.Join(root, "default.cageplugin")
	if err := os.WriteFile(command, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := pluginhost.WriteManifest(pluginhost.ProjectDir(project), pluginhost.Manifest{Kind: "secrets", Name: "default", Command: filepath.Base(command)}); err != nil {
		t.Fatal(err)
	}
	loaded := LoadSeats(project, config.Resolved{Secrets: config.Secrets{Plugins: map[string]config.SecretStore{"ordinary": {Plugin: "default"}}}}, VMMeta{})
	defer loaded.Close()
	seat := loaded.Seats["secrets"]["ordinary"]
	if seat.Plugin != nil || seat.Err != nil {
		t.Fatalf("default secret seat: %#v", seat)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("default secret plugin started: %v", err)
	}
}

// Manifest client:false (or omitted) means LoadSeats must not spawn the
// runtime plugin process.
func TestLoadSeatsDoesNotStartUndeclaredRuntimePlugin(t *testing.T) {
	project := t.TempDir()
	root := filepath.Join(pluginhost.ProjectDir(project), "runtime", "default")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(project, "started")
	command := filepath.Join(root, "default.cageplugin")
	if err := os.WriteFile(command, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := pluginhost.WriteManifest(pluginhost.ProjectDir(project), pluginhost.Manifest{Kind: "runtime", Name: "default", Command: filepath.Base(command)}); err != nil {
		t.Fatal(err)
	}
	loaded := LoadSeats(project, config.Resolved{Runtime: config.Runtime{Plugins: map[string]config.RuntimePlugin{"ordinary": {Plugin: "default"}}}}, VMMeta{})
	defer loaded.Close()
	seat := loaded.Seats["runtime"]["ordinary"]
	if seat.Plugin != nil || seat.Err != nil {
		t.Fatalf("undeclared runtime seat: %#v", seat)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("undeclared runtime plugin started: %v", err)
	}
}

// A client-declared fs seat is callable over HTTP and echoes the payload
// unchanged.
func TestLoadSeatsCallsConfiguredGenericPlugin(t *testing.T) {
	var plugins config.FSPlugins
	if err := yaml.Unmarshal([]byte("ordinary: {plugin: fake}\n"), &plugins); err != nil {
		t.Fatal(err)
	}
	loaded := loadSeats(t.TempDir(), config.Resolved{
		Runtime: config.Runtime{Workdir: "/work"},
		Layout:  config.Layout{Mode: "flat"},
		FS:      config.FS{Plugins: plugins},
	}, VMMeta{InstanceID: "default", BackendVMID: "cage-test"}, func(_ string, context string, seat config.ConfiguredSeat) (clientplugin.Capability, bool, func(), error) {
		if context != "fs" || seat.Name != "ordinary" || seat.PluginID != "fake" {
			t.Fatalf("loaded seat: %s/%+v", context, seat)
		}
		return loadedFSPlugin{}, true, func() {}, nil
	})
	seat := loaded.Seats["fs"]["ordinary"]
	if seat.Plugin == nil || seat.Err != nil {
		t.Fatalf("seat: %#v", seat)
	}
	project := testProject(t, "default")
	server := New(Options{
		Project:   project,
		Authorize: func(*http.Request, string, string) bool { return true },
		LoadSeats: func(Entry, VMMeta) *LoadedSeats { return loaded },
	})
	defer server.Close()
	response := call(server, http.MethodPost, callPath("default", "default", "fs", "ordinary"), `{"operation":"echo","payload":{"unchanged":true}}`)
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	var got struct {
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Payload) != `{"unchanged":true}` {
		t.Fatalf("payload: %s", got.Payload)
	}
}
