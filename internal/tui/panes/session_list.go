package panes

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/local/oc-manager/internal/model"
)

// SessionSelectedMsg is emitted when user presses Enter on a session
type SessionSelectedMsg struct {
	Session model.Session
}

// SessionItem wraps model.Session for bubbles/list compatibility
type SessionItem struct {
	Session model.Session
	Tags    []string
}

// Title returns the display title for the list item
func (i SessionItem) Title() string {
	title := i.Session.Title
	if i.Session.ParentID != "" {
		title += " (fork)"
	}
	// truncate long titles
	if len(title) > 50 {
		title = title[:47] + "..."
	}
	return title
}

// Description returns the secondary text for the list item
func (i SessionItem) Description() string {
	return formatRelativeTime(i.Session.TimeUpdated) + tagDots(i.Tags)
}

// FilterValue returns the string used for filtering
func (i SessionItem) FilterValue() string { return i.Session.Title }

// SessionList is the left pane showing all sessions
type SessionList struct {
	list    list.Model
	focused bool
	Width   int
	Height  int
}

// NewSessionList creates a new SessionList
func NewSessionList(width, height int) SessionList {
	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = true
	// Minimal custom styling for selection
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.Foreground(lipgloss.Color("205")).BorderForeground(lipgloss.Color("205"))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.Foreground(lipgloss.Color("205")).BorderForeground(lipgloss.Color("205"))

	l := list.New([]list.Item{}, delegate, width, height)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(false)
	l.SetShowHelp(false)
	l.Title = "Sessions"

	// Create the struct
	s := SessionList{
		list:    l,
		focused: false,
		Width:   width,
		Height:  height,
	}
	// Apply sizing logic immediately
	s.SetSize(width, height)

	return s
}

// SetSessions updates the displayed sessions
func (s *SessionList) SetSessions(sessions []model.Session, tags map[string][]string) {
	items := make([]list.Item, len(sessions))
	for i, sess := range sessions {
		t := []string{}
		if val, ok := tags[sess.ID]; ok {
			t = val
		}
		items[i] = SessionItem{
			Session: sess,
			Tags:    t,
		}
	}
	s.list.SetItems(items)
	s.list.Title = fmt.Sprintf("Sessions (%d)", len(sessions))
}

// SetFocused controls whether this pane has focus (styling)
func (s *SessionList) SetFocused(focused bool) {
	s.focused = focused
}

// SetSize updates pane dimensions
func (s *SessionList) SetSize(width, height int) {
	s.Width = width
	s.Height = height

	// Determine available content area inside border
	w := width - 2
	h := height - 2
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}

	s.list.SetSize(w, h)
}

// SelectedSession returns the currently highlighted session, or nil
func (s *SessionList) SelectedSession() *model.Session {
	item := s.list.SelectedItem()
	if item == nil {
		return nil
	}
	if sessionItem, ok := item.(SessionItem); ok {
		return &sessionItem.Session
	}
	return nil
}

// Init satisfies tea.Model
func (s SessionList) Init() tea.Cmd { return nil }

// Update handles keyboard input when this pane is focused
func (s SessionList) Update(msg tea.Msg) (SessionList, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if !s.focused {
			return s, nil
		}

		if msg.String() == "enter" {
			if sess := s.SelectedSession(); sess != nil {
				return s, func() tea.Msg {
					return SessionSelectedMsg{Session: *sess}
				}
			}
		}

	case tea.WindowSizeMsg:
		// When window resizes, we might need to update our size.
		// BUT usually the parent calls SetSize explicitly based on layout.
		// However, passing it down is safe if we are the root, which we are not.
		// We'll trust SetSize is called by parent, but if we receive it directly, handle it.
		s.SetSize(msg.Width, msg.Height)
		return s, nil
	}

	s.list, cmd = s.list.Update(msg)
	return s, cmd
}

// View renders the pane
func (s SessionList) View() string {
	borderColor := lipgloss.Color("240") // Gray
	if s.focused {
		borderColor = lipgloss.Color("205") // Pink/Purple
	}

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(s.Width - 2).
		Height(s.Height - 2)

	if len(s.list.Items()) == 0 {
		return style.Render("No sessions found.\nStart a conversation in OpenCode to see it here.")
	}

	return style.Render(s.list.View())
}

// formatRelativeTime converts unix timestamp (milliseconds) to "2 days ago" style
func formatRelativeTime(ts int64) string {
	t := time.UnixMilli(ts)
	dur := time.Since(t)
	switch {
	case dur < time.Hour:
		return fmt.Sprintf("%dm ago", int(dur.Minutes()))
	case dur < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(dur.Hours()))
	case dur < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(dur.Hours()/24))
	default:
		return t.Format("Jan 2, 2006")
	}
}

// tagDots returns a short string like "  ● ● ●" for up to 3 tags
func tagDots(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	n := len(tags)
	if n > 3 {
		n = 3
	}
	return "  " + strings.Repeat("● ", n)
}
