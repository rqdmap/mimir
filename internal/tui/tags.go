package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/local/oc-manager/internal/model"
	"github.com/local/oc-manager/internal/tui/panes"
)

type TagFilterByNameMsg struct{ TagName string }

type RemoveTagMsg struct {
	SessionID string
	TagName   string
}

type TagsItem struct {
	Tag   model.Tag
	Count int
}

func (i TagsItem) Title() string { return i.Tag.Name }
func (i TagsItem) Description() string {
	if i.Count == 1 {
		return "1 session"
	}
	return fmt.Sprintf("%d sessions", i.Count)
}
func (i TagsItem) FilterValue() string { return i.Tag.Name }

type TagsView struct {
	tags         []model.Tag
	counts       map[string]int
	searchFilter string
	list         list.Model
	width        int
	height       int
	theme        panes.Theme
}

func NewTagsView(width, height int, theme panes.Theme) TagsView {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = true
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(theme.Accent).
		BorderForeground(theme.Accent)
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(theme.Accent).
		BorderForeground(theme.Accent)

	l := list.New([]list.Item{}, delegate, 0, 0)
	l.Title = "Tags"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.DisableQuitKeybindings()
	l.Styles.Title = lipgloss.NewStyle().
		Background(theme.AccentBg).
		Foreground(theme.AccentFg).
		Padding(0, 1)

	v := TagsView{
		list:   l,
		width:  width,
		height: height,
		counts: make(map[string]int),
		theme:  theme,
	}
	v.setListSize(width, height)
	return v
}

func (v *TagsView) SetTags(tags []model.Tag, counts map[string]int) {
	v.tags = tags
	v.counts = counts
	v.list.Title = fmt.Sprintf("Tags (%d)", len(tags))
	v.applyFilter()
}

func (v *TagsView) SetFilter(q string) {
	v.searchFilter = q
	v.applyFilter()
}

func (v *TagsView) applyFilter() {
	q := strings.ToLower(v.searchFilter)
	var items []list.Item
	for _, t := range v.tags {
		if q == "" || strings.Contains(strings.ToLower(t.Name), q) {
			items = append(items, TagsItem{Tag: t, Count: v.counts[t.Name]})
		}
	}
	v.list.SetItems(items)
}

func (v *TagsView) SetSize(width, height int) {
	v.width = width
	v.height = height
	v.setListSize(width, height)
}

func (v *TagsView) setListSize(width, height int) {
	w := width - 2
	h := height - 2
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	v.list.SetSize(w, h)
}

func (v TagsView) SelectedTag() *TagsItem {
	item, ok := v.list.SelectedItem().(TagsItem)
	if !ok {
		return nil
	}
	return &item
}

func (v TagsView) Update(msg tea.Msg) (TagsView, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "enter" {
			if sel := v.SelectedTag(); sel != nil {
				tagName := sel.Tag.Name
				return v, func() tea.Msg { return TagFilterByNameMsg{TagName: tagName} }
			}
		}
	}

	var cmd tea.Cmd
	v.list, cmd = v.list.Update(msg)
	return v, cmd
}

func (v TagsView) View() string {
	borderColor := v.theme.BorderUnfocused

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(v.width - 2).
		Height(v.height - 2)

	if len(v.tags) == 0 {
		return style.Align(lipgloss.Center, lipgloss.Center).
			Render("No tags yet.\n\nPress 't' on a selected session\nto add the first tag.")
	}

	return style.Render(v.list.View())
}

func ParseCommaSeparatedTags(input string) []string {
	parts := strings.Split(input, ",")
	result := []string{}
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
