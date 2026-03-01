// Package tui provides the terminal user interface for oc-manager.
// input.go implements the overlay input mode system for note editing,
// idea capture, and tag entry.
package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// InputTarget describes what kind of content we are capturing.
type InputTarget int

const (
	// InputTargetNone means no input is active.
	InputTargetNone InputTarget = iota
	// InputTargetNote means we are editing a multi-line session note.
	InputTargetNote
	// InputTargetIdea means we are capturing a single-line idea.
	InputTargetIdea
	// InputTargetTag means we are adding comma-separated tags.
	InputTargetTag
)

// InputSavedNoteMsg is emitted when the user saves a note.
type InputSavedNoteMsg struct {
	SessionID string
	Note      string
}

// InputSavedIdeaMsg is emitted when the user saves an idea.
type InputSavedIdeaMsg struct {
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

// InputMode manages overlay input fields for notes, ideas, and tags.
// It is designed to be held by the App model and activated/deactivated
// in response to messages from panes.
type InputMode struct {
	target      InputTarget
	sessionID   string
	currentNote string // pre-fill value for note editing

	textarea  textarea.Model
	textinput textinput.Model

	active bool
	width  int
	height int
}

// NewInputMode creates a new InputMode with the given terminal dimensions.
func NewInputMode(width, height int) InputMode {
	ta := textarea.New()
	ta.SetWidth(56)
	ta.SetHeight(8)
	ta.ShowLineNumbers = false
	ta.CharLimit = 0 // no limit

	ti := textinput.New()
	ti.Width = 56

	return InputMode{
		target:    InputTargetNone,
		textarea:  ta,
		textinput: ti,
		width:     width,
		height:    height,
	}
}

// ActivateNote sets up the textarea pre-filled with the current note content
// and activates input mode targeting note editing.
func (im *InputMode) ActivateNote(sessionID, currentNote string) {
	im.target = InputTargetNote
	im.sessionID = sessionID
	im.currentNote = currentNote
	im.active = true

	ta := textarea.New()
	ta.SetWidth(56)
	ta.SetHeight(8)
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetValue(currentNote)
	ta.Focus()
	im.textarea = ta
}

// ActivateIdea sets up the single-line textinput for capturing a new idea
// linked to the given session and activates input mode.
func (im *InputMode) ActivateIdea(sessionID string) {
	im.target = InputTargetIdea
	im.sessionID = sessionID
	im.active = true

	ti := textinput.New()
	ti.Placeholder = "Capture idea..."
	ti.Width = 56
	ti.Focus()
	im.textinput = ti
}

// ActivateTag sets up the single-line textinput for entering comma-separated
// tags and activates input mode.
func (im *InputMode) ActivateTag(sessionID string) {
	im.target = InputTargetTag
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
	im.sessionID = ""
	im.currentNote = ""
}

// Init implements tea.Model. Returns nil because no initial commands are needed.
func (im InputMode) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model. Handles key events for the active input target.
//
// For textarea (note mode):
//   - ctrl+s or ctrl+enter: emit InputSavedNoteMsg and deactivate
//   - esc: emit InputCancelledMsg and deactivate
//   - all other keys: forwarded to the textarea component
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
		case InputTargetNote:
			switch msg.String() {
			case "ctrl+s", "ctrl+enter":
				note := im.textarea.Value()
				sessionID := im.sessionID
				im.Deactivate()
				return im, func() tea.Msg {
					return InputSavedNoteMsg{SessionID: sessionID, Note: note}
				}
			case "esc":
				im.Deactivate()
				return im, func() tea.Msg { return InputCancelledMsg{} }
			default:
				var cmd tea.Cmd
				im.textarea, cmd = im.textarea.Update(msg)
				return im, cmd
			}

		case InputTargetIdea:
			switch msg.String() {
			case "enter":
				content := im.textinput.Value()
				sessionID := im.sessionID
				im.Deactivate()
				return im, func() tea.Msg {
					return InputSavedIdeaMsg{SessionID: sessionID, Content: content}
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
// For note mode: multi-line textarea with title and save/cancel hints.
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
	case InputTargetNote:
		title := titleStyle.Render("Edit Note")
		hint := hintStyle.Render("[Ctrl+S] save  [Esc] cancel")
		content = lipgloss.JoinVertical(lipgloss.Left,
			title,
			"",
			im.textarea.View(),
			"",
			hint,
		)

	case InputTargetIdea:
		title := titleStyle.Render("Capture Idea")
		prompt := lipgloss.NewStyle().
			Foreground(lipgloss.Color("250")).
			Render("Capture idea (linked to current session):")
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
