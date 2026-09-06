package contextapi

import (
	"fmt"

	"github.com/appmatter/cage/internal/config"
	"github.com/appmatter/cage/internal/pluginhost"
	plugin "github.com/appmatter/cage/pkg/plugin/v1/client"
)

// LoadedSeats is the configured capability set and the processes it owns.
type LoadedSeats struct {
	Seats map[string]map[string]Seat
	close []func()
}

func (s *LoadedSeats) Close() {
	if s == nil {
		return
	}
	for _, close := range s.close {
		close()
	}
}

type capabilityLoader func(string, string, config.ConfiguredSeat) (plugin.Capability, bool, func(), error)

// LoadSeats discovers optional capabilities from the active resolved config.
func LoadSeats(projectRoot string, r config.Resolved, meta VMMeta) *LoadedSeats {
	return loadSeats(projectRoot, r, meta, configuredCapability)
}

func configuredCapability(projectRoot, context string, seat config.ConfiguredSeat) (plugin.Capability, bool, func(), error) {
	cmd, manifest, err := pluginhost.ResolveCommand(projectRoot, context, seat.PluginID)
	if err != nil {
		if context == "secrets" {
			return nil, false, nil, nil
		}
		return nil, false, nil, err
	}
	if !manifest.Client {
		return nil, false, nil, nil
	}

	var loaded *pluginhost.CapabilityClient
	switch context {
	case "runtime":
		loaded, _, err = pluginhost.DispenseRuntimeCapability(cmd)
	case "fs":
		loaded, _, err = pluginhost.DispenseFSCapability(cmd)
	case "network":
		loaded, _, err = pluginhost.DispenseNetworkCapability(cmd)
	case "secrets":
		loaded, _, err = pluginhost.DispenseSecretsCapability(cmd)
	default:
		return nil, false, nil, fmt.Errorf("unknown plugin context %q", context)
	}
	if err != nil {
		return nil, false, nil, err
	}
	if loaded == nil {
		return nil, false, nil, nil
	}
	return loaded.Capability, true, loaded.Close, nil
}

func loadSeats(projectRoot string, r config.Resolved, meta VMMeta, load capabilityLoader) *LoadedSeats {
	out := &LoadedSeats{Seats: map[string]map[string]Seat{}}
	add := func(context string, seat config.ConfiguredSeat, resolved plugin.Context) {
		if out.Seats[context] == nil {
			out.Seats[context] = map[string]Seat{}
		}
		capability, declared, close, err := load(projectRoot, context, seat)
		if err != nil {
			out.Seats[context][seat.Name] = Seat{Context: resolved, Err: fmt.Errorf("plugin unavailable")}
			return
		}
		if !declared {
			out.Seats[context][seat.Name] = Seat{Context: resolved}
			return
		}
		out.close = append(out.close, close)
		out.Seats[context][seat.Name] = Seat{Context: resolved, Plugin: capability}
	}
	for _, seat := range r.Runtime.PluginSeats() {
		context, err := RuntimeContext(r, seat.YAML, meta)
		if err == nil {
			add("runtime", seat, context)
		}
	}
	for _, seat := range r.FS.PluginSeats() {
		context, err := FSContext(projectRoot, r, seat.YAML, meta)
		if err == nil {
			add("fs", seat, context)
		}
	}
	for _, seat := range r.Network.PluginSeats() {
		context, err := NetworkContext(r, seat.YAML, meta)
		if err == nil {
			add("network", seat, context)
		}
	}
	for _, seat := range r.Secrets.PluginSeats() {
		context, err := SecretsContext(seat.YAML, meta)
		if err == nil {
			add("secrets", seat, context)
		}
	}
	return out
}
