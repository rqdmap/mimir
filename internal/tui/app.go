package tui

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/local/oc-manager/internal/db"
	"github.com/local/oc-manager/internal/model"
	"github.com/local/oc-manager/internal/tui/panes"
)

// FocusedPane tracks which pane has keyboard focus.
type FocusedPane int

const (
	FocusSessionList FocusedPane = iota
	FocusConversation
	FocusMetadata
)

// AppTab tracks which left-pane tab is active.
type AppTab int

const (
	TabSessions AppTab = iota
	TabIdeas
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

type sessionIdeasRefreshedMsg struct {
	ideas []model.Idea
}

type oneSessionTagsRefreshedMsg struct {
	sessionID string
	tags      []string
	meta      model.SessionMeta
}

// App is the top-level BubbleTea model.
type App struct {
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

	searchMode  bool
	searchQuery string

	loading       bool // true while initial sessions are loading
	hideSubAgents bool // true = hide sessions with parent_id != ""

	showHelp  bool
	activeTab AppTab
}

// NewApp creates an App with both databases wired in.
func NewApp(opencodeDB, managerDB *sql.DB) App {
	a := App{
		focus:         FocusSessionList,
		activeTab:     TabSessions,
		sessionTags:   make(map[string][]string),
		opencodeDB:    opencodeDB,
		managerDB:     managerDB,
		loading:       true,
		hideSubAgents: true,
	}

	// Run idempotent migrations (note → idea, etc.)
	if managerDB != nil {
		_ = db.RunMigrations(managerDB)
	}

	// Initialise panes with zero size; recalcLayout will size them on first WindowSizeMsg.
	a.sessionList = panes.NewSessionList(0, 0)
	a.conversation = panes.NewConversationPane(0, 0)
	a.metadata = panes.NewMetadataPane(0, 0)
	a.ideasView = NewIdeasView(0, 0)
	a.inputMode = NewInputMode(0, 0)
	a.tagFilter = NewTagFilterView(0, 0)

	a.sessionList.SetFocused(true)
	a.sessionList.SetLoading(true)

	return a
}

// --- tea.Model interface ---

func (a App) Init() tea.Cmd {
	return tea.Batch(a.loadInitialSessions(), a.loadIdeas())
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
		cmd := a.recalcLayout()
		return a, cmd

	case tea.KeyMsg:
		return a.handleKey(msg)

	// --- Async result messages ---

	case sessionsRefreshedMsg:
		a.sessions = msg.sessions
		a.sessionTags = msg.sessionTags
		a.loading = false
		a.sessionList.SetLoading(false)
		a.applyFilters()
		a.err = ""
		return a, nil

	case sessionLoadedMsg:
		a.conversation.SetMessages(msg.messages)
		a.metadata.SetSessionMeta(msg.meta)
		a.metadata.SetMessageCount(len(msg.messages))
		a.err = ""
		if a.managerDB != nil && msg.meta.SessionID != "" {
			cmds = append(cmds, a.reloadSessionIdeas(msg.meta.SessionID))
		}
		return a, tea.Batch(cmds...)

	case ideasLoadedMsg:
		cmd := a.ideasView.SetIdeas(msg.ideas)
		return a, cmd

	case sessionIdeasRefreshedMsg:
		a.metadata.SetSessionIdeas(msg.ideas)
		return a, nil

	case errMsg:
		a.err = msg.err
		return a, nil

	// --- InputMode result messages ---

	case InputSavedTagMsg:
		if a.managerDB != nil {
			for _, tag := range msg.Tags {
				_ = db.AddSessionTag(a.managerDB, msg.SessionID, tag)
			}
			cmds = append(cmds, a.reloadOneSessionTags(msg.SessionID))
		}
		return a, tea.Batch(cmds...)

	case InputSavedIdeaMsg:
		if a.managerDB != nil && msg.Content != "" {
			if msg.IdeaID != "" {
				_ = db.UpdateIdea(a.managerDB, msg.IdeaID, msg.Content)
			} else {
				_, _ = db.AddIdea(a.managerDB, msg.Content, msg.SessionID)
				if msg.SessionID != "" {
					cmds = append(cmds, a.reloadSessionIdeas(msg.SessionID))
				}
			}
			// Always refresh idea list
			cmds = append(cmds, a.loadIdeas())
		}
		return a, tea.Batch(cmds...)

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

	case panes.ConvRendererReadyMsg:
		var cmd tea.Cmd
		a.conversation, cmd = a.conversation.Update(msg)
		return a, cmd

	case panes.AddTagMsg:
		a.inputMode.ActivateTag(msg.SessionID)
		return a, nil

	case ExitIdeasMsg:
		// no-op: Ideas is now a tab
		return a, nil

	case DeleteIdeaConfirmedMsg:
		if a.managerDB != nil {
			_ = db.DeleteIdea(a.managerDB, msg.ID)
			cmds = append(cmds, a.loadIdeas())
		}
		return a, tea.Batch(cmds...)

	case IdeaSessionRequestMsg:
		for _, s := range a.sessions {
			if s.ID == msg.SessionID {
				if a.selectedSession == nil || a.selectedSession.ID != msg.SessionID {
					a.selectedSession = &s
					if a.opencodeDB != nil {
						cmds = append(cmds, a.loadSession(s))
					}
				}
				break
			}
		}
		return a, tea.Batch(cmds...)

	case EditIdeaMsg:
		if idea := a.ideasView.SelectedIdea(); idea != nil && idea.ID == msg.ID {
			a.inputMode.ActivateIdeaEdit(idea.ID, idea.Content)
		}
		return a, nil

	case RemoveTagMsg:
		if a.managerDB != nil {
			_ = db.RemoveSessionTag(a.managerDB, msg.SessionID, msg.TagName)
			cmds = append(cmds, a.reloadOneSessionTags(msg.SessionID))
		}
		return a, tea.Batch(cmds...)

	case sessionMetaRefreshedMsg:
		a.metadata.SetSessionMeta(msg.meta)
		return a, nil

	case oneSessionTagsRefreshedMsg:
		a.sessionTags[msg.sessionID] = msg.tags
		a.metadata.SetSessionMeta(msg.meta)
		a.applyFilters()
		return a, nil

	}

	// Delegate to active pane and ideas view as needed
	if a.activeTab == TabIdeas {
		var cmd tea.Cmd
		a.ideasView, cmd = a.ideasView.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Main state — delegate to focused pane
	switch a.focus {
	case FocusSessionList:
		if a.activeTab == TabSessions {
			var cmd tea.Cmd
			a.sessionList, cmd = a.sessionList.Update(msg)
			cmds = append(cmds, cmd)
		}
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

	// Status bar line
	statusBar := a.buildStatusBar()
	contentHeight := a.height - 1
	if contentHeight < 0 {
		contentHeight = 0
	}

	tabHeader := a.renderTabHeader(a.activeTab)
	var leftPane string
	if a.activeTab == TabSessions {
		leftPane = lipgloss.JoinVertical(lipgloss.Left, tabHeader, a.sessionList.View())
	} else {
		leftPane = lipgloss.JoinVertical(lipgloss.Left, tabHeader, a.ideasView.View())
	}

	var mainView string
	switch {
	case a.width >= 120:
		// 3-pane
		mainView = lipgloss.JoinHorizontal(lipgloss.Top,
			leftPane,
			a.conversation.View(),
			a.metadata.View(),
		)
	case a.width >= 80:
		// 2-pane (no metadata)
		mainView = lipgloss.JoinHorizontal(lipgloss.Top,
			leftPane,
			a.conversation.View(),
		)
	default:
		// Single pane — only show focused
		switch a.focus {
		case FocusSessionList:
			mainView = leftPane
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

	// Help overlay — eat all keys; ctrl+c still quits, everything else closes help
	if a.showHelp {
		if key == "ctrl+c" {
			return a, tea.Quit
		}
		a.showHelp = false
		return a, nil
	}

	// Search mode — intercept all keys
	if a.searchMode {
		switch key {
		case "esc":
			a.searchMode = false
			// keep searchQuery — user exits typing but filter stays active
			a.applyFilters()
			return a, nil
		case "backspace":
			if len(a.searchQuery) > 0 {
				a.searchQuery = a.searchQuery[:len(a.searchQuery)-1]
			}
		default:
			if len(key) == 1 { // printable character
				a.searchQuery += key
			}
		}
		a.applyFilters()
		return a, nil
	}

	// Global keys
	switch key {
	case "ctrl+c", KeyQuit:
		return a, tea.Quit

	case "esc":
		if a.searchQuery != "" {
			a.searchQuery = ""
			a.applyFilters()
		}
		return a, nil

	case KeyRefresh:
		a.err = ""
		if a.opencodeDB != nil {
			return a, a.loadInitialSessions()
		}
		return a, nil

	case KeyHelp:
		a.showHelp = !a.showHelp
		return a, nil

	case "[":
		a.activeTab = TabIdeas
		a.setFocus(FocusSessionList)
		return a, nil


	case "]":
		a.activeTab = TabSessions
		a.setFocus(FocusSessionList)
		return a, nil

	case KeyIdeas:
		a.activeTab = TabIdeas
		a.setFocus(FocusSessionList)
		return a, nil


	case "T":
		// Open tag filter overlay
		if a.managerDB != nil {
			tags, _ := db.ListAllTags(a.managerDB)
			a.tagFilter.SetTags(tags)
		}
		a.tagFilter.Activate()
		return a, nil

	case "A":
		a.hideSubAgents = !a.hideSubAgents
		a.applyFilters()
		return a, nil

	case KeySearch:
		a.searchMode = true
		return a, nil

	case "tab":
		a.cycleFocusForward()
		return a, nil

	case "shift+tab":
		a.cycleFocusBackward()
		return a, nil
	}

	// 'i' — capture idea from any pane
	// In Ideas tab: always standalone (no session link)
	// In Sessions tab: linked to the currently selected session
	if key == "i" {
		sessionID := ""
		if a.activeTab == TabSessions && a.selectedSession != nil {
			sessionID = a.selectedSession.ID
		}
		a.inputMode.ActivateIdea(sessionID)
		return a, nil
	}

	// If in Ideas tab and session list pane focused, intercept Enter
	if a.activeTab == TabIdeas && a.focus == FocusSessionList {
		if key == "enter" {
			idea := a.ideasView.SelectedIdea()
			if idea == nil {
				return a, nil
			}
			if idea.SourceSessionID == "" {
				a.err = "No linked session"
				return a, nil
			}
			// Find the session and jump to it
			a.activeTab = TabSessions
			for _, s := range a.sessions {
				if s.ID == idea.SourceSessionID {
					a.selectedSession = &s
					a.sessionList.SelectByID(s.ID)
					a.metadata.ClearSession()
					a.conversation.SetMessages(nil)
					if a.opencodeDB != nil {
						return a, a.loadSession(s)
					}
					return a, nil
				}
			}
			a.err = "Session not found"
			return a, nil
		}
		// For all other keys in Ideas tab, delegate to ideasView
		var cmd tea.Cmd
		a.ideasView, cmd = a.ideasView.Update(msg)
		return a, cmd
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

func (a *App) recalcLayout() tea.Cmd {
	h := a.height - 1 // reserve 1 line for status bar
	if h < 0 {
		h = 0
	}
	var cmds []tea.Cmd
	var leftPaneW int

	switch {
	case a.width >= 120:
		listW := int(float64(a.width) * 0.30)
		convW := int(float64(a.width) * 0.50)
		metaW := a.width - listW - convW
		leftPaneW = listW
		a.sessionList.SetSize(listW, h-1)
		if cmd := a.conversation.SetSize(convW, h); cmd != nil {
			cmds = append(cmds, cmd)
		}
		a.metadata.SetSize(metaW, h)

	case a.width >= 80:
		listW := int(float64(a.width) * 0.35)
		convW := a.width - listW
		leftPaneW = listW
		a.sessionList.SetSize(listW, h-1)
		if cmd := a.conversation.SetSize(convW, h); cmd != nil {
			cmds = append(cmds, cmd)
		}
		a.metadata.SetSize(0, h)

	default:
		leftPaneW = a.width
		a.sessionList.SetSize(a.width, h-1)
		if cmd := a.conversation.SetSize(a.width, h); cmd != nil {
			cmds = append(cmds, cmd)
		}
		a.metadata.SetSize(a.width, h)
	}

	if cmd := a.ideasView.SetSize(leftPaneW, h-1); cmd != nil {
		cmds = append(cmds, cmd)
	}

	a.inputMode.width = a.width
	a.inputMode.height = a.height
	a.tagFilter.SetSize(a.width, a.height)
	return tea.Batch(cmds...)
}

// --- Async DB commands ---

func (a App) loadInitialSessions() tea.Cmd {
	opencodeDB := a.opencodeDB
	managerDB := a.managerDB
	return func() tea.Msg {
		if opencodeDB == nil {
			return errMsg{err: "opencode.db not available"}
		}
		var sessions []model.Session
		var tags map[string][]string
		var sessErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			sessions, sessErr = db.ListSessions(opencodeDB)
		}()
		go func() {
			defer wg.Done()
			if managerDB != nil {
				var err2 error
				tags, err2 = db.ListAllSessionTags(managerDB)
				if err2 != nil {
					tags = make(map[string][]string)
				}
			} else {
				tags = make(map[string][]string)
			}
		}()
		wg.Wait()
		if sessErr != nil {
			return errMsg{err: sessErr.Error()}
		}
		return sessionsRefreshedMsg{sessions: sessions, sessionTags: tags}
	}
}

func (a App) loadSession(sess model.Session) tea.Cmd {
	opencodeDB := a.opencodeDB
	managerDB := a.managerDB
	return func() tea.Msg {
		if opencodeDB == nil {
			return errMsg{err: "opencode.db not available"}
		}
		var msgs []model.Message
		var meta model.SessionMeta
		var msgsErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			msgs, msgsErr = db.LoadSessionMessages(opencodeDB, sess.ID)
		}()
		go func() {
			defer wg.Done()
			if managerDB != nil {
				meta, _ = db.GetSessionMeta(managerDB, sess.ID)
			}
		}()
		wg.Wait()
		if msgsErr != nil {
			return errMsg{err: msgsErr.Error()}
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

// reloadSessionIdeas reloads ideas linked to the given session.
func (a App) reloadSessionIdeas(sessionID string) tea.Cmd {
	managerDB := a.managerDB
	return func() tea.Msg {
		if managerDB == nil {
			return nil
		}
		ideas, err := db.GetIdeasForSession(managerDB, sessionID)
		if err != nil {
			return nil // non-fatal
		}
		return sessionIdeasRefreshedMsg{ideas: ideas}
	}
}

// reloadOneSessionTags reloads tags for a single session in-place and updates the metadata pane.
// Avoids a full session list reload on every tag add/remove.
func (a App) reloadOneSessionTags(sessionID string) tea.Cmd {
	managerDB := a.managerDB
	return func() tea.Msg {
		if managerDB == nil {
			return nil
		}
		tags, _ := db.GetSessionTags(managerDB, sessionID)
		meta, _ := db.GetSessionMeta(managerDB, sessionID)
		return oneSessionTagsRefreshedMsg{sessionID: sessionID, tags: tags, meta: meta}
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

// filterSessionsByTitle returns sessions whose title contains query (case-insensitive).
func filterSessionsByTitle(sessions []model.Session, query string) []model.Session {
	if query == "" {
		return sessions
	}
	q := strings.ToLower(query)
	var out []model.Session
	for _, s := range sessions {
		if strings.Contains(strings.ToLower(s.Title), q) {
			out = append(out, s)
		}
	}
	return out
}

// applyFilters rebuilds the session list applying sub-agent filter and search filter.
func (a *App) applyFilters() {
	sessions := a.sessions
	// Filter sub-agents if requested
	if a.hideSubAgents {
		var filtered []model.Session
		for _, s := range sessions {
			if s.ParentID == "" {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered
	}
	// Apply search filter
	if a.searchQuery != "" {
		sessions = filterSessionsByTitle(sessions, a.searchQuery)
	}
	// Build tags subset
	tags := make(map[string][]string)
	for _, s := range sessions {
		if t, ok := a.sessionTags[s.ID]; ok {
			tags[s.ID] = t
		}
	}
	a.sessionList.SetSessions(sessions, tags)
}

// --- Additional async message types for metadata refresh ---

type sessionMetaRefreshedMsg struct {
	meta model.SessionMeta
}


// --- View helpers ---

// renderTabHeader returns a 1-line tab header for the left pane.
func (a App) renderTabHeader(active AppTab) string {
	inactive := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Padding(0, 1)
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252")).Padding(0, 1)
	sess := inactive.Render("Sessions")
	ideas := inactive.Render("Ideas")
	if active == TabSessions {
		sess = activeStyle.Render("Sessions")
	} else {
		ideas = activeStyle.Render("Ideas")
	}
	return ideas + "  " + sess
}


func (a App) buildStatusBar() string {
	if a.searchMode {
		searchBar := fmt.Sprintf("Search: %s_  │  Searching titles only  │  [Esc] cancel", a.searchQuery)
		return StatusBarStyle.Render(searchBar)
	} else if a.searchQuery != "" {
		filterBar := fmt.Sprintf("Filter: %s  │  [/] edit  [Esc] clear", a.searchQuery)
		return StatusBarStyle.Render(filterBar)
	}

	var parts []string


	// Focus hint
	switch a.focus {
	case FocusSessionList:
		if a.activeTab == TabIdeas {
			parts = append(parts, "[↑↓/jk] navigate  [Enter] jump to session  []] sessions tab")
		} else {
			agentHint := "[A] show sub-agents"
			if !a.hideSubAgents {
				agentHint = "[A] hide sub-agents"
			}
			parts = append(parts, "[↑↓/jk] navigate  [Enter] open  [[] ideas  [i] idea  [T] tags  "+agentHint)
		}
	case FocusConversation:
		parts = append(parts, "[↑↓/jk] scroll  [ctrl+d/u] page")
	case FocusMetadata:
		parts = append(parts, "[t] tag  [i] idea  [[] ideas  []] sessions")
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
  [/]               Search sessions (title only)
	  [[] / []]         Switch Ideas / Sessions tab
  [T]               Filter by tag
  [A]               Toggle sub-agent sessions
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
  [i]               Capture idea

  In Ideas Tab:
  [Enter]           Jump to linked session
  [e]               Edit idea
  [d]               Delete idea (confirm y/n)`

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
