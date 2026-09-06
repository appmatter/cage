package contextapi

import (
	"bytes"
	"encoding/json"
	"regexp"

	"github.com/appmatter/cage/internal/config"
	plugin "github.com/appmatter/cage/pkg/plugin/v1/client"
	fsplugin "github.com/appmatter/cage/pkg/plugin/v1/fs"
	"gopkg.in/yaml.v3"
)

// VMMeta is safe selected-VM identity embedded in resolved contexts.
type VMMeta struct {
	InstanceID  string `json:"instanceID,omitempty"`
	BackendVMID string `json:"backendVMID,omitempty"`
}

// FSContext creates the complete resolved fs input for a configured seat.
func FSContext(projectRoot string, r config.Resolved, seatYAML []byte, meta VMMeta) (plugin.Context, error) {
	mounts := make([]fsplugin.Path, len(r.Mounts))
	copies := make([]fsplugin.Path, len(r.Copies))
	for i, p := range r.Mounts {
		mounts[i] = fsplugin.Path{Host: p.Host, Guest: p.Guest, Path: p.Target, Permission: p.Permission}
	}
	for i, p := range r.Copies {
		copies[i] = fsplugin.Path{Host: p.Host, Guest: p.Guest, Path: p.Target, Permission: p.Permission}
	}
	data, err := json.Marshal(fsplugin.Context{
		ProjectRoot: projectRoot,
		Workdir:     r.Runtime.Workdir,
		Layout:      r.Layout.Mode,
		Mounts:      mounts,
		Copies:      copies,
		Deny:        r.Deny,
		SeatYAML:    seatYAML,
		InstanceID:  meta.InstanceID,
		BackendVMID: meta.BackendVMID,
	})
	return plugin.Context{Kind: "fs", Data: data}, err
}

// RuntimeContext contains no ambient host environment or guest command input.
func RuntimeContext(r config.Resolved, seatYAML []byte, meta VMMeta) (plugin.Context, error) {
	data, err := json.Marshal(struct {
		Workdir     string `json:"workdir"`
		GOOS        string `json:"goos"`
		SeatYAML    []byte `json:"seatYAML"`
		InstanceID  string `json:"instanceID,omitempty"`
		BackendVMID string `json:"backendVMID,omitempty"`
	}{r.Runtime.Workdir, r.Runtime.GOOS, seatYAML, meta.InstanceID, meta.BackendVMID})
	return plugin.Context{Kind: "runtime", Data: data}, err
}

// NetworkPolicy is the non-secret network policy available to client plugins.
type NetworkPolicy struct {
	ProxyEnabled bool `json:"proxyEnabled"`
	Logging      bool `json:"logging"`
	MITM         bool `json:"mitm"`
}

// NetworkContext contains minimal resolved network policy and safe seat config.
func NetworkContext(r config.Resolved, seatYAML []byte, meta VMMeta) (plugin.Context, error) {
	safeYAML, err := stripSecretValues(seatYAML)
	if err != nil {
		return plugin.Context{Kind: "network"}, err
	}
	data, err := json.Marshal(struct {
		Policy      NetworkPolicy `json:"policy"`
		SeatYAML    []byte        `json:"seatYAML"`
		InstanceID  string        `json:"instanceID,omitempty"`
		BackendVMID string        `json:"backendVMID,omitempty"`
	}{
		Policy: NetworkPolicy{
			ProxyEnabled: r.Network.ProxyEnabled(),
			Logging:      r.Network.LoggingEnabled(),
			MITM:         r.Network.MITMEnabled(),
		},
		SeatYAML:    safeYAML,
		InstanceID:  meta.InstanceID,
		BackendVMID: meta.BackendVMID,
	})
	return plugin.Context{Kind: "network", Data: data}, err
}

var secretValue = regexp.MustCompile(`\{\{\s*secrets\.`)

// stripSecretValues removes secret references from opaque network seat YAML.
func stripSecretValues(raw []byte) ([]byte, error) {
	if !secretValue.Match(raw) {
		return append([]byte(nil), raw...), nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	for _, node := range doc.Content {
		stripSecretNode(node)
	}
	var out bytes.Buffer
	encoder := yaml.NewEncoder(&out)
	defer encoder.Close()
	if err := encoder.Encode(&doc); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func stripSecretNode(node *yaml.Node) bool {
	switch node.Kind {
	case yaml.ScalarNode:
		found := secretValue.MatchString(node.Value)
		if found {
			node.Value = ""
		}
		return found
	case yaml.SequenceNode:
		found := false
		kept := node.Content[:0]
		for _, child := range node.Content {
			childFound := stripSecretNode(child)
			found = found || childFound
			if child.Kind == yaml.ScalarNode && childFound {
				continue
			}
			kept = append(kept, child)
		}
		node.Content = kept
		return found
	case yaml.MappingNode:
		found := false
		kept := node.Content[:0]
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			valueFound := stripSecretNode(value)
			found = found || valueFound
			if value.Kind == yaml.ScalarNode && valueFound {
				continue
			}
			kept = append(kept, key, value)
		}
		node.Content = kept
		return found
	default:
		return false
	}
}

// SecretsContext deliberately has no values. Secrets seats must not be exposed
// unless an approved capability explicitly accepts this empty context.
func SecretsContext(seatYAML []byte, meta VMMeta) (plugin.Context, error) {
	data, err := json.Marshal(struct {
		SeatYAML    []byte `json:"seatYAML"`
		InstanceID  string `json:"instanceID,omitempty"`
		BackendVMID string `json:"backendVMID,omitempty"`
	}{seatYAML, meta.InstanceID, meta.BackendVMID})
	return plugin.Context{Kind: "secrets", Data: data}, err
}
