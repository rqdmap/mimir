package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/local/oc-manager/internal/config"
	"github.com/local/oc-manager/internal/db"
	tui "github.com/local/oc-manager/internal/tui"
	"github.com/local/oc-manager/internal/tui/panes"
)

func main() {
	listSessions := flag.Bool("list-sessions", false, "List sessions and exit")
	flag.Parse()

	opencodeDB, err := db.OpenOpencodeDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not open OpenCode database: %v\n", err)
	}

	managerDB, err := db.OpenManagerDB()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: could not open manager database: %v\n", err)
		os.Exit(1)
	}

	if *listSessions {
		if opencodeDB == nil {
			fmt.Fprintf(os.Stderr, "Error: opencode.db not available\n")
			os.Exit(1)
		}
		sessions, err := db.ListSessions(opencodeDB)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing sessions: %v\n", err)
			os.Exit(1)
		}
		for _, s := range sessions {
			id := s.ID
			if len(id) > 8 {
				id = id[:8]
			}
			fmt.Printf("[%s] %s\n", id, s.Title)
		}
		os.Exit(0)
	}

	cfg := config.Load()

	themeName := cfg.Theme
	if envTheme := os.Getenv("MIMIR_THEME"); envTheme != "" {
		themeName = envTheme
	}
	theme := panes.ThemeByName(themeName)

	app := tui.NewApp(opencodeDB, managerDB, theme, tui.Options{
		AutoPreview: cfg.AutoPreview,
		ListRatio:   cfg.Layout.ListRatio,
		MetaRatio:   cfg.Layout.MetaRatio,
	})
	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
