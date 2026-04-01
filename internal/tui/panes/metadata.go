package panes

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/local/oc-manager/internal/model"
)

// MetadataPane shows session metadata: tags, usage stats
type MetadataPane struct {
	meta         model.SessionMeta
	messageCount int
	focused      bool
	width        int
	height       int
	hasSession   bool // false = no session selected yet
	isSubAgent   bool
	theme        Theme
	usage        model.SessionUsage
	hasUsage     bool
	sessionTitle string
}

func NewMetadataPane(width, height int, theme Theme) MetadataPane {
	return MetadataPane{
		width:  width,
		height: height,
		theme:  theme,
	}
}

func (m *MetadataPane) SetSessionMeta(meta model.SessionMeta, isSubAgent bool) {
	m.meta = meta
	m.hasSession = true
	m.isSubAgent = isSubAgent
}

func (m *MetadataPane) SetSessionTitle(title string) {
	m.sessionTitle = title
}

func (m *MetadataPane) SetMessageCount(n int) {
	m.messageCount = n
}

func (m *MetadataPane) SetUsage(u model.SessionUsage) {
	m.usage = u
	m.hasUsage = true
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
	m.usage = model.SessionUsage{}
	m.hasUsage = false
	m.sessionTitle = ""
}

func (m MetadataPane) Init() tea.Cmd { return nil }

func (m MetadataPane) Update(msg tea.Msg) (MetadataPane, tea.Cmd) {
	return m, nil
}

func fmtTokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%dK", n/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
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

	if !m.hasSession {
		return style.Align(lipgloss.Center, lipgloss.Center).Render("Select a session\nto view details.")
	}

	// Title
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(m.theme.TextNormal).Background(m.theme.AccentBg).Padding(0, 1)
	title := titleStyle.Render("Metadata")

	// Sections
	innerW := m.width - 4
	if innerW < 1 {
		innerW = 1
	}

	sessHeader := lipgloss.NewStyle().Foreground(m.theme.TextMuted).Render("─── Session ───")
	wrapStyle := lipgloss.NewStyle().Width(innerW)
	sessTitle := wrapStyle.Foreground(m.theme.TextNormal).Render(m.sessionTitle)
	sessID := wrapStyle.Foreground(m.theme.TextMuted).Render(m.meta.SessionID)

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

	// Usage section
	usageHeader := lipgloss.NewStyle().Foreground(m.theme.TextMuted).Render("─── Usage ───")
	var usageBody string
	if !m.hasSession {
	} else if !m.hasUsage {
		usageBody = lipgloss.NewStyle().Foreground(m.theme.TextMuted).Italic(true).Render("Loading...")
	} else {
		reqsLabel := "Human"
		if m.isSubAgent {
			reqsLabel = "SubAgent"
		}
		reqsLine := fmt.Sprintf("Reqs    %8s  (%s)", fmt.Sprintf("%d", m.usage.UserTurns), reqsLabel)
		inputLine := fmt.Sprintf("Input   %8s", fmtTokens(m.usage.InputTokens+m.usage.CacheRead+m.usage.CacheWrite))
		outputLine := fmt.Sprintf("Output  %8s", fmtTokens(m.usage.OutputTokens))
		var cacheLine string
		if m.usage.CacheRead > 0 {
			cacheLine = fmt.Sprintf("Cache   %8s  %.0f%%", fmtTokens(m.usage.CacheRead), m.usage.CachePercent)
		} else {
			cacheLine = fmt.Sprintf("Cache   %8s", fmtTokens(m.usage.CacheRead))
		}
		usageBody = strings.Join([]string{reqsLine, inputLine, outputLine, cacheLine}, "\n")
	}

	// Models section (only when hasUsage and models non-empty)
	var modelsSection string
	if m.hasSession && m.hasUsage && len(m.usage.Models) > 0 {
		modelsHeader := lipgloss.NewStyle().Foreground(m.theme.TextMuted).Render("─── Models ───")
		var modelLines []string
		maxShow := 3
		for i, mdl := range m.usage.Models {
			if i >= maxShow {
				modelLines = append(modelLines, lipgloss.NewStyle().Foreground(m.theme.TextMuted).Render(fmt.Sprintf("+%d more", len(m.usage.Models)-maxShow)))
				break
			}
			modelLines = append(modelLines, lipgloss.NewStyle().Foreground(m.theme.TextNormal).Render(mdl))
		}
		modelsSection = "\n" + modelsHeader + "\n" + strings.Join(modelLines, "\n")
	}

	// Combine
	content := lipgloss.JoinVertical(lipgloss.Left,
		"\n",
		sessHeader,
		sessTitle,
		sessID,
		"\n",
		tagHeader,
		tagsView,
		"\n",
		usageHeader,
		usageBody,
		modelsSection,
	)

	return style.Render(lipgloss.JoinVertical(lipgloss.Center, title, content))
}
