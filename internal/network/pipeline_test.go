package network

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

type stubFilter struct {
	name  string
	allow bool
	reason string
}

func (s *stubFilter) Name() string           { return s.name }
func (s *stubFilter) Configure([]byte) error { return nil }
func (s *stubFilter) Check(netplugin.Request) (netplugin.Decision, error) {
	return netplugin.Decision{Allow: s.allow, Reason: s.reason}, nil
}

func TestPipelineCheckAnyDenyWins(t *testing.T) {
	p := NewPipeline([]FilterSeat{
		{Priority: 1, Filter: &stubFilter{name: "a", allow: true, reason: "a-ok"}},
		{Priority: 2, Filter: &stubFilter{name: "b", allow: false, reason: "b-deny"}},
	})
	d, err := p.Check(netplugin.Request{Host: "example.com", Port: 443})
	if err != nil {
		t.Fatal(err)
	}
	if d.Allow || d.Reason != "b-deny" {
		t.Fatalf("got %#v", d)
	}
}

func TestPipelineCheckNoFiltersAllow(t *testing.T) {
	p := NewPipeline(nil)
	d, err := p.Check(netplugin.Request{Host: "x"})
	if err != nil || !d.Allow {
		t.Fatalf("got %#v err=%v", d, err)
	}
}

func TestPipelineDialDenied(t *testing.T) {
	p := NewPipeline([]FilterSeat{
		{Priority: 1, Filter: &stubFilter{allow: false, reason: "nope"}},
	})
	_, err := p.Dial(context.Background(), netplugin.Request{Host: "x", Port: 80})
	if err == nil || err.Error() == "" {
		t.Fatalf("err=%v", err)
	}
}

func TestPipelineDialAllowed(t *testing.T) {
	var dialed string
	p := NewPipeline([]FilterSeat{
		{Priority: 1, Filter: &stubFilter{allow: true, reason: "ok"}},
	})
	p.Dialer = func(ctx context.Context, network, address string) (net.Conn, error) {
		dialed = address
		return &nopConn{}, nil
	}
	c, err := p.Dial(context.Background(), netplugin.Request{Host: "example.com", Port: 443})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if dialed != "example.com:443" {
		t.Fatalf("dialed=%q", dialed)
	}
}

type nopConn struct{}

func (nopConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (nopConn) Write(b []byte) (int, error)      { return len(b), nil }
func (nopConn) Close() error                     { return nil }
func (nopConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (nopConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (nopConn) SetDeadline(time.Time) error      { return nil }
func (nopConn) SetReadDeadline(time.Time) error  { return nil }
func (nopConn) SetWriteDeadline(time.Time) error { return nil }