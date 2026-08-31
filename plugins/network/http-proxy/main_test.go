package main

import (
	"os"
	"testing"

	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

func TestResolveTemplate(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	got, err := resolveTemplate("Bearer {{ env.OPENAI_API_KEY }}")
	if err != nil || got != "Bearer sk-test" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if _, err := resolveTemplate("{{ secrets.x.Y }}"); err == nil {
		t.Fatal("expected secrets error")
	}
	if _, err := resolveTemplate("{{ env.MISSING_CAGE_VAR }}"); err == nil {
		t.Fatal("expected missing env")
	}
	got, err = resolveTemplate("literal")
	if err != nil || got != "literal" {
		t.Fatalf("literal %q %v", got, err)
	}
}

func TestPrepareJoin(t *testing.T) {
	h := &HTTPProxy{}
	raw := []byte(`
openai:
  url: https://api.openai.com/v1
  headers:
    Authorization: "Bearer literal-key"
`)
	if err := h.Configure(raw); err != nil {
		t.Fatal(err)
	}
	out, err := h.Prepare(netplugin.PrepareIn{
		Endpoint: "openai",
		Method:   "POST",
		Path:     "/chat/completions?n=1",
		Header:   map[string][]string{"Host": {"gateway"}, "X-Guest": {"1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.UpstreamHost != "api.openai.com" || out.UpstreamPort != 443 {
		t.Fatalf("host/port %#v", out)
	}
	if out.UpstreamURL != "https://api.openai.com/v1/chat/completions?n=1" {
		t.Fatalf("url %q", out.UpstreamURL)
	}
	if out.Header["Authorization"][0] != "Bearer literal-key" {
		t.Fatalf("auth %#v", out.Header)
	}
	if _, ok := out.Header["Host"]; ok {
		t.Fatal("host should be stripped")
	}
	if out.Header["X-Guest"][0] != "1" {
		t.Fatal("guest header kept")
	}
}

func TestConfigureRequiresURL(t *testing.T) {
	h := &HTTPProxy{}
	if err := h.Configure([]byte("x:\n  headers: {}\n")); err == nil {
		t.Fatal("expected url required")
	}
	_ = os.Getenv
}
