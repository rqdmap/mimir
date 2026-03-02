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

func newConvRendererCmd(width int) tea.Cmd {
	return func() tea.Msg {
		r, _ := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(width),
		)
		return ConvRendererReadyMsg{Renderer: r, Width: width}
	}
}

// ConversationPane is the center panel displaying a session's full chat history.
type ConversationPane struct {
	viewport       viewport.Model
	messages       []model.Message
	focused        bool
	width          int
	height         int
	ready          bool
	ideaMode       bool
	renderer       *glamour.TermRenderer
	rendererWidth  int
}

func NewConversationPane(width, height int) ConversationPane {
	vp := viewport.New(width-2, height-4)
	vp.SetContent("Select a session from the list to view the conversation.")
	return ConversationPane{
		viewport:      vp,
		messages:      nil,
		focused:       false,
		width:         width,
		height:        height,
		ready:         true,
	}
}

// SetMessages updates the messages displayed in this pane.
func (c *ConversationPane) SetMessages(messages []model.Message) {
	c.messages = messages
	c.viewport.SetContent(c.renderContent())
	c.viewport.GotoTop()
}

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
		return newConvRendererCmd(inner)
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
		c.viewport.SetContent(c.renderContent())
		return c, nil
	case tea.KeyMsg:
		if !c.focused {
			return c, nil
		}
		switch msg.String() {
		case "j", "down":
			c.viewport.LineDown(1)
		case "k", "up":
			c.viewport.LineUp(1)
		case "ctrl+d":
			c.viewport.HalfViewDown()
		case "ctrl+u":
			c.viewport.HalfViewUp()
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
	borderColor := lipgloss.Color("240") // gray when unfocused
	if c.focused {
		borderColor = lipgloss.Color("#7D56F4") // purple when focused
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

// renderMarkdown renders text as markdown using glamour, falling back to plain text.
func renderMarkdown(text string, width int) (result string) {
	defer func() {
		if r := recover(); r != nil {
			result = text
		}
	}()
	if width <= 0 {
		width = 80
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return text
	}
	out, err := r.Render(text)
	if err != nil {
		return text
	}
	return out
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
	if len(c.messages) == 0 {
		return "Select a session from the list to view the conversation."
	}

	var sb strings.Builder
	hasRenderable := false

	for _, msg := range c.messages {
		// role header
		sb.WriteString(renderRoleHeader(msg.Role))
		sb.WriteString("\n")

		for _, part := range msg.Parts {
			switch part.Type {
			case model.PartTypeText:
				// Use glamour with recovery; fall back to plain text
				rendered := renderMarkdownCached(c.renderer, part.Text)
				sb.WriteString(rendered)
				hasRenderable = true

case model.PartTypeTool:
			status := part.ToolStatus
			if status == "" {
				status = "running"
			}
			line := fmt.Sprintf("[⚙ %s] ── %s ──", part.ToolName, status)
			sb.WriteString(lipgloss.NewStyle().Faint(true).Render(line))
			sb.WriteString("\n")
			if part.ToolOutput != "" {
				out := stripANSI(part.ToolOutput)
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
