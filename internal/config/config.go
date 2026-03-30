package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

var validTabNames = map[string]bool{
	"sessions": true,
	"ideas":    true,
	"tags":     true,
	"stats":    true,
}

var DefaultTabOrder = []string{"ideas", "sessions", "tags"}
var DefaultRatio = []int{2, 5, 2}

type LayoutConfig struct {
	Ratio    []int    `json:"ratio"`
	TabOrder []string `json:"tab_order"`
}

type Config struct {
	AutoPreview         bool         `json:"auto_preview"`
	Theme               string       `json:"theme"`
	Layout              LayoutConfig `json:"layout"`
	ExportDir           string       `json:"export_dir"`             // directory for exported markdown files; defaults to cwd
	TriliumURL          string       `json:"trilium_url"`            // e.g. "http://localhost:8080"
	TriliumToken        string       `json:"trilium_token"`          // ETAPI auth token
	TriliumParentNoteID string       `json:"trilium_parent_note_id"` // parent note; defaults to "root"
}

func defaultConfig() Config {
	return Config{
		AutoPreview: false,
		Theme:       "",
		ExportDir:   "",
		Layout: LayoutConfig{
			Ratio:    append([]int{}, DefaultRatio...),
			TabOrder: append([]string{}, DefaultTabOrder...),
		},
		TriliumParentNoteID: "root",
	}
}

func NormalizeRatio(r []int) [3]int {
	if len(r) != 3 {
		return [3]int{DefaultRatio[0], DefaultRatio[1], DefaultRatio[2]}
	}
	sum := 0
	for _, v := range r {
		if v < 0 {
			return [3]int{DefaultRatio[0], DefaultRatio[1], DefaultRatio[2]}
		}
		sum += v
	}
	if sum == 0 {
		return [3]int{DefaultRatio[0], DefaultRatio[1], DefaultRatio[2]}
	}
	return [3]int{r[0], r[1], r[2]}
}

func NormalizeTabOrder(order []string) []string {
	seen := make(map[string]bool)
	var result []string
	for _, name := range order {
		if validTabNames[name] && !seen[name] {
			result = append(result, name)
			seen[name] = true
		}
	}
	for _, name := range DefaultTabOrder {
		if !seen[name] {
			result = append(result, name)
			seen[name] = true
		}
	}
	return result
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
	if len(cfg.Layout.Ratio) == 0 {
		cfg.Layout.Ratio = append([]int{}, DefaultRatio...)
	}
	if len(cfg.Layout.TabOrder) == 0 {
		cfg.Layout.TabOrder = append([]string{}, DefaultTabOrder...)
	}
	return cfg
}
