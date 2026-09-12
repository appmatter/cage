package network

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/appmatter/cage/internal/proxy"
	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

// RunDir is .cage/run/<vmID> under projectRoot.
func RunDir(projectRoot, vmID string) string {
	if projectRoot == "" {
		projectRoot = "."
	}
	return filepath.Join(projectRoot, ".cage", "run", vmID)
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
		if err := proxy.WriteHTTPPorts(opts.ProjectRoot, opts.VMID, opts.HTTPProxy.Ports()); err != nil {
			_ = srv.Close()
			return err
		}
	}
	st := proxy.State{
		PID:            os.Getpid(),
		Port:           srv.Port(),
		HTTPPort:       httpSrv.Port(),
		BindHost:       bindHost,
		AllowedSources: append([]string{}, opts.AllowedSources...),
	}
	if err := proxy.WriteState(opts.ProjectRoot, opts.VMID, st); err != nil {
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
