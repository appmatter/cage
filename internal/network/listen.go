package network

import (
	"fmt"
	"net"
	"os"
	"strings"
)

// ListenBindHost returns the host for proxy listeners.
// Guests reach the host via softnet gateway. Prefer a VM-facing IPv4 when present;
// otherwise 0.0.0.0. Override with CAGE_PROXY_BIND.
// Peer isolation is enforced by SourceAllowlist (guest IP), not bind address alone.
func ListenBindHost() string {
	if v := strings.TrimSpace(os.Getenv("CAGE_PROXY_BIND")); v != "" {
		return v
	}
	if h := vmFacingIPv4(); h != "" {
		return h
	}
	return "0.0.0.0"
}

func vmFacingIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		prefer := strings.Contains(name, "bridge") ||
			strings.Contains(name, "vmnet") ||
			strings.Contains(name, "vmenet") ||
			strings.Contains(name, "soft")
		if !prefer {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok || ipNet.IP == nil {
				continue
			}
			ip4 := ipNet.IP.To4()
			if ip4 == nil || ip4.IsLoopback() || !ip4.IsPrivate() {
				continue
			}
			return ip4.String()
		}
	}
	return ""
}

// ListenAddr builds host:port for a proxy listener (ephemeral when port <= 0).
func ListenAddr(host string, port int) string {
	if host == "" {
		host = ListenBindHost()
	}
	if port <= 0 {
		return net.JoinHostPort(host, "0")
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", port))
}
