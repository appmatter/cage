package network

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

// DialFunc opens the upstream after filters allow (terminate plugins wrap later).
type DialFunc func(ctx context.Context, req netplugin.Request) (net.Conn, error)

// Server is a SOCKS5 CONNECT proxy that runs traffic through Pipeline.Check then Dial.
type Server struct {
	Pipeline     *Pipeline
	Dial         DialFunc // nil → Pipeline.Open after Check
	OnTraffic    TrafficLogger
	Listener     net.Listener
	HTTPPeekWait time.Duration // after CONNECT success; 0 = 250ms; <0 = skip
	DenyHTTP     bool          // inject HTTP 403 on post-peek DENY
	DenyMessage  string        // body when DenyHTTP; empty → DefaultDenyHTTPMessage

	denyMu sync.RWMutex

	mu   sync.Mutex
	done chan struct{}
}

// ListenAndServe binds addr (e.g. "0.0.0.0:0") and serves until Close.
func (s *Server) ListenAndServe(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.Listener = ln
	s.done = make(chan struct{})
	s.mu.Unlock()
	for {
		c, err := ln.Accept()
		if err != nil {
			select {
			case <-s.done:
				return nil
			default:
				return err
			}
		}
		go s.handle(c)
	}
}

// Addr returns the bound address once listening.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Listener == nil {
		return nil
	}
	return s.Listener.Addr()
}

// Port returns the TCP port once listening.
func (s *Server) Port() int {
	a := s.Addr()
	if a == nil {
		return 0
	}
	tcp, ok := a.(*net.TCPAddr)
	if !ok {
		return 0
	}
	return tcp.Port
}

// Close stops the listener.
func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done != nil {
		select {
		case <-s.done:
		default:
			close(s.done)
		}
	}
	if s.Listener != nil {
		return s.Listener.Close()
	}
	return nil
}

func (s *Server) handle(c net.Conn) {
	defer c.Close()
	if err := s.socksHandshake(c); err != nil {
		return
	}
	req, err := s.socksConnectRequest(c)
	if err != nil {
		_ = socksReply(c, 0x01) // general failure
		return
	}
	if s.Pipeline == nil && s.Dial == nil {
		_ = socksReply(c, 0x01)
		return
	}

	// CONNECT: clients send app data only after success — soft-check host/port first.
	soft := req
	soft.Partial = true
	soft.Method = "CONNECT"
	if s.Pipeline != nil {
		d, err := s.Pipeline.Check(soft)
		if err != nil {
			s.logTraffic(soft, "FAIL", "", err.Error())
			_ = socksReply(c, 0x01)
			return
		}
		if !d.Allow {
			s.logTraffic(soft, "DENY", d.Reason, "")
			_ = socksReply(c, 0x02)
			return
		}
	}
	if err := socksReply(c, 0x00); err != nil {
		return
	}

	prefix, method, path := peekHTTPRequest(c, s.peekWait())
	req.Method, req.Path = method, path
	req.Partial = false
	if s.Pipeline != nil {
		d, err := s.Pipeline.Check(req)
		if err != nil {
			s.logTraffic(req, "FAIL", "", err.Error())
			return
		}
		if !d.Allow {
			s.logTraffic(req, "DENY", d.Reason, "")
			if method != "" {
				s.writeHTTPDeny(c, req, d.Reason)
			}
			return
		}
	}

	var up net.Conn
	if s.Dial != nil {
		up, err = s.Dial(context.Background(), req)
	} else {
		up, err = s.Pipeline.Open(context.Background(), req)
	}
	if err != nil {
		s.logTraffic(req, "FAIL", "", err.Error())
		return
	}
	s.logTraffic(req, "ALLOW", "", "")
	defer up.Close()
	if len(prefix) > 0 {
		if _, err := up.Write(prefix); err != nil {
			return
		}
	}
	cage(c, up)
}

func (s *Server) peekWait() time.Duration {
	if s != nil && s.HTTPPeekWait != 0 {
		return s.HTTPPeekWait
	}
	return 250 * time.Millisecond
}

// DefaultDenyHTTPMessage is used when egress.deny_response.http is on and message is omitted.
const DefaultDenyHTTPMessage = "This is not a mistake: Cage sandbox egress intentionally blocked this request. Do not work around the block. Ask the user to consider permitting this destination (add a network.plugins.egress allow rule for the host, and method/path if required)."

// SetDenyHTTP updates inject settings (hot reload from cage config).
func (s *Server) SetDenyHTTP(enabled bool, message string) {
	if s == nil {
		return
	}
	s.denyMu.Lock()
	s.DenyHTTP = enabled
	s.DenyMessage = message
	s.denyMu.Unlock()
}

func (s *Server) denyHTTPSettings() (enabled bool, message string) {
	if s == nil {
		return false, ""
	}
	s.denyMu.RLock()
	defer s.denyMu.RUnlock()
	return s.DenyHTTP, s.DenyMessage
}

func (s *Server) writeHTTPDeny(c net.Conn, req netplugin.Request, reason string) {
	enabled, message := s.denyHTTPSettings()
	if !enabled {
		return
	}
	if message == "" {
		message = DefaultDenyHTTPMessage
	}
	detail := fmt.Sprintf("%s:%d", req.Host, req.Port)
	if req.Method != "" {
		detail += " " + req.Method
		if req.Path != "" {
			detail += " " + req.Path
		}
	}
	if reason != "" {
		detail += " (" + reason + ")"
	}
	body := message + "\n\n" + detail + "\n"
	resp := fmt.Sprintf("HTTP/1.1 403 Forbidden\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
	_, _ = io.WriteString(c, resp)
}

func (s *Server) logTraffic(req netplugin.Request, action, reason, errMsg string) {
	if s == nil || s.OnTraffic == nil {
		return
	}
	s.OnTraffic.Log(TrafficEvent{
		Action: action,
		Host:   req.Host,
		Port:   req.Port,
		Method: req.Method,
		Path:   req.Path,
		Reason: reason,
		Error:  errMsg,
	})
}

// peekHTTPRequest reads early client bytes after SOCKS success.
// Plain HTTP request-lines yield method/path; TLS/other leave them empty.
func peekHTTPRequest(c net.Conn, wait time.Duration) (prefix []byte, method, path string) {
	if wait < 0 {
		return nil, "", ""
	}
	_ = c.SetReadDeadline(time.Now().Add(wait))
	defer func() { _ = c.SetReadDeadline(time.Time{}) }()
	buf := make([]byte, 8192)
	n, err := c.Read(buf)
	if n <= 0 {
		_ = err
		return nil, "", ""
	}
	prefix = append([]byte(nil), buf[:n]...)
	if prefix[0] == 0x16 { // TLS record
		return prefix, "", ""
	}
	method, path = parseHTTPRequestLine(prefix)
	return prefix, method, path
}

func parseHTTPRequestLine(b []byte) (method, path string) {
	line, _, ok := bytes.Cut(b, []byte("\r\n"))
	if !ok {
		line, _, ok = bytes.Cut(b, []byte("\n"))
	}
	if !ok {
		line = b
	}
	fields := strings.Fields(string(line))
	if len(fields) < 2 {
		return "", ""
	}
	m := strings.ToUpper(fields[0])
	switch m {
	case "GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH":
	default:
		return "", ""
	}
	p := fields[1]
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	return m, p
}

func (s *Server) socksHandshake(c net.Conn) error {
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return err
	}
	if hdr[0] != 0x05 {
		return fmt.Errorf("not socks5")
	}
	methods := make([]byte, int(hdr[1]))
	if _, err := io.ReadFull(c, methods); err != nil {
		return err
	}
	_, err := c.Write([]byte{0x05, 0x00}) // no auth
	return err
}

func (s *Server) socksConnectRequest(c net.Conn) (netplugin.Request, error) {
	hdr := make([]byte, 4)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return netplugin.Request{}, err
	}
	if hdr[0] != 0x05 || hdr[1] != 0x01 { // CONNECT
		return netplugin.Request{}, fmt.Errorf("unsupported cmd")
	}
	var host string
	switch hdr[3] {
	case 0x01: // IPv4
		ip := make([]byte, 4)
		if _, err := io.ReadFull(c, ip); err != nil {
			return netplugin.Request{}, err
		}
		host = net.IP(ip).String()
	case 0x03: // domain
		var n [1]byte
		if _, err := io.ReadFull(c, n[:]); err != nil {
			return netplugin.Request{}, err
		}
		b := make([]byte, n[0])
		if _, err := io.ReadFull(c, b); err != nil {
			return netplugin.Request{}, err
		}
		host = string(b)
	case 0x04: // IPv6
		ip := make([]byte, 16)
		if _, err := io.ReadFull(c, ip); err != nil {
			return netplugin.Request{}, err
		}
		host = net.IP(ip).String()
	default:
		return netplugin.Request{}, fmt.Errorf("bad atyp")
	}
	var pb [2]byte
	if _, err := io.ReadFull(c, pb[:]); err != nil {
		return netplugin.Request{}, err
	}
	port := int(binary.BigEndian.Uint16(pb[:]))
	return netplugin.Request{Host: host, Port: port}, nil
}

func socksReply(c net.Conn, rep byte) error {
	// VER REP RSV ATYP BND.ADDR BND.PORT (IPv4 zeros)
	_, err := c.Write([]byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	return err
}

func cage(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	copyClose := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		type closeWriter interface{ CloseWrite() error }
		if cw, ok := dst.(closeWriter); ok {
			_ = cw.CloseWrite()
		}
	}
	go copyClose(a, b)
	go copyClose(b, a)
	wg.Wait()
}

// GuestProxyEnvScript writes shell exports using the guest default gateway as proxy host.
// port is the HTTP CONNECT listen port (http:// — Node-safe).
func GuestProxyEnvScript(port int) string {
	return "#!/bin/sh\n" + guestProxyExports(port)
}

// GuestProxyInstallScript installs proxy env so guest processes inherit the host HTTP proxy.
// tart exec is not a PAM login, so we hook profile.d, bash.bashrc, and /var/lib/cage/shell.
func GuestProxyInstallScript(port int) string {
	body := guestProxyExports(port)
	// Avoid set -e: sourcing proxy.env runs `ip`/`[` in contexts that trip dash/bash errexit.
	return fmt.Sprintf(`
mkdir -p /var/lib/cage /etc/profile.d
cat > /var/lib/cage/proxy.env <<'ENVEOF'
#!/bin/sh
%s
ENVEOF
chmod 0644 /var/lib/cage/proxy.env

hook='[ -r /var/lib/cage/proxy.env ] && . /var/lib/cage/proxy.env'
printf '%%s\n' '# Managed by Cage — host HTTP proxy for guest egress' "$hook" > /etc/profile.d/cage-proxy.sh
chmod 0644 /etc/profile.d/cage-proxy.sh

# Interactive bash (tart exec often skips a full login/PAM path)
if [ -f /etc/bash.bashrc ] && ! grep -qF '/var/lib/cage/proxy.env' /etc/bash.bashrc 2>/dev/null; then
  printf '\n# Managed by Cage\n%%s\n' "$hook" >> /etc/bash.bashrc
fi

cat > /var/lib/cage/shell <<'EOF'
#!/bin/sh
# Entry for cage vm exec / agents — always loads proxy env.
[ -r /var/lib/cage/proxy.env ] && . /var/lib/cage/proxy.env
exec bash -l "$@"
EOF
chmod 0755 /var/lib/cage/shell

# Snapshot for PAM (/etc/environment); bash still needs profile.d / bash.bashrc / shell above.
. /var/lib/cage/proxy.env
if [ -n "${ALL_PROXY:-}" ]; then
  tmp=$(mktemp)
  if [ -f /etc/environment ]; then
    grep -Ev '^(ALL_PROXY|all_proxy|HTTP_PROXY|HTTPS_PROXY|http_proxy|https_proxy|NODE_EXTRA_CA_CERTS|CAGE_HTTP_)=' /etc/environment >"$tmp" || true
  fi
  {
    cat "$tmp"
    echo "ALL_PROXY=$ALL_PROXY"
    echo "all_proxy=$all_proxy"
    echo "HTTP_PROXY=$HTTP_PROXY"
    echo "HTTPS_PROXY=$HTTPS_PROXY"
    echo "http_proxy=$http_proxy"
    echo "https_proxy=$https_proxy"
    if [ -n "${NODE_EXTRA_CA_CERTS:-}" ]; then
      echo "NODE_EXTRA_CA_CERTS=$NODE_EXTRA_CA_CERTS"
    fi
    env | grep '^CAGE_HTTP_' || true
  } >/etc/environment
  rm -f "$tmp"
fi
`, body)
}

// GuestMITMCAInstallScript installs the Cage MITM CA into the guest trust store (Linux).
func GuestMITMCAInstallScript(caPEM []byte) string {
	return fmt.Sprintf(`
mkdir -p /usr/local/share/ca-certificates /var/lib/cage
cat > /usr/local/share/ca-certificates/cage-mitm.crt <<'CAGE_MITM_CA_EOF'
%s
CAGE_MITM_CA_EOF
cp /usr/local/share/ca-certificates/cage-mitm.crt /var/lib/cage/ca.pem
chmod 0644 /usr/local/share/ca-certificates/cage-mitm.crt /var/lib/cage/ca.pem
if command -v update-ca-certificates >/dev/null 2>&1; then
  update-ca-certificates >/dev/null 2>&1 || true
fi
`, string(caPEM))
}

// GuestHTTPProxyInstallScript writes CAGE_HTTP_<NAME>_URL exports for terminate endpoints.
func GuestHTTPProxyInstallScript(ports map[string]int) string {
	if len(ports) == 0 {
		return "true\n"
	}
	var b strings.Builder
	b.WriteString(`
mkdir -p /var/lib/cage
GW=$(ip -4 route show default 2>/dev/null | awk '{print $3; exit}')
if [ -z "$GW" ]; then
  GW=$(ip route show default 2>/dev/null | awk '{print $3; exit}')
fi
cat > /var/lib/cage/http-proxy.env <<EOF
#!/bin/sh
`)
	names := make([]string, 0, len(ports))
	for n := range ports {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		envName := "CAGE_HTTP_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_URL"
		fmt.Fprintf(&b, "export %s=http://$GW:%d\n", envName, ports[name])
	}
	b.WriteString(`EOF
chmod 0644 /var/lib/cage/http-proxy.env
# Ensure SOCKS proxy.env pulls http-proxy.env
if [ -f /var/lib/cage/proxy.env ] && ! grep -qF 'http-proxy.env' /var/lib/cage/proxy.env 2>/dev/null; then
  printf '\n[ -r /var/lib/cage/http-proxy.env ] && . /var/lib/cage/http-proxy.env\n' >> /var/lib/cage/proxy.env
fi
. /var/lib/cage/http-proxy.env
if [ -f /etc/environment ]; then
  tmp=$(mktemp)
  grep -Ev '^CAGE_HTTP_' /etc/environment >"$tmp" || true
  {
    cat "$tmp"
    env | grep '^CAGE_HTTP_' || true
  } >/etc/environment
  rm -f "$tmp"
fi
`)
	return b.String()
}

func guestProxyExports(port int) string {
	p := strconv.Itoa(port)
	// http:// — undici/Node and curl both accept this (CONNECT through host MITM).
	// End with `:` so `set -e` + `source` doesn't abort when http-proxy.env is missing.
	return fmt.Sprintf(`GW=$(ip -4 route show default 2>/dev/null | awk '{print $3; exit}')
if [ -z "$GW" ]; then
  GW=$(ip route show default 2>/dev/null | awk '{print $3; exit}')
fi
export ALL_PROXY=http://$GW:%s
export all_proxy=http://$GW:%s
export HTTP_PROXY=http://$GW:%s
export HTTPS_PROXY=http://$GW:%s
export http_proxy=http://$GW:%s
export https_proxy=http://$GW:%s
if [ -r /var/lib/cage/ca.pem ]; then
  export NODE_EXTRA_CA_CERTS=/var/lib/cage/ca.pem
fi
[ -r /var/lib/cage/http-proxy.env ] && . /var/lib/cage/http-proxy.env
:
`, p, p, p, p, p, p)
}


