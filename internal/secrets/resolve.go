package secrets

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/appmatter/cage/internal/config"
	"github.com/appmatter/cage/internal/pluginhost"
)

var (
	seatRefRe    = regexp.MustCompile(`\{\{\s*secrets\.([A-Za-z0-9_-]+)\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)
	anySecretsRe = regexp.MustCompile(`\{\{\s*secrets\.`)
)

// Values is seat → var → resolved secret.
type Values map[string]map[string]string

// ContainsTemplate reports whether s has a {{ secrets.* }} placeholder.
func ContainsTemplate(s string) bool {
	return anySecretsRe.MatchString(s)
}

// Apply replaces {{ secrets.<seat>.<var> }} in s using values.
func Apply(s string, values Values) (string, error) {
	var first error
	out := seatRefRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := seatRefRe.FindStringSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		seat, name := sub[1], sub[2]
		v, ok := values[seat][name]
		if !ok {
			if first == nil {
				first = fmt.Errorf("unknown secret %s.%s", seat, name)
			}
			return m
		}
		return v
	})
	if first != nil {
		return "", first
	}
	if ContainsTemplate(out) {
		return "", fmt.Errorf("unresolved secrets template in %q", s)
	}
	return out, nil
}

// ApplyBytes runs Apply on the string form of b.
func ApplyBytes(b []byte, values Values) ([]byte, error) {
	out, err := Apply(string(b), values)
	if err != nil {
		return nil, err
	}
	return []byte(out), nil
}

// Resolve loads secrets plugins and resolves all seats in dependency order.
// projectRoot is used to find installed plugin binaries.
func Resolve(projectRoot string, seats map[string]config.SecretStore) (Values, error) {
	if len(seats) == 0 {
		return Values{}, nil
	}
	order, err := orderSeats(seats)
	if err != nil {
		return nil, err
	}

	out := Values{}
	clients := map[string]*pluginhost.SecretsClient{}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	for _, seat := range order {
		store := seats[seat]
		pluginID := store.PluginID(seat)
		client, ok := clients[pluginID]
		if !ok {
			cmdPath, _, err := pluginhost.ResolveCommand(projectRoot, "secrets", pluginID)
			if err != nil {
				return nil, fmt.Errorf("secrets.plugins.%s: %w (install with: cage plugin install -l ./plugins/secrets/%s)", seat, err, pluginID)
			}
			client, err = pluginhost.DispenseSecrets(cmdPath)
			if err != nil {
				return nil, fmt.Errorf("secrets.plugins.%s: %w", seat, err)
			}
			clients[pluginID] = client
		}

		cfgYAML, err := yaml.Marshal(struct {
			Account string `yaml:"account,omitempty"`
			App     *bool  `yaml:"app,omitempty"`
			Region  string `yaml:"region,omitempty"`
		}{Account: store.Account, App: store.App, Region: store.Region})
		if err != nil {
			return nil, err
		}
		if err := client.Store.Configure(cfgYAML); err != nil {
			return nil, fmt.Errorf("secrets.plugins.%s configure: %w", seat, err)
		}

		refs := make(map[string]string, len(store.Vars))
		for name, ref := range store.Vars {
			resolved, err := Apply(ref, out)
			if err != nil {
				return nil, fmt.Errorf("secrets.plugins.%s.%s: %w", seat, name, err)
			}
			refs[name] = resolved
		}
		vals, err := client.Store.Resolve(refs)
		if err != nil {
			return nil, fmt.Errorf("secrets.plugins.%s: %w", seat, err)
		}
		out[seat] = vals
	}
	return out, nil
}

func orderSeats(seats map[string]config.SecretStore) ([]string, error) {
	deps := map[string][]string{}
	for seat, store := range seats {
		seen := map[string]bool{}
		var list []string
		add := func(dep string) error {
			if dep == seat {
				return fmt.Errorf("secrets.plugins.%s: depends on itself", seat)
			}
			if _, ok := seats[dep]; !ok {
				return fmt.Errorf("secrets.plugins.%s: unknown dependency %q", seat, dep)
			}
			if !seen[dep] {
				seen[dep] = true
				list = append(list, dep)
			}
			return nil
		}
		for _, u := range store.Uses {
			if err := add(u); err != nil {
				return nil, err
			}
		}
		for _, v := range store.Vars {
			for _, m := range seatRefRe.FindAllStringSubmatch(v, -1) {
				if err := add(m[1]); err != nil {
					return nil, err
				}
			}
		}
		deps[seat] = list
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
	var order []string
	var visit func(string, []string) error
	visit = func(n string, stack []string) error {
		switch color[n] {
		case black:
			return nil
		case gray:
			return fmt.Errorf("secrets: dependency cycle: %s", strings.Join(append(stack, n), " -> "))
		}
		color[n] = gray
		for _, d := range deps[n] {
			if err := visit(d, append(stack, n)); err != nil {
				return err
			}
		}
		color[n] = black
		order = append(order, n)
		return nil
	}
	names := make([]string, 0, len(seats))
	for s := range seats {
		names = append(names, s)
	}
	sort.Strings(names)
	for _, s := range names {
		if err := visit(s, nil); err != nil {
			return nil, err
		}
	}
	return order, nil
}
