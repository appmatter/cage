package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/appmatter/cage/internal/climenu"
)

// ListConfigs returns .cage/cage*.yaml paths under projectRoot.
func ListConfigs(projectRoot string) ([]string, error) {
	dir := filepath.Join(projectRoot, ".cage")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var yamls []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		if (ext == ".yaml" || ext == ".yml") && strings.HasPrefix(name, "cage") {
			yamls = append(yamls, filepath.Join(dir, name))
		}
	}
	return yamls, nil
}

// Resolve picks a config path: explicit, sole file, or interactive select if multiple.
func Resolve(projectRoot, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	yamls, err := ListConfigs(projectRoot)
	if err != nil {
		return "", err
	}
	switch len(yamls) {
	case 0:
		return "", fmt.Errorf("no cage*.yaml in %s", filepath.Join(projectRoot, ".cage"))
	case 1:
		return yamls[0], nil
	default:
		return selectConfig(yamls)
	}
}

func selectConfig(yamls []string) (string, error) {
	items := make([]climenu.Item, 0, len(yamls))
	for _, p := range yamls {
		items = append(items, climenu.Item{Value: p, Label: p})
	}
	picked, err := climenu.One("Select config", items)
	if err != nil {
		if !climenu.IsTTY() {
			return "", fmt.Errorf("multiple configs; pass --config (non-interactive)")
		}
		return "", err
	}
	return picked, nil
}
