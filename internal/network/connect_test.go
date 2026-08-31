package network

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

type allowAll struct{}

func (allowAll) Name() string           { return "allow" }
func (allowAll) Configure([]byte) error { return nil }
func (allowAll) Check(netplugin.Request) (netplugin.Decision, error) {
	return netplugin.Decision{Allow: true}, nil
}

type denyPath struct{}

func (denyPath) Name() string           { return "deny-path" }
func (denyPath) Configure([]byte) error { return nil }
func (denyPath) Check(req netplugin.Request) (netplugin.Decision, error) {
	if req.Partial {
		return netplugin.Decision{Allow: true}, nil
	}
	if req.Path == "/blocked" {
		return netplugin.Decision{Allow: false, Reason: "path"}, nil
	}
	return netplugin.Decision{Allow: true}, nil
}

type injectTerm struct{}

func (injectTerm) Name() string           { return "inject" }
func (injectTerm) Configure([]byte) error { return nil }
func (injectTerm) Prepare(in netplugin.PrepareIn) (netplugin.PrepareOut, error) {
	hdr := map[string][]string{}
	for k, v := range in.Header {
		hdr[k] = append([]string{}, v...)
	}
	hdr["Authorization"] = []string{"Bearer injected"}
	return netplugin.PrepareOut{
		UpstreamHost: "example.com",
		UpstreamPort: 443,
		UpstreamURL:  "https://example.com/",
		Header:       hdr,
	}, nil
}

func mustSelfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "upstream"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"example.com"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func TestHTTPProxyMITMMethodPathInject(t *testing.T) {
	root := t.TempDir()
	mitm, err := LoadOrCreateCA(root)
	if err != nil {
		t.Fatal(err)
	}

	upTLS := mustSelfSigned(t)
	upstreamLN, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{upTLS}})
	if err != nil {
		t.Fatal(err)
	}
	defer upstreamLN.Close()
	upPort := upstreamLN.Addr().(*net.TCPAddr).Port
	gotAuth := make(chan string, 1)
	go func() {
		c, err := upstreamLN.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		tc := c.(*tls.Conn)
		_ = tc.Handshake()
		buf := make([]byte, 8192)
		n, _ := tc.Read(buf)
		for _, l := range strings.Split(string(buf[:n]), "\n") {
			if strings.HasPrefix(strings.ToLower(strings.TrimSpace(l)), "authorization:") {
				gotAuth <- strings.TrimSpace(l)
				break
			}
		}
		_, _ = io.WriteString(tc, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok")
	}()

	pipe := NewPipeline([]FilterSeat{{Priority: 1, Filter: allowAll{}}})
	srv := &HTTPProxyServer{
		Pipeline:     pipe,
		MITM:         mitm,
		Terminate:    injectTerm{},
		HostToEP:     map[string]string{"example.com": "openai"},
		DenyHTTP:     true,
		DenyMessage:  "denied",
		UpstreamTLS:  &tls.Config{InsecureSkipVerify: true},
		Dial: func(_ context.Context, req netplugin.Request) (net.Conn, error) {
			return net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", upPort))
		},
	}
	go func() { _ = srv.ListenAndServe("127.0.0.1:0") }()
	defer srv.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && srv.Port() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if srv.Port() == 0 {
		t.Fatal("no port")
	}

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(mitm.CAPEM())
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", srv.Port())}),
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				ServerName: "example.com",
			},
		},
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get("https://example.com/v1/chat")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(body) != "ok" {
		t.Fatalf("status=%d body=%q", resp.StatusCode, body)
	}
	select {
	case a := <-gotAuth:
		if !strings.Contains(a, "Bearer injected") {
			t.Fatalf("auth=%q", a)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no upstream auth header")
	}
}

func TestHTTPProxyMITMDenyPath(t *testing.T) {
	root := t.TempDir()
	mitm, err := LoadOrCreateCA(root)
	if err != nil {
		t.Fatal(err)
	}
	pipe := NewPipeline([]FilterSeat{{Priority: 1, Filter: denyPath{}}})
	srv := &HTTPProxyServer{
		Pipeline:    pipe,
		MITM:        mitm,
		DenyHTTP:    true,
		DenyMessage: "nope",
		Dial: func(_ context.Context, req netplugin.Request) (net.Conn, error) {
			return nil, fmt.Errorf("should not dial")
		},
	}
	go func() { _ = srv.ListenAndServe("127.0.0.1:0") }()
	defer srv.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && srv.Port() == 0 {
		time.Sleep(5 * time.Millisecond)
	}

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(mitm.CAPEM())
	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", srv.Port())}),
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				ServerName: "example.com",
			},
		},
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get("https://example.com/blocked")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}
