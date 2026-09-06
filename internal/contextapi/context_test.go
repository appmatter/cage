package contextapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/appmatter/cage/internal/config"
	fsplugin "github.com/appmatter/cage/pkg/plugin/v1/fs"
)

func TestFSContextIncludesResolvedData(t *testing.T) {
	for _, layout := range []string{"flat", "host"} {
		in := config.Resolved{Runtime: config.Runtime{Workdir: "/work"}, Layout: config.Layout{Mode: layout}, Mounts: []config.ResolvedPath{{Host: "/project/a", Target: "a", Guest: "/work/a", Permission: "ro"}}, Copies: []config.ResolvedPath{{Host: "/project/b", Target: "b", Guest: "/work/b", Permission: "rw"}}, Deny: []string{".env"}}
		out, err := FSContext("/project", in, []byte("seat: value\n"), VMMeta{})
		if err != nil {
			t.Fatal(err)
		}
		var got fsplugin.Context
		if err := json.Unmarshal(out.Data, &got); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out.Data), `"projectRoot"`) || strings.Contains(string(out.Data), `"ProjectRoot"`) {
			t.Fatalf("unstable fs context JSON: %s", out.Data)
		}
		if out.Kind != "fs" || got.ProjectRoot != "/project" || got.Workdir != "/work" || got.Layout != layout || got.Mounts[0].Host != "/project/a" || got.Copies[0].Host != "/project/b" || got.Deny[0] != ".env" || string(got.SeatYAML) != "seat: value\n" {
			t.Fatalf("%+v", got)
		}
	}
}

func TestNetworkContextStripsSecretHeaders(t *testing.T) {
	disabled, logging, mitm := true, true, false
	out, err := NetworkContext(config.Resolved{Network: config.Network{Proxy: config.NetworkProxy{
		Disabled: &disabled,
		Logging:  &logging,
		MITM:     &mitm,
	}}}, []byte("url: https://proxy.example\nheaders:\n  Authorization: 'Bearer {{ secrets.api.token }}'\n  X-Trace: enabled\n"), VMMeta{})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Policy   NetworkPolicy
		SeatYAML []byte
	}
	if err := json.Unmarshal(out.Data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Policy.ProxyEnabled || !got.Policy.Logging || got.Policy.MITM {
		t.Fatalf("policy: %+v", got.Policy)
	}
	if len(got.SeatYAML) == 0 || strings.Contains(string(got.SeatYAML), "secrets.api.token") {
		t.Fatalf("unsafe seat yaml: %s", got.SeatYAML)
	}
	if strings.Contains(string(got.SeatYAML), "Authorization") || !strings.Contains(string(got.SeatYAML), "X-Trace") {
		t.Fatalf("headers: %s", got.SeatYAML)
	}
	if strings.Contains(string(out.Data), "proxy.example") {
		t.Fatalf("network endpoint leaked into context: %s", out.Data)
	}
}

func TestFSPluginReceivesResolvedContext(t *testing.T) {
	for _, layout := range []string{"flat", "host"} {
		context, err := FSContext("/project", config.Resolved{Runtime: config.Runtime{Workdir: "/work"}, Layout: config.Layout{Mode: layout}, Mounts: []config.ResolvedPath{{Host: "/project/a", Target: "a", Guest: "/work/a"}}, Copies: []config.ResolvedPath{{Host: "/project/b", Target: "b", Guest: "/work/b"}}, Deny: []string{".env"}}, []byte("include: ['**/*']\n"), VMMeta{InstanceID: "default", BackendVMID: "cage-abc"})
		if err != nil {
			t.Fatal(err)
		}
		plugin := &fakePlugin{caps: []string{"inspect"}}
		project := testProject(t, "default")
		server := New(Options{
			Project:   project,
			Authorize: func(*http.Request, string, string) bool { return true },
			LoadSeats: func(Entry, VMMeta) *LoadedSeats {
				return &LoadedSeats{Seats: map[string]map[string]Seat{"fs": {"ordinary": {Context: context, Plugin: plugin}}}}
			},
		})
		defer server.Close()
		if response := call(server, http.MethodPost, callPath("default", "default", "fs", "ordinary"), `{"operation":"inspect","payload":null}`); response.Code != 200 {
			t.Fatal(response.Code)
		}
		var got fsplugin.Context
		if err := json.Unmarshal(plugin.configured.Data, &got); err != nil {
			t.Fatal(err)
		}
		if got.ProjectRoot != "/project" || got.Workdir != "/work" || got.Layout != layout || len(got.Mounts) != 1 || len(got.Copies) != 1 || len(got.Deny) != 1 || string(got.SeatYAML) != "include: ['**/*']\n" {
			t.Fatalf("%+v", got)
		}
		if got.InstanceID != "default" || got.BackendVMID != "cage-abc" {
			t.Fatalf("vm meta: %+v", got)
		}
	}
}
