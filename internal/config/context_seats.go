package config

import (
	"sort"

	"gopkg.in/yaml.v3"
)

// ConfiguredSeat is one server-selected plugin seat and its opaque YAML.
type ConfiguredSeat struct {
	Name     string
	PluginID string
	YAML     []byte
}

// PluginSeats returns every fs seat without assigning behavior to its name.
func (f FS) PluginSeats() []ConfiguredSeat { return configuredNodeSeats(f.Plugins.Seats) }

// PluginSeats returns every network seat without assigning behavior to its name.
func (n Network) PluginSeats() []ConfiguredSeat { return configuredNodeSeats(n.Plugins.Seats) }

// PluginSeats returns every runtime seat.
func (r Runtime) PluginSeats() []ConfiguredSeat {
	seats := make([]ConfiguredSeat, 0, len(r.Plugins))
	for name, spec := range r.Plugins {
		raw, _ := yaml.Marshal(spec)
		seats = append(seats, ConfiguredSeat{Name: name, PluginID: spec.PluginID(name), YAML: raw})
	}
	sort.Slice(seats, func(i, j int) bool { return seats[i].Name < seats[j].Name })
	return seats
}

// PluginSeats returns every secrets seat.
func (s Secrets) PluginSeats() []ConfiguredSeat {
	seats := make([]ConfiguredSeat, 0, len(s.Plugins))
	for name, spec := range s.Plugins {
		raw, _ := yaml.Marshal(spec)
		seats = append(seats, ConfiguredSeat{Name: name, PluginID: spec.PluginID(name), YAML: raw})
	}
	sort.Slice(seats, func(i, j int) bool { return seats[i].Name < seats[j].Name })
	return seats
}

func fsPluginNodes(p FSPlugins) map[string]yaml.Node {
	out := mergePluginNodes(p.Seats, nil)
	if out == nil {
		out = map[string]yaml.Node{}
	}
	if p.Mention != nil {
		if _, ok := out["mention"]; !ok {
			var node yaml.Node
			_ = node.Encode(p.Mention)
			out["mention"] = node
		}
	}
	if p.SecretsScanner != nil {
		if _, ok := out["secrets_scanner"]; !ok {
			var node yaml.Node
			_ = node.Encode(p.SecretsScanner)
			out["secrets_scanner"] = node
		}
	}
	return out
}

func networkPluginNodes(p NetworkPlugins) map[string]yaml.Node {
	out := mergePluginNodes(p.Seats, nil)
	if out == nil {
		out = map[string]yaml.Node{}
	}
	if p.Egress != nil {
		if _, ok := out["egress"]; !ok {
			var node yaml.Node
			_ = node.Encode(p.Egress)
			out["egress"] = node
		}
	}
	if p.HTTPProxy != nil {
		if _, ok := out["http-proxy"]; !ok {
			var node yaml.Node
			_ = node.Encode(p.HTTPProxy)
			out["http-proxy"] = node
		}
	}
	if p.PostgresProxy != nil {
		if _, ok := out["postgres-proxy"]; !ok {
			var node yaml.Node
			_ = node.Encode(p.PostgresProxy)
			out["postgres-proxy"] = node
		}
	}
	return out
}

func syncFSPluginViews(p *FSPlugins) error {
	p.Mention = nil
	p.SecretsScanner = nil
	if node, ok := p.Seats["mention"]; ok {
		var seat Mention
		if err := node.Decode(&seat); err != nil {
			return err
		}
		p.Mention = &seat
	}
	if node, ok := p.Seats["secrets_scanner"]; ok {
		var seat SecretsScanner
		if err := node.Decode(&seat); err != nil {
			return err
		}
		p.SecretsScanner = &seat
	}
	return nil
}

func syncNetworkPluginViews(p *NetworkPlugins) error {
	p.Egress = nil
	p.HTTPProxy = nil
	p.PostgresProxy = nil
	if node, ok := p.Seats["egress"]; ok {
		var seat Egress
		if err := node.Decode(&seat); err != nil {
			return err
		}
		p.Egress = &seat
	}
	if node, ok := p.Seats["http-proxy"]; ok {
		var seat ProtocolProxies
		if err := node.Decode(&seat); err != nil {
			return err
		}
		p.HTTPProxy = &seat
	}
	if node, ok := p.Seats["postgres-proxy"]; ok {
		var seat ProtocolProxies
		if err := node.Decode(&seat); err != nil {
			return err
		}
		p.PostgresProxy = &seat
	}
	return nil
}

func configuredNodeSeats(nodes map[string]yaml.Node) []ConfiguredSeat {
	seats := make([]ConfiguredSeat, 0, len(nodes))
	for name, node := range nodes {
		var identity struct {
			Plugin string `yaml:"plugin"`
		}
		_ = node.Decode(&identity)
		raw, _ := yaml.Marshal(&node)
		pluginID := identity.Plugin
		if pluginID == "" {
			pluginID = name
		}
		seats = append(seats, ConfiguredSeat{Name: name, PluginID: pluginID, YAML: raw})
	}
	sort.Slice(seats, func(i, j int) bool { return seats[i].Name < seats[j].Name })
	return seats
}

func mergePluginNodes(base, over map[string]yaml.Node) map[string]yaml.Node {
	if len(base) == 0 && len(over) == 0 {
		return nil
	}
	out := map[string]yaml.Node{}
	for name, node := range base {
		out[name] = cloneNode(node)
	}
	for name, node := range over {
		if current, ok := out[name]; ok {
			out[name] = mergeNode(current, node)
		} else {
			out[name] = cloneNode(node)
		}
	}
	return out
}

func mergeNode(base, over yaml.Node) yaml.Node {
	if base.Kind != yaml.MappingNode || over.Kind != yaml.MappingNode {
		return cloneNode(over)
	}
	out := cloneNode(base)
	for i := 0; i < len(over.Content); i += 2 {
		key, value := over.Content[i], over.Content[i+1]
		found := -1
		for j := 0; j < len(out.Content); j += 2 {
			if out.Content[j].Value == key.Value {
				found = j
				break
			}
		}
		if found < 0 {
			out.Content = append(out.Content, cloneNodePtr(*key), cloneNodePtr(*value))
			continue
		}
		out.Content[found+1] = cloneNodePtr(mergeNode(*out.Content[found+1], *value))
	}
	return out
}

func cloneNode(node yaml.Node) yaml.Node {
	out := node
	out.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		out.Content[i] = cloneNodePtr(*child)
	}
	return out
}

func cloneNodePtr(node yaml.Node) *yaml.Node {
	out := cloneNode(node)
	return &out
}
