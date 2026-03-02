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
	TabTags
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

type tagsLoadedMsg struct {
	tags   []model.Tag
	counts map[string]int
}

type sessionsBatchLoadedMsg struct {
	sessions    []model.Session
	sessionTags map[string][]string
	loadedCount int
	totalCount  int
	done        bool
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
	tagsView  TagsView

	searchMode  bool
	searchQuery string

	ideasSearchQuery string
	tagsSearchQuery  string
	activeTagFilter  string

	loading       bool // true while initial sessions are loading
	loadedCount   int  // how many sessions have been loaded so far
	totalCount    int  // total session count (from COUNT query)
	hideSubAgents bool // true = hide sessions with parent_id != ""

	showHelp     bool
	activeTab    AppTab
	glamourStyle string
}

// NewApp creates an App with both databases wired in.
func NewApp(opencodeDB, managerDB *sql.DB, glamourStyle string) App {
	a := App{
		focus:         FocusSessionList,
		activeTab:     TabIdeas,
		sessionTags:   make(map[string][]string),
		opencodeDB:    opencodeDB,
		managerDB:     managerDB,
		loading:       true,
		hideSubAgents: true,
		glamourStyle:  glamourStyle,
	}

	// Run idempotent migrations (note → idea, etc.)
	if managerDB != nil {
		_ = db.RunMigrations(managerDB)
	}

	// Initialise panes with zero size; recalcLayout will size them on first WindowSizeMsg.
	a.sessionList = panes.NewSessionList(0, 0)
	a.conversation = panes.NewConversationPane(0, 0, glamourStyle)
	a.metadata = panes.NewMetadataPane(0, 0)
	a.ideasView = NewIdeasView(0, 0, glamourStyle)
	a.inputMode = NewInputMode(0, 0)
	a.tagsView = NewTagsView(0, 0)

	a.sessionList.SetFocused(true)
	a.sessionList.SetLoading(true)

	return a
}

// --- tea.Model interface ---

func (a App) Init() tea.Cmd {
	return tea.Batch(a.startProgressiveLoad(), a.loadIdeas(), a.loadTagsWithCounts())
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	if a.inputMode.IsActive() {
		var cmd tea.Cmd
		a.inputMode, cmd = a.inputMode.Update(msg)
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

	case sessionsBatchLoadedMsg:
		a.sessions = append(a.sessions, msg.sessions...)
		for k, v := range msg.sessionTags {
			a.sessionTags[k] = v
		}
		a.loadedCount = msg.loadedCount
		a.totalCount = msg.totalCount
		a.applyFilters()
		a.err = ""
		if msg.done {
			a.loading = false
			a.sessionList.SetLoading(false)
			return a, nil
		}
		return a, a.loadNextBatch(msg.loadedCount, msg.totalCount)

	case sessionsRefreshedMsg:
		a.sessions = msg.sessions
		a.sessionTags = msg.sessionTags
		a.loading = false
		a.sessionList.SetLoading(false)
		a.applyFilters()
		a.err = ""
		return a, nil

	case sessionLoadedMsg:
		cmd := a.conversation.SetMessages(msg.messages, msg.meta.SessionID)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
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

	case tagsLoadedMsg:
		a.tagsView.SetTags(msg.tags, msg.counts)
		return a, nil

	case sessionIdeasRefreshedMsg:
		a.metadata.SetSessionIdeas(msg.ideas)
		return a, nil

	case errMsg:
		a.err = msg.err
		return a, nil

	case InputTagsUpdatedMsg:
		if a.managerDB != nil {
			for _, tag := range msg.AddTags {
				_ = db.AddSessionTag(a.managerDB, msg.SessionID, tag)
			}
			for _, tag := range msg.RemoveTags {
				_ = db.RemoveSessionTag(a.managerDB, msg.SessionID, tag)
			}
			cmds = append(cmds, a.reloadOneSessionTags(msg.SessionID))
			cmds = append(cmds, a.loadTagsWithCounts())
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
			cmds = append(cmds, a.loadIdeas())
		}
		return a, tea.Batch(cmds...)

	case InputCancelledMsg:
		return a, nil

	case TagFilterByNameMsg:
		a.activeTagFilter = msg.TagName
		a.activeTab = TabSessions
		a.setFocus(FocusSessionList)
		a.applyFilters()
		return a, nil

	case panes.SessionSelectedMsg:
		a.selectedSession = &msg.Session
		a.metadata.ClearSession()
		a.conversation.SetMessages(nil, "")
		if a.opencodeDB != nil {
			cmds = append(cmds, a.loadSession(msg.Session))
		}
		return a, tea.Batch(cmds...)

	case panes.ConvRendererReadyMsg:
		var cmd tea.Cmd
		a.conversation, cmd = a.conversation.Update(msg)
		return a, cmd

	case panes.AsyncConvRenderMsg:
		var cmd tea.Cmd
		a.conversation, cmd = a.conversation.Update(msg)
		return a, cmd

	case ExitIdeasMsg:
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
			cmds = append(cmds, a.loadTagsWithCounts())
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

	if a.activeTab == TabIdeas {
		var cmd tea.Cmd
		a.ideasView, cmd = a.ideasView.Update(msg)
		cmds = append(cmds, cmd)
	} else if a.activeTab == TabTags {
		var cmd tea.Cmd
		a.tagsView, cmd = a.tagsView.Update(msg)
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

	// Status bar line
	statusBar := a.buildStatusBar()
	contentHeight := a.height - 1
	if contentHeight < 0 {
		contentHeight = 0
	}

	tabHeader := a.renderTabHeader(a.activeTab)
	var leftPane string
	switch a.activeTab {
	case TabSessions:
		leftPane = lipgloss.JoinVertical(lipgloss.Left, tabHeader, a.sessionList.View())
	case TabIdeas:
		leftPane = lipgloss.JoinVertical(lipgloss.Left, tabHeader, a.ideasView.View())
	case TabTags:
		leftPane = lipgloss.JoinVertical(lipgloss.Left, tabHeader, a.tagsView.View())
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
			a.applyFilters()
			return a, nil
		case "backspace":
			switch a.activeTab {
			case TabSessions:
				if len(a.searchQuery) > 0 {
					a.searchQuery = a.searchQuery[:len(a.searchQuery)-1]
				}
			case TabIdeas:
				if len(a.ideasSearchQuery) > 0 {
					a.ideasSearchQuery = a.ideasSearchQuery[:len(a.ideasSearchQuery)-1]
				}
			case TabTags:
				if len(a.tagsSearchQuery) > 0 {
					a.tagsSearchQuery = a.tagsSearchQuery[:len(a.tagsSearchQuery)-1]
				}
			}
		default:
			if len(key) == 1 {
				switch a.activeTab {
				case TabSessions:
					a.searchQuery += key
				case TabIdeas:
					a.ideasSearchQuery += key
				case TabTags:
					a.tagsSearchQuery += key
				}
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
		switch a.activeTab {
		case TabSessions:
			a.searchQuery = ""
			a.activeTagFilter = ""
		case TabIdeas:
			a.ideasSearchQuery = ""
		case TabTags:
			a.tagsSearchQuery = ""
		}
		a.applyFilters()
		return a, nil

	case KeyRefresh:
		switch a.activeTab {
		case TabSessions:
			a.err = ""
			a.sessions = nil
			a.loadedCount = 0
			a.totalCount = 0
			a.loading = true
			a.sessionList.SetLoading(true)
			if a.opencodeDB != nil {
				return a, a.startProgressiveLoad()
			}
		case TabIdeas:
			return a, a.loadIdeas()
		case TabTags:
			return a, a.loadTagsWithCounts()
		}
		return a, nil

	case KeyHelp:
		a.showHelp = !a.showHelp
		return a, nil

	case "[":
		a.activeTab = (a.activeTab + 1) % 3
		a.setFocus(FocusSessionList)
		return a, nil

	case "]":
		a.activeTab = (a.activeTab + 2) % 3
		a.setFocus(FocusSessionList)
		return a, nil

	case KeyIdeas:
		a.activeTab = TabIdeas
		a.setFocus(FocusSessionList)
		return a, nil

	case "T":
		a.activeTab = TabTags
		a.setFocus(FocusSessionList)
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

	if key == "t" {
		if a.activeTab == TabSessions && a.selectedSession != nil {
			existingTags := a.sessionTags[a.selectedSession.ID]
			a.inputMode.ActivateTag(a.selectedSession.ID, a.selectedSession.Title, existingTags)
			return a, nil
		}
	}

	// 'i' — capture idea from any pane
	// In Ideas tab: always standalone (no session link)
	// In Sessions tab: linked to the currently selected session
	if key == "i" {
		sessionID := ""
		sessionTitle := ""
		if a.activeTab == TabSessions && a.selectedSession != nil {
			sessionID = a.selectedSession.ID
			sessionTitle = a.selectedSession.Title
		}
		a.inputMode.ActivateIdea(sessionID, sessionTitle)
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
					a.conversation.SetMessages(nil, "")
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

	if a.activeTab == TabTags && a.focus == FocusSessionList {
		var cmd tea.Cmd
		a.tagsView, cmd = a.tagsView.Update(msg)
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
	a.tagsView.SetSize(leftPaneW, h-1)
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

func (a App) startProgressiveLoad() tea.Cmd {
	opencodeDB := a.opencodeDB
	managerDB := a.managerDB
	return func() tea.Msg {
		if opencodeDB == nil {
			return errMsg{err: "opencode.db not available"}
		}
		const batchSize = 100
		var total int
		var sessions []model.Session
		var totalErr, sessErr error
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			total, totalErr = db.CountSessions(opencodeDB)
		}()
		go func() {
			defer wg.Done()
			sessions, sessErr = db.ListSessionsPage(opencodeDB, batchSize, 0)
		}()
		wg.Wait()
		if totalErr != nil {
			return errMsg{err: totalErr.Error()}
		}
		if sessErr != nil {
			return errMsg{err: sessErr.Error()}
		}
		tags := make(map[string][]string)
		if managerDB != nil {
			allTags, err2 := db.ListAllSessionTags(managerDB)
			if err2 == nil {
				for _, s := range sessions {
					if t, ok := allTags[s.ID]; ok {
						tags[s.ID] = t
					}
				}
			}
		}
		loaded := len(sessions)
		return sessionsBatchLoadedMsg{
			sessions:    sessions,
			sessionTags: tags,
			loadedCount: loaded,
			totalCount:  total,
			done:        loaded >= total,
		}
	}
}

func (a App) loadNextBatch(offset, total int) tea.Cmd {
	opencodeDB := a.opencodeDB
	managerDB := a.managerDB
	return func() tea.Msg {
		if opencodeDB == nil {
			return errMsg{err: "opencode.db not available"}
		}
		const batchSize = 100
		sessions, err := db.ListSessionsPage(opencodeDB, batchSize, offset)
		if err != nil {
			return errMsg{err: err.Error()}
		}
		tags := make(map[string][]string)
		if managerDB != nil {
			allTags, err2 := db.ListAllSessionTags(managerDB)
			if err2 == nil {
				for _, s := range sessions {
					if t, ok := allTags[s.ID]; ok {
						tags[s.ID] = t
					}
				}
			}
		}
		loaded := offset + len(sessions)
		return sessionsBatchLoadedMsg{
			sessions:    sessions,
			sessionTags: tags,
			loadedCount: loaded,
			totalCount:  total,
			done:        loaded >= total,
		}
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
		if meta.SessionID == "" {
			meta.SessionID = sess.ID
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

func (a App) loadTagsWithCounts() tea.Cmd {
	managerDB := a.managerDB
	return func() tea.Msg {
		if managerDB == nil {
			return tagsLoadedMsg{tags: nil, counts: make(map[string]int)}
		}
		tags, counts, err := db.ListTagsWithSessionCounts(managerDB)
		if err != nil {
			return errMsg{err: err.Error()}
		}
		return tagsLoadedMsg{tags: tags, counts: counts}
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

func filterSessionsByTagName(sessions []model.Session, sessionTags map[string][]string, tagName string) []model.Session {
	var out []model.Session
	for _, s := range sessions {
		for _, t := range sessionTags[s.ID] {
			if t == tagName {
				out = append(out, s)
				break
			}
		}
	}
	return out
}

// applyFilters rebuilds the session list applying sub-agent filter and search filter.
func (a *App) applyFilters() {
	sessions := a.sessions
	if a.hideSubAgents {
		var filtered []model.Session
		for _, s := range sessions {
			if s.ParentID == "" {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered
	}
	if a.searchQuery != "" {
		sessions = filterSessionsByTitle(sessions, a.searchQuery)
	}
	if a.activeTagFilter != "" {
		sessions = filterSessionsByTagName(sessions, a.sessionTags, a.activeTagFilter)
	}
	tags := make(map[string][]string)
	for _, s := range sessions {
		if t, ok := a.sessionTags[s.ID]; ok {
			tags[s.ID] = t
		}
	}
	a.sessionList.SetSessions(sessions, tags)

	a.ideasView.SetFilter(a.ideasSearchQuery)
	a.tagsView.SetFilter(a.tagsSearchQuery)
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
	tags := inactive.Render("Tags")
	switch active {
	case TabSessions:
		sess = activeStyle.Render("Sessions")
	case TabIdeas:
		ideas = activeStyle.Render("Ideas")
	case TabTags:
		tags = activeStyle.Render("Tags")
	}
	return ideas + "  " + sess + "  " + tags
}

func (a App) buildStatusBar() string {
	if a.searchMode {
		var q string
		var hint string
		switch a.activeTab {
		case TabSessions:
			q = a.searchQuery
			hint = "session titles"
		case TabIdeas:
			q = a.ideasSearchQuery
			hint = "ideas"
		case TabTags:
			q = a.tagsSearchQuery
			hint = "tags"
		}
		searchBar := fmt.Sprintf("Search: %s_  │  Filtering %s  │  [Esc] done", q, hint)
		return StatusBarStyle.Render(searchBar)
	}

	switch a.activeTab {
	case TabSessions:
		var filterParts []string
		if a.activeTagFilter != "" {
			filterParts = append(filterParts, fmt.Sprintf("Tag: #%s", a.activeTagFilter))
		}
		if a.searchQuery != "" {
			filterParts = append(filterParts, fmt.Sprintf("Search: %s", a.searchQuery))
		}
		if len(filterParts) > 0 {
			filterBar := strings.Join(filterParts, "  │  ") + "  │  [/] edit  [Esc] clear"
			return StatusBarStyle.Render(filterBar)
		}
	case TabIdeas:
		if a.ideasSearchQuery != "" {
			filterBar := fmt.Sprintf("Filter: %s  │  [/] edit  [Esc] clear", a.ideasSearchQuery)
			return StatusBarStyle.Render(filterBar)
		}
	case TabTags:
		if a.tagsSearchQuery != "" {
			filterBar := fmt.Sprintf("Filter: %s  │  [/] edit  [Esc] clear", a.tagsSearchQuery)
			return StatusBarStyle.Render(filterBar)
		}
	}

	if a.loading && a.totalCount > 0 {
		bar := StatusBarStyle.Render(fmt.Sprintf("Loading %d/%d sessions...", a.loadedCount, a.totalCount))
		if a.err != "" {
			return bar + ErrorStyle.Render("  ✗ "+a.err)
		}
		return bar
	}

	var parts []string

	switch a.focus {
	case FocusSessionList:
		switch a.activeTab {
		case TabIdeas:
			parts = append(parts, "[↑↓/jk] navigate  [Enter] jump to session  [/] search  [Esc] clear  [[]]] switch tab")
		case TabTags:
			parts = append(parts, "[↑↓/jk] navigate  [Enter] filter sessions  [/] search  [Esc] clear  [[]]] switch tab")
		default:
			agentHint := "[A] show sub-agents"
			if !a.hideSubAgents {
				agentHint = "[A] hide sub-agents"
			}
			parts = append(parts, "[↑↓/jk] navigate  [Enter] open  [i] idea  [t] tag  [/] search  [Esc] clear  "+agentHint)
		}
	case FocusConversation:
		parts = append(parts, "[↑↓/jk] scroll  [ctrl+d/u] page")
	case FocusMetadata:
		parts = append(parts, "[[] ideas  []] sessions  [T] tags")
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
  [[] / []]         Cycle tabs (Ideas / Sessions / Tags)
  [T]               Switch to Tags tab
  [A]               Toggle sub-agent sessions
  [?]               Toggle this help
  [q / Ctrl+C]      Quit

  In Session List:
  [↑ ↓ / j k]       Navigate
  [Enter]           Open session
  [i]               Capture idea (linked to session)
  [t]               Add tag to session

  In Conversation:
  [↑ ↓ / j k]       Scroll
  [Ctrl+D / Ctrl+U] Page down/up

  In Ideas Tab:
  [Enter]           Jump to linked session
  [e]               Edit idea
  [d]               Delete idea (confirm y/n)

  In Tags Tab:
  [Enter]           Filter sessions by tag`

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
