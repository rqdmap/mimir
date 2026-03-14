package panes

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/local/oc-manager/internal/model"
)

// MetadataPane shows session metadata: tags, session ideas, stats
type MetadataPane struct {
	meta         model.SessionMeta
	messageCount int
	focused      bool
	width        int
	height       int
	hasSession   bool // false = no session selected yet
	sessionIdeas []model.Idea
	ideaMode     bool
	selectedIdea *model.Idea
	theme        Theme
}

func NewMetadataPane(width, height int, theme Theme) MetadataPane {
	return MetadataPane{
		width:  width,
		height: height,
		theme:  theme,
	}
}

func (m *MetadataPane) SetSessionMeta(meta model.SessionMeta) {
	m.meta = meta
	m.hasSession = true
}

func (m *MetadataPane) SetSessionIdeas(ideas []model.Idea) {
	m.sessionIdeas = ideas
}

func (m *MetadataPane) SetIdeaMeta(idea model.Idea, sessionTitle string) {
	m.ideaMode = true
	m.selectedIdea = &idea
}

func (m *MetadataPane) ClearIdea() {
	m.ideaMode = false
	m.selectedIdea = nil
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
	m.messageCount = 0
}

func (m MetadataPane) Init() tea.Cmd { return nil }

func (m MetadataPane) Update(msg tea.Msg) (MetadataPane, tea.Cmd) {
	return m, nil
}

func (m MetadataPane) View() string {
	borderColor := m.theme.BorderUnfocused
	if m.focused {
		borderColor = m.theme.BorderFocused
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(m.width-2).
		Height(m.height-2).
		Padding(0, 1)

	if m.ideaMode && m.selectedIdea != nil && m.selectedIdea.SourceSessionID == "" {
		return style.Align(lipgloss.Center, lipgloss.Center).Render("No linked session")
	}
	if !m.hasSession {
		return style.Align(lipgloss.Center, lipgloss.Center).Render("Select a session\nto view details.")
	}

	// Title
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(m.theme.TextNormal).Background(m.theme.AccentBg).Padding(0, 1)
	title := titleStyle.Render("Metadata")

	// Sections
	// Tags
	tagHeader := lipgloss.NewStyle().Bold(true).Foreground(m.theme.TextNormal).Render("Tags")
	var tagsView string
	if len(m.meta.Tags) == 0 {
		tagsView = lipgloss.NewStyle().Foreground(m.theme.TextMuted).Render("No tags yet.")
	} else {
		var renderedTags []string
		dot := lipgloss.NewStyle().Foreground(m.theme.Accent).Render("●")
		for _, t := range m.meta.Tags {
			renderedTags = append(renderedTags, fmt.Sprintf("%s %s", dot, t))
		}
		tagsView = lipgloss.NewStyle().Width(m.width - 4).Render(strings.Join(renderedTags, "  "))
	}

	// Session Ideas
	ideasHeader := lipgloss.NewStyle().Bold(true).Foreground(m.theme.TextNormal).Render("Session Ideas")
	var ideasView string
	if len(m.sessionIdeas) == 0 {
		ideasView = lipgloss.NewStyle().Foreground(m.theme.TextMuted).Render("No ideas yet.")
	} else {
		var ideaLines []string
		for _, idea := range m.sessionIdeas {
			content := idea.Content
			if len(content) > 40 {
				content = content[:40] + "..."
			}
			ideaLines = append(ideaLines, "• "+content)
		}
		ideasView = strings.Join(ideaLines, "\n")
	}

	// Selected Idea (if in idea mode)
	var selectedIdeaSection string
	if m.ideaMode && m.selectedIdea != nil {
		sidHeader := lipgloss.NewStyle().Bold(true).Foreground(m.theme.TextNormal).Render("Selected Idea")
		content := m.selectedIdea.Content
		if len(content) > 200 {
			content = content[:200] + "..."
		}
		sidContent := lipgloss.NewStyle().Italic(true).Foreground(m.theme.TextMuted).Render(fmt.Sprintf("%q", content))

		var sourceStr string
		if m.selectedIdea.SourceSessionID != "" {
			sourceStr = "Session: " + m.selectedIdea.SourceSessionID[:8] + "..."
		} else {
			sourceStr = "(no linked session)"
		}
		sidSource := lipgloss.NewStyle().Foreground(m.theme.TextMuted).Render(sourceStr)
		sidTime := lipgloss.NewStyle().Foreground(m.theme.TextMuted).Render(time.UnixMilli(m.selectedIdea.TimeCreated).Format("Jan 02, 2006 15:04"))

		selectedIdeaSection = "\n" + sidHeader + "\n" + sidContent + "\n" + sidSource + "\n" + sidTime
	}

	// Stats
	statsHeader := lipgloss.NewStyle().Foreground(m.theme.TextMuted).Render("─── Stats ───")
	statsView := fmt.Sprintf("Messages: %d", m.messageCount)

	// Combine
	content := lipgloss.JoinVertical(lipgloss.Left,
		"\n",
		tagHeader,
		tagsView,
		"\n",
		ideasHeader,
		ideasView,
		selectedIdeaSection,
		"\n",
		statsHeader,
		statsView,
	)

	return style.Render(lipgloss.JoinVertical(lipgloss.Center, title, content))
}
