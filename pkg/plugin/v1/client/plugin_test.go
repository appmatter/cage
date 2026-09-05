package client

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/go-plugin"
)

type testCapability struct{}

func (testCapability) Name() string            { return "test" }
func (testCapability) Capabilities() []string  { return []string{"call"} }
func (testCapability) Configure(Context) error { return nil }
func (testCapability) Call(Request) (Response, error) {
	return Response{Payload: json.RawMessage(`null`)}, nil
}

func TestRegisterAddsClientService(t *testing.T) {
	plugins := map[string]plugin.Plugin{}
	Register(plugins, testCapability{})
	if _, ok := plugins[PluginName]; !ok {
		t.Fatal("client service missing")
	}
}

func TestRegisterNilIsNoop(t *testing.T) {
	plugins := map[string]plugin.Plugin{}
	Register(plugins, nil)
	if _, ok := plugins[PluginName]; ok {
		t.Fatal("nil capability registered")
	}
}
