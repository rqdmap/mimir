package tui

import "github.com/charmbracelet/lipgloss"

var (
	StatusBarStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	ErrorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	HelpStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)
