package main

import (
	"encoding/json"

	"github.com/hashicorp/go-plugin"

	client "github.com/appmatter/cage/pkg/plugin/v1/client"
	fs "github.com/appmatter/cage/pkg/plugin/v1/fs"
)

type service struct{}

func (service) Name() string { return "helper" }

type capability struct{}

func (capability) Name() string                   { return "helper" }
func (capability) Capabilities() []string         { return []string{"echo"} }
func (capability) Configure(client.Context) error { return nil }
func (capability) Call(in client.Request) (client.Response, error) {
	return client.Response{Payload: append(json.RawMessage(nil), in.Payload...)}, nil
}

func main() {
	plugins := fs.PluginMap(service{})
	client.Register(plugins, capability{})
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: fs.Handshake,
		Plugins:         plugins,
	})
}
