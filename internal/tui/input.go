// Package tui provides the terminal user interface for oc-manager.
// input.go implements the overlay input mode system for idea capture and tag entry.
package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// InputTarget describes what kind of content we are capturing.
type InputTarget int

const (
	// InputTargetNone means no input is active.
	InputTargetNone InputTarget = iota
	// InputTargetIdea means we are capturing a single-line idea.
	InputTargetIdea
	// InputTargetTag means we are adding comma-separated tags.
	InputTargetTag
)

// InputSavedIdeaMsg is emitted when the user saves an idea.
type InputSavedIdeaMsg struct {
	IdeaID    string // non-empty when editing an existing idea
	SessionID string
	Content   string
}

// InputSavedTagMsg is emitted when the user saves tags.
// Tags are already parsed from comma-separated input.
type InputSavedTagMsg struct {
	SessionID string
	Tags      []string // parsed from comma-separated input
}

// InputCancelledMsg is emitted when the user cancels input (Esc).
type InputCancelledMsg struct{}

// InputMode manages overlay input fields for ideas and tags.
// It is designed to be held by the App model and activated/deactivated
// in response to messages from panes.
type InputMode struct {
	target    InputTarget
	ideaID    string // non-empty when editing an existing idea
	sessionID string

	textinput textinput.Model

	active bool
	width  int
	height int
}

// NewInputMode creates a new InputMode with the given terminal dimensions.
func NewInputMode(width, height int) InputMode {
	ti := textinput.New()
	ti.Width = 56

	return InputMode{
		target:    InputTargetNone,
		textinput: ti,
		width:     width,
		height:    height,
	}
}

// ActivateIdea sets up the single-line textinput for capturing a new idea
// linked to the given session and activates input mode.
func (im *InputMode) ActivateIdea(sessionID string) {
	im.target = InputTargetIdea
	im.ideaID = ""
	im.sessionID = sessionID
	im.active = true

	ti := textinput.New()
	ti.Placeholder = "Capture idea..."
	ti.Width = 56
	ti.Focus()
	im.textinput = ti
}

// ActivateIdeaEdit sets up the textinput pre-filled with existing idea content for editing.
func (im *InputMode) ActivateIdeaEdit(ideaID, content string) {
	im.target = InputTargetIdea
	im.ideaID = ideaID
	im.sessionID = ""
	im.active = true

	ti := textinput.New()
	ti.Placeholder = "Edit idea..."
	ti.Width = 56
	ti.SetValue(content)
	ti.CursorEnd()
	ti.Focus()
	im.textinput = ti
}

// ActivateTag sets up the single-line textinput for entering comma-separated
// tags and activates input mode.
func (im *InputMode) ActivateTag(sessionID string) {
	im.target = InputTargetTag
	im.ideaID = ""
	im.sessionID = sessionID
	im.active = true

	ti := textinput.New()
	ti.Placeholder = "work, ai, idea"
	ti.Width = 56
	ti.Focus()
	im.textinput = ti
}

// IsActive returns whether input mode is currently active.
func (im InputMode) IsActive() bool {
	return im.active
}

// Deactivate resets all input state and marks the mode inactive.
func (im *InputMode) Deactivate() {
	im.active = false
	im.target = InputTargetNone
	im.ideaID = ""
	im.sessionID = ""
}

// Init implements tea.Model. Returns nil because no initial commands are needed.
func (im InputMode) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model. Handles key events for the active input target.
//
// For textinput (idea/tag mode):
//   - enter: emit InputSavedIdeaMsg or InputSavedTagMsg and deactivate
//   - esc: emit InputCancelledMsg and deactivate
//   - all other keys: forwarded to the textinput component
func (im InputMode) Update(msg tea.Msg) (InputMode, tea.Cmd) {
	if !im.active {
		return im, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch im.target {
		case InputTargetIdea:
			switch msg.String() {
			case "enter":
				content := im.textinput.Value()
				ideaID := im.ideaID
				sessionID := im.sessionID
				im.Deactivate()
				return im, func() tea.Msg {
					return InputSavedIdeaMsg{IdeaID: ideaID, SessionID: sessionID, Content: content}
				}
			case "esc":
				im.Deactivate()
				return im, func() tea.Msg { return InputCancelledMsg{} }
			default:
				var cmd tea.Cmd
				im.textinput, cmd = im.textinput.Update(msg)
				return im, cmd
			}

		case InputTargetTag:
			switch msg.String() {
			case "enter":
				raw := im.textinput.Value()
				tags := parseCommaSeparatedTags(raw)
				sessionID := im.sessionID
				im.Deactivate()
				return im, func() tea.Msg {
					return InputSavedTagMsg{SessionID: sessionID, Tags: tags}
				}
			case "esc":
				im.Deactivate()
				return im, func() tea.Msg { return InputCancelledMsg{} }
			default:
				var cmd tea.Cmd
				im.textinput, cmd = im.textinput.Update(msg)
				return im, cmd
			}
		}
	}

	return im, nil
}

// View renders the input overlay centered within the terminal.
// Returns an empty string if not active.
//
// For idea/tag mode: single-line textinput with appropriate prompt and hints.
func (im InputMode) View() string {
	if !im.active {
		return ""
	}

	const boxWidth = 60

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(boxWidth).
		Padding(1, 2)

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("252"))

	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Italic(true)

	var content string

	switch im.target {
	case InputTargetIdea:
		var title, prompt string
		if im.ideaID != "" {
			title = titleStyle.Render("Edit Idea")
			prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Render("Edit idea content:")
		} else if im.sessionID != "" {
			title = titleStyle.Render("Capture Idea")
			prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Render("Capture idea (linked to current session):")
		} else {
			title = titleStyle.Render("Capture Idea")
			prompt = lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Render("Capture standalone idea (no session link):")
		}
		hint := hintStyle.Render("[Enter] save  [Esc] cancel")
		content = lipgloss.JoinVertical(lipgloss.Left,
			title,
			"",
			prompt,
			im.textinput.View(),
			"",
			hint,
		)

	case InputTargetTag:
		title := titleStyle.Render("Add Tags")
		prompt := lipgloss.NewStyle().
			Foreground(lipgloss.Color("250")).
			Render("Add tags (comma-separated, e.g. work,ai):")
		hint := hintStyle.Render("[Enter] save  [Esc] cancel")
		content = lipgloss.JoinVertical(lipgloss.Left,
			title,
			"",
			prompt,
			im.textinput.View(),
			"",
			hint,
		)

	default:
		return ""
	}

	box := borderStyle.Render(content)

	return lipgloss.Place(
		im.width, im.height,
		lipgloss.Center, lipgloss.Center,
		box,
	)
}

// parseCommaSeparatedTags splits a comma-separated string like "work, ai, idea"
// into a slice of trimmed, non-empty tag strings.
func parseCommaSeparatedTags(input string) []string {
	parts := strings.Split(input, ",")
	var tags []string
	for _, p := range parts {
		t := strings.TrimSpace(p)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}
