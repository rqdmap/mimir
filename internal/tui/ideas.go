package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/local/oc-manager/internal/model"
)

// Messages
type ExitIdeasMsg struct{}
type EditIdeaMsg struct{ ID string }
type DeleteIdeaConfirmedMsg struct{ ID string }

type IdeasView struct {
	ideas        []model.Idea
	list         list.Model
	preview      viewport.Model
	width        int
	height       int
	confirmDel   bool
	deleteTarget string
	renderer     *glamour.TermRenderer
}

type IdeaItem struct {
	Idea model.Idea
}

func (i IdeaItem) Title() string {
	content := i.Idea.Content
	// Simple truncation for title if too long, though list handles this well usually
	if len(content) > 50 {
		return content[:50] + "..."
	}
	return content
}

func (i IdeaItem) Description() string {
	ts := time.UnixMilli(i.Idea.TimeCreated).Format("Jan 02, 2006 15:04")
	if i.Idea.SourceSessionID != "" {
		return fmt.Sprintf("%s • Session: %s", ts, i.Idea.SourceSessionID)
	}
	return ts
}

func (i IdeaItem) FilterValue() string { return i.Idea.Content }

func NewIdeasView(width, height int) IdeasView {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Idea Notebook"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.DisableQuitKeybindings()
	// Adjust list styles if needed
	l.Styles.Title = lipgloss.NewStyle().
		Background(lipgloss.Color("62")).
		Foreground(lipgloss.Color("230")).
		Padding(0, 1)

	vp := viewport.New(0, 0)

	r, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(0),
	)

	v := IdeasView{
		list:     l,
		preview:  vp,
		width:    width,
		height:   height,
		renderer: r,
	}
	// Initial sizing
	v.SetSize(width, height)
	return v
}

func (v *IdeasView) SetIdeas(ideas []model.Idea) {
	v.ideas = ideas
	items := make([]list.Item, len(ideas))
	for i, idea := range ideas {
		items[i] = IdeaItem{Idea: idea}
	}
	v.list.SetItems(items)
	v.updatePreview()
}

func (v *IdeasView) SetSize(width, height int) {
	v.width = width
	v.height = height

	listWidth := int(float64(width) * 0.4)
	if listWidth < 20 {
		listWidth = 20
	}
	previewWidth := width - listWidth - 4 // Account for borders/padding

	v.list.SetSize(listWidth, height-2)
	v.preview.Width = previewWidth
	v.preview.Height = height - 2

	if v.renderer != nil {
		v.renderer, _ = glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(previewWidth),
		)
	}
	v.updatePreview()
}

func (v IdeasView) Init() tea.Cmd {
	return nil
}

func (v IdeasView) Update(msg tea.Msg) (IdeasView, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.SetSize(msg.Width, msg.Height)

	case tea.KeyMsg:
		// Handle Delete Confirmation
		if v.confirmDel {
			switch msg.String() {
			case "y", "Y":
				cmd = func() tea.Msg { return DeleteIdeaConfirmedMsg{ID: v.deleteTarget} }
				cmds = append(cmds, cmd)
				v.confirmDel = false
				v.deleteTarget = ""
				return v, tea.Batch(cmds...)
			case "n", "N", "esc":
				v.confirmDel = false
				v.deleteTarget = ""
				return v, nil
			default:
				return v, nil // Ignore other keys while confirming
			}
		}

		// Normal Navigation
		switch msg.String() {
		case "esc", "q":
			if v.list.FilterState() != list.Filtering {
				return v, func() tea.Msg { return ExitIdeasMsg{} }
			}
		case "e":
			if v.list.FilterState() != list.Filtering {
				if sel, ok := v.list.SelectedItem().(IdeaItem); ok {
					return v, func() tea.Msg { return EditIdeaMsg{ID: sel.Idea.ID} }
				}
			}
		case "d":
			if v.list.FilterState() != list.Filtering {
				if sel, ok := v.list.SelectedItem().(IdeaItem); ok {
					v.confirmDel = true
					v.deleteTarget = sel.Idea.ID
					return v, nil
				}
			}
		}
	}

	// Update List
	// We only update list if we are not confirming delete,
	// BUT we also need to allow filtering input if filter is active.
	// Since we trapped keys above for confirmDel, it's safe.
	// We trapped keys for d/e/q/esc above, but only if not filtering (for some).
	// Let bubbles/list handle the rest.

	prevSel := v.list.Index()
	v.list, cmd = v.list.Update(msg)
	cmds = append(cmds, cmd)

	if v.list.Index() != prevSel {
		v.updatePreview()
	}

	// Update Preview (viewport)
	v.preview, cmd = v.preview.Update(msg)
	cmds = append(cmds, cmd)

	return v, tea.Batch(cmds...)
}

func (v *IdeasView) updatePreview() {
	if len(v.ideas) == 0 {
		v.preview.SetContent("")
		return
	}

	sel := v.list.SelectedItem()
	if sel == nil {
		v.preview.SetContent("")
		return
	}

	idea := sel.(IdeaItem).Idea
	content := idea.Content

	rendered, err := v.renderer.Render(content)
	if err != nil {
		rendered = content
	}

	v.preview.SetContent(rendered)
}

func (v IdeasView) View() string {
	if len(v.ideas) == 0 {
		return v.viewEmpty()
	}

	listView := v.list.View()

	// Style the list view container
	listStyle := lipgloss.NewStyle().
		Width(v.list.Width()).
		Height(v.height).
		Border(lipgloss.RoundedBorder(), false, true, false, false).
		BorderForeground(lipgloss.Color("63")) // Purple-ish

	previewStyle := lipgloss.NewStyle().
		Width(v.width-v.list.Width()-2). // -2 for border
		Height(v.height).
		Padding(1, 2)

	renderedList := listStyle.Render(listView)
	renderedPreview := previewStyle.Render(v.preview.View())

	mainView := lipgloss.JoinHorizontal(lipgloss.Top, renderedList, renderedPreview)

	if v.confirmDel {
		return v.overlayConfirmation(mainView)
	}

	return mainView
}

func (v IdeasView) viewEmpty() string {
	msg := "No ideas yet.\n\nPress i on any session to capture an idea.\n\n[q/Esc] Back to sessions"

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 3).
		Align(lipgloss.Center).
		Width(50).
		Render(msg)

	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		box,
	)
}

func (v IdeasView) overlayConfirmation(background string) string {
	// Simple overlay: centered box on top of content?
	// Lipgloss doesn't do "layers" easily on top of existing string without potentially messing up ANSI.
	// Best approach for TUI modal:
	// Use lipgloss.Place to center the modal, and if possible, place it *over* the background.
	// But placing over a string background is hard.
	// Alternative: Just return the modal centered on a blank/dimmed background, ignoring the underlying view for now
	// OR (better): Since we have the background string, we can try to center the modal in a new layer.
	// But let's stick to the simplest reliable method:
	// Render the background, then just append the modal? No.
	// Let's just return the modal centered. It's a "modal mode".
	// The user knows context.

	modalText := fmt.Sprintf("Delete idea?\n\n\"%s\"\n\n[y/N]", v.truncatedDeleteTarget())

	modal := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("196")). // Red
		Padding(1, 2).
		Align(lipgloss.Center).
		Width(40).
		Render(modalText)

	// To make it look like an overlay, we'd need to manipulate the background string.
	// For now, let's just return the modal centered, assuming it takes focus.
	// If we want to keep context, we can try to just print it.

	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		modal,
	)
}

func (v IdeasView) truncatedDeleteTarget() string {
	// Find the idea content
	for _, idea := range v.ideas {
		if idea.ID == v.deleteTarget {
			if len(idea.Content) > 30 {
				return idea.Content[:27] + "..."
			}
			return idea.Content
		}
	}
	return ""
}
