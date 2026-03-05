package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type LayoutConfig struct {
	ListRatio float64 `json:"list_ratio"`
	MetaRatio float64 `json:"meta_ratio"`
}

type Config struct {
	AutoPreview bool         `json:"auto_preview"`
	Theme       string       `json:"theme"`
	Layout      LayoutConfig `json:"layout"`
}

func defaultConfig() Config {
	return Config{
		AutoPreview: false,
		Theme:       "",
		Layout: LayoutConfig{
			ListRatio: 0.27,
			MetaRatio: 0.16,
		},
	}
}

func configPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "mimir", "config.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "mimir", "config.json")
}

func Load() Config {
	cfg := defaultConfig()
	data, err := os.ReadFile(configPath())
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg
	}
	return cfg
}
