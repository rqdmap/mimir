package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type InputTarget int

const (
	InputTargetNone InputTarget = iota
	InputTargetIdea
	InputTargetTag
)

type InputSavedIdeaMsg struct {
	IdeaID    string
	SessionID string
	Content   string
}

type InputTagsUpdatedMsg struct {
	SessionID  string
	AddTags    []string
	RemoveTags []string
}

type InputCancelledMsg struct{}

type InputMode struct {
	target       InputTarget
	ideaID       string
	sessionID    string
	sessionTitle string

	textinput textinput.Model

	workingTags  []string
	originalTags []string
	tagListFocus bool
	tagListIdx   int

	active bool
	width  int
	height int
}

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

func (im *InputMode) ActivateIdea(sessionID, sessionTitle string) {
	im.target = InputTargetIdea
	im.ideaID = ""
	im.sessionID = sessionID
	im.sessionTitle = sessionTitle
	im.active = true

	ti := textinput.New()
	ti.Placeholder = "Capture idea..."
	ti.Width = 56
	ti.Focus()
	im.textinput = ti
}

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

func (im *InputMode) ActivateTag(sessionID, sessionTitle string, existingTags []string) {
	im.target = InputTargetTag
	im.ideaID = ""
	im.sessionID = sessionID
	im.sessionTitle = sessionTitle
	im.active = true

	im.originalTags = make([]string, len(existingTags))
	copy(im.originalTags, existingTags)
	im.workingTags = make([]string, len(existingTags))
	copy(im.workingTags, existingTags)
	im.tagListFocus = false
	im.tagListIdx = 0

	ti := textinput.New()
	ti.Placeholder = "Add tag..."
	ti.Width = 56
	ti.Focus()
	im.textinput = ti
}

func (im InputMode) IsActive() bool {
	return im.active
}

func (im *InputMode) Deactivate() {
	im.active = false
	im.target = InputTargetNone
	im.ideaID = ""
	im.sessionID = ""
	im.sessionTitle = ""
	im.workingTags = nil
	im.originalTags = nil
	im.tagListFocus = false
	im.tagListIdx = 0
}

func (im InputMode) Init() tea.Cmd {
	return nil
}

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
			key := msg.String()
			switch key {
			case "esc":
				addTags, removeTags := diffTagSlices(im.originalTags, im.workingTags)
				sessionID := im.sessionID
				im.Deactivate()
				if len(addTags) == 0 && len(removeTags) == 0 {
					return im, func() tea.Msg { return InputCancelledMsg{} }
				}
				return im, func() tea.Msg {
					return InputTagsUpdatedMsg{SessionID: sessionID, AddTags: addTags, RemoveTags: removeTags}
				}

			case "enter":
				if im.tagListFocus {
					im.tagListFocus = false
					im.textinput.Focus()
				} else {
					raw := strings.TrimSpace(im.textinput.Value())
					if raw != "" && !containsTag(im.workingTags, raw) {
						im.workingTags = append(im.workingTags, raw)
					}
					ti := textinput.New()
					ti.Placeholder = "Add tag..."
					ti.Width = 56
					ti.Focus()
					im.textinput = ti
				}

			case "up":
				if im.tagListFocus {
					if im.tagListIdx > 0 {
						im.tagListIdx--
					}
				} else if len(im.workingTags) > 0 {
					im.tagListFocus = true
					im.tagListIdx = len(im.workingTags) - 1
					im.textinput.Blur()
				}

			case "k":
				if im.tagListFocus {
					if im.tagListIdx > 0 {
						im.tagListIdx--
					}
				} else {
					var cmd tea.Cmd
					im.textinput, cmd = im.textinput.Update(msg)
					return im, cmd
				}

			case "down":
				if im.tagListFocus {
					if im.tagListIdx < len(im.workingTags)-1 {
						im.tagListIdx++
					} else {
						im.tagListFocus = false
						im.textinput.Focus()
					}
				}

			case "j":
				if im.tagListFocus {
					if im.tagListIdx < len(im.workingTags)-1 {
						im.tagListIdx++
					} else {
						im.tagListFocus = false
						im.textinput.Focus()
					}
				} else {
					var cmd tea.Cmd
					im.textinput, cmd = im.textinput.Update(msg)
					return im, cmd
				}

			case "d", "x":
				if im.tagListFocus {
					im.deleteSelectedTag()
				} else {
					var cmd tea.Cmd
					im.textinput, cmd = im.textinput.Update(msg)
					return im, cmd
				}

			case "backspace", "delete":
				if im.tagListFocus {
					im.deleteSelectedTag()
				} else {
					var cmd tea.Cmd
					im.textinput, cmd = im.textinput.Update(msg)
					return im, cmd
				}

			default:
				if !im.tagListFocus {
					var cmd tea.Cmd
					im.textinput, cmd = im.textinput.Update(msg)
					return im, cmd
				}
			}
		}
	}

	return im, nil
}

func (im *InputMode) deleteSelectedTag() {
	if im.tagListIdx < len(im.workingTags) {
		im.workingTags = append(im.workingTags[:im.tagListIdx], im.workingTags[im.tagListIdx+1:]...)
		if im.tagListIdx >= len(im.workingTags) && im.tagListIdx > 0 {
			im.tagListIdx--
		}
		if len(im.workingTags) == 0 {
			im.tagListFocus = false
			im.textinput.Focus()
		}
	}
}

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

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)

	var content string

	switch im.target {
	case InputTargetIdea:
		var title, prompt string
		if im.ideaID != "" {
			title = titleStyle.Render("Edit Idea")
			prompt = labelStyle.Render("Edit idea content:")
		} else if im.sessionID != "" {
			title = titleStyle.Render("Capture Idea")
			sessionLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Bold(true).Render(im.sessionTitle)
			prompt = labelStyle.Render("Linked to: ") + sessionLabel
		} else {
			title = titleStyle.Render("Capture Idea")
			prompt = labelStyle.Render("Standalone idea (no session link):")
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
		sessionLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("63")).Bold(true).Render(im.sessionTitle)

		selectedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Bold(true)
		normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))

		var tagLines []string
		if len(im.workingTags) == 0 {
			tagLines = append(tagLines, hintStyle.Render("  (none)"))
		} else {
			for i, t := range im.workingTags {
				if im.tagListFocus && i == im.tagListIdx {
					tagLines = append(tagLines, selectedStyle.Render("▶ "+t))
				} else {
					tagLines = append(tagLines, normalStyle.Render("  "+t))
				}
			}
		}

		var hintText string
		if im.tagListFocus {
			hintText = "[j/k] navigate  [d/x] delete  [Enter] back  [Esc] done"
		} else {
			hintText = "[Enter] add tag  [↑] select tags  [Esc] done"
		}

		lines := []string{
			title,
			"",
			labelStyle.Render("Session: ") + sessionLabel,
			"",
			labelStyle.Render("Tags:"),
		}
		lines = append(lines, tagLines...)
		lines = append(lines,
			"",
			labelStyle.Render("New tag:"),
			im.textinput.View(),
			"",
			hintStyle.Render(hintText),
		)
		content = lipgloss.JoinVertical(lipgloss.Left, lines...)

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

func diffTagSlices(original, working []string) (addTags, removeTags []string) {
	origSet := make(map[string]bool, len(original))
	for _, t := range original {
		origSet[t] = true
	}
	workSet := make(map[string]bool, len(working))
	for _, t := range working {
		workSet[t] = true
	}
	for _, t := range working {
		if !origSet[t] {
			addTags = append(addTags, t)
		}
	}
	for _, t := range original {
		if !workSet[t] {
			removeTags = append(removeTags, t)
		}
	}
	return
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}
