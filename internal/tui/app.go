package tui

import (
	"database/sql"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/local/oc-manager/internal/db"
	"github.com/local/oc-manager/internal/model"
	"github.com/local/oc-manager/internal/tui/panes"
)

// AppState tracks which top-level view is active.
type AppState int

const (
	StateMain  AppState = iota // 3-pane main view
	StateIdeas                 // Idea notebook full-screen
)

// FocusedPane tracks which pane has keyboard focus.
type FocusedPane int

const (
	FocusSessionList FocusedPane = iota
	FocusConversation
	FocusMetadata
)

// --- Async message types ---

type sessionLoadedMsg struct {
	messages []model.Message
	meta     model.SessionMeta
}

type sessionsRefreshedMsg struct {
	sessions    []model.Session
	sessionTags map[string][]string
}

type ideasLoadedMsg struct {
	ideas []model.Idea
}

type errMsg struct{ err string }

// App is the top-level BubbleTea model.
type App struct {
	state           AppState
	focus           FocusedPane
	sessions        []model.Session
	sessionTags     map[string][]string
	selectedSession *model.Session

	sessionList  panes.SessionList
	conversation panes.ConversationPane
	metadata     panes.MetadataPane
	ideasView    IdeasView

	opencodeDB *sql.DB
	managerDB  *sql.DB

	width  int
	height int
	err    string

	inputMode InputMode
	tagFilter TagFilterView

	showHelp bool
}

// NewApp creates an App with both databases wired in.
func NewApp(opencodeDB, managerDB *sql.DB) App {
	a := App{
		state:       StateMain,
		focus:       FocusSessionList,
		sessionTags: make(map[string][]string),
		opencodeDB:  opencodeDB,
		managerDB:   managerDB,
	}

	// Initialise panes with zero size; recalcLayout will size them on first WindowSizeMsg.
	a.sessionList = panes.NewSessionList(0, 0)
	a.conversation = panes.NewConversationPane(0, 0)
	a.metadata = panes.NewMetadataPane(0, 0)
	a.ideasView = NewIdeasView(0, 0)
	a.inputMode = NewInputMode(0, 0)
	a.tagFilter = NewTagFilterView(0, 0)

	a.sessionList.SetFocused(true)

	return a
}

// --- tea.Model interface ---

func (a App) Init() tea.Cmd {
	return a.loadInitialSessions()
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// When inputMode is active, forward ALL messages to it and return early.
	if a.inputMode.IsActive() {
		var cmd tea.Cmd
		a.inputMode, cmd = a.inputMode.Update(msg)
		return a, cmd
	}

	// When tagFilter is active, forward ALL messages to it and return early.
	if a.tagFilter.IsActive() {
		var cmd tea.Cmd
		a.tagFilter, cmd = a.tagFilter.Update(msg)
		return a, cmd
	}

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.recalcLayout()
		return a, nil

	case tea.KeyMsg:
		return a.handleKey(msg)

	// --- Async result messages ---

	case sessionsRefreshedMsg:
		a.sessions = msg.sessions
		a.sessionTags = msg.sessionTags
		a.sessionList.SetSessions(a.sessions, a.sessionTags)
		a.err = ""
		return a, nil

	case sessionLoadedMsg:
		a.conversation.SetMessages(msg.messages)
		a.metadata.SetSessionMeta(msg.meta)
		a.metadata.SetMessageCount(len(msg.messages))
		a.err = ""
		return a, nil

	case ideasLoadedMsg:
		a.ideasView.SetIdeas(msg.ideas)
		return a, nil

	case errMsg:
		a.err = msg.err
		return a, nil

	// --- InputMode result messages ---

	case InputSavedNoteMsg:
		if a.managerDB != nil {
			_ = db.UpsertSessionNote(a.managerDB, msg.SessionID, msg.Note)
			cmds = append(cmds, a.reloadSessionMeta(msg.SessionID))
		}
		return a, tea.Batch(cmds...)

	case InputSavedTagMsg:
		if a.managerDB != nil {
			for _, tag := range msg.Tags {
				_ = db.AddSessionTag(a.managerDB, msg.SessionID, tag)
			}
			cmds = append(cmds, a.reloadSessionTagsAndList(msg.SessionID))
		}
		return a, tea.Batch(cmds...)

	case InputSavedIdeaMsg:
		if a.managerDB != nil && msg.Content != "" {
			_, _ = db.AddIdea(a.managerDB, msg.Content, msg.SessionID)
		}
		return a, nil

	case InputCancelledMsg:
		// InputMode already deactivated itself; nothing to do.
		return a, nil

	// --- TagFilterView result messages ---

	case TagFilterSelectedMsg:
		a.tagFilter.Deactivate()
		if a.managerDB != nil && a.opencodeDB != nil {
			tagName := msg.TagName
			opencodeDB := a.opencodeDB
			managerDB := a.managerDB
			cmds = append(cmds, func() tea.Msg {
				sessionIDs, _ := db.ListSessionsByTag(managerDB, tagName)
				sessions, _ := db.ListSessions(opencodeDB)
				filtered := filterSessionsByIDs(sessions, sessionIDs)
				tags := make(map[string][]string)
				for _, s := range filtered {
					t, _ := db.GetSessionTags(managerDB, s.ID)
					tags[s.ID] = t
				}
				return sessionsRefreshedMsg{sessions: filtered, sessionTags: tags}
			})
		}
		return a, tea.Batch(cmds...)

	case TagFilterExitMsg:
		a.tagFilter.Deactivate()
		return a, nil

	// --- Pane-emitted messages ---

	case panes.SessionSelectedMsg:
		a.selectedSession = &msg.Session
		a.metadata.ClearSession()
		a.conversation.SetMessages(nil)
		if a.opencodeDB != nil {
			cmds = append(cmds, a.loadSession(msg.Session))
		}
		return a, tea.Batch(cmds...)

	case panes.AddTagMsg:
		a.inputMode.ActivateTag(msg.SessionID)
		return a, nil

	case panes.EditNoteMsg:
		a.inputMode.ActivateNote(msg.SessionID, msg.CurrentNote)
		return a, nil

	case panes.AddIdeaMsg:
		a.inputMode.ActivateIdea(msg.SessionID)
		return a, nil

	case panes.OpenIdeaNotebookMsg:
		a.state = StateIdeas
		if a.managerDB != nil {
			cmds = append(cmds, a.loadIdeas())
		}
		return a, tea.Batch(cmds...)

	case ExitIdeasMsg:
		a.state = StateMain
		return a, nil

	case DeleteIdeaConfirmedMsg:
		if a.managerDB != nil {
			_ = db.DeleteIdea(a.managerDB, msg.ID)
			cmds = append(cmds, a.loadIdeas())
		}
		return a, tea.Batch(cmds...)

	case EditIdeaMsg:
		// Activate idea input; for edit we pass an empty sessionID
		a.inputMode.ActivateIdea(msg.ID)
		return a, nil

	case RemoveTagMsg:
		if a.managerDB != nil {
			_ = db.RemoveSessionTag(a.managerDB, msg.SessionID, msg.TagName)
			cmds = append(cmds, a.reloadSessionTagsAndList(msg.SessionID))
		}
		return a, tea.Batch(cmds...)
	case sessionMetaRefreshedMsg:
		a.metadata.SetSessionMeta(msg.meta)
		return a, nil

	case sessionAndListRefreshedMsg:
		a.sessions = msg.sessions
		a.sessionTags = msg.sessionTags
		a.sessionList.SetSessions(a.sessions, a.sessionTags)
		a.metadata.SetSessionMeta(msg.meta)
		a.err = ""
		return a, nil

	}

	// Delegate to active pane
	if a.state == StateIdeas {
		var cmd tea.Cmd
		a.ideasView, cmd = a.ideasView.Update(msg)
		return a, cmd
	}

	// Main state — delegate to focused pane
	switch a.focus {
	case FocusSessionList:
		var cmd tea.Cmd
		a.sessionList, cmd = a.sessionList.Update(msg)
		cmds = append(cmds, cmd)
	case FocusConversation:
		var cmd tea.Cmd
		a.conversation, cmd = a.conversation.Update(msg)
		cmds = append(cmds, cmd)
	case FocusMetadata:
		var cmd tea.Cmd
		a.metadata, cmd = a.metadata.Update(msg)
		cmds = append(cmds, cmd)
	}

	return a, tea.Batch(cmds...)
}

func (a App) View() string {
	// InputMode overlay takes full screen when active.
	if a.inputMode.IsActive() {
		return a.inputMode.View()
	}

	// TagFilter overlay takes full screen when active.
	if a.tagFilter.IsActive() {
		return a.tagFilter.View()
	}

	if a.state == StateIdeas {
		return a.ideasView.View()
	}

	// Status bar line
	statusBar := a.buildStatusBar()
	contentHeight := a.height - 1
	if contentHeight < 0 {
		contentHeight = 0
	}

	var mainView string
	switch {
	case a.width >= 120:
		// 3-pane
		mainView = lipgloss.JoinHorizontal(lipgloss.Top,
			a.sessionList.View(),
			a.conversation.View(),
			a.metadata.View(),
		)
	case a.width >= 80:
		// 2-pane (no metadata)
		mainView = lipgloss.JoinHorizontal(lipgloss.Top,
			a.sessionList.View(),
			a.conversation.View(),
		)
	default:
		// Single pane — only show focused
		switch a.focus {
		case FocusSessionList:
			mainView = a.sessionList.View()
		case FocusConversation:
			mainView = a.conversation.View()
		case FocusMetadata:
			mainView = a.metadata.View()
		}
	}

	if a.showHelp {
		mainView = a.overlayHelp(mainView)
	}

	return lipgloss.JoinVertical(lipgloss.Left, mainView, statusBar)
}

// --- Key handling ---

func (a App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// If in Ideas view, delegate there
	if a.state == StateIdeas {
		var cmd tea.Cmd
		a.ideasView, cmd = a.ideasView.Update(msg)
		return a, cmd
	}

	// Global keys
	switch key {
	case "ctrl+c", KeyQuit:
		return a, tea.Quit

	case KeyRefresh:
		a.err = ""
		if a.opencodeDB != nil {
			return a, a.loadInitialSessions()
		}
		return a, nil

	case KeyHelp:
		a.showHelp = !a.showHelp
		return a, nil

	case KeyIdeas:
		a.state = StateIdeas
		var cmds []tea.Cmd
		if a.managerDB != nil {
			cmds = append(cmds, a.loadIdeas())
		}
		return a, tea.Batch(cmds...)

	case "T":
		// Open tag filter overlay
		if a.managerDB != nil {
			tags, _ := db.ListAllTags(a.managerDB)
			a.tagFilter.SetTags(tags)
		}
		a.tagFilter.Activate()
		return a, nil

	case "tab":
		a.cycleFocusForward()
		return a, nil

	case "shift+tab":
		a.cycleFocusBackward()
		return a, nil
	}

	// Delegate to focused pane
	var cmd tea.Cmd
	switch a.focus {
	case FocusSessionList:
		a.sessionList, cmd = a.sessionList.Update(msg)
	case FocusConversation:
		a.conversation, cmd = a.conversation.Update(msg)
	case FocusMetadata:
		a.metadata, cmd = a.metadata.Update(msg)
	}

	return a, cmd
}

// --- Focus management ---

func (a *App) cycleFocusForward() {
	if a.width >= 120 {
		// 3-pane cycle
		switch a.focus {
		case FocusSessionList:
			a.setFocus(FocusConversation)
		case FocusConversation:
			a.setFocus(FocusMetadata)
		case FocusMetadata:
			a.setFocus(FocusSessionList)
		}
	} else {
		// 2-pane or 1-pane cycle
		switch a.focus {
		case FocusSessionList:
			a.setFocus(FocusConversation)
		case FocusConversation:
			if a.width >= 80 {
				a.setFocus(FocusSessionList)
			} else {
				a.setFocus(FocusMetadata)
			}
		case FocusMetadata:
			a.setFocus(FocusSessionList)
		}
	}
}

func (a *App) cycleFocusBackward() {
	if a.width >= 120 {
		switch a.focus {
		case FocusSessionList:
			a.setFocus(FocusMetadata)
		case FocusConversation:
			a.setFocus(FocusSessionList)
		case FocusMetadata:
			a.setFocus(FocusConversation)
		}
	} else {
		switch a.focus {
		case FocusSessionList:
			if a.width < 80 {
				a.setFocus(FocusMetadata)
			} else {
				a.setFocus(FocusConversation)
			}
		case FocusConversation:
			a.setFocus(FocusSessionList)
		case FocusMetadata:
			a.setFocus(FocusConversation)
		}
	}
}

func (a *App) setFocus(f FocusedPane) {
	a.focus = f
	a.sessionList.SetFocused(f == FocusSessionList)
	a.conversation.SetFocused(f == FocusConversation)
	a.metadata.SetFocused(f == FocusMetadata)
}

// --- Layout ---

func (a *App) recalcLayout() {
	h := a.height - 1 // reserve 1 line for status bar
	if h < 0 {
		h = 0
	}

	switch {
	case a.width >= 120:
		listW := int(float64(a.width) * 0.30)
		convW := int(float64(a.width) * 0.50)
		metaW := a.width - listW - convW
		a.sessionList.SetSize(listW, h)
		a.conversation.SetSize(convW, h)
		a.metadata.SetSize(metaW, h)

	case a.width >= 80:
		listW := int(float64(a.width) * 0.35)
		convW := a.width - listW
		a.sessionList.SetSize(listW, h)
		a.conversation.SetSize(convW, h)
		a.metadata.SetSize(0, h)

	default:
		a.sessionList.SetSize(a.width, h)
		a.conversation.SetSize(a.width, h)
		a.metadata.SetSize(a.width, h)
	}

	a.ideasView.SetSize(a.width, a.height)

	// Update modal dimensions
	a.inputMode.width = a.width
	a.inputMode.height = a.height
	a.tagFilter.SetSize(a.width, a.height)
}

// --- Async DB commands ---

func (a App) loadInitialSessions() tea.Cmd {
	opencodeDB := a.opencodeDB
	managerDB := a.managerDB
	return func() tea.Msg {
		if opencodeDB == nil {
			return errMsg{err: "opencode.db not available"}
		}
		sessions, err := db.ListSessions(opencodeDB)
		if err != nil {
			return errMsg{err: err.Error()}
		}
		tags := make(map[string][]string)
		if managerDB != nil {
			for _, s := range sessions {
				t, _ := db.GetSessionTags(managerDB, s.ID)
				tags[s.ID] = t
			}
		}
		return sessionsRefreshedMsg{sessions: sessions, sessionTags: tags}
	}
}

func (a App) loadSession(sess model.Session) tea.Cmd {
	opencodeDB := a.opencodeDB
	managerDB := a.managerDB
	return func() tea.Msg {
		msgs, err := db.LoadSessionMessages(opencodeDB, sess.ID)
		if err != nil {
			return errMsg{err: err.Error()}
		}
		var meta model.SessionMeta
		if managerDB != nil {
			meta, _ = db.GetSessionMeta(managerDB, sess.ID)
		}
		return sessionLoadedMsg{messages: msgs, meta: meta}
	}
}

func (a App) loadIdeas() tea.Cmd {
	managerDB := a.managerDB
	return func() tea.Msg {
		if managerDB == nil {
			return ideasLoadedMsg{ideas: nil}
		}
		ideas, err := db.ListIdeas(managerDB)
		if err != nil {
			return errMsg{err: err.Error()}
		}
		return ideasLoadedMsg{ideas: ideas}
	}
}

// reloadSessionMeta reloads just the session metadata for display.
func (a App) reloadSessionMeta(sessionID string) tea.Cmd {
	managerDB := a.managerDB
	return func() tea.Msg {
		if managerDB == nil {
			return nil
		}
		meta, err := db.GetSessionMeta(managerDB, sessionID)
		if err != nil {
			return errMsg{err: err.Error()}
		}
		// Return a sessionLoadedMsg with just meta updated (messages stay).
		return sessionMetaRefreshedMsg{meta: meta}
	}
}

// reloadSessionTagsAndList reloads session tags for one session and refreshes the full session list tags.
func (a App) reloadSessionTagsAndList(sessionID string) tea.Cmd {
	managerDB := a.managerDB
	opencodeDB := a.opencodeDB
	return func() tea.Msg {
		if managerDB == nil {
			return nil
		}
		sessions, err := db.ListSessions(opencodeDB)
		if err != nil {
			return errMsg{err: err.Error()}
		}
		tags := make(map[string][]string)
		for _, s := range sessions {
			t, _ := db.GetSessionTags(managerDB, s.ID)
			tags[s.ID] = t
		}
		// Also reload the meta for the updated session so metadata pane reflects new tags.
		meta, _ := db.GetSessionMeta(managerDB, sessionID)
		return sessionAndListRefreshedMsg{
			sessions:    sessions,
			sessionTags: tags,
			meta:        meta,
		}
	}
}

// filterSessionsByIDs returns only sessions whose IDs appear in the given list.
func filterSessionsByIDs(sessions []model.Session, ids []string) []model.Session {
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	var out []model.Session
	for _, s := range sessions {
		if idSet[s.ID] {
			out = append(out, s)
		}
	}
	return out
}

// --- Additional async message types for metadata refresh ---

type sessionMetaRefreshedMsg struct {
	meta model.SessionMeta
}

type sessionAndListRefreshedMsg struct {
	sessions    []model.Session
	sessionTags map[string][]string
	meta        model.SessionMeta
}


// --- View helpers ---

func (a App) buildStatusBar() string {
	var parts []string

	// Focus hint
	switch a.focus {
	case FocusSessionList:
		parts = append(parts, "[↑↓/jk] navigate  [Enter] open  [T] filter by tag")
	case FocusConversation:
		parts = append(parts, "[↑↓/jk] scroll  [ctrl+d/u] page")
	case FocusMetadata:
		parts = append(parts, "[t] tag  [n] note  [i] idea  [I] all ideas")
	}

	parts = append(parts, "[Tab] focus  [r] refresh  [?] help  [q] quit")

	statusText := strings.Join(parts, "  │  ")
	bar := StatusBarStyle.Render(statusText)

	if a.err != "" {
		errText := ErrorStyle.Render("  ✗ " + a.err)
		return bar + errText
	}

	return bar
}

func (a App) overlayHelp(background string) string {
	helpText := `Keybindings

  [Tab / Shift+Tab]  Cycle pane focus
  [r]               Refresh sessions
  [I]               Open Idea Notebook
  [T]               Filter by tag
  [?]               Toggle this help
  [q / Ctrl+C]      Quit

  In Session List:
  [↑ ↓ / j k]       Navigate
  [Enter]           Open session

  In Conversation:
  [↑ ↓ / j k]       Scroll
  [Ctrl+D / Ctrl+U] Page down/up

  In Metadata:
  [t]               Add tag
  [n]               Edit note
  [i]               Capture idea
  [I]               Open Idea Notebook

  In Idea Notebook:
  [e]               Edit idea
  [d]               Delete idea (confirm y/n)
  [q / Esc]         Back to sessions`

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("63")).
		Padding(1, 3).
		Width(56).
		Render(HelpStyle.Render(helpText))

	_ = background // background string not layered — just show centered help
	return lipgloss.Place(
		a.width, a.height,
		lipgloss.Center, lipgloss.Center,
		box,
	)
}
