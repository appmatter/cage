// Package proxy provides the application boundary for Cage's host proxy.
package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// State is written under .cage/run/<id>/proxy.json.
type State struct {
	PID            int      `json:"pid"`
	Port           int      `json:"port"`      // SOCKS5
	HTTPPort       int      `json:"http_port"` // HTTP CONNECT (+ MITM)
	BindHost       string   `json:"bind_host,omitempty"`
	AllowedSources []string `json:"allowed_sources,omitempty"` // guest IPv4s
}

func runDir(projectRoot, vmID string) string {
	if projectRoot == "" {
		projectRoot = "."
	}
	return filepath.Join(projectRoot, ".cage", "run", vmID)
}

func proxyStatePath(projectRoot, vmID string) string {
	return filepath.Join(runDir(projectRoot, vmID), "proxy.json")
}

func egressConfigPath(projectRoot, vmID string) string {
	return filepath.Join(runDir(projectRoot, vmID), "egress.yaml")
}

func httpProxyConfigPath(projectRoot, vmID string) string {
	return filepath.Join(runDir(projectRoot, vmID), "http-proxy.yaml")
}

// ResolvedYAMLPath is .cage/run/<vmID>/http-proxy.resolved.yaml under projectRoot.
func ResolvedYAMLPath(projectRoot, vmID string) string {
	return filepath.Join(runDir(projectRoot, vmID), "http-proxy.resolved.yaml")
}

// writeOwnerOnlyFile creates or replaces path at 0600 on the first open.
func writeOwnerOnlyFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func httpProxyStatePath(projectRoot, vmID string) string {
	return filepath.Join(runDir(projectRoot, vmID), "http-proxy.json")
}

func readyPath(projectRoot, vmID string) string {
	return filepath.Join(runDir(projectRoot, vmID), "proxy.ready")
}

// LogPath is .cage/run/<vmID>/proxy.log under projectRoot.
func LogPath(projectRoot, vmID string) string {
	return filepath.Join(runDir(projectRoot, vmID), "proxy.log")
}

// HTTPPorts is name → listen port under .cage/run/<id>/http-proxy.json.
type HTTPPorts map[string]int

// WriteHTTPPorts persists http-proxy.json.
func WriteHTTPPorts(projectRoot, vmID string, ports HTTPPorts) error {
	dir := runDir(projectRoot, vmID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	type entry struct {
		Port int `json:"port"`
	}
	wrapped := map[string]entry{}
	for k, p := range ports {
		wrapped[k] = entry{Port: p}
	}
	b, err := json.MarshalIndent(wrapped, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(httpProxyStatePath(projectRoot, vmID), append(b, '\n'), 0o644)
}

// ReadHTTPPorts loads http-proxy.json if present.
func ReadHTTPPorts(projectRoot, vmID string) (HTTPPorts, error) {
	b, err := os.ReadFile(httpProxyStatePath(projectRoot, vmID))
	if err != nil {
		return nil, err
	}
	var wrapped map[string]struct {
		Port int `json:"port"`
	}
	if err := json.Unmarshal(b, &wrapped); err != nil {
		return nil, err
	}
	out := HTTPPorts{}
	for k, v := range wrapped {
		out[k] = v.Port
	}
	return out, nil
}

// StartOptions configures the detached proxy-serve child.
type StartOptions struct {
	EgressYAML            []byte
	HTTPProxyYAML         []byte // templates → .cage/run/<id>/http-proxy.yaml (no secret values)
	HTTPProxyResolvedYAML []byte // optional substituted yaml → .cage/run/<id>/http-proxy.resolved.yaml (0600, deleted after read)
	Logging               bool
	ConfigPath            string
	DenyHTTP              bool
	DenyMessage           string
	Softnet               bool     // host-only softnet active; advisory SOFTNET log when Logging
	MITM                  bool     // HTTPS break/re-encrypt (default on when proxy enabled)
	AllowedSources        []string // guest IPv4s allowed to dial this proxy
}

// Start launches `cage proxy-serve` in the background and waits for proxy.json.
func Start(projectRoot, vmID, cageBin string, opts StartOptions) (State, error) {
	_ = Stop(projectRoot, vmID)
	dir := runDir(projectRoot, vmID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return State{}, err
	}
	egPath := egressConfigPath(projectRoot, vmID)
	eg := opts.EgressYAML
	if len(eg) == 0 {
		eg = []byte("{}\n")
	}
	if err := os.WriteFile(egPath, eg, 0o644); err != nil {
		return State{}, err
	}
	hpPath := ""
	resolvedPath := ""
	if len(opts.HTTPProxyYAML) > 0 && string(opts.HTTPProxyYAML) != "{}\n" && string(opts.HTTPProxyYAML) != "null\n" {
		hpPath = httpProxyConfigPath(projectRoot, vmID)
		if err := os.WriteFile(hpPath, opts.HTTPProxyYAML, 0o644); err != nil {
			return State{}, err
		}
		resolvedPath = ResolvedYAMLPath(projectRoot, vmID)
		_ = os.Remove(resolvedPath)
		if len(opts.HTTPProxyResolvedYAML) > 0 {
			if err := writeOwnerOnlyFile(resolvedPath, opts.HTTPProxyResolvedYAML); err != nil {
				_ = os.Remove(resolvedPath)
				return State{}, err
			}
		} else {
			resolvedPath = ""
		}
	} else {
		_ = os.Remove(httpProxyConfigPath(projectRoot, vmID))
		_ = os.Remove(httpProxyStatePath(projectRoot, vmID))
		_ = os.Remove(ResolvedYAMLPath(projectRoot, vmID))
	}
	ready := readyPath(projectRoot, vmID)
	_ = os.Remove(ready)
	_ = os.Remove(proxyStatePath(projectRoot, vmID))

	args := []string{"proxy-serve",
		"--project", projectRoot,
		"--id", vmID,
		"--egress", egPath,
		"--ready", ready,
	}
	if hpPath != "" {
		args = append(args, "--http-proxy", hpPath)
	}
	if opts.ConfigPath != "" {
		args = append(args, "--config", opts.ConfigPath)
	}
	if opts.Logging {
		args = append(args, "--log")
	}
	if opts.Softnet {
		args = append(args, "--softnet")
	}
	if opts.DenyHTTP {
		args = append(args, "--deny-http")
		if opts.DenyMessage != "" {
			args = append(args, "--deny-message", opts.DenyMessage)
		}
	}
	if opts.MITM {
		args = append(args, "--mitm")
	}
	for _, ip := range opts.AllowedSources {
		if ip != "" {
			args = append(args, "--allow-ip", ip)
		}
	}
	cmd := exec.Command(cageBin, args...)
	// Detach from the operator TTY — traffic stays in proxy.log; follow with `cage vm logs -f`.
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		if resolvedPath != "" {
			_ = os.Remove(resolvedPath)
		}
		return State{}, fmt.Errorf("proxy-serve open %s: %w", os.DevNull, err)
	}
	defer devNull.Close()
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		if resolvedPath != "" {
			_ = os.Remove(resolvedPath)
		}
		return State{}, fmt.Errorf("proxy-serve start: %w", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	st, err := readState(projectRoot, vmID)
	if err != nil {
		_ = cmd.Process.Kill()
		if resolvedPath != "" {
			_ = os.Remove(resolvedPath)
		}
		return State{}, fmt.Errorf("proxy ready: %w", err)
	}
	return st, nil
}

func readState(projectRoot, vmID string) (State, error) {
	b, err := os.ReadFile(proxyStatePath(projectRoot, vmID))
	if err != nil {
		return State{}, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, err
	}
	return st, nil
}

// WriteState persists proxy.json.
func WriteState(projectRoot, vmID string, st State) error {
	dir := runDir(projectRoot, vmID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(proxyStatePath(projectRoot, vmID), append(b, '\n'), 0o644)
}

// Stop kills the proxy process for vmID if running.
func Stop(projectRoot, vmID string) error {
	st, err := readState(projectRoot, vmID)
	if err != nil {
		return nil
	}
	if st.PID > 0 && isCageProxyPID(st.PID) {
		_ = syscall.Kill(st.PID, syscall.SIGTERM)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if err := syscall.Kill(st.PID, 0); err != nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if isCageProxyPID(st.PID) {
			_ = syscall.Kill(st.PID, syscall.SIGKILL)
		}
	}
	_ = os.Remove(proxyStatePath(projectRoot, vmID))
	_ = os.Remove(readyPath(projectRoot, vmID))
	_ = os.Remove(httpProxyStatePath(projectRoot, vmID))
	return nil
}

// isCageProxyPID reports whether pid looks like a live cage proxy-serve process.
func isCageProxyPID(pid int) bool {
	if pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "args=").Output()
	if err != nil {
		return false
	}
	args := string(out)
	return strings.Contains(args, "proxy-serve")
}
