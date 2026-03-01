package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

type model struct{}

func (m model) Init() tea.Cmd { return nil }
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok && msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	return m, nil
}
func (m model) View() string { return "ocm starting...\n" }

func main() {
	listSessions := flag.Bool("list-sessions", false, "List sessions and exit")
	flag.Parse()

	if *listSessions {
		fmt.Println("TODO: list sessions")
		os.Exit(0)
	}

	p := tea.NewProgram(model{})
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
