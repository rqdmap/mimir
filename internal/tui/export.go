package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/local/oc-manager/internal/tui/panes"
)

// ExportConfirmedMsg is sent when the user confirms an export.
type ExportConfirmedMsg struct {
	IncludeMetadata  bool
	IncludeText      bool
	IncludeTool      bool
	IncludeReasoning bool
}

// ExportCancelledMsg is sent when the user cancels the export overlay.
type ExportCancelledMsg struct{}

// ExportDoneMsg is sent after the file has been written (success or error).
type ExportDoneMsg struct {
	Path string
	Err  error
}

// exportItem is a single toggle row in the checklist.
type exportItem struct {
	label   string
	enabled bool
}

// ExportOverlay is a modal checklist overlay for configuring export options.
type ExportOverlay struct {
	active bool
	items  []exportItem
	cursor int
	width  int
	height int
	theme  panes.Theme
}

func NewExportOverlay(width, height int, theme panes.Theme) ExportOverlay {
	return ExportOverlay{
		width:  width,
		height: height,
		theme:  theme,
		items: []exportItem{
			{"Conversation text (user & assistant)", true},
			{"Session metadata (ID, directory, tags…)", true},
			{"Tool calls (name, input, output)", false},
			{"Reasoning / thinking blocks", false},
		},
	}
}

func (e *ExportOverlay) Activate() {
	e.active = true
	e.cursor = 0
}

func (e *ExportOverlay) IsActive() bool {
	return e.active
}

func (e *ExportOverlay) SetSize(width, height int) {
	e.width = width
	e.height = height
}

func (e ExportOverlay) Update(msg tea.Msg) (ExportOverlay, tea.Cmd) {
	if !e.active {
		return e, nil
	}

	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return e, nil
	}

	switch km.String() {
	case "esc", "q":
		e.active = false
		return e, func() tea.Msg { return ExportCancelledMsg{} }

	case "up", "k":
		if e.cursor > 0 {
			e.cursor--
		}

	case "down", "j":
		if e.cursor < len(e.items)-1 {
			e.cursor++
		}

	case " ":
		e.items[e.cursor].enabled = !e.items[e.cursor].enabled

	case "enter":
		opts := e.buildOpts()
		e.active = false
		return e, func() tea.Msg { return ExportConfirmedMsg(opts) }
	}

	return e, nil
}

func (e ExportOverlay) buildOpts() ExportConfirmedMsg {
	// item order must match the slice in Activate / NewExportOverlay
	return ExportConfirmedMsg{
		IncludeText:      e.items[0].enabled,
		IncludeMetadata:  e.items[1].enabled,
		IncludeTool:      e.items[2].enabled,
		IncludeReasoning: e.items[3].enabled,
	}
}

func (e ExportOverlay) View() string {
	const boxWidth = 60

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(e.theme.AccentBg).
		Width(boxWidth).
		Padding(1, 2)

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(e.theme.TextNormal)
	hintStyle := lipgloss.NewStyle().Foreground(e.theme.TextMuted).Italic(true)
	normalStyle := lipgloss.NewStyle().Foreground(e.theme.TextNormal)
	selectedStyle := lipgloss.NewStyle().Foreground(e.theme.Accent).Bold(true)
	checkedStyle := lipgloss.NewStyle().Foreground(e.theme.BorderFocused)

	lines := []string{
		titleStyle.Render("Export Session as Markdown"),
		"",
		hintStyle.Render("Select what to include:"),
		"",
	}

	for i, item := range e.items {
		check := "[ ]"
		if item.enabled {
			check = checkedStyle.Render("[✓]")
		}
		row := check + "  " + item.label
		if i == e.cursor {
			lines = append(lines, selectedStyle.Render("▶ "+row))
		} else {
			lines = append(lines, normalStyle.Render("  "+row))
		}
	}

	lines = append(lines,
		"",
		hintStyle.Render("[↑↓/jk] navigate  [Space] toggle  [Enter] export  [Esc] cancel"),
	)

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	box := borderStyle.Render(content)

	return lipgloss.Place(
		e.width, e.height,
		lipgloss.Center, lipgloss.Center,
		box,
	)
}
