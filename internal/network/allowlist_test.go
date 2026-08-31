package network

import (
	"io"
	"net"
	"testing"
	"time"
)

func TestSourceAllowlistPeerIsolation(t *testing.T) {
	allow := &SourceAllowlist{}
	allow.SetStrings([]string{"127.0.0.1"})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	fln := AllowlistListener(ln, allow)

	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := fln.Accept()
		if err != nil {
			return
		}
		accepted <- c
	}()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	select {
	case got := <-accepted:
		_, _ = io.WriteString(got, "ok")
		_ = got.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("allowed peer not accepted")
	}
}

func TestSourceAllowlistEmptyDenies(t *testing.T) {
	allow := &SourceAllowlist{}
	if allow.Contains(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}) {
		t.Fatal("empty allowlist must deny")
	}
}

func TestParseGuestIPv4Lines(t *testing.T) {
	out := `2: eth0    inet 192.168.64.2/24 brd 192.168.64.255 scope global eth0
3: eth0    inet 10.0.0.5/8 scope global secondary eth0
`
	ips := ParseGuestIPv4Lines(out)
	if len(ips) != 2 {
		t.Fatalf("got %v", ips)
	}
	if ips[0].String() != "192.168.64.2" || ips[1].String() != "10.0.0.5" {
		t.Fatalf("got %v", ips)
	}
}

func TestListenBindHostNeverEmpty(t *testing.T) {
	t.Setenv("CAGE_PROXY_BIND", "")
	if h := ListenBindHost(); h == "" {
		t.Fatal("empty bind host")
	}
}

func TestListenBindHostOverride(t *testing.T) {
	t.Setenv("CAGE_PROXY_BIND", "127.0.0.1")
	if got := ListenBindHost(); got != "127.0.0.1" {
		t.Fatalf("got %q", got)
	}
}
