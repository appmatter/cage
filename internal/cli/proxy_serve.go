package cli

import (
	"fmt"
	"os"
	"runtime"
	"sort"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/appmatter/cage/internal/config"
	"github.com/appmatter/cage/internal/network"
	"github.com/appmatter/cage/internal/pluginhost"
	"github.com/appmatter/cage/internal/termlog"
	netplugin "github.com/appmatter/cage/pkg/plugin/v1/network"
)

func newProxyServeCmd() *cobra.Command {
	var (
		projectRoot   string
		id            string
		egressPath    string
		httpProxyPath string
		readyPath     string
		configPath    string
		logTraffic    bool
		denyHTTP      bool
		denyMessage   string
		softnet       bool
		mitm          bool
		allowIPs      []string
	)
	cmd := &cobra.Command{
		Use:    "proxy-serve",
		Hidden: true,
		Short:  "Run host SOCKS5 + HTTP CONNECT (+ MITM) (spawned by vm start)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				return fmt.Errorf("--id is required")
			}
			if len(allowIPs) == 0 {
				return fmt.Errorf("--allow-ip is required (guest source address)")
			}
			var seats []network.FilterSeat
			var closers []func()
			defer func() {
				for i := len(closers) - 1; i >= 0; i-- {
					closers[i]()
				}
			}()
			if egressPath != "" {
				raw, err := os.ReadFile(egressPath)
				if err != nil {
					return err
				}
				if len(raw) > 0 && string(raw) != "{}\n" && string(raw) != "null\n" {
					var eg config.Egress
					if err := yaml.Unmarshal(raw, &eg); err != nil {
						return err
					}
					f, closer, err := loadEgressFilter(projectRoot, raw)
					if err != nil {
						return err
					}
					closers = append(closers, closer)
					pri := 1
					if eg.Priority != nil {
						pri = *eg.Priority
					}
					seats = append(seats, network.FilterSeat{Priority: pri, Filter: f})
				}
			}
			pipe := network.NewPipeline(seats)
			var traffic network.TrafficLogger
			if logTraffic {
				logFile, err := network.OpenProxyLog(projectRoot, id)
				if err != nil {
					return err
				}
				defer logFile.Close()
				traffic = network.MultiTrafficLogger{
					network.NewJSONLTrafficLogger(logFile),
					network.HumanTrafficLogger{Print: termlog.CLI},
				}
			}
			reload := network.EgressReloadOpts{Traffic: traffic}
			if configPath != "" {
				reload.ConfigPaths = func() ([]string, error) {
					return config.ChainPaths(configPath)
				}
				reload.FromConfig = func() ([]byte, error) {
					r, err := config.LoadResolved(projectRoot, configPath, runtime.GOOS)
					if err != nil {
						return nil, err
					}
					if r.Network.Plugins.Egress == nil {
						return []byte("{}\n"), nil
					}
					return yaml.Marshal(r.Network.Plugins.Egress)
				}
			}

			opts := network.ServeProxyOpts{
				ProjectRoot:    projectRoot,
				VMID:           id,
				EgressPath:     egressPath,
				ReadyPath:      readyPath,
				Pipeline:       pipe,
				Traffic:        traffic,
				Reload:         reload,
				DenyHTTP:       denyHTTP,
				DenyMessage:    denyMessage,
				Softnet:        softnet,
				MITM:           mitm,
				AllowedSources: allowIPs,
			}

			if httpProxyPath != "" {
				raw, err := os.ReadFile(httpProxyPath)
				if err != nil {
					return err
				}
				if len(raw) > 0 && string(raw) != "{}\n" && string(raw) != "null\n" {
					var pp config.ProtocolProxies
					if err := yaml.Unmarshal(raw, &pp); err != nil {
						return fmt.Errorf("http-proxy config: %w", err)
					}
					term, closer, err := loadHTTPProxyTerminate(projectRoot, raw)
					if err != nil {
						return err
					}
					closers = append(closers, closer)
					opts.HTTPProxy = &network.HTTPTerminate{Terminate: term}
					opts.Terminate = term
					hostMap, err := network.ParseHostEndpointMap(raw)
					if err != nil {
						return err
					}
					opts.HostToEP = hostMap
					var names []string
					for n := range pp.Endpoints {
						names = append(names, n)
					}
					sort.Strings(names)
					for _, n := range names {
						opts.HTTPListen = append(opts.HTTPListen, network.HTTPEndpointListen{
							Name:   n,
							Listen: pp.Endpoints[n].Listen,
						})
					}
				}
			}

			return network.ServeProxyForeground(opts)
		},
	}
	cmd.Flags().StringVar(&projectRoot, "project", ".", "project root")
	cmd.Flags().StringVar(&id, "id", "", "VM id")
	cmd.Flags().StringVar(&egressPath, "egress", "", "path to egress yaml (may be empty object)")
	cmd.Flags().StringVar(&httpProxyPath, "http-proxy", "", "path to http-proxy yaml")
	cmd.Flags().StringVar(&readyPath, "ready", "", "path written when listening")
	cmd.Flags().StringVar(&configPath, "config", "", "active cage yaml (hot-reload egress)")
	cmd.Flags().BoolVar(&logTraffic, "log", false, "log CONNECT events to proxy.log + stderr")
	cmd.Flags().BoolVar(&softnet, "softnet", false, "host-only softnet active (advisory SOFTNET log line)")
	cmd.Flags().BoolVar(&denyHTTP, "deny-http", false, "inject HTTP 403 on plain-HTTP egress DENY")
	cmd.Flags().StringVar(&denyMessage, "deny-message", "", "body for --deny-http (default built-in)")
	cmd.Flags().BoolVar(&mitm, "mitm", false, "HTTPS MITM on HTTP CONNECT (method/path + inject)")
	cmd.Flags().StringArrayVar(&allowIPs, "allow-ip", nil, "guest IPv4 allowed to use this proxy (repeatable)")
	return cmd
}

func loadEgressFilter(projectRoot string, raw []byte) (netplugin.Filter, func(), error) {
	cmdPath, _, err := pluginhost.ResolveCommand(projectRoot, "network", "egress")
	if err != nil {
		return nil, nil, fmt.Errorf("network/egress: %w (install with: cage plugin install -l ./plugins/network/egress)", err)
	}
	client, err := pluginhost.DispenseNetworkFilter(cmdPath)
	if err != nil {
		return nil, nil, err
	}
	if err := client.Filter.Configure(raw); err != nil {
		client.Close()
		return nil, nil, err
	}
	return client.Filter, client.Close, nil
}

func loadHTTPProxyTerminate(projectRoot string, raw []byte) (netplugin.Terminate, func(), error) {
	cmdPath, _, err := pluginhost.ResolveCommand(projectRoot, "network", "http-proxy")
	if err != nil {
		return nil, nil, fmt.Errorf("network/http-proxy: %w (install with: cage plugin install -l ./plugins/network/http-proxy)", err)
	}
	client, err := pluginhost.DispenseNetworkTerminate(cmdPath)
	if err != nil {
		return nil, nil, err
	}
	if err := client.Terminate.Configure(raw); err != nil {
		client.Close()
		return nil, nil, err
	}
	return client.Terminate, client.Close, nil
}
