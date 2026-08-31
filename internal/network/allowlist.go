package network

import (
	"net"
	"sync"
)

// SourceAllowlist restricts proxy accepts to guest IPs for this VM.
// Empty list denies everyone (fail closed) until Set.
type SourceAllowlist struct {
	mu    sync.RWMutex
	allow map[string]struct{}
}

// Set replaces the allowlist with the given IPv4 addresses.
func (a *SourceAllowlist) Set(ips []net.IP) {
	next := make(map[string]struct{}, len(ips))
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			next[ip4.String()] = struct{}{}
		}
	}
	a.mu.Lock()
	a.allow = next
	a.mu.Unlock()
}

// SetStrings parses IP strings and Set them (invalid entries skipped).
func (a *SourceAllowlist) SetStrings(ss []string) {
	var ips []net.IP
	for _, s := range ss {
		if ip := net.ParseIP(s); ip != nil {
			ips = append(ips, ip)
		}
	}
	a.Set(ips)
}

// Contains reports whether addr's host IP is allowed.
func (a *SourceAllowlist) Contains(addr net.Addr) bool {
	if a == nil || addr == nil {
		return false
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		host = addr.String()
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		host = ip4.String()
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.allow) == 0 {
		return false
	}
	_, ok := a.allow[host]
	return ok
}

// Strings returns a stable copy of allowed IPv4 strings.
func (a *SourceAllowlist) Strings() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]string, 0, len(a.allow))
	for s := range a.allow {
		out = append(out, s)
	}
	return out
}

// AllowlistListener wraps ln and drops connections not on the allowlist.
func AllowlistListener(ln net.Listener, allow *SourceAllowlist) net.Listener {
	if allow == nil {
		allow = &SourceAllowlist{}
	}
	return &allowlistListener{Listener: ln, allow: allow}
}

type allowlistListener struct {
	net.Listener
	allow *SourceAllowlist
}

func (l *allowlistListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if l.allow.Contains(c.RemoteAddr()) {
			return c, nil
		}
		_ = c.Close()
	}
}
