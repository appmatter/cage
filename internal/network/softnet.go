package network

import (
	"fmt"
	"os/exec"
)

// SoftnetHostOnlyArgs returns tart ExtraRunArgs that lock guest egress to the host gateway.
func SoftnetHostOnlyArgs() ([]string, error) {
	if _, err := exec.LookPath("softnet"); err != nil {
		return nil, fmt.Errorf("softnet not found on PATH (required when network proxy is enabled); brew trust --formula cirruslabs/cli/softnet && brew install cirruslabs/cli/softnet")
	}
	return []string{
		"--net-softnet-block=0.0.0.0/0",
		"--net-softnet-allow=@host",
	}, nil
}

// SoftnetActiveEvent is logged when host-only softnet is on and proxy logging is enabled.
// Softnet itself does not emit per-packet drop events to Cage.
func SoftnetActiveEvent() TrafficEvent {
	return TrafficEvent{
		Action: "SOFTNET",
		Reason: "host-only active — direct guest internet dropped outside SOCKS (not logged per packet)",
	}
}

// SoftnetProbeArgv is a guest command that tries direct TCP to 1.1.1.1:53 (bypasses SOCKS).
// Non-zero exit under host-only softnet means the drop worked.
func SoftnetProbeArgv() []string {
	return []string{"bash", "-c", "timeout 3 bash -c 'echo >/dev/tcp/1.1.1.1/53'"}
}

// SoftnetProbeEvent builds the start-time probe result for proxy.log.
func SoftnetProbeEvent(directReachable bool) TrafficEvent {
	e := TrafficEvent{Action: "SOFTNET", Host: "1.1.1.1", Port: 53}
	if directReachable {
		e.Reason = "direct egress reached internet — host-only may be broken"
		e.Error = "probe connected"
		return e
	}
	e.Reason = "direct egress blocked (expected)"
	return e
}
