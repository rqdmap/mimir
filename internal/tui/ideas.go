package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/local/oc-manager/internal/model"
	"github.com/local/oc-manager/internal/tui/panes"
)

type ExitIdeasMsg struct{}
type EditIdeaMsg struct{ ID string }
type DeleteIdeaConfirmedMsg struct{ ID string }
type IdeaSessionRequestMsg struct{ SessionID string }
type IdeaSelectedMsg struct{ Idea model.Idea }

type IdeasView struct {
	ideas        []model.Idea
	searchFilter string
	list         list.Model
	width        int
	height       int
	confirmDel   bool
	deleteTarget string
	theme        panes.Theme
}

type IdeaItem struct {
	Idea model.Idea
}

func (i IdeaItem) Title() string {
	content := i.Idea.Content
	if idx := strings.IndexByte(content, '\n'); idx >= 0 {
		content = content[:idx]
	}
	if len([]rune(content)) > 50 {
		runes := []rune(content)
		return string(runes[:50]) + "..."
	}
	return content
}

func (i IdeaItem) Description() string {
	ts := time.UnixMilli(i.Idea.TimeCreated).Format("Jan 02, 2006 15:04")
	if i.Idea.SourceSessionID != "" {
		return fmt.Sprintf("%s • linked to session", ts)
	}
	return ts
}

func (i IdeaItem) FilterValue() string { return i.Idea.Content }

func NewIdeasView(width, height int, theme panes.Theme) IdeasView {
	l := list.New([]list.Item{}, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Idea Notebook"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.DisableQuitKeybindings()
	l.Styles.Title = lipgloss.NewStyle().
		Background(theme.AccentBg).
		Foreground(theme.AccentFg).
		Padding(0, 1)

	v := IdeasView{
		list:   l,
		width:  width,
		height: height,
		theme:  theme,
	}
	v.SetSize(width, height)
	return v
}

func (v *IdeasView) SetIdeas(ideas []model.Idea) tea.Cmd {
	v.ideas = ideas
	v.applyFilter()
	return v.selectionChangedCmd()
}

func (v *IdeasView) SetFilter(q string) {
	v.searchFilter = q
	v.applyFilter()
}

func (v *IdeasView) applyFilter() {
	wasEmpty := len(v.list.Items()) == 0
	q := strings.ToLower(v.searchFilter)
	var items []list.Item
	for _, idea := range v.ideas {
		if q == "" || strings.Contains(strings.ToLower(idea.Content), q) {
			items = append(items, IdeaItem{Idea: idea})
		}
	}
	v.list.SetItems(items)
	if wasEmpty && len(items) > 0 {
		v.list.Select(0)
	}
}

func (v *IdeasView) SetSize(width, height int) {
	v.width = width
	v.height = height
	v.list.SetSize(width, height-2)
}

func (v *IdeasView) SelectedIdea() *model.Idea {
	sel, ok := v.list.SelectedItem().(IdeaItem)
	if !ok {
		return nil
	}
	idea := sel.Idea
	return &idea
}

func (v *IdeasView) selectionChangedCmd() tea.Cmd {
	idea := v.SelectedIdea()
	if idea == nil {
		return nil
	}
	selected := *idea
	return func() tea.Msg { return IdeaSelectedMsg{Idea: selected} }
}

func (v IdeasView) Init() tea.Cmd {
	return nil
}

func (v IdeasView) Update(msg tea.Msg) (IdeasView, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if v.confirmDel {
			switch msg.String() {
			case "y", "Y":
				targetID := v.deleteTarget
				cmd = func() tea.Msg { return DeleteIdeaConfirmedMsg{ID: targetID} }
				cmds = append(cmds, cmd)
				v.confirmDel = false
				v.deleteTarget = ""
				return v, tea.Batch(cmds...)
			case "n", "N", "esc":
				v.confirmDel = false
				v.deleteTarget = ""
				return v, nil
			default:
				return v, nil
			}
		}

		switch msg.String() {
		case "esc", "q":
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

	prevSel := v.list.Index()
	v.list, cmd = v.list.Update(msg)
	cmds = append(cmds, cmd)

	if v.list.Index() != prevSel {
		cmds = append(cmds, v.selectionChangedCmd())
	}

	return v, tea.Batch(cmds...)
}

func (v IdeasView) View() string {
	if len(v.ideas) == 0 {
		return v.viewEmpty()
	}

	listStyle := lipgloss.NewStyle().
		Width(v.width).
		Height(v.height).
		BorderForeground(v.theme.BorderFocused)

	rendered := listStyle.Render(v.list.View())

	if v.confirmDel {
		return v.overlayConfirmation(rendered)
	}

	return rendered
}

func (v IdeasView) viewEmpty() string {
	msg := "No ideas yet.\n\nPress i on any session to capture an idea."

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(v.theme.BorderFocused).
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
	modalText := fmt.Sprintf("Delete idea?\n\n\"%s\"\n\n[y/N]", v.truncatedDeleteTarget())

	modal := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("196")).
		Padding(1, 2).
		Align(lipgloss.Center).
		Width(40).
		Render(modalText)

	_ = background
	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		modal,
	)
}

func (v IdeasView) truncatedDeleteTarget() string {
	for _, idea := range v.ideas {
		if idea.ID == v.deleteTarget {
			runes := []rune(idea.Content)
			if len(runes) > 30 {
				return string(runes[:27]) + "..."
			}
			return idea.Content
		}
	}
	return ""
}
