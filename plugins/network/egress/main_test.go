package main

import (
	"testing"

	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

func TestCheckHostPortMethodPath(t *testing.T) {
	e := &Egress{}
	if err := e.Configure([]byte(`
allow:
  - host: api.openai.com
    port: 443
    method: POST
    path: /v1/chat/completions
  - host: "*.npmjs.org"
  - host: "*"
    port: 53
`)); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		req   netplugin.Request
		allow bool
	}{
		{netplugin.Request{Host: "api.openai.com", Port: 443, Method: "POST", Path: "/v1/chat/completions"}, true},
		{netplugin.Request{Host: "api.openai.com", Port: 443, Method: "GET", Path: "/v1/chat/completions"}, false},
		{netplugin.Request{Host: "api.openai.com", Port: 80, Method: "POST", Path: "/v1/chat/completions"}, false},
		{netplugin.Request{Host: "api.openai.com", Port: 443}, false}, // needs method/path
		{netplugin.Request{Host: "api.openai.com", Port: 443, Partial: true}, true},
		{netplugin.Request{Host: "registry.npmjs.org", Port: 443}, true},
		{netplugin.Request{Host: "foo.npmjs.org", Port: 443}, true},
		{netplugin.Request{Host: "npmjs.org", Port: 443}, true},
		{netplugin.Request{Host: "dns.google", Port: 53}, true},
		{netplugin.Request{Host: "evil.example", Port: 443}, false},
	}
	for _, tc := range cases {
		d, err := e.Check(tc.req)
		if err != nil {
			t.Fatalf("%+v: %v", tc.req, err)
		}
		if d.Allow != tc.allow {
			t.Fatalf("%+v allow=%v want %v reason=%q", tc.req, d.Allow, tc.allow, d.Reason)
		}
	}
}

func TestCheckDeny(t *testing.T) {
	e := &Egress{}
	if err := e.Configure([]byte(`
allow:
  - host: "*"
deny:
  - host: evil.example
    port: 443
  - host: api.openai.com
    method: GET
`)); err != nil {
		t.Fatal(err)
	}
	if d, _ := e.Check(netplugin.Request{Host: "evil.example", Port: 443}); d.Allow {
		t.Fatal("deny host")
	}
	if d, _ := e.Check(netplugin.Request{Host: "api.openai.com", Port: 443, Method: "GET", Path: "/"}); d.Allow {
		t.Fatal("deny method")
	}
	if d, _ := e.Check(netplugin.Request{Host: "api.openai.com", Port: 443, Method: "POST", Path: "/"}); !d.Allow {
		t.Fatal("allow other method")
	}
	if d, _ := e.Check(netplugin.Request{Host: "ok.example", Port: 80}); !d.Allow {
		t.Fatal("allow rest")
	}
}

func TestCheckDenyOnly(t *testing.T) {
	e := &Egress{}
	if err := e.Configure([]byte(`
deny:
  - host: blocked.example
`)); err != nil {
		t.Fatal(err)
	}
	if d, _ := e.Check(netplugin.Request{Host: "blocked.example", Port: 443}); d.Allow {
		t.Fatal("denied")
	}
	if d, _ := e.Check(netplugin.Request{Host: "ok.example", Port: 443}); !d.Allow {
		t.Fatal("allowed")
	}
}

func TestConfigureReload(t *testing.T) {
	e := &Egress{}
	if err := e.Configure([]byte(`allow: [{host: a.example}]`)); err != nil {
		t.Fatal(err)
	}
	if d, _ := e.Check(netplugin.Request{Host: "a.example", Port: 80}); !d.Allow {
		t.Fatal("a")
	}
	if err := e.Configure([]byte(`allow: [{host: b.example}]`)); err != nil {
		t.Fatal(err)
	}
	if d, _ := e.Check(netplugin.Request{Host: "a.example", Port: 80}); d.Allow {
		t.Fatal("a should be gone")
	}
	if d, _ := e.Check(netplugin.Request{Host: "b.example", Port: 80}); !d.Allow {
		t.Fatal("b")
	}
}
