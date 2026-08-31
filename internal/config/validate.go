package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

const defaultPluginPriority = 1

var secretTemplateRef = regexp.MustCompile(`\{\{\s*secrets\.([A-Za-z0-9_-]+)\.`)

type pluginSeat struct {
	Name     string
	Context  string
	Stage    string
	Priority *int
	Set      bool // Priority explicitly set in config
}

// ValidateFile checks plugin priorities and secrets DAG after merge.
func ValidateFile(f File) error {
	if err := validatePluginPriorities(f); err != nil {
		return err
	}
	return validateSecrets(f)
}

func validatePluginPriorities(f File) error {
	var seats []pluginSeat
	for name, spec := range f.Runtime.Plugins {
		if spec.Image == "" {
			continue // harness seats are not backend stage for priority
		}
		seats = append(seats, pluginSeat{
			Name:     name,
			Context:  "runtime",
			Stage:    "backend",
			Priority: spec.Priority,
			Set:      spec.Priority != nil,
		})
	}
	if f.Network.Plugins.Egress != nil {
		seats = append(seats, pluginSeat{
			Name:     "egress",
			Context:  "network",
			Stage:    "filter",
			Priority: f.Network.Plugins.Egress.Priority,
			Set:      f.Network.Plugins.Egress.Priority != nil,
		})
	}
	if p := f.Network.Plugins.HTTPProxy; p != nil && (len(p.Endpoints) > 0 || p.Priority != nil) {
		seats = append(seats, pluginSeat{
			Name:     "http-proxy",
			Context:  "network",
			Stage:    "terminate",
			Priority: p.Priority,
			Set:      p.Priority != nil,
		})
	}
	if p := f.Network.Plugins.PostgresProxy; p != nil && (len(p.Endpoints) > 0 || p.Priority != nil) {
		seats = append(seats, pluginSeat{
			Name:     "postgres-proxy",
			Context:  "network",
			Stage:    "terminate",
			Priority: p.Priority,
			Set:      p.Priority != nil,
		})
	}

	type key struct{ context, stage string }
	groups := map[key][]pluginSeat{}
	for _, s := range seats {
		k := key{s.Context, s.Stage}
		groups[k] = append(groups[k], s)
	}
	var keys []key
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].context != keys[j].context {
			return keys[i].context < keys[j].context
		}
		return keys[i].stage < keys[j].stage
	})

	for _, k := range keys {
		g := groups[k]
		if len(g) < 2 {
			continue
		}
		seen := map[int]string{}
		for _, s := range g {
			if !s.Set {
				return fmt.Errorf("%s/%s: plugins %s share stage; each must set priority explicitly",
					k.context, k.stage, seatNames(g))
			}
			prio := *s.Priority
			if other, ok := seen[prio]; ok {
				return fmt.Errorf("%s/%s: plugins %q and %q both have priority %d",
					k.context, k.stage, other, s.Name, prio)
			}
			seen[prio] = s.Name
		}
	}
	return nil
}

func seatNames(seats []pluginSeat) string {
	names := make([]string, len(seats))
	for i, s := range seats {
		names[i] = s.Name
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func validateSecrets(f File) error {
	type storeMeta struct {
		plugin string
		store  SecretStore
	}
	byAlias := map[string]storeMeta{}
	for plugin, aliases := range f.Secrets {
		for alias, store := range aliases {
			if prev, ok := byAlias[alias]; ok {
				return fmt.Errorf("secrets: alias %q used by %q and %q (must be unique across plugins)",
					alias, prev.plugin, plugin)
			}
			byAlias[alias] = storeMeta{plugin: plugin, store: store}
		}
	}

	// deps[alias] = aliases it depends on
	deps := map[string][]string{}
	for alias, meta := range byAlias {
		seen := map[string]bool{}
		var list []string
		add := func(dep string) error {
			if dep == alias {
				return fmt.Errorf("secrets.%s.%s: depends on itself", meta.plugin, alias)
			}
			if _, ok := byAlias[dep]; !ok {
				return fmt.Errorf("secrets.%s.%s: unknown dependency %q", meta.plugin, alias, dep)
			}
			if !seen[dep] {
				seen[dep] = true
				list = append(list, dep)
			}
			return nil
		}
		for _, u := range meta.store.Uses {
			if err := add(u); err != nil {
				return err
			}
		}
		for _, v := range meta.store.Vars {
			for _, m := range secretTemplateRef.FindAllStringSubmatch(v, -1) {
				if err := add(m[1]); err != nil {
					return err
				}
			}
		}
		deps[alias] = list
	}

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[string]int{}
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
		return nil
	}
	aliases := make([]string, 0, len(byAlias))
	for a := range byAlias {
		aliases = append(aliases, a)
	}
	sort.Strings(aliases)
	for _, a := range aliases {
		if err := visit(a, nil); err != nil {
			return err
		}
	}
	return nil
}

// EffectivePriority returns the seat priority or default when alone/omitted.
func EffectivePriority(p *int) int {
	if p == nil {
		return defaultPluginPriority
	}
	return *p
}
