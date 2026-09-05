package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type configYAML struct {
	Path  string `yaml:"path,omitempty"`
	Login string `yaml:"login,omitempty"`
}

type parsedConfig struct {
	Path  string
	Login string // browser | device_code
}

func parseConfig(raw []byte) (parsedConfig, error) {
	var cfg configYAML
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return parsedConfig{}, fmt.Errorf("openai-oauth configure: %w", err)
		}
	}
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		var err error
		path, err = defaultAuthPath()
		if err != nil {
			return parsedConfig{}, err
		}
	}
	path = expandHome(path)
	login := strings.ToLower(strings.TrimSpace(cfg.Login))
	if login == "" {
		login = "browser"
	}
	switch login {
	case "browser", "device_code":
	default:
		return parsedConfig{}, fmt.Errorf("openai-oauth: login must be browser or device_code")
	}
	return parsedConfig{Path: path, Login: login}, nil
}

func expandHome(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}
