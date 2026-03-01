package tui

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/list"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/local/oc-manager/internal/model"
)

// Messages for tag operations

// TagFilterSelectedMsg is emitted when the user selects a tag in filter mode.
type TagFilterSelectedMsg struct{ TagName string }

// TagFilterExitMsg is emitted when the user presses Esc to exit filter mode.
type TagFilterExitMsg struct{}

// RemoveTagMsg is emitted when user wants to remove a specific tag from a session.
type RemoveTagMsg struct {
	SessionID string
	TagName   string
}

// TagItem implements list.Item for tags.
type TagItem struct {
	Tag model.Tag
}

func (t TagItem) Title() string       { return t.Tag.Name }
func (t TagItem) Description() string { return "" }
func (t TagItem) FilterValue() string { return t.Tag.Name }

// TagFilterView is a full-screen overlay for browsing and selecting a tag to filter by.
type TagFilterView struct {
	list   list.Model
	width  int
	height int
	active bool
}

// tagDelegate is a custom delegate to render the dot
type tagDelegate struct{}

func (d tagDelegate) Height() int                             { return 1 }
func (d tagDelegate) Spacing() int                            { return 0 }
func (d tagDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d tagDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	t, ok := listItem.(TagItem)
	if !ok {
		return
	}

	str := t.Tag.Name
	dot := lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Render("●")
	
	// Unselected state
	itemStr := fmt.Sprintf("%s %s", dot, str)
	
	// Selected state
	if index == m.Index() {
		itemStr = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Render(fmt.Sprintf("%s %s", dot, str))
	}

	fmt.Fprint(w, itemStr)
}

// NewTagFilterView creates a new tag filter view.
func NewTagFilterView(width, height int) TagFilterView {
	delegate := tagDelegate{}

	l := list.New([]list.Item{}, delegate, width, height)

	l.Title = "Filter by Tag"
	l.SetShowHelp(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))

	return TagFilterView{
		list:   l,
		width:  width,
		height: height,
		active: false,
	}
}

// SetTags updates the list of tags.
func (tf *TagFilterView) SetTags(tags []model.Tag) {
	items := make([]list.Item, len(tags))
	for i, t := range tags {
		items[i] = TagItem{Tag: t}
	}
	tf.list.SetItems(items)
}

// SetSize updates the size of the view.
func (tf *TagFilterView) SetSize(width, height int) {
	tf.width = width
	tf.height = height
	// Add some padding for the border
	listWidth := width - 4
	listHeight := height - 4
	if listWidth < 0 {
		listWidth = 0
	}
	if listHeight < 0 {
		listHeight = 0
	}
	tf.list.SetSize(listWidth, listHeight)
}

// Activate shows the view.
func (tf *TagFilterView) Activate() {
	tf.active = true
	tf.list.ResetSelected()
}

// Deactivate hides the view.
func (tf *TagFilterView) Deactivate() {
	tf.active = false
	tf.list.ResetFilter()
}

// IsActive returns whether the view is currently active.
func (tf TagFilterView) IsActive() bool {
	return tf.active
}

// Init initializes the view.
func (tf TagFilterView) Init() tea.Cmd {
	return nil
}

// Update handles messages.
func (tf TagFilterView) Update(msg tea.Msg) (TagFilterView, tea.Cmd) {
	if !tf.active {
		return tf, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if selectedItem := tf.list.SelectedItem(); selectedItem != nil {
				return tf, func() tea.Msg {
					return TagFilterSelectedMsg{TagName: selectedItem.(TagItem).Tag.Name}
				}
			}
		case "esc":
			return tf, func() tea.Msg { return TagFilterExitMsg{} }
		}
	}

	var cmd tea.Cmd
	tf.list, cmd = tf.list.Update(msg)
	return tf, cmd
}

// View renders the view.
func (tf TagFilterView) View() string {
	if !tf.active {
		return ""
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7D56F4")).
		Width(tf.width-2).
		Height(tf.height-2).
		Padding(1, 2)

	// Render custom list items with dots if needed, but list.Model handles items.
	// We need to customize the delegate if we want the dot.
	// For now, let's just render the list within the border.

	// Ensure title includes help hint if list doesn't show it
	content := tf.list.View()

	// Add footer hint
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).MarginTop(1)
	help := helpStyle.Render("[Enter] filter  [Esc] cancel")

	return style.Render(lipgloss.JoinVertical(lipgloss.Left, content, help))
}

// ParseCommaSeparatedTags splits "work, ai, idea" into ["work", "ai", "idea"].
// Trims whitespace, lowercases, skips empty strings.
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
