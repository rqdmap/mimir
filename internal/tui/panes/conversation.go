package panes

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/local/oc-manager/internal/model"
)

// ConvRendererReadyMsg is delivered when the glamour renderer is built asynchronously.
type ConvRendererReadyMsg struct {
	Renderer *glamour.TermRenderer
	Width    int
}

// AsyncConvRenderMsg is delivered when background markdown rendering completes.
// SessionID guards against stale renders (ignored if current session changed).
type AsyncConvRenderMsg struct {
	SessionID string
	Content   string
}

func newConvRendererCmd(width int, theme Theme) tea.Cmd {
	return func() tea.Msg {
		r, err := glamour.NewTermRenderer(
			theme.GlamourOption(),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			r, _ = glamour.NewTermRenderer(
				glamour.WithStylePath("dark"),
				glamour.WithWordWrap(width),
			)
		}
		return ConvRendererReadyMsg{Renderer: r, Width: width}
	}
}

// ConversationPane is the center panel displaying a session's full chat history.
type ConversationPane struct {
	viewport         viewport.Model
	messages         []model.Message
	focused          bool
	width            int
	height           int
	ready            bool
	ideaMode         bool
	renderer         *glamour.TermRenderer
	rendererWidth    int
	currentSessionID string
	theme            Theme

	rawLines   []string
	plainLines []string

	convSearchMode    bool
	convSearchQuery   string
	convSearchMatches []int
	convSearchIdx     int
}

func NewConversationPane(width, height int, theme Theme) ConversationPane {
	vp := viewport.New(width-2, height-4)
	vp.SetContent("Select a session from the list to view the conversation.")
	return ConversationPane{
		viewport: vp,
		messages: nil,
		focused:  false,
		width:    width,
		height:   height,
		ready:    true,
		theme:    theme,
	}
}

// SetMessages updates the messages displayed in this pane.
func (c *ConversationPane) SetMessages(messages []model.Message, sessionID string) tea.Cmd {
	c.messages = messages
	c.currentSessionID = sessionID
	c.clearConvSearch()
	if len(messages) == 0 {
		c.viewport.SetContent("Select a session from the list to view the conversation.")
		c.viewport.GotoTop()
		return nil
	}
	c.viewport.SetContent("Rendering conversation...")
	c.viewport.GotoTop()
	renderer := c.renderer
	return func() tea.Msg {
		content := renderContentStandalone(messages, renderer)
		return AsyncConvRenderMsg{SessionID: sessionID, Content: content}
	}
}

func (c *ConversationPane) clearConvSearch() {
	c.convSearchMode = false
	c.convSearchQuery = ""
	c.convSearchMatches = nil
	c.convSearchIdx = 0
	c.rawLines = nil
	c.plainLines = nil
}

func (c ConversationPane) SearchMode() bool      { return c.convSearchMode }
func (c ConversationPane) SearchQuery() string   { return c.convSearchQuery }
func (c ConversationPane) SearchMatchCount() int { return len(c.convSearchMatches) }
func (c ConversationPane) SearchMatchIdx() int   { return c.convSearchIdx }

// SetFocused controls focus state (affects border styling).
func (c *ConversationPane) SetFocused(focused bool) {
	c.focused = focused
}

// SetSize updates the pane dimensions.
func (c *ConversationPane) SetSize(width, height int) tea.Cmd {
	c.width = width
	c.height = height
	c.viewport.Width = width - 2
	c.viewport.Height = height - 4
	inner := width - 2 - 4
	if inner <= 0 {
		inner = 80
	}
	if inner != c.rendererWidth {
		return newConvRendererCmd(inner, c.theme)
	}
	return nil
}

// SetIdeaContent sets idea mode and renders markdown content to the viewport.
func (c *ConversationPane) SetIdeaContent(content string) {
	c.ideaMode = true
	rendered := renderMarkdownCached(c.renderer, content)
	c.viewport.SetContent(rendered)
	c.viewport.GotoTop()
}

// ClearIdeaContent clears idea mode and empties the viewport.
func (c *ConversationPane) ClearIdeaContent() {
	c.ideaMode = false
	c.viewport.SetContent("")
}

// Init satisfies tea.Model.
func (c ConversationPane) Init() tea.Cmd { return nil }

// Update handles keyboard input and window resize.
func (c ConversationPane) Update(msg tea.Msg) (ConversationPane, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case ConvRendererReadyMsg:
		c.renderer = msg.Renderer
		c.rendererWidth = msg.Width
		if len(c.messages) == 0 {
			// No messages loaded yet — nothing to re-render.
			return c, nil
		}
		// Re-render asynchronously with the new renderer to avoid blocking the
		// main goroutine (glamour rendering of long sessions takes 100ms–5s).
		messages := c.messages
		sessionID := c.currentSessionID
		renderer := msg.Renderer
		return c, func() tea.Msg {
			content := renderContentStandalone(messages, renderer)
			return AsyncConvRenderMsg{SessionID: sessionID, Content: content}
		}
	case AsyncConvRenderMsg:
		if msg.SessionID == c.currentSessionID {
			c.rawLines = strings.Split(msg.Content, "\n")
			c.plainLines = make([]string, len(c.rawLines))
			for i, line := range c.rawLines {
				c.plainLines[i] = stripANSI(line)
			}
			if c.convSearchQuery != "" {
				c.updateConvSearchHighlights()
			} else {
				c.viewport.SetContent(msg.Content)
				c.viewport.GotoTop()
			}
		}
		return c, nil
	case tea.KeyMsg:
		if !c.focused {
			return c, nil
		}
		if c.convSearchMode {
			switch msg.String() {
			case "esc", "enter":
				c.convSearchMode = false
			case "backspace":
				if len(c.convSearchQuery) > 0 {
					c.convSearchQuery = c.convSearchQuery[:len(c.convSearchQuery)-1]
					c.updateConvSearchHighlights()
				}
			default:
				if k := msg.String(); len(k) == 1 {
					c.convSearchQuery += k
					c.updateConvSearchHighlights()
				}
			}
			return c, nil
		}
		switch msg.String() {
		case "/":
			if len(c.rawLines) > 0 {
				c.convSearchMode = true
				c.convSearchQuery = ""
				c.convSearchMatches = nil
				c.convSearchIdx = 0
				c.viewport.SetContent(strings.Join(c.rawLines, "\n"))
			}
		case "n":
			if len(c.convSearchMatches) > 0 {
				c.convSearchIdx = (c.convSearchIdx + 1) % len(c.convSearchMatches)
				c.updateConvSearchHighlights()
			}
		case "N":
			if len(c.convSearchMatches) > 0 {
				n := len(c.convSearchMatches)
				c.convSearchIdx = (c.convSearchIdx - 1 + n) % n
				c.updateConvSearchHighlights()
			}
		case "j", "down":
			c.viewport.LineDown(1)
		case "k", "up":
			c.viewport.LineUp(1)
		case "ctrl+d":
			c.viewport.HalfViewDown()
		case "ctrl+u":
			c.viewport.HalfViewUp()
		case "g":
			c.viewport.GotoTop()
		case "G":
			c.viewport.GotoBottom()
		}
	case tea.WindowSizeMsg:
		cmd := c.SetSize(msg.Width, msg.Height)
		return c, cmd
	}

	c.viewport, cmd = c.viewport.Update(msg)
	return c, cmd
}

// View renders the pane including border and title.
func (c ConversationPane) View() string {
	borderColor := c.theme.BorderUnfocused
	if c.focused {
		borderColor = c.theme.BorderFocused
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(borderColor).
		Render("Conversation")

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(c.width - 2).
		Height(c.height - 2)

	inner := title + "\n" + c.viewport.View()
	return style.Render(inner)
}

// renderMarkdownCached renders text as markdown using a pre-built glamour renderer.
func renderMarkdownCached(r *glamour.TermRenderer, text string) (result string) {
	if r == nil {
		return text
	}
	defer func() {
		if rec := recover(); rec != nil {
			result = text
		}
	}()
	out, err := r.Render(text)
	if err != nil {
		return text
	}
	return out
}

// ansiEscape strips ANSI terminal escape codes from s.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

// renderRoleHeader renders a styled header line for a message role.
func renderRoleHeader(role string) string {
	label := strings.ToUpper(role)
	tag := fmt.Sprintf("[%s]", label)
	// 50-char separator after the tag
	sep := strings.Repeat("─", 40)
	return lipgloss.NewStyle().Bold(true).Render(tag) + "  " + sep
}

// renderContent builds the full conversation string from all messages.
func (c *ConversationPane) renderContent() string {
	return renderContentStandalone(c.messages, c.renderer)
}

// renderContentStandalone is the goroutine-safe version of renderContent.
// All TUI state is passed as explicit arguments.
func renderContentStandalone(messages []model.Message, renderer *glamour.TermRenderer) string {
	if len(messages) == 0 {
		return "Select a session from the list to view the conversation."
	}

	var sb strings.Builder
	hasRenderable := false

	for _, msg := range messages {
		// role header
		sb.WriteString(renderRoleHeader(msg.Role))
		sb.WriteString("\n")

		for _, part := range msg.Parts {
			switch part.Type {
			case model.PartTypeText:
				// Use glamour with recovery; fall back to plain text
				rendered := renderMarkdownCached(renderer, part.Text)
				sb.WriteString(rendered)
				hasRenderable = true

			case model.PartTypeTool:
				status := part.ToolStatus
				if status == "" {
					status = "running"
				}
				statusStyle := lipgloss.NewStyle().Faint(true)
				switch status {
				case "complete":
					statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
				case "error":
					statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
				case "running":
					statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
				}
				toolNameStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
				line := fmt.Sprintf("[⚙ %s] %s",
					toolNameStyle.Render(part.ToolName),
					statusStyle.Render("── "+status+" ──"),
				)
				sb.WriteString(line)
				sb.WriteString("\n")
				if part.ToolInput != "" {
					inp := part.ToolInput
					if len(inp) > 500 {
						inp = inp[:500] + "..."
					}
					sb.WriteString(lipgloss.NewStyle().Faint(true).Render("  ↳ " + inp))
					sb.WriteString("\n")
				}
				if part.ToolOutput != "" {
					out := part.ToolOutput
					if len(out) > 4000 {
						out = out[:4000]
					}
					out = stripANSI(out)
					if len(out) > 2000 {
						out = out[:2000] + "\n... [truncated]"
					}
					sb.WriteString(lipgloss.NewStyle().Faint(true).Render(out))
					sb.WriteString("\n")
				}
			case model.PartTypeReasoning:
				line := "[🧠 Reasoning] ── (hidden) ──"
				sb.WriteString(lipgloss.NewStyle().Faint(true).Italic(true).Render(line))
				sb.WriteString("\n")

			case model.PartTypeFile:
				fname := part.Filename
				if fname == "" {
					fname = "attachment"
				}
				line := fmt.Sprintf("[📄 %s] ── [Image: %s] ──", fname, fname)
				sb.WriteString(lipgloss.NewStyle().Faint(true).Render(line))
				sb.WriteString("\n")

			case model.PartTypePatch:
				files := part.Text
				if files == "" {
					files = "(no files)"
				}
				line := fmt.Sprintf("[📦 Changes] ── %s ──", files)
				sb.WriteString(lipgloss.NewStyle().Faint(true).Render(line))
				sb.WriteString("\n")

			case model.PartTypeUnknown:
				// skip silently

			default:
				// skip unknown types silently
			}
		}

		sb.WriteString("\n")
	}

	content := sb.String()
	if !hasRenderable {
		content += "\nThis session contains only tool calls with no readable text."
	}
	return content
}

var (
	convSearchMatchStyle   = lipgloss.NewStyle().Background(lipgloss.Color("226")).Foreground(lipgloss.Color("0"))
	convSearchCurrentStyle = lipgloss.NewStyle().Background(lipgloss.Color("208")).Foreground(lipgloss.Color("0")).Bold(true)
)

func (c *ConversationPane) updateConvSearchHighlights() {
	q := strings.ToLower(c.convSearchQuery)

	c.convSearchMatches = nil
	if q != "" {
		for i, plain := range c.plainLines {
			if strings.Contains(strings.ToLower(plain), q) {
				c.convSearchMatches = append(c.convSearchMatches, i)
			}
		}
	}
	if c.convSearchIdx >= len(c.convSearchMatches) {
		c.convSearchIdx = 0
	}

	if q == "" || len(c.rawLines) == 0 {
		c.viewport.SetContent(strings.Join(c.rawLines, "\n"))
		return
	}

	matchSet := make(map[int]bool, len(c.convSearchMatches))
	for _, m := range c.convSearchMatches {
		matchSet[m] = true
	}
	currentLine := -1
	if len(c.convSearchMatches) > 0 {
		currentLine = c.convSearchMatches[c.convSearchIdx]
	}

	var sb strings.Builder
	for i, plain := range c.plainLines {
		if matchSet[i] {
			sb.WriteString(injectSearchHighlight(plain, c.convSearchQuery, i == currentLine))
		} else {
			sb.WriteString(c.rawLines[i])
		}
		if i < len(c.plainLines)-1 {
			sb.WriteString("\n")
		}
	}

	c.viewport.SetContent(sb.String())
	if currentLine >= 0 {
		c.viewport.SetYOffset(currentLine)
	}
}

func injectSearchHighlight(plain, query string, isCurrent bool) string {
	style := convSearchMatchStyle
	if isCurrent {
		style = convSearchCurrentStyle
	}
	lowerPlain := strings.ToLower(plain)
	lowerQuery := strings.ToLower(query)
	var sb strings.Builder
	pos := 0
	for pos < len(plain) {
		idx := strings.Index(lowerPlain[pos:], lowerQuery)
		if idx < 0 {
			sb.WriteString(plain[pos:])
			break
		}
		abs := pos + idx
		sb.WriteString(plain[pos:abs])
		sb.WriteString(style.Render(plain[abs : abs+len(query)]))
		pos = abs + len(query)
	}
	return sb.String()
}

// ScrollHalfDown scrolls the conversation viewport half a page down.
// Safe to call even when the pane is not focused.
func (c *ConversationPane) ScrollHalfDown() {
	c.viewport.HalfViewDown()
}

// ScrollHalfUp scrolls the conversation viewport half a page up.
// Safe to call even when the pane is not focused.
func (c *ConversationPane) ScrollHalfUp() {
	c.viewport.HalfViewUp()
}

// ScrollLineDown scrolls the conversation viewport one line down.
// Safe to call even when the pane is not focused (for mouse scroll cross-pane).
func (c *ConversationPane) ScrollLineDown(n int) {
	c.viewport.LineDown(n)
}

// ScrollLineUp scrolls the conversation viewport one line up.
// Safe to call even when the pane is not focused (for mouse scroll cross-pane).
func (c *ConversationPane) ScrollLineUp(n int) {
	c.viewport.LineUp(n)
}
