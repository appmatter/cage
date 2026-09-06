// Package proxy provides the application boundary for Cage's host proxy.
package proxy

import "github.com/appmatter/cage/internal/network"

type State = network.ProxyState

type StartOptions = network.StartDetachedProxyOpts

type HTTPPorts = network.HTTPProxyPorts

func Start(projectRoot, vmID, cageBin string, opts StartOptions) (State, error) {
	return network.StartDetachedProxy(projectRoot, vmID, cageBin, opts)
}

func Stop(projectRoot, vmID string) error {
	return network.StopDetachedProxy(projectRoot, vmID)
}

func ReadHTTPPorts(projectRoot, vmID string) (HTTPPorts, error) {
	return network.ReadHTTPProxyState(projectRoot, vmID)
}

func LogPath(projectRoot, vmID string) string {
	return network.ProxyLogPath(projectRoot, vmID)
}
