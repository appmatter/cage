package network

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// ParseGuestIPv4Lines extracts IPv4 addresses from `ip -4 -o addr` style output
// or plain one-IP-per-line lists.
func ParseGuestIPv4Lines(out string) []net.IP {
	seen := map[string]struct{}{}
	var ips []net.IP
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// ip -4 -o addr: "2: eth0    inet 192.168.64.2/24 ..."
		fields := strings.Fields(line)
		cand := line
		for i, f := range fields {
			if f == "inet" && i+1 < len(fields) {
				cand = fields[i+1]
				break
			}
		}
		cand = strings.Split(cand, "/")[0]
		ip := net.ParseIP(cand)
		if ip == nil {
			continue
		}
		ip4 := ip.To4()
		if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
			continue
		}
		s := ip4.String()
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		ips = append(ips, ip4)
	}
	return ips
}

// DiscoverTartGuestIPv4 returns global IPv4 addresses inside a running Tart VM.
func DiscoverTartGuestIPv4(vmID string) ([]net.IP, error) {
	if vmID == "" {
		return nil, fmt.Errorf("vm id required")
	}
	cmd := exec.Command("tart", "exec", vmID, "sh", "-c",
		`ip -4 -o addr show scope global 2>/dev/null || true`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("tart exec ip addr: %w\n%s", err, out)
	}
	ips := ParseGuestIPv4Lines(string(out))
	if len(ips) == 0 {
		return nil, fmt.Errorf("no guest IPv4 found for %q (output %q)", vmID, strings.TrimSpace(string(out)))
	}
	return ips, nil
}
