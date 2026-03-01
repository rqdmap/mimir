package panes

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/local/oc-manager/internal/model"
)

// Messages that this pane emits for parent to handle
type AddTagMsg struct{ SessionID string }
type EditNoteMsg struct {
	SessionID   string
	CurrentNote string
}
type AddIdeaMsg struct{ SessionID string }
type OpenIdeaNotebookMsg struct{}

// MetadataPane shows session metadata: tags, note, idea count, stats
type MetadataPane struct {
	meta         model.SessionMeta
	ideaCount    int
	messageCount int
	focused      bool
	width        int
	height       int
	hasSession   bool // false = no session selected yet
}

func NewMetadataPane(width, height int) MetadataPane {
	return MetadataPane{
		width:  width,
		height: height,
	}
}

func (m *MetadataPane) SetSessionMeta(meta model.SessionMeta) {
	m.meta = meta
	m.hasSession = true
}

func (m *MetadataPane) SetIdeaCount(n int) {
	m.ideaCount = n
}

func (m *MetadataPane) SetMessageCount(n int) {
	m.messageCount = n
}

func (m *MetadataPane) SetFocused(focused bool) {
	m.focused = focused
}

func (m *MetadataPane) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *MetadataPane) ClearSession() {
	m.hasSession = false
	m.meta = model.SessionMeta{}
	m.ideaCount = 0
	m.messageCount = 0
}

func (m MetadataPane) Init() tea.Cmd { return nil }

func (m MetadataPane) Update(msg tea.Msg) (MetadataPane, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !m.hasSession {
			return m, nil
		}
		switch msg.String() {
		case "t":
			return m, func() tea.Msg { return AddTagMsg{SessionID: m.meta.SessionID} }
		case "n":
			return m, func() tea.Msg { return EditNoteMsg{SessionID: m.meta.SessionID, CurrentNote: m.meta.Note} }
		case "i":
			return m, func() tea.Msg { return AddIdeaMsg{SessionID: m.meta.SessionID} }
		case "I":
			return m, func() tea.Msg { return OpenIdeaNotebookMsg{} }
		}
	}
	return m, nil
}

func (m MetadataPane) View() string {
	borderColor := lipgloss.Color("240") // gray
	if m.focused {
		borderColor = lipgloss.Color("57") // purple
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(m.width-2).
		Height(m.height-2).
		Padding(0, 1)

	if !m.hasSession {
		return style.Align(lipgloss.Center, lipgloss.Center).Render("Select a session\nto view details.")
	}

	// Title
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")).Background(lipgloss.Color("235")).Padding(0, 1)
	title := titleStyle.Render("Metadata")

	// Sections
	// Tags
	tagHeader := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250")).Render("Tags")
	var tagsView string
	if len(m.meta.Tags) == 0 {
		tagsView = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("No tags yet.\n[t] to add one")
	} else {
		var renderedTags []string
		dot := lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Render("●")
		for _, t := range m.meta.Tags {
			renderedTags = append(renderedTags, fmt.Sprintf("%s %s", dot, t))
		}
		tagsView = lipgloss.NewStyle().Width(m.width - 4).Render(strings.Join(renderedTags, "  "))
		tagsView += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("[t] add tag")
	}

	// Notes
	noteHeader := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250")).Render("Notes")
	var noteView string
	if m.meta.Note == "" {
		noteView = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("No notes yet.\nPress n to add.")
	} else {
		// Truncate note if too long for preview
		preview := m.meta.Note
		if len(preview) > 100 {
			preview = preview[:97] + "..."
		}
		noteView = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("246")).Render(fmt.Sprintf("%q", preview))
		noteView += "\n" + lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("[n] edit")
	}

	// Ideas
	ideaHeader := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("250")).Render("Ideas")
	var ideaView string
	if m.ideaCount == 0 {
		ideaView = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("No ideas.\n[i] to capture")
	} else {
		ideaView = fmt.Sprintf("%d captured\n", m.ideaCount)
		ideaView += lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("[i] add  [I] view all")
	}

	// Stats
	statsHeader := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("─── Stats ───")
	statsView := fmt.Sprintf("Messages: %d", m.messageCount)

	// Combine
	content := lipgloss.JoinVertical(lipgloss.Left,
		"\n",
		tagHeader,
		tagsView,
		"\n",
		noteHeader,
		noteView,
		"\n",
		ideaHeader,
		ideaView,
		"\n",
		statsHeader,
		statsView,
	)

	return style.Render(lipgloss.JoinVertical(lipgloss.Center, title, content))
}
