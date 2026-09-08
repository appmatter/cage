package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// webConfig is local UI config.
type webConfig struct {
	Listen     string `json:"listen"`
	Supervisor string `json:"supervisor"`
	Cage       string `json:"cage"`
	Token      string `json:"token,omitempty"`
	DataDir    string `json:"data_dir,omitempty"`
}

func defaultWebConfig() webConfig {
	return webConfig{
		Listen:     "127.0.0.1:3000",
		Supervisor: "ws://127.0.0.1:8081/v1/supervisor",
		Cage:       "http://127.0.0.1:8081",
	}
}

func loadWebConfig(path string) (webConfig, error) {
	cfg := defaultWebConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, fmt.Errorf("config %q: %w", path, err)
		}
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return webConfig{}, fmt.Errorf("config %q: %w", path, err)
	}
	return cfg, nil
}
