package tui

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/local/oc-manager/internal/db"
	"github.com/local/oc-manager/internal/export"
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
	TabStats
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

// confirmCreateDirMsg is sent when doExport detects the target directory
// does not exist, so we can ask the user whether to create it.
type confirmCreateDirMsg struct {
	dir  string
	sess model.Session
	msgs []model.Message
	tags []string
	opts export.Options
}

type clearFlashMsg struct{}

type flashMsg struct {
	text    string
	isError bool
}

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

type statsDataLoadedMsg struct {
	period        model.StatsPeriod
	models        []model.ModelStat
	agents        []model.AgentStat
	daily         []model.DailyPoint
	modelDaily    []model.ModelDailyPoint
	userDaily     []model.UserDailyPoint
	userRequests  int
	humanRequests int
	totalSessions int
}

type sessionUsageLoadedMsg struct {
	sessionID string
	usage     model.SessionUsage
}

type Options struct {
	AutoPreview         bool
	Ratio               [3]int
	TabOrder            []string
	ExportDir           string
	TriliumURL          string
	TriliumToken        string
	TriliumParentNoteID string
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
	statsView    StatsView

	opencodeDB *sql.DB
	managerDB  *sql.DB

	width  int
	height int
	err    string

	inputMode           InputMode
	tagsView            TagsView
	exportOverlay       ExportOverlay
	exportDir           string
	triliumURL          string
	triliumToken        string
	triliumParentNoteID string
	flash               flashMsg
	searchMode          bool
	searchQuery         string

	confirmCreateDir  bool
	pendingCreateDir  string
	pendingExportSess model.Session
	pendingExportMsgs []model.Message
	pendingExportTags []string
	pendingExportOpts export.Options

	ideasSearchQuery string
	tagsSearchQuery  string
	statsSearchQuery string
	activeTagFilter  string

	loading       bool
	loadedCount   int
	totalCount    int
	hideSubAgents bool

	showHelp    bool
	showWelcome bool
	activeTab   AppTab
	theme       panes.Theme

	autoPreview  bool
	ratio        [3]int
	tabOrder     []string
	ideaShowConv bool
}

func tabNameToAppTab(name string) AppTab {
	switch name {
	case "sessions":
		return TabSessions
	case "tags":
		return TabTags
	case "stats":
		return TabStats
	default:
		return TabIdeas
	}
}

func (a App) nextTab(delta int) AppTab {
	n := len(a.tabOrder)
	if n == 0 {
		return a.activeTab
	}
	cur := 0
	for i, name := range a.tabOrder {
		if tabNameToAppTab(name) == a.activeTab {
			cur = i
			break
		}
	}
	next := ((cur+delta)%n + n) % n
	return tabNameToAppTab(a.tabOrder[next])
}

// NewApp creates an App with both databases wired in.
func NewApp(opencodeDB, managerDB *sql.DB, theme panes.Theme, opts Options) App {
	defaultTab := TabIdeas
	if len(opts.TabOrder) > 0 {
		defaultTab = tabNameToAppTab(opts.TabOrder[0])
	}
	a := App{
		focus:               FocusSessionList,
		activeTab:           defaultTab,
		sessionTags:         make(map[string][]string),
		opencodeDB:          opencodeDB,
		managerDB:           managerDB,
		loading:             true,
		hideSubAgents:       true,
		theme:               theme,
		autoPreview:         opts.AutoPreview,
		ratio:               opts.Ratio,
		tabOrder:            opts.TabOrder,
		exportDir:           opts.ExportDir,
		triliumURL:          opts.TriliumURL,
		triliumToken:        opts.TriliumToken,
		triliumParentNoteID: opts.TriliumParentNoteID,
	}

	// First-launch: show welcome overlay with common commands
	if managerDB != nil {
		val, _ := db.GetSetting(managerDB, "onboarding_done")
		if val == "" {
			a.showWelcome = true
			_ = db.SetSetting(managerDB, "onboarding_done", "1")
		}
	}

	// Initialise panes with zero size; recalcLayout will size them on first WindowSizeMsg.
	a.sessionList = panes.NewSessionList(0, 0, theme)
	a.conversation = panes.NewConversationPane(0, 0, theme)
	a.metadata = panes.NewMetadataPane(0, 0, theme)
	a.ideasView = NewIdeasView(0, 0, theme)
	a.inputMode = NewInputMode(0, 0, theme)
	a.tagsView = NewTagsView(0, 0, theme)
	a.statsView = newStatsView(theme)
	a.exportOverlay = NewExportOverlay(0, 0, theme, opts.TriliumURL != "" && opts.TriliumToken != "")

	a.sessionList.SetFocused(true)
	a.sessionList.SetLoading(true)

	return a
}

// --- tea.Model interface ---

func (a App) Init() tea.Cmd {
	return tea.Batch(a.startProgressiveLoad(), a.loadIdeas(), a.loadTagsWithCounts(), tea.EnableBracketedPaste)
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Export overlay takes priority over inputMode
	if a.exportOverlay.IsActive() {
		var cmd tea.Cmd
		a.exportOverlay, cmd = a.exportOverlay.Update(msg)
		return a, cmd
	}

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

	case tea.MouseMsg:
		if msg.Type == tea.MouseWheelDown && a.focus == FocusSessionList && a.activeTab == TabSessions {
			a.conversation.ScrollLineDown(3)
			return a, nil
		}
		if msg.Type == tea.MouseWheelUp && a.focus == FocusSessionList && a.activeTab == TabSessions {
			a.conversation.ScrollLineUp(3)
			return a, nil
		}
		var cmd tea.Cmd
		a.conversation, cmd = a.conversation.Update(msg)
		return a, cmd

	case sessionsBatchLoadedMsg:
		// Deduplicate by ID: ORDER BY time_updated DESC is unstable when
		// opencode writes to the DB mid-load, causing sessions near batch
		// boundaries to appear in multiple batches.
		seenIDs := make(map[string]bool, len(a.sessions))
		for _, s := range a.sessions {
			seenIDs[s.ID] = true
		}
		for _, s := range msg.sessions {
			if !seenIDs[s.ID] {
				seenIDs[s.ID] = true
				a.sessions = append(a.sessions, s)
			}
		}
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
		if a.activeTab != TabIdeas || a.ideaShowConv {
			cmd := a.conversation.SetMessages(msg.messages, msg.meta.SessionID)
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			a.metadata.SetSessionMeta(msg.meta, a.selectedSession != nil && a.selectedSession.ParentID != "")
			if a.selectedSession != nil {
				a.metadata.SetSessionTitle(a.selectedSession.Title)
			}
		}
		a.err = ""
		if a.managerDB != nil && msg.meta.SessionID != "" {
			cmds = append(cmds, a.reloadSessionIdeas(msg.meta.SessionID))
		}
		if a.opencodeDB != nil && msg.meta.SessionID != "" {
			cmds = append(cmds, a.loadSessionUsageCmd(msg.meta.SessionID))
		}
		return a, tea.Batch(cmds...)

	case ideasLoadedMsg:
		cmd := a.ideasView.SetIdeas(msg.ideas)
		return a, cmd

	case tagsLoadedMsg:
		a.tagsView.SetTags(msg.tags, msg.counts)
		a.tagsView.SetSessions(a.sessions, a.sessionTags)
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
		a.setFocus(FocusConversation)
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

	case IdeaSelectedMsg:
		if a.activeTab != TabIdeas {
			return a, nil
		}
		a.ideaShowConv = false
		a.conversation.SetIdeaContent(msg.Idea.Content)
		a.metadata.SetIdeaMeta(msg.Idea, "")
		if msg.Idea.SourceSessionID != "" {
			for _, s := range a.sessions {
				if s.ID == msg.Idea.SourceSessionID {
					a.metadata.SetIdeaMeta(msg.Idea, s.Title)
					break
				}
			}
		}
		return a, nil

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

	case editorDoneMsg:
		if msg.err != nil {
			a.err = fmt.Sprintf("editor: %v", msg.err)
			return a, nil
		}
		if msg.content != "" && a.managerDB != nil {
			_ = db.UpdateIdea(a.managerDB, msg.ideaID, msg.content)
			cmds = append(cmds, a.loadIdeas())
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

	case DeleteTagMsg:
		if a.managerDB != nil {
			if err := db.DeleteTag(a.managerDB, msg.TagName); err != nil {
				a.err = fmt.Sprintf("delete tag: %v", err)
			} else {
				if a.activeTagFilter == msg.TagName {
					a.activeTagFilter = ""
					a.applyFilters()
				}
				cmds = append(cmds, a.loadTagsWithCounts())
			}
		}
		return a, tea.Batch(cmds...)

	case ManageTagRemoveMsg:
		if a.managerDB != nil {
			_ = db.RemoveSessionTag(a.managerDB, msg.SessionID, msg.TagName)
			cmds = append(cmds, a.reloadOneSessionTags(msg.SessionID))
			cmds = append(cmds, a.loadTagsWithCounts())
		}
		return a, tea.Batch(cmds...)

	case ActivateRenameMsg:
		a.inputMode.ActivateRename(msg.TagName)
		return a, nil

	case InputRenamedTagMsg:
		if a.managerDB != nil {
			if err := db.RenameTag(a.managerDB, msg.OldName, msg.NewName); err != nil {
				a.err = fmt.Sprintf("rename tag: %v", err)
			} else {
				if a.activeTagFilter == msg.OldName {
					a.activeTagFilter = msg.NewName
				}
				for sid, tags := range a.sessionTags {
					for i, t := range tags {
						if t == msg.OldName {
							a.sessionTags[sid][i] = msg.NewName
						}
					}
				}
				a.applyFilters()
				cmds = append(cmds, a.loadTagsWithCounts())
			}
		}
		return a, tea.Batch(cmds...)

	case ManageSessionJumpMsg:
		a.selectedSession = &msg.Session
		a.activeTab = TabSessions
		a.sessionList.SelectByID(msg.Session.ID)
		a.metadata.ClearSession()
		a.conversation.SetMessages(nil, "")
		if a.opencodeDB != nil {
			cmds = append(cmds, a.loadSession(msg.Session))
		}
		return a, tea.Batch(cmds...)

	case ManageTagExitMsg:
		return a, nil

	case sessionMetaRefreshedMsg:
		a.metadata.SetSessionMeta(msg.meta, a.selectedSession != nil && a.selectedSession.ParentID != "")
		if a.selectedSession != nil {
			a.metadata.SetSessionTitle(a.selectedSession.Title)
		}
		return a, nil

	case oneSessionTagsRefreshedMsg:
		a.sessionTags[msg.sessionID] = msg.tags
		a.metadata.SetSessionMeta(msg.meta, a.selectedSession != nil && a.selectedSession.ParentID != "")
		if a.selectedSession != nil {
			a.metadata.SetSessionTitle(a.selectedSession.Title)
		}
		a.applyFilters()
		return a, nil

	case ExportConfirmedMsg:
		if a.selectedSession == nil {
			a.flash = flashMsg{text: "No session selected", isError: true}
			return a, nil
		}
		sess := *a.selectedSession
		msgs := a.conversation.Messages()
		tags := a.sessionTags[sess.ID]
		opts := export.Options{
			IncludeMetadata:  msg.IncludeMetadata,
			IncludeText:      msg.IncludeText,
			IncludeTool:      msg.IncludeTool,
			IncludeReasoning: msg.IncludeReasoning,
		}
		dir := a.exportDir
		if msg.Destination == "trilium" {
			return a, a.doExportTrilium(sess, msgs, tags, opts)
		}
		return a, a.doExport(sess, msgs, tags, opts, dir)
	case ExportCancelledMsg:
		return a, nil

	case ExportDoneMsg:
		if msg.Err != nil {
			a.flash = flashMsg{text: msg.Err.Error(), isError: true}
		} else {
			a.flash = flashMsg{text: "Exported → " + msg.Path}
		}
		return a, tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
			return clearFlashMsg{}
		})

	case confirmCreateDirMsg:
		a.confirmCreateDir = true
		a.pendingCreateDir = msg.dir
		a.pendingExportSess = msg.sess
		a.pendingExportMsgs = msg.msgs
		a.pendingExportTags = msg.tags
		a.pendingExportOpts = msg.opts
		return a, nil

	case clearFlashMsg:
		a.flash = flashMsg{}
		return a, nil

	case statsDataLoadedMsg:
		a.statsView.SetData(msg.period, msg.models, msg.agents, msg.daily, msg.modelDaily, msg.userDaily, msg.userRequests, msg.humanRequests, msg.totalSessions)
		return a, nil

	case sessionUsageLoadedMsg:
		if a.selectedSession != nil && msg.sessionID == a.selectedSession.ID {
			a.metadata.SetUsage(msg.usage)
		}
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
	// Export overlay takes full screen when active.
	if a.exportOverlay.IsActive() {
		return a.exportOverlay.View()
	}

	// InputMode overlay takes full screen when active.
	if a.inputMode.IsActive() {
		return a.inputMode.View()
	}

	// Confirm-create-directory overlay
	if a.confirmCreateDir {
		return a.viewConfirmCreateDir()
	}

	// Status bar (2 lines)
	statusBar := a.buildStatusBar()
	contentHeight := a.height - 2
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
	case TabStats:
		statsContent := a.statsView.View()
		full := lipgloss.JoinVertical(lipgloss.Left, a.renderTabHeader(TabStats), statsContent, statusBar)
		if a.showHelp {
			full = a.overlayHelp(full)
		}
		if a.flash.text != "" {
			full = a.overlayFlash(full)
		}
		return full
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

	full := lipgloss.JoinVertical(lipgloss.Left, mainView, statusBar)
	if a.showWelcome {
		full = a.overlayWelcome(full)
	}
	if a.showHelp {
		full = a.overlayHelp(full)
	}
	if a.flash.text != "" {
		full = a.overlayFlash(full)
	}
	return full
}

// --- Key handling ---

func (a App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if a.flash.text != "" {
		if key == "ctrl+c" {
			return a, tea.Quit
		}
		a.flash = flashMsg{}
		return a, nil
	}

	// Help overlay
	if a.showHelp {
		if key == "ctrl+c" {
			return a, tea.Quit
		}
		a.showHelp = false
		return a, nil
	}

	// Welcome overlay
	if a.showWelcome {
		if key == "ctrl+c" {
			return a, tea.Quit
		}
		a.showWelcome = false
		return a, nil
	}

	// Confirm-create-directory dialog
	if a.confirmCreateDir {
		switch key {
		case "ctrl+c":
			return a, tea.Quit
		case "y", "Y", "enter":
			dir := a.pendingCreateDir
			sess := a.pendingExportSess
			msgs := a.pendingExportMsgs
			tags := a.pendingExportTags
			opts := a.pendingExportOpts
			a.confirmCreateDir = false
			a.pendingCreateDir = ""
			return a, func() tea.Msg {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return ExportDoneMsg{Err: fmt.Errorf("create directory: %w", err)}
				}
				md := export.RenderMarkdown(sess, msgs, tags, opts)
				slug := export.Slugify(sess.Title)
				path := filepath.Join(dir, slug+".md")
				if err := os.WriteFile(path, []byte(md), 0644); err != nil {
					return ExportDoneMsg{Err: err}
				}
				return ExportDoneMsg{Path: path}
			}
		default:
			// Any other key cancels
			a.confirmCreateDir = false
			a.pendingCreateDir = ""
			a.flash = flashMsg{text: "Export cancelled"}
			return a, tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
				return clearFlashMsg{}
			})
		}
	}

	// Conversation search mode — forward ALL keys directly to the pane
	if a.focus == FocusConversation && a.conversation.SearchMode() {
		var cmd tea.Cmd
		a.conversation, cmd = a.conversation.Update(msg)
		return a, cmd
	}

	// Search mode — intercept all keys
	if a.searchMode {
		// Bracketed paste: msg.Paste == true, content in msg.Runes
		if msg.Paste {
			pastedText := string(msg.Runes)
			switch a.activeTab {
			case TabSessions:
				a.searchQuery += pastedText
			case TabIdeas:
				a.ideasSearchQuery += pastedText
			case TabTags:
				a.tagsSearchQuery += pastedText
			case TabStats:
				a.statsSearchQuery += pastedText
			}
			a.applyFilters()
			return a, nil
		}
		switch key {
		case "esc":
			a.searchMode = false
			a.applyFilters()
			return a, nil
		case "ctrl+u":
			switch a.activeTab {
			case TabSessions:
				a.searchQuery = ""
			case TabIdeas:
				a.ideasSearchQuery = ""
			case TabTags:
				a.tagsSearchQuery = ""
			case TabStats:
				a.statsSearchQuery = ""
			}
		case "ctrl+w":
			switch a.activeTab {
			case TabSessions:
				a.searchQuery = deleteLastWord(a.searchQuery)
			case TabIdeas:
				a.ideasSearchQuery = deleteLastWord(a.ideasSearchQuery)
			case TabTags:
				a.tagsSearchQuery = deleteLastWord(a.tagsSearchQuery)
			case TabStats:
				a.statsSearchQuery = deleteLastWord(a.statsSearchQuery)
			}
		case "backspace":
			switch a.activeTab {
			case TabSessions:
				if len([]rune(a.searchQuery)) > 0 {
					runes := []rune(a.searchQuery)
					a.searchQuery = string(runes[:len(runes)-1])
				}
			case TabIdeas:
				if len([]rune(a.ideasSearchQuery)) > 0 {
					runes := []rune(a.ideasSearchQuery)
					a.ideasSearchQuery = string(runes[:len(runes)-1])
				}
			case TabTags:
				if len([]rune(a.tagsSearchQuery)) > 0 {
					runes := []rune(a.tagsSearchQuery)
					a.tagsSearchQuery = string(runes[:len(runes)-1])
				}
			case TabStats:
				if len([]rune(a.statsSearchQuery)) > 0 {
					runes := []rune(a.statsSearchQuery)
					a.statsSearchQuery = string(runes[:len(runes)-1])
				}
			}
		default:
			if msg.Type == tea.KeyRunes {
				switch a.activeTab {
				case TabSessions:
					a.searchQuery += string(msg.Runes)
				case TabIdeas:
					a.ideasSearchQuery += string(msg.Runes)
				case TabTags:
					a.tagsSearchQuery += string(msg.Runes)
				case TabStats:
					a.statsSearchQuery += string(msg.Runes)
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
		if a.focus == FocusConversation {
			a.setFocus(FocusSessionList)
			return a, nil
		}
		switch a.activeTab {
		case TabSessions:
			a.searchQuery = ""
			a.activeTagFilter = ""
		case TabIdeas:
			a.ideasSearchQuery = ""
		case TabTags:
			if a.tagsView.manageMode || a.tagsView.confirmDeleteTag || a.tagsView.manageConfirmDel {
				var cmd tea.Cmd
				a.tagsView, cmd = a.tagsView.Update(msg)
				a.applyFilters()
				return a, cmd
			}
			a.tagsSearchQuery = ""
		case TabStats:
			a.statsSearchQuery = ""
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

	case KeyExport:
		if a.activeTab != TabSessions {
			return a, nil
		}
		if a.selectedSession != nil {
			a.flash = flashMsg{}
			a.exportOverlay.SetSize(a.width, a.height)
			a.exportOverlay.Activate()
		} else {
			a.flash = flashMsg{text: "No session selected", isError: true}
		}
		return a, nil

	case KeyHelp:
		a.showHelp = !a.showHelp
		return a, nil

	case "[":
		a.activeTab = a.nextTab(-1)
		a.setFocus(FocusSessionList)
		return a, a.onTabSwitch()

	case "]":
		a.activeTab = a.nextTab(1)
		a.setFocus(FocusSessionList)
		return a, a.onTabSwitch()

	case KeyIdeas:
		a.activeTab = TabIdeas
		a.setFocus(FocusSessionList)
		return a, a.onTabSwitch()

	case "T":
		a.activeTab = TabTags
		a.setFocus(FocusSessionList)
		return a, nil

	case "S":
		a.activeTab = TabStats
		a.setFocus(FocusSessionList)
		if a.statsView.loading {
			return a, a.loadStatsDataCmd(a.statsView.period)
		}
		return a, nil

	case "A":
		a.hideSubAgents = !a.hideSubAgents
		a.applyFilters()
		return a, nil

	case KeySearch:
		if a.focus != FocusConversation {
			a.searchMode = true
			return a, nil
		}
		// FocusConversation: fall through to pane delegation below

	case "tab":
		if a.activeTab == TabIdeas {
			return a, a.toggleIdeaConvView()
		}
		if a.activeTab == TabStats {
			var cmd tea.Cmd
			a.statsView, cmd = a.statsView.handleKey(msg)
			return a, cmd
		}
		a.cycleFocusForward()
		return a, nil

	case "shift+tab":
		if a.activeTab == TabStats {
			var cmd tea.Cmd
			a.statsView, cmd = a.statsView.handleKey(msg)
			return a, cmd
		}
		a.cycleFocusBackward()
		return a, nil
	}

	if a.activeTab == TabStats {
		if a.statsView.section != statsSectionChart {
			switch key {
			case "1":
				a.statsView.loading = true
				return a, a.loadStatsDataCmd(model.PeriodToday)
			case "7":
				a.statsView.loading = true
				return a, a.loadStatsDataCmd(model.Period7d)
			case "3":
				a.statsView.loading = true
				return a, a.loadStatsDataCmd(model.Period30d)
			case "0":
				a.statsView.loading = true
				return a, a.loadStatsDataCmd(model.PeriodAll)
			}
		}
		var cmd tea.Cmd
		a.statsView, cmd = a.statsView.handleKey(msg)
		return a, cmd
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
	navKeys := map[string]bool{
		"j": true, "k": true, "up": true, "down": true,
		"ctrl+d": true, "ctrl+u": true, "g": true, "G": true,
	}

	if a.activeTab == TabIdeas && a.focus == FocusSessionList {
		if a.ideasView.confirmDel {
			var cmd tea.Cmd
			a.ideasView, cmd = a.ideasView.Update(msg)
			return a, cmd
		}
		if key == "E" {
			if idea := a.ideasView.SelectedIdea(); idea != nil {
				return a, a.openIdeaInEditor(idea.ID, idea.Content)
			}
			return a, nil
		}
		if key == "enter" {
			if a.ideasView.SelectedIdea() != nil {
				a.setFocus(FocusConversation)
			}
			return a, nil
		}
		var cmd tea.Cmd
		a.ideasView, cmd = a.ideasView.Update(msg)
		return a, cmd
	}

	if a.activeTab == TabTags && a.focus == FocusSessionList {
		var cmd tea.Cmd
		wasManageMode := a.tagsView.manageMode
		a.tagsView, cmd = a.tagsView.Update(msg)
		if !wasManageMode && a.tagsView.manageMode {
			if sel := a.tagsView.SelectedManageSession(); sel != nil {
				a.selectedSession = sel
				a.metadata.ClearSession()
				a.conversation.SetMessages(nil, "")
				if a.opencodeDB != nil {
					return a, tea.Batch(cmd, a.loadSession(*sel))
				}
			}
		} else if wasManageMode {
			if key == "enter" {
				if sel := a.tagsView.SelectedManageSession(); sel != nil {
					a.selectedSession = sel
					a.metadata.ClearSession()
					a.conversation.SetMessages(nil, "")
					a.setFocus(FocusConversation)
					if a.opencodeDB != nil {
						return a, a.loadSession(*sel)
					}
					return a, nil
				}
			} else if a.autoPreview && navKeys[key] {
				if sel := a.tagsView.SelectedManageSession(); sel != nil {
					if a.selectedSession == nil || a.selectedSession.ID != sel.ID {
						a.selectedSession = sel
						a.metadata.ClearSession()
						a.conversation.SetMessages(nil, "")
						if a.opencodeDB != nil {
							return a, tea.Batch(cmd, a.loadSession(*sel))
						}
					}
				}
			}
		}
		return a, cmd
	}

	// Cross-pane scroll: ctrl+d/u in session list scrolls conversation without switching focus (lazygit-style).
	if a.focus == FocusSessionList && a.activeTab == TabSessions {
		switch key {
		case "ctrl+d":
			a.conversation.ScrollHalfDown()
			return a, nil
		case "ctrl+u":
			a.conversation.ScrollHalfUp()
			return a, nil
		}
	}

	if a.autoPreview && a.activeTab == TabSessions && a.focus == FocusSessionList && navKeys[key] {
		var navCmd tea.Cmd
		a.sessionList, navCmd = a.sessionList.Update(msg)
		if sel := a.sessionList.SelectedSession(); sel != nil {
			if a.selectedSession == nil || sel.ID != a.selectedSession.ID {
				a.selectedSession = sel
				a.metadata.ClearSession()
				a.conversation.SetMessages(nil, "")
				if a.opencodeDB != nil {
					return a, tea.Batch(navCmd, a.loadSession(*sel))
				}
			}
		}
		return a, navCmd
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
	switch a.focus {
	case FocusSessionList:
		a.setFocus(FocusConversation)
	default:
		a.setFocus(FocusSessionList)
	}
}

func (a *App) cycleFocusBackward() {
	switch a.focus {
	case FocusSessionList:
		a.setFocus(FocusConversation)
	default:
		a.setFocus(FocusSessionList)
	}
}

func (a *App) setFocus(f FocusedPane) {
	a.focus = f
	a.sessionList.SetFocused(f == FocusSessionList)
	a.conversation.SetFocused(f == FocusConversation)
	a.metadata.SetFocused(f == FocusMetadata)
}

func (a *App) onTabSwitch() tea.Cmd {
	if a.activeTab == TabIdeas {
		a.ideaShowConv = false
		if idea := a.ideasView.SelectedIdea(); idea != nil {
			a.conversation.SetIdeaContent(idea.Content)
			sessionTitle := ""
			for _, s := range a.sessions {
				if s.ID == idea.SourceSessionID {
					sessionTitle = s.Title
					break
				}
			}
			a.metadata.SetIdeaMeta(*idea, sessionTitle)
			return nil
		}
		a.conversation.ClearIdeaContent()
		a.metadata.ClearIdea()
		return nil
	}
	if a.activeTab == TabStats {
		if a.statsView.loading {
			return a.loadStatsDataCmd(a.statsView.period)
		}
		return nil
	}
	a.ideaShowConv = false
	a.conversation.ClearIdeaContent()
	a.metadata.ClearIdea()
	return a.tryAutoLoadSelected()
}

func (a *App) toggleIdeaConvView() tea.Cmd {
	idea := a.ideasView.SelectedIdea()
	if idea == nil || idea.SourceSessionID == "" {
		return nil
	}
	a.ideaShowConv = !a.ideaShowConv
	if a.ideaShowConv {
		for _, s := range a.sessions {
			if s.ID == idea.SourceSessionID {
				if a.opencodeDB != nil {
					a.conversation.ClearIdeaContent()
					return a.loadSession(s)
				}
				return nil
			}
		}
		return nil
	}
	a.conversation.SetIdeaContent(idea.Content)
	return nil
}

// --- Layout ---

func (a *App) recalcLayout() tea.Cmd {
	h := a.height - 2 // reserve 2 lines for status bar
	if h < 0 {
		h = 0
	}
	var cmds []tea.Cmd

	r := a.ratio
	total3 := r[0] + r[1] + r[2]
	total2 := r[0] + r[1]

	var listW int
	switch {
	case a.width >= 120:
		listW = r[0] * a.width / total3
		metaW := r[2] * a.width / total3
		convW := a.width - listW - metaW
		if convW < 10 {
			convW = 10
		}
		a.sessionList.SetSize(listW, h-1)
		if cmd := a.conversation.SetSize(convW, h); cmd != nil {
			cmds = append(cmds, cmd)
		}
		a.metadata.SetSize(metaW, h)

	case a.width >= 80:
		listW = r[0] * a.width / total2
		convW := a.width - listW
		a.sessionList.SetSize(listW, h-1)
		if cmd := a.conversation.SetSize(convW, h); cmd != nil {
			cmds = append(cmds, cmd)
		}
		a.metadata.SetSize(0, h)

	default:
		listW = a.width
		a.sessionList.SetSize(a.width, h-1)
		if cmd := a.conversation.SetSize(a.width, h); cmd != nil {
			cmds = append(cmds, cmd)
		}
		a.metadata.SetSize(a.width, h)
	}

	a.ideasView.SetSize(listW, h-1)

	a.inputMode.width = a.width
	a.inputMode.height = a.height
	a.exportOverlay.SetSize(a.width, a.height)
	a.tagsView.SetSize(listW, h-1)

	contentW := a.width
	if contentW < 4 {
		contentW = 4
	}
	a.statsView.SetSize(contentW, h-1)
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

func (a App) loadStatsDataCmd(period model.StatsPeriod) tea.Cmd {
	ocDB := a.opencodeDB
	return func() tea.Msg {
		since := sinceMs(period)
		models, _ := db.GetUsageByModel(ocDB, since)
		agents, _ := db.GetUsageByAgent(ocDB, since)
		daily, _ := db.GetDailyUsage(ocDB, 0)
		modelDaily, _ := db.GetDailyUsageByModel(ocDB, 0)
		userDaily, _ := db.GetDailyUserRequestsByProvider(ocDB, 0)
		userReqs, _ := db.GetUserRequestCount(ocDB, since)
		humanReqs, _ := db.GetHumanRequestCount(ocDB, since)
		totalSessions, _ := db.GetDistinctSessionCount(ocDB, since)
		return statsDataLoadedMsg{period: period, models: models, agents: agents, daily: daily, modelDaily: modelDaily, userDaily: userDaily, userRequests: userReqs, humanRequests: humanReqs, totalSessions: totalSessions}
	}
}

func (a App) loadSessionUsageCmd(sessionID string) tea.Cmd {
	ocDB := a.opencodeDB
	return func() tea.Msg {
		if ocDB == nil {
			return nil
		}
		usage, _ := db.GetSessionUsage(ocDB, sessionID)
		return sessionUsageLoadedMsg{sessionID: sessionID, usage: usage}
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

func deleteLastWord(s string) string {
	runes := []rune(s)
	i := len(runes) - 1
	for i >= 0 && runes[i] == ' ' {
		i--
	}
	for i >= 0 && runes[i] != ' ' {
		i--
	}
	return string(runes[:i+1])
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
	a.tagsView.SetSessions(a.sessions, a.sessionTags)
	a.statsView.SetFilter(a.statsSearchQuery)
}

func (a *App) tryAutoLoadSelected() tea.Cmd {
	sel := a.sessionList.SelectedSession()
	if sel == nil {
		return nil
	}
	if a.selectedSession != nil && sel.ID == a.selectedSession.ID {
		return nil
	}
	a.selectedSession = sel
	a.metadata.ClearSession()
	a.conversation.SetMessages(nil, "")
	if a.opencodeDB != nil {
		return a.loadSession(*sel)
	}
	return nil
}

// expandTilde replaces a leading "~/" or bare "~" with the user's home directory.
func expandTilde(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// doExport writes the session as a Markdown file asynchronously.
func (a App) doExport(sess model.Session, messages []model.Message, tags []string, opts export.Options, dir string) tea.Cmd {
	return func() tea.Msg {
		if dir == "" {
			var err error
			dir, err = os.Getwd()
			if err != nil {
				return ExportDoneMsg{Err: err}
			}
		} else {
			dir = expandTilde(dir)
		}

		// If the directory does not exist, ask the user before creating it.
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			return confirmCreateDirMsg{
				dir:  dir,
				sess: sess,
				msgs: messages,
				tags: tags,
				opts: opts,
			}
		}

		md := export.RenderMarkdown(sess, messages, tags, opts)
		slug := export.Slugify(sess.Title)
		path := filepath.Join(dir, slug+".md")
		if err := os.WriteFile(path, []byte(md), 0644); err != nil {
			return ExportDoneMsg{Err: err}
		}
		return ExportDoneMsg{Path: path}
	}
}

type editorDoneMsg struct {
	ideaID  string
	content string
	err     error
}

func (a App) openIdeaInEditor(ideaID, content string) tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
	}

	f, err := os.CreateTemp("", "mimir-idea-*.md")
	if err != nil {
		return func() tea.Msg { return editorDoneMsg{ideaID: ideaID, err: err} }
	}
	tmpPath := f.Name()
	if _, err := f.WriteString(content); err != nil {
		f.Close()
		return func() tea.Msg { return editorDoneMsg{ideaID: ideaID, err: err} }
	}
	f.Close()

	c := exec.Command(editor, tmpPath)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		defer os.Remove(tmpPath)
		if err != nil {
			return editorDoneMsg{ideaID: ideaID, err: err}
		}
		raw, readErr := os.ReadFile(tmpPath)
		if readErr != nil {
			return editorDoneMsg{ideaID: ideaID, err: readErr}
		}
		return editorDoneMsg{ideaID: ideaID, content: strings.TrimRight(string(raw), "\n")}
	})
}

func (a App) doExportTrilium(sess model.Session, messages []model.Message, tags []string, opts export.Options) tea.Cmd {
	triliumURL := a.triliumURL
	triliumToken := a.triliumToken
	triliumParent := a.triliumParentNoteID
	return func() tea.Msg {
		md := export.RenderMarkdown(sess, messages, tags, opts)
		cfg := export.TriliumConfig{URL: triliumURL, Token: triliumToken, ParentNoteID: triliumParent}
		if err := export.UploadSession(cfg, sess.Title, md); err != nil {
			return ExportDoneMsg{Err: err}
		}
		return ExportDoneMsg{Path: "Trilium: " + sess.Title}
	}
}

// --- Additional async message types for metadata refresh ---

type sessionMetaRefreshedMsg struct {
	meta model.SessionMeta
}

// --- View helpers ---

func (a App) renderTabHeader(active AppTab) string {
	inactive := lipgloss.NewStyle().Foreground(a.theme.TextMuted).Padding(0, 1)
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(a.theme.TextNormal).Padding(0, 1)
	tabLabel := map[AppTab]string{
		TabSessions: "Sessions",
		TabIdeas:    "Ideas",
		TabTags:     "Tags",
		TabStats:    "Stats",
	}
	var parts []string
	for _, name := range a.tabOrder {
		tab := tabNameToAppTab(name)
		label := tabLabel[tab]
		if tab == active {
			parts = append(parts, activeStyle.Render(label))
		} else {
			parts = append(parts, inactive.Render(label))
		}
	}
	return strings.Join(parts, "  ")
}

func (a App) buildStatusBar() string {
	keyStyle := lipgloss.NewStyle().Foreground(a.theme.TextNormal)
	descStyle := lipgloss.NewStyle().Foreground(a.theme.TextMuted)
	errStyle := lipgloss.NewStyle().Foreground(a.theme.ErrorText)

	// hk renders a "key desc" pair with key highlighted
	hk := func(key, desc string) string {
		return keyStyle.Render(key) + " " + descStyle.Render(desc)
	}

	// Line 2 (global) — always the same
	line2Parts := []string{
		hk("jk", "navigate"),
		hk("[ ]", "tabs"),
		hk("Tab", "focus"),
		hk("/", "search"),
		hk("?", "help"),
		hk("q", "quit"),
	}
	line2 := strings.Join(line2Parts, "  ")

	// --- Build line 1 (context-specific) ---

	// Search mode: show search input on line 1
	if a.searchMode {
		var q, hint string
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
		case TabStats:
			q = a.statsSearchQuery
			hint = "models/agents"
		}
		line1 := descStyle.Render(fmt.Sprintf("Search: %s_  │  Filtering %s  │  [Esc] done", q, hint))
		return line1 + "\n" + line2
	}

	// Conversation search active
	if a.focus == FocusConversation && (a.conversation.SearchMode() || a.conversation.SearchMatchCount() > 0) {
		var line1 string
		if a.conversation.SearchMode() {
			matchInfo := ""
			if a.conversation.SearchMatchCount() > 0 {
				matchInfo = fmt.Sprintf(" (%d/%d)", a.conversation.SearchMatchIdx()+1, a.conversation.SearchMatchCount())
			} else if a.conversation.SearchQuery() != "" {
				matchInfo = " (no matches)"
			}
			line1 = fmt.Sprintf("/ %s_%s  │  [n/N] navigate  [Esc/Enter] close", a.conversation.SearchQuery(), matchInfo)
		} else {
			line1 = fmt.Sprintf("[n/N] navigate  (%d/%d: %q)  │  [/] new search",
				a.conversation.SearchMatchIdx()+1, a.conversation.SearchMatchCount(), a.conversation.SearchQuery())
		}
		return descStyle.Render(line1) + "\n" + line2
	}

	// Active filter display
	var filterLine string
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
			filterLine = strings.Join(filterParts, "  │  ") + "  │  [/] edit  [Esc] clear"
		}
	case TabIdeas:
		if a.ideasSearchQuery != "" {
			filterLine = fmt.Sprintf("Filter: %s  │  [/] edit  [Esc] clear", a.ideasSearchQuery)
		}
	case TabTags:
		if a.tagsSearchQuery != "" {
			filterLine = fmt.Sprintf("Filter: %s  │  [/] edit  [Esc] clear", a.tagsSearchQuery)
		}
	case TabStats:
		if a.statsSearchQuery != "" {
			filterLine = fmt.Sprintf("Filter: %s  │  [/] edit  [Esc] clear", a.statsSearchQuery)
		}
	}
	if filterLine != "" {
		return descStyle.Render(filterLine) + "\n" + line2
	}

	// Loading state
	if a.loading && a.totalCount > 0 {
		line1 := fmt.Sprintf("Loading %d/%d sessions...", a.loadedCount, a.totalCount)
		bar := descStyle.Render(line1)
		if a.err != "" {
			bar += errStyle.Render("  ✗ " + a.err)
		}
		return bar + "\n" + line2
	}

	// Normal context hints — build as key/desc pairs, truncate to fit width
	// jk, /, Tab, [ ], ?, q are in line 2 — only show context-specific keys here
	var hints []struct{ key, desc string }
	hp := func(k, d string) struct{ key, desc string } { return struct{ key, desc string }{k, d} }
	switch a.activeTab {
	case TabStats:
		hints = append(hints, hp("Tab", "section"), hp("s", "sort"))
		if a.statsView.section == statsSectionChart {
			hints = append(hints, hp("h/l", "cursor"), hp("0/$", "first/last"))
		} else {
			hints = append([]struct{ key, desc string }{hp("1", "Today"), hp("7", "7d"), hp("3", "30d"), hp("0", "All")}, hints...)
		}
	default:
		switch a.focus {
		case FocusSessionList:
			switch a.activeTab {
			case TabIdeas:
				ideaDesc := "show session"
				if a.ideaShowConv {
					ideaDesc = "show idea"
				}
				hints = append(hints, hp("Enter", "open"), hp("e", "edit"), hp("E", "$EDITOR"), hp("d", "delete"), hp("Tab", ideaDesc), hp("r", "refresh"))
			case TabTags:
				hints = append(hints, hp("Enter", "view sessions"), hp("d", "delete"), hp("r", "rename"))
			default:
				agentDesc := "show agents"
				if !a.hideSubAgents {
					agentDesc = "hide agents"
				}
				hints = append(hints, hp("Enter", "open"), hp("ctrl+d/u", "preview"), hp("i", "idea"), hp("t", "tag"), hp("r", "refresh"), hp("A", agentDesc))
			}
		case FocusConversation:
			hints = append(hints, hp("ctrl+d/u", "page"), hp("g/G", "top/bottom"), hp("n/N", "match"), hp("Esc", "back"))
		case FocusMetadata:
			hints = append(hints, hp("Esc", "back"))
		}
	}

	line1 := truncateStyledHints(hints, a.width, hk)
	if a.err != "" {
		line1 += errStyle.Render("  ✗ " + a.err)
	}
	return line1 + "\n" + line2
}

// truncateStyledHints builds styled "key desc" pairs joined by "  ", dropping
// hints from the end when the total visual width exceeds maxWidth.
func truncateStyledHints(hints []struct{ key, desc string }, maxWidth int, hk func(string, string) string) string {
	sep := "  "
	sepW := len(sep)
	for n := len(hints); n > 0; n-- {
		totalW := 0
		for i := 0; i < n; i++ {
			// plain width: "key desc"
			totalW += len(hints[i].key) + 1 + len(hints[i].desc)
			if i > 0 {
				totalW += sepW
			}
		}
		if totalW <= maxWidth || n == 1 {
			var parts []string
			for i := 0; i < n; i++ {
				parts = append(parts, hk(hints[i].key, hints[i].desc))
			}
			return strings.Join(parts, sep)
		}
	}
	return ""
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
  [Enter]           Open session (focus shifts to conversation)
  [Ctrl+D / Ctrl+U] Scroll conversation preview without leaving list
  [i]               Capture idea (linked to session)
  [Ctrl+E]          Export session as Markdown
  [t]               Add tag to session

  In Conversation:
  [↑ ↓ / j k]       Scroll
  [Ctrl+D / Ctrl+U] Page down/up
  [g / G]           Top / bottom
  [/]               Search conversation
  [n / N]           Next / prev match
  [Esc]             Return focus to session list
  [Esc / Enter]     Exit search

  In Ideas Tab:
  [Enter]           Focus conversation pane
  [e]               Edit idea
  [d]               Delete idea (confirm y/n)

  In Tags Tab:
  [Enter]           View sessions with tag
  [d]               Delete tag
  [r]               Rename tag`

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(a.theme.BorderFocused).
		Padding(1, 3).
		Width(56).
		Render(lipgloss.NewStyle().Foreground(a.theme.TextMuted).Render(helpText))

	_ = background // background string not layered — just show centered help
	return lipgloss.Place(
		a.width, a.height,
		lipgloss.Center, lipgloss.Center,
		box,
	)
}

func (a App) overlayWelcome(background string) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(a.theme.Accent)
	keyStyle := lipgloss.NewStyle().Foreground(a.theme.Accent).Bold(true)
	sepStyle := lipgloss.NewStyle().Foreground(a.theme.TextMuted)
	descStyle := lipgloss.NewStyle().Foreground(a.theme.TextNormal)
	hintStyle := lipgloss.NewStyle().Foreground(a.theme.TextMuted).Italic(true)

	type entry struct {
		keys []string
		desc string
	}
	entries := []entry{
		{[]string{"j", "k"}, "Navigate list"},
		{[]string{"Enter"}, "Open session"},
		{[]string{"[", "]"}, "Switch tabs"},
		{[]string{"Tab"}, "Cycle pane focus"},
		{[]string{"/"}, "Search"},
		{[]string{"?"}, "Full keybinding help"},
		{[]string{"q"}, "Quit"},
	}

	// Build key columns and find max width for alignment
	maxKeyW := 0
	var keyColumns []string
	for _, e := range entries {
		var parts []string
		for _, k := range e.keys {
			parts = append(parts, keyStyle.Render(k))
		}
		col := strings.Join(parts, sepStyle.Render(" / "))
		keyColumns = append(keyColumns, col)
		if w := lipgloss.Width(col); w > maxKeyW {
			maxKeyW = w
		}
	}

	var rows []string
	for i, e := range entries {
		col := keyColumns[i]
		pad := strings.Repeat(" ", maxKeyW-lipgloss.Width(col))
		row := "  " + col + pad + "   " + descStyle.Render(e.desc)
		rows = append(rows, row)
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Welcome to mimir"),
		"",
		strings.Join(rows, "\n"),
		"",
		hintStyle.Render("Press any key to start"),
	)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(a.theme.BorderFocused).
		Padding(1, 3).
		Render(content)

	_ = background
	return lipgloss.Place(
		a.width, a.height,
		lipgloss.Center, lipgloss.Center,
		box,
	)
}

func (a App) viewConfirmCreateDir() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(a.theme.Accent)
	normalStyle := lipgloss.NewStyle().Foreground(a.theme.TextNormal)
	pathStyle := lipgloss.NewStyle().Foreground(a.theme.BorderFocused).Bold(true)
	hintStyle := lipgloss.NewStyle().Foreground(a.theme.TextMuted).Italic(true)

	content := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Create Directory?"),
		"",
		normalStyle.Render("Export directory does not exist:"),
		"",
		pathStyle.Render("  "+a.pendingCreateDir),
		"",
		normalStyle.Render("Create it now?"),
		"",
		hintStyle.Render("[y/Enter] create   [any other key] cancel"),
	)

	box := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(a.theme.AccentBg).
		Padding(1, 3).
		Width(60).
		Render(content)

	return lipgloss.Place(
		a.width, a.height,
		lipgloss.Center, lipgloss.Center,
		box,
	)
}

func (a App) overlayFlash(background string) string {
	borderColor := a.theme.BorderFocused
	title := "✓ Export complete"
	if a.flash.isError {
		borderColor = a.theme.ErrorText
		title = "✗ Export failed"
	}

	body := lipgloss.NewStyle().
		Foreground(a.theme.TextNormal).
		Width(60).
		Render(a.flash.text)

	hint := lipgloss.NewStyle().
		Foreground(a.theme.TextMuted).
		Render("[any key] dismiss")

	content := lipgloss.JoinVertical(lipgloss.Left,
		lipgloss.NewStyle().Bold(true).Foreground(borderColor).Render(title),
		"",
		body,
		"",
		hint,
	)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(1, 3).
		Width(68).
		Render(content)

	_ = background
	return lipgloss.Place(
		a.width, a.height,
		lipgloss.Center, lipgloss.Center,
		box,
	)
}
