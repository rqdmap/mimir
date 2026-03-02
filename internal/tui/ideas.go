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

// ideaRendererReadyMsg is emitted when the glamour renderer is ready (async).
type ideaRendererReadyMsg struct {
	renderer      *glamour.TermRenderer
	rendererWidth int
}

// IdeaSessionRequestMsg asks the app to load the linked session into the conversation pane.
type IdeaSessionRequestMsg struct{ SessionID string }

// ideaPreviewRenderedMsg carries the glamour-rendered preview text.
type ideaPreviewRenderedMsg struct{ content string }
type IdeasView struct {
	ideas         []model.Idea
	list          list.Model
	preview       viewport.Model
	width         int
	height        int
	confirmDel    bool
	deleteTarget  string
	renderer      *glamour.TermRenderer
	rendererWidth int
	glamourStyle  string
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

// newRendererCmd creates a glamour renderer asynchronously so the UI thread is never blocked.
func newRendererCmd(width int, style string) tea.Cmd {
	return func() tea.Msg {
		r, _ := glamour.NewTermRenderer(
			glamour.WithStandardStyle(style),
			glamour.WithWordWrap(width),
		)
		return ideaRendererReadyMsg{renderer: r, rendererWidth: width}
	}
}

func NewIdeasView(width, height int, glamourStyle string) IdeasView {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Idea Notebook"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.DisableQuitKeybindings()
	l.Styles.Title = lipgloss.NewStyle().
		Background(lipgloss.Color("62")).
		Foreground(lipgloss.Color("230")).
		Padding(0, 1)

	vp := viewport.New(0, 0)

	v := IdeasView{
		list:         l,
		preview:      vp,
		width:        width,
		height:       height,
		glamourStyle: glamourStyle,
	}
	v.SetSize(width, height)
	return v
}

func (v *IdeasView) SetIdeas(ideas []model.Idea) tea.Cmd {
	v.ideas = ideas
	items := make([]list.Item, len(ideas))
	for i, idea := range ideas {
		items[i] = IdeaItem{Idea: idea}
	}
	v.list.SetItems(items)
	return v.renderPreviewCmd()
}

func (v *IdeasView) SetSize(width, height int) tea.Cmd {
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

	// Only recreate renderer when preview width changes — do it asynchronously.
	if previewWidth > 0 && previewWidth != v.rendererWidth {
		return newRendererCmd(previewWidth, v.glamourStyle)
	}
	return v.renderPreviewCmd()
}

// SelectedIdea returns the currently highlighted idea, or nil if none.
func (v *IdeasView) SelectedIdea() *model.Idea {
	sel, ok := v.list.SelectedItem().(IdeaItem)
	if !ok {
		return nil
	}
	idea := sel.Idea
	return &idea
}

func (v IdeasView) Init() tea.Cmd {
	return nil
}

func (v IdeasView) Update(msg tea.Msg) (IdeasView, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		cmds = append(cmds, v.SetSize(msg.Width, msg.Height))

	case ideaRendererReadyMsg:
		v.renderer = msg.renderer
		v.rendererWidth = msg.rendererWidth
		return v, v.renderPreviewCmd()

	case ideaPreviewRenderedMsg:
		v.preview.SetContent(msg.content)
		return v, nil

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
			// Ideas is now a tab — no-op here; use [ to switch to Sessions tab
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
	prevSel := v.list.Index()
	v.list, cmd = v.list.Update(msg)
	cmds = append(cmds, cmd)

	if v.list.Index() != prevSel {
		cmds = append(cmds, v.renderPreviewCmd())
		// Feature 5: auto-load linked session in conversation pane on navigation.
		if sel, ok := v.list.SelectedItem().(IdeaItem); ok && sel.Idea.SourceSessionID != "" {
			sid := sel.Idea.SourceSessionID
			cmds = append(cmds, func() tea.Msg { return IdeaSessionRequestMsg{SessionID: sid} })
		}
	}

	// Update Preview (viewport)
	v.preview, cmd = v.preview.Update(msg)
	cmds = append(cmds, cmd)

	return v, tea.Batch(cmds...)
}

// renderPreviewCmd returns a tea.Cmd that renders the selected idea's preview
// in a background goroutine (avoids blocking the UI thread with glamour).
func (v *IdeasView) renderPreviewCmd() tea.Cmd {
	if len(v.ideas) == 0 {
		v.preview.SetContent("")
		return nil
	}

	sel := v.list.SelectedItem()
	if sel == nil {
		v.preview.SetContent("")
		return nil
	}

	content := sel.(IdeaItem).Idea.Content
	renderer := v.renderer // capture for goroutine

	return func() tea.Msg {
		if renderer == nil {
			return ideaPreviewRenderedMsg{content: content}
		}
		rendered, err := renderer.Render(content)
		if err != nil {
			rendered = content
		}
		return ideaPreviewRenderedMsg{content: rendered}
	}
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
