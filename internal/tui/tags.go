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

type ManageSessionJumpMsg struct {
	Session model.Session
}

type DeleteTagMsg struct{ TagName string }
type ActivateRenameMsg struct{ TagName string }
type ManageTagRemoveMsg struct {
	TagName   string
	SessionID string
}
type ManageTagExitMsg struct{}

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

type ManageSessionItem struct {
	Session model.Session
}

func (i ManageSessionItem) Title() string {
	if i.Session.Title == "" {
		id := i.Session.ID
		if len(id) > 8 {
			id = id[:8]
		}
		return "[" + id + "...]"
	}
	return i.Session.Title
}

func (i ManageSessionItem) Description() string {
	id := i.Session.ID
	if len(id) > 20 {
		id = id[:20] + "..."
	}
	return id
}

func (i ManageSessionItem) FilterValue() string { return i.Session.Title }

type TagsView struct {
	tags             []model.Tag
	counts           map[string]int
	searchFilter     string
	list             list.Model
	width            int
	height           int
	theme            panes.Theme
	manageMode       bool
	managingTagName  string
	allSessions      []model.Session
	sessionTags      map[string][]string
	manageSessions   []model.Session
	manageList       list.Model
	manageConfirmDel bool
	manageDelTarget  string
	confirmDeleteTag bool
	deleteTagTarget  string
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

	manageDelegate := list.NewDefaultDelegate()
	manageDelegate.ShowDescription = true
	manageDelegate.Styles.SelectedTitle = manageDelegate.Styles.SelectedTitle.
		Foreground(theme.Accent).
		BorderForeground(theme.Accent)
	manageDelegate.Styles.SelectedDesc = manageDelegate.Styles.SelectedDesc.
		Foreground(theme.Accent).
		BorderForeground(theme.Accent)

	ml := list.New([]list.Item{}, manageDelegate, 0, 0)
	ml.SetShowHelp(false)
	ml.SetShowStatusBar(false)
	ml.SetFilteringEnabled(false)
	ml.DisableQuitKeybindings()
	ml.Styles.Title = lipgloss.NewStyle().
		Background(theme.AccentBg).
		Foreground(theme.AccentFg).
		Padding(0, 1)

	v := TagsView{
		list:        l,
		manageList:  ml,
		width:       width,
		height:      height,
		counts:      make(map[string]int),
		sessionTags: make(map[string][]string),
		theme:       theme,
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

func (v *TagsView) SetSessions(sessions []model.Session, sessionTags map[string][]string) {
	v.allSessions = sessions
	v.sessionTags = sessionTags
	if v.manageMode {
		v.filterManageSessions()
	}
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

func (v *TagsView) filterManageSessions() {
	v.manageSessions = nil
	for _, s := range v.allSessions {
		tags := v.sessionTags[s.ID]
		for _, tag := range tags {
			if tag == v.managingTagName {
				v.manageSessions = append(v.manageSessions, s)
				break
			}
		}
	}
	v.initManageList()
}

func (v *TagsView) initManageList() {
	var items []list.Item
	for _, s := range v.manageSessions {
		items = append(items, ManageSessionItem{Session: s})
	}
	v.manageList.SetItems(items)
	v.manageList.Title = "Sessions with #" + v.managingTagName
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
	v.manageList.SetSize(w, h)
}

func (v TagsView) SelectedTag() *TagsItem {
	item, ok := v.list.SelectedItem().(TagsItem)
	if !ok {
		return nil
	}
	return &item
}

func (v TagsView) SelectedManageSession() *model.Session {
	item, ok := v.manageList.SelectedItem().(ManageSessionItem)
	if !ok {
		return nil
	}
	return &item.Session
}

func (v TagsView) Update(msg tea.Msg) (TagsView, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if v.manageMode {
			if v.manageConfirmDel {
				switch msg.String() {
				case "y", "Y":
					targetID := v.manageDelTarget
					tagName := v.managingTagName
					v.manageConfirmDel = false
					v.manageDelTarget = ""
					v.removeManageSession(targetID)
					removeCmd := func() tea.Msg { return ManageTagRemoveMsg{TagName: tagName, SessionID: targetID} }
					if len(v.manageSessions) == 0 {
						v.manageMode = false
						v.managingTagName = ""
						return v, tea.Batch(removeCmd, func() tea.Msg { return ManageTagExitMsg{} })
					}
					v.initManageList()
					return v, removeCmd
				case "n", "N", "esc":
					v.manageConfirmDel = false
					v.manageDelTarget = ""
					return v, nil
				default:
					return v, nil // swallow all other keys during confirmation
				}
			}
			switch msg.String() {
			case "esc":
				v.manageMode = false
				v.manageConfirmDel = false
				v.manageDelTarget = ""
				v.managingTagName = ""
				return v, func() tea.Msg { return ManageTagExitMsg{} }
			case "enter":
				if item, ok := v.manageList.SelectedItem().(ManageSessionItem); ok {
					session := item.Session
					return v, func() tea.Msg { return ManageSessionJumpMsg{Session: session} }
				}
				return v, nil
			case "d", "x":
				if item, ok := v.manageList.SelectedItem().(ManageSessionItem); ok {
					v.manageConfirmDel = true
					v.manageDelTarget = item.Session.ID
				}
				return v, nil
			default:
				var cmd tea.Cmd
				v.manageList, cmd = v.manageList.Update(msg)
				return v, cmd
			}
		}
		if v.confirmDeleteTag {
			switch msg.String() {
			case "y", "Y":
				tagName := v.deleteTagTarget
				v.confirmDeleteTag = false
				v.deleteTagTarget = ""
				return v, func() tea.Msg { return DeleteTagMsg{TagName: tagName} }
			case "n", "N", "esc":
				v.confirmDeleteTag = false
				v.deleteTagTarget = ""
				return v, nil
			default:
				return v, nil // swallow all keys during confirmation
			}
		}
		switch msg.String() {
		case "enter":
			if sel := v.SelectedTag(); sel != nil {
				v.manageMode = true
				v.managingTagName = sel.Tag.Name
				v.filterManageSessions()
			}
			return v, nil
		case "d":
			if sel := v.SelectedTag(); sel != nil {
				v.confirmDeleteTag = true
				v.deleteTagTarget = sel.Tag.Name
			}
			return v, nil
		case "r":
			if sel := v.SelectedTag(); sel != nil {
				tagName := sel.Tag.Name
				return v, func() tea.Msg { return ActivateRenameMsg{TagName: tagName} }
			}
			return v, nil
		}
	}

	var cmd tea.Cmd
	v.list, cmd = v.list.Update(msg)
	return v, cmd
}

func (v *TagsView) removeManageSession(sessionID string) {
	newSessions := v.manageSessions[:0]
	for _, s := range v.manageSessions {
		if s.ID != sessionID {
			newSessions = append(newSessions, s)
		}
	}
	v.manageSessions = newSessions
}

func (v TagsView) View() string {
	borderColor := v.theme.BorderUnfocused

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(v.width - 2).
		Height(v.height - 2)

	if v.manageMode {
		if len(v.manageSessions) == 0 {
			return style.Align(lipgloss.Center, lipgloss.Center).Render("No sessions with this tag.")
		}

		mainView := style.Render(v.manageList.View())

		if v.manageConfirmDel {
			return v.overlayManageConfirm(mainView)
		}
		return mainView
	}

	if len(v.tags) == 0 {
		return style.Align(lipgloss.Center, lipgloss.Center).
			Render("No tags yet.\n\nPress 't' on a selected session\nto add the first tag.")
	}

	mainView := style.Render(v.list.View())
	if v.confirmDeleteTag {
		return v.overlayDeleteTagConfirm()
	}
	return mainView
}

func (v TagsView) overlayManageConfirm(background string) string {
	sessionTitle := v.manageDelTarget
	for _, s := range v.manageSessions {
		if s.ID == v.manageDelTarget {
			title := s.Title
			if title == "" {
				idLen := min(8, len(s.ID))
				title = "[" + s.ID[:idLen] + "...]"
			}
			if len(title) > 35 {
				title = title[:32] + "..."
			}
			sessionTitle = title
			break
		}
	}

	modalText := fmt.Sprintf("Remove tag #%s from this session?\n\n\"%s\"\n\n[y / N]", v.managingTagName, sessionTitle)

	modal := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("196")).
		Padding(1, 2).
		Align(lipgloss.Center).
		Width(46).
		Render(modalText)

	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		modal,
	)
}

func (v TagsView) overlayDeleteTagConfirm() string {
	count := v.counts[v.deleteTagTarget]
	var countMsg string
	if count == 0 {
		countMsg = "Orphan tag — no sessions."
	} else if count == 1 {
		countMsg = "Will remove from 1 session."
	} else {
		countMsg = fmt.Sprintf("Will remove from %d sessions.", count)
	}

	modalText := fmt.Sprintf("Delete tag #%s?\n\n%s\n\n[y / N]", v.deleteTagTarget, countMsg)

	modal := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("196")).
		Padding(1, 2).
		Align(lipgloss.Center).
		Width(40).
		Render(modalText)

	return lipgloss.Place(
		v.width, v.height,
		lipgloss.Center, lipgloss.Center,
		modal,
	)
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
