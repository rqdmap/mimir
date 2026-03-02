package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/local/oc-manager/internal/db"
	tui "github.com/local/oc-manager/internal/tui"
	"github.com/muesli/termenv"
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

	glamourStyle := os.Getenv("GLAMOUR_STYLE")
	if glamourStyle == "" || glamourStyle == "auto" {
		if termenv.HasDarkBackground() {
			glamourStyle = "dark"
		} else {
			glamourStyle = "light"
		}
	} else if strings.HasPrefix(glamourStyle, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			glamourStyle = filepath.Join(home, glamourStyle[2:])
		}
	}

	app := tui.NewApp(opencodeDB, managerDB, glamourStyle)
	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
