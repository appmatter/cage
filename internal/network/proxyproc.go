package network

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

	"gopkg.in/yaml.v3"

	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

// ProxyState is written under .cage/run/<id>/proxy.json.
type ProxyState struct {
	PID            int      `json:"pid"`
	Port           int      `json:"port"`      // SOCKS5
	HTTPPort       int      `json:"http_port"` // HTTP CONNECT (+ MITM)
	BindHost       string   `json:"bind_host,omitempty"`
	AllowedSources []string `json:"allowed_sources,omitempty"` // guest IPv4s
}

// RunDir is .cage/run/<vmID> under projectRoot.
func RunDir(projectRoot, vmID string) string {
	if projectRoot == "" {
		projectRoot = "."
	}
	return filepath.Join(projectRoot, ".cage", "run", vmID)
}

func proxyStatePath(projectRoot, vmID string) string {
	return filepath.Join(RunDir(projectRoot, vmID), "proxy.json")
}

func egressConfigPath(projectRoot, vmID string) string {
	return filepath.Join(RunDir(projectRoot, vmID), "egress.yaml")
}

func httpProxyConfigPath(projectRoot, vmID string) string {
	return filepath.Join(RunDir(projectRoot, vmID), "http-proxy.yaml")
}

func httpProxyStatePath(projectRoot, vmID string) string {
	return filepath.Join(RunDir(projectRoot, vmID), "http-proxy.json")
}

// HTTPProxyPorts is name → listen port under .cage/run/<id>/http-proxy.json.
type HTTPProxyPorts map[string]int

// WriteHTTPProxyState persists http-proxy.json.
func WriteHTTPProxyState(projectRoot, vmID string, ports HTTPProxyPorts) error {
	dir := RunDir(projectRoot, vmID)
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

// ReadHTTPProxyState loads http-proxy.json if present.
func ReadHTTPProxyState(projectRoot, vmID string) (HTTPProxyPorts, error) {
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
	out := HTTPProxyPorts{}
	for k, v := range wrapped {
		out[k] = v.Port
	}
	return out, nil
}

// StartDetachedProxyOpts configures the detached proxy-serve child.
type StartDetachedProxyOpts struct {
	EgressYAML            []byte
	HTTPProxyYAML         []byte // templates → .cage/run/<id>/http-proxy.yaml (no secret values)
	HTTPProxyResolvedYAML []byte // optional substituted yaml for Configure only (temp file)
	Logging               bool
	ConfigPath            string
	DenyHTTP              bool
	DenyMessage           string
	Softnet               bool     // host-only softnet active; advisory SOFTNET log when Logging
	MITM                  bool     // HTTPS break/re-encrypt (default on when proxy enabled)
	AllowedSources        []string // guest IPv4s allowed to dial this proxy
}

// StartDetachedProxy launches `cage proxy-serve` in the background and waits for proxy.json.
func StartDetachedProxy(projectRoot, vmID, cageBin string, opts StartDetachedProxyOpts) (ProxyState, error) {
	_ = StopDetachedProxy(projectRoot, vmID)
	dir := RunDir(projectRoot, vmID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ProxyState{}, err
	}
	egPath := egressConfigPath(projectRoot, vmID)
	eg := opts.EgressYAML
	if len(eg) == 0 {
		eg = []byte("{}\n")
	}
	if err := os.WriteFile(egPath, eg, 0o644); err != nil {
		return ProxyState{}, err
	}
	hpPath := ""
	hpResolvedPath := ""
	if len(opts.HTTPProxyYAML) > 0 && string(opts.HTTPProxyYAML) != "{}\n" && string(opts.HTTPProxyYAML) != "null\n" {
		hpPath = httpProxyConfigPath(projectRoot, vmID)
		if err := os.WriteFile(hpPath, opts.HTTPProxyYAML, 0o644); err != nil {
			return ProxyState{}, err
		}
		if len(opts.HTTPProxyResolvedYAML) > 0 {
			f, err := os.CreateTemp("", "cage-http-proxy-resolved-*.yaml")
			if err != nil {
				return ProxyState{}, err
			}
			hpResolvedPath = f.Name()
			if err := f.Chmod(0o600); err != nil {
				f.Close()
				_ = os.Remove(hpResolvedPath)
				return ProxyState{}, err
			}
			if _, err := f.Write(opts.HTTPProxyResolvedYAML); err != nil {
				f.Close()
				_ = os.Remove(hpResolvedPath)
				return ProxyState{}, err
			}
			if err := f.Close(); err != nil {
				_ = os.Remove(hpResolvedPath)
				return ProxyState{}, err
			}
		}
	} else {
		_ = os.Remove(httpProxyConfigPath(projectRoot, vmID))
		_ = os.Remove(httpProxyStatePath(projectRoot, vmID))
	}
	ready := filepath.Join(dir, "proxy.ready")
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
	if hpResolvedPath != "" {
		args = append(args, "--http-proxy-resolved", hpResolvedPath)
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
		return ProxyState{}, fmt.Errorf("proxy-serve open %s: %w", os.DevNull, err)
	}
	defer devNull.Close()
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		if hpResolvedPath != "" {
			_ = os.Remove(hpResolvedPath)
		}
		return ProxyState{}, fmt.Errorf("proxy-serve start: %w", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	st, err := ReadProxyState(projectRoot, vmID)
	if err != nil {
		_ = cmd.Process.Kill()
		return ProxyState{}, fmt.Errorf("proxy ready: %w", err)
	}
	return st, nil
}

// ReadProxyState loads proxy.json if present.
func ReadProxyState(projectRoot, vmID string) (ProxyState, error) {
	b, err := os.ReadFile(proxyStatePath(projectRoot, vmID))
	if err != nil {
		return ProxyState{}, err
	}
	var st ProxyState
	if err := json.Unmarshal(b, &st); err != nil {
		return ProxyState{}, err
	}
	return st, nil
}

// WriteProxyState persists proxy.json.
func WriteProxyState(projectRoot, vmID string, st ProxyState) error {
	dir := RunDir(projectRoot, vmID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(proxyStatePath(projectRoot, vmID), append(b, '\n'), 0o644)
}

// StopDetachedProxy kills the proxy process for vmID if running.
func StopDetachedProxy(projectRoot, vmID string) error {
	st, err := ReadProxyState(projectRoot, vmID)
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
	_ = os.Remove(filepath.Join(RunDir(projectRoot, vmID), "proxy.ready"))
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

// EgressReloadOpts wires hot reload from cage config and/or egress.yaml.
type EgressReloadOpts struct {
	EgressPath  string
	FromConfig  func() ([]byte, error) // nil → only watch egress file
	ConfigPaths func() ([]string, error)
	Traffic     TrafficLogger
}

// ServeProxyOpts is the proxy-serve foreground bundle.
type ServeProxyOpts struct {
	ProjectRoot    string
	VMID           string
	EgressPath     string
	ReadyPath      string
	Pipeline       *Pipeline
	Traffic        TrafficLogger
	Reload         EgressReloadOpts
	DenyHTTP       bool
	DenyMessage    string
	Softnet        bool
	MITM           bool
	HTTPProxy      *HTTPTerminate
	HTTPListen     []HTTPEndpointListen
	Terminate      netplugin.Terminate // for MITM Host inject (same as HTTPProxy.Terminate)
	HostToEP       map[string]string
	AllowedSources []string // guest IPv4s; empty → deny all peers
}

// ServeProxyForeground runs SOCKS + HTTP CONNECT servers (used by proxy-serve child).
func ServeProxyForeground(opts ServeProxyOpts) error {
	if len(opts.AllowedSources) == 0 {
		return fmt.Errorf("proxy-serve: at least one --allow-ip (guest source) is required")
	}
	allow := &SourceAllowlist{}
	allow.SetStrings(opts.AllowedSources)
	bindHost := ListenBindHost()
	srv := &Server{
		Pipeline: opts.Pipeline, OnTraffic: opts.Traffic,
		DenyHTTP: opts.DenyHTTP, DenyMessage: opts.DenyMessage, Allow: allow,
	}
	errCh := make(chan error, 2)
	go func() {
		errCh <- srv.ListenAndServe(ListenAddr(bindHost, 0))
	}()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if srv.Port() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if srv.Port() == 0 {
		_ = srv.Close()
		return fmt.Errorf("socks did not bind")
	}

	httpSrv := &HTTPProxyServer{
		Pipeline:    opts.Pipeline,
		OnTraffic:   opts.Traffic,
		DenyHTTP:    opts.DenyHTTP,
		DenyMessage: opts.DenyMessage,
		Terminate:   opts.Terminate,
		HostToEP:    opts.HostToEP,
		Allow:       allow,
	}
	if opts.MITM {
		mitm, err := LoadOrCreateCA(opts.ProjectRoot)
		if err != nil {
			_ = srv.Close()
			return fmt.Errorf("mitm ca: %w", err)
		}
		httpSrv.MITM = mitm
	}
	go func() {
		errCh <- httpSrv.ListenAndServe(ListenAddr(bindHost, 0))
	}()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if httpSrv.Port() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if httpSrv.Port() == 0 {
		_ = httpSrv.Close()
		_ = srv.Close()
		return fmt.Errorf("http proxy did not bind")
	}
	defer httpSrv.Close()

	if opts.HTTPProxy != nil && len(opts.HTTPListen) > 0 {
		opts.HTTPProxy.BindHost = bindHost
		opts.HTTPProxy.Allow = allow
		opts.HTTPProxy.Pipeline = opts.Pipeline
		opts.HTTPProxy.OnTraffic = opts.Traffic
		opts.HTTPProxy.DenyHTTP = opts.DenyHTTP
		opts.HTTPProxy.DenyMessage = opts.DenyMessage
		if err := opts.HTTPProxy.Start(opts.HTTPListen); err != nil {
			_ = srv.Close()
			return err
		}
		defer opts.HTTPProxy.Close()
		if err := WriteHTTPProxyState(opts.ProjectRoot, opts.VMID, opts.HTTPProxy.Ports()); err != nil {
			_ = srv.Close()
			return err
		}
	}
	st := ProxyState{
		PID:            os.Getpid(),
		Port:           srv.Port(),
		HTTPPort:       httpSrv.Port(),
		BindHost:       bindHost,
		AllowedSources: append([]string{}, opts.AllowedSources...),
	}
	if err := WriteProxyState(opts.ProjectRoot, opts.VMID, st); err != nil {
		_ = srv.Close()
		return err
	}
	if opts.ReadyPath != "" {
		_ = os.WriteFile(opts.ReadyPath, []byte(strconv.Itoa(st.HTTPPort)+"\n"), 0o644)
	}
	if opts.Softnet && opts.Traffic != nil {
		opts.Traffic.Log(SoftnetActiveEvent())
	}
	stopWatch := make(chan struct{})
	reload := opts.Reload
	if opts.Pipeline != nil && len(opts.Pipeline.Filters) > 0 && opts.EgressPath != "" {
		reload.EgressPath = opts.EgressPath
		if reload.Traffic == nil {
			reload.Traffic = opts.Traffic
		}
		go watchEgressReload(reload, opts.Pipeline.Filters, srv, httpSrv, stopWatch)
	}
	err := <-errCh
	close(stopWatch)
	_ = srv.Close()
	return err
}

type fileStamp struct {
	mod  time.Time
	size int64
}

func stampOf(path string) (fileStamp, bool) {
	st, err := os.Stat(path)
	if err != nil {
		return fileStamp{}, false
	}
	return fileStamp{mod: st.ModTime(), size: st.Size()}, true
}

func stampsEqual(a, b map[string]fileStamp) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		o, ok := b[k]
		if !ok || !v.mod.Equal(o.mod) || v.size != o.size {
			return false
		}
	}
	return true
}

func collectStamps(paths []string) map[string]fileStamp {
	out := make(map[string]fileStamp, len(paths))
	for _, p := range paths {
		if s, ok := stampOf(p); ok {
			out[p] = s
		}
	}
	return out
}

func configPathsNow(opts EgressReloadOpts) []string {
	if opts.ConfigPaths == nil {
		return nil
	}
	paths, err := opts.ConfigPaths()
	if err != nil {
		return nil
	}
	return paths
}

func logEgressReload(t TrafficLogger, source string) {
	if t == nil {
		return
	}
	t.Log(TrafficEvent{Action: "RELOAD", Reason: source})
}

// watchEgressReload reloads when cage config chain or egress.yaml changes.
func watchEgressReload(opts EgressReloadOpts, seats []FilterSeat, srv *Server, httpSrv *HTTPProxyServer, stop <-chan struct{}) {
	egressPath := opts.EgressPath
	configPaths := configPathsNow(opts)
	watch := append(append([]string{}, configPaths...), egressPath)
	last := collectStamps(watch)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if refreshed := configPathsNow(opts); refreshed != nil {
				configPaths = refreshed
			}
			watch = append(append([]string{}, configPaths...), egressPath)
			cur := collectStamps(watch)
			if stampsEqual(last, cur) {
				continue
			}

			configChanged := false
			for _, p := range configPaths {
				if last[p] != cur[p] {
					configChanged = true
					break
				}
			}
			if !configChanged {
				for p := range last {
					if p == egressPath {
						continue
					}
					if _, ok := cur[p]; !ok {
						configChanged = true
						break
					}
				}
				for p := range cur {
					if p == egressPath {
						continue
					}
					if _, ok := last[p]; !ok {
						configChanged = true
						break
					}
				}
			}

			var raw []byte
			var err error
			src := "egress.yaml"
			if configChanged && opts.FromConfig != nil {
				raw, err = opts.FromConfig()
				if err != nil {
					logEgressReload(opts.Traffic, "error: "+err.Error())
					last = cur
					continue
				}
				if err := os.WriteFile(egressPath, raw, 0o644); err != nil {
					logEgressReload(opts.Traffic, "error: "+err.Error())
					last = cur
					continue
				}
				if s, ok := stampOf(egressPath); ok {
					cur[egressPath] = s
				}
				src = "config"
			} else {
				raw, err = os.ReadFile(egressPath)
				if err != nil {
					last = cur
					continue
				}
			}
			applyDenyHTTPFromEgressYAML(srv, httpSrv, raw)
			ok := true
			for _, seat := range seats {
				if seat.Filter == nil {
					continue
				}
				if err := seat.Filter.Configure(raw); err != nil {
					logEgressReload(opts.Traffic, "error: "+err.Error())
					ok = false
					break
				}
			}
			if ok {
				logEgressReload(opts.Traffic, src)
			}
			last = cur
		}
	}
}

func applyDenyHTTPFromEgressYAML(srv *Server, httpSrv *HTTPProxyServer, raw []byte) {
	var eg struct {
		DenyResponse *struct {
			HTTP    bool   `yaml:"http"`
			Message string `yaml:"message"`
		} `yaml:"deny_response"`
	}
	if err := yaml.Unmarshal(raw, &eg); err != nil {
		return
	}
	enabled, msg := false, ""
	if eg.DenyResponse != nil {
		enabled = eg.DenyResponse.HTTP
		msg = eg.DenyResponse.Message
	}
	if srv != nil {
		srv.SetDenyHTTP(enabled, msg)
	}
	if httpSrv != nil {
		httpSrv.SetDenyHTTP(enabled, msg)
	}
}
