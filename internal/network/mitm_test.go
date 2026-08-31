package network

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateCA(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "proj")
	if err := os.MkdirAll(filepath.Join(root, ".cage"), 0o755); err != nil {
		t.Fatal(err)
	}
	m1, err := LoadOrCreateCA(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(m1.CAPEM()) == 0 {
		t.Fatal("empty pem")
	}
	m2, err := LoadOrCreateCA(root)
	if err != nil {
		t.Fatal(err)
	}
	if string(m1.CAPEM()) != string(m2.CAPEM()) {
		t.Fatal("ca not stable across loads")
	}
	leaf, err := m1.LeafForHost("api.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(leaf.Certificate) < 1 {
		t.Fatal("no leaf")
	}
	cert, err := x509.ParseCertificate(leaf.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := cert.VerifyHostname("api.example.com"); err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(m1.CAPEM()) {
		t.Fatal("append ca")
	}
	if _, err := cert.Verify(x509.VerifyOptions{DNSName: "api.example.com", Roots: roots}); err != nil {
		t.Fatal(err)
	}
	_ = tls.Certificate{}
}

func TestParseHostEndpointMap(t *testing.T) {
	raw := []byte(`
openai:
  url: https://api.openai.com/v1
  headers:
    Authorization: "Bearer x"
priority: 1
`)
	m, err := ParseHostEndpointMap(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m["api.openai.com"] != "openai" {
		t.Fatalf("got %#v", m)
	}
}
