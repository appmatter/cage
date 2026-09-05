package network

import (
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

func TestSOCKSAllowAndDeny(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	go func() {
		for {
			c, err := upstream.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(io.Discard, c)
			}(c)
		}
	}()
	upPort := upstream.Addr().(*net.TCPAddr).Port

	var logs []TrafficEvent
	logFn := TrafficLogger(MultiTrafficLogger{&captureTraffic{&logs}})

	pipe := NewPipeline([]FilterSeat{
		{Priority: 1, Filter: &stubFilter{allow: true, reason: "ok"}},
	})
	srv := &Server{Pipeline: pipe, OnTraffic: logFn, HTTPPeekWait: -1}
	go func() { _ = srv.ListenAndServe("127.0.0.1:0") }()
	defer srv.Close()
	waitPort(t, srv)

	conn, err := socksDial(t, srv.Port(), "127.0.0.1", upPort)
	if err != nil {
		t.Fatalf("allow dial: %v", err)
	}
	_ = conn.Close()
	waitTraffic(t, &logs, 1)
	if logs[0].Action != "ALLOW" || logs[0].Host != "127.0.0.1" {
		t.Fatalf("allow logs=%v", logs)
	}

	logs = nil
	denyPipe := NewPipeline([]FilterSeat{
		{Priority: 1, Filter: &stubFilter{allow: false, reason: "nope"}},
	})
	denySrv := &Server{Pipeline: denyPipe, OnTraffic: logFn, HTTPPeekWait: -1}
	go func() { _ = denySrv.ListenAndServe("127.0.0.1:0") }()
	defer denySrv.Close()
	waitPort(t, denySrv)
	if _, err := socksDial(t, denySrv.Port(), "127.0.0.1", upPort); err == nil {
		t.Fatal("expected deny")
	}
	waitTraffic(t, &logs, 1)
	if logs[0].Action != "DENY" || logs[0].Reason != "nope" {
		t.Fatalf("deny logs=%v", logs)
	}
}

type captureTraffic struct{ out *[]TrafficEvent }

func (c *captureTraffic) Log(e TrafficEvent) { *c.out = append(*c.out, e) }

func waitPort(t *testing.T, s *Server) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Port() > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not bind")
}

func waitTraffic(t *testing.T, logs *[]TrafficEvent, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(*logs) >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("want %d traffic events, got %v", n, *logs)
}

func socksDial(t *testing.T, proxyPort int, host string, port int) (net.Conn, error) {
	t.Helper()
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", proxyPort), 2*time.Second)
	if err != nil {
		return nil, err
	}
	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		c.Close()
		return nil, err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(c, resp); err != nil || resp[0] != 0x05 || resp[1] != 0x00 {
		c.Close()
		return nil, fmt.Errorf("greeting %v %v", resp, err)
	}
	req := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	req = append(req, []byte(host)...)
	req = append(req, byte(port>>8), byte(port))
	if _, err := c.Write(req); err != nil {
		c.Close()
		return nil, err
	}
	rep := make([]byte, 10)
	if _, err := io.ReadFull(c, rep); err != nil {
		c.Close()
		return nil, err
	}
	if rep[1] != 0x00 {
		c.Close()
		return nil, fmt.Errorf("socks rep=%d", rep[1])
	}
	return c, nil
}

func TestGuestProxyEnvScript(t *testing.T) {
	s := GuestProxyEnvScript(1080)
	if !strings.Contains(s, "ALL_PROXY") || !strings.Contains(s, "1080") || !strings.Contains(s, "http://") {
		t.Fatalf("script=%q", s)
	}
	if strings.Contains(s, "socks5h://") {
		t.Fatalf("expected http:// proxy, got socks: %q", s)
	}
	if !strings.Contains(s, "http-proxy.env") {
		t.Fatalf("expected http-proxy.env hook: %q", s)
	}
	if !strings.Contains(s, "--use-env-proxy") {
		t.Fatalf("expected NODE_OPTIONS --use-env-proxy: %q", s)
	}
	inst := GuestProxyInstallScript(1080)
	if !strings.Contains(inst, "/etc/profile.d/cage-proxy.sh") ||
		!strings.Contains(inst, "/etc/environment") ||
		!strings.Contains(inst, "/var/lib/cage/shell") ||
		!strings.Contains(inst, "/var/lib/cage/runtime.env") ||
		!strings.Contains(inst, "http://") {
		t.Fatalf("install=%q", inst)
	}
	ca := GuestMITMCAInstallScript([]byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"))
	if !strings.Contains(ca, "cage-mitm.crt") || !strings.Contains(ca, "update-ca-certificates") {
		t.Fatalf("ca install=%q", ca)
	}
}

func TestParseHTTPRequestLine(t *testing.T) {
	m, p := parseHTTPRequestLine([]byte("POST /v1/chat?x=1 HTTP/1.1\r\nHost: x\r\n"))
	if m != "POST" || p != "/v1/chat" {
		t.Fatalf("got %q %q", m, p)
	}
	m, p = parseHTTPRequestLine([]byte{0x16, 0x03, 0x01})
	if m != "" || p != "" {
		t.Fatalf("tls %q %q", m, p)
	}
}

func TestSOCKSHTTPMethodPath(t *testing.T) {
	upstream, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	got := make(chan []byte, 1)
	go func() {
		c, err := upstream.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 256)
		n, _ := c.Read(buf)
		got <- append([]byte(nil), buf[:n]...)
	}()
	upPort := upstream.Addr().(*net.TCPAddr).Port

	var logs []TrafficEvent
	f := &recordingFilter{}
	pipe := NewPipeline([]FilterSeat{{Priority: 1, Filter: f}})
	srv := &Server{Pipeline: pipe, OnTraffic: &captureTraffic{&logs}, HTTPPeekWait: time.Second}
	go func() { _ = srv.ListenAndServe("127.0.0.1:0") }()
	defer srv.Close()
	waitPort(t, srv)

	c, err := socksDial(t, srv.Port(), "127.0.0.1", upPort)
	if err != nil {
		t.Fatal(err)
	}
	reqLine := "POST /v1/chat HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"
	if _, err := c.Write([]byte(reqLine)); err != nil {
		t.Fatal(err)
	}
	select {
	case b := <-got:
		if !strings.HasPrefix(string(b), "POST /v1/chat") {
			t.Fatalf("upstream got %q", b)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("upstream timeout")
	}
	_ = c.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(logs) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(logs) != 1 || logs[0].Action != "ALLOW" || logs[0].Method != "POST" || logs[0].Path != "/v1/chat" {
		t.Fatalf("logs=%v filter=%v", logs, f.last)
	}
}

func TestSOCKSHTTPDenyInject(t *testing.T) {
	var logs []TrafficEvent
	f := &recordingFilter{}
	pipe := NewPipeline([]FilterSeat{{Priority: 1, Filter: f}})
	srv := &Server{
		Pipeline:     pipe,
		OnTraffic:    &captureTraffic{&logs},
		HTTPPeekWait: time.Second,
		DenyHTTP:     true,
	}
	go func() { _ = srv.ListenAndServe("127.0.0.1:0") }()
	defer srv.Close()
	waitPort(t, srv)

	c, err := socksDial(t, srv.Port(), "blocked.example", 80)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("POST /secret HTTP/1.1\r\nHost: blocked.example\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1024)
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := c.Read(buf)
	if n <= 0 {
		t.Fatalf("read n=%d err=%v", n, err)
	}
	got := string(buf[:n])
	if !strings.Contains(got, "403") || !strings.Contains(got, "intentionally blocked") {
		t.Fatalf("resp=%q", got)
	}
	waitTraffic(t, &logs, 1)
	if logs[0].Action != "DENY" {
		t.Fatalf("logs=%v", logs)
	}
}

func TestSOCKSHTTPDenyNoInjectByDefault(t *testing.T) {
	f := &recordingFilter{}
	pipe := NewPipeline([]FilterSeat{{Priority: 1, Filter: f}})
	srv := &Server{Pipeline: pipe, HTTPPeekWait: time.Second}
	go func() { _ = srv.ListenAndServe("127.0.0.1:0") }()
	defer srv.Close()
	waitPort(t, srv)

	c, err := socksDial(t, srv.Port(), "blocked.example", 80)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("POST /secret HTTP/1.1\r\nHost: blocked.example\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	_ = c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	n, _ := c.Read(buf)
	if n > 0 {
		t.Fatalf("expected empty close, got %q", buf[:n])
	}
}

type recordingFilter struct {
	last netplugin.Request
}

func (r *recordingFilter) Name() string           { return "rec" }
func (r *recordingFilter) Configure([]byte) error { return nil }
func (r *recordingFilter) Check(req netplugin.Request) (netplugin.Decision, error) {
	r.last = req
	if req.Partial {
		return netplugin.Decision{Allow: true, Reason: "soft"}, nil
	}
	if req.Method == "POST" && req.Path == "/v1/chat" {
		return netplugin.Decision{Allow: true, Reason: "ok"}, nil
	}
	return netplugin.Decision{Allow: false, Reason: "need post"}, nil
}
