# AGENTS.md — Development Guidelines for mimir

## Project Overview

mimir is a terminal UI (TUI) for browsing and managing OpenCode sessions, built with Go and Bubble Tea. It reads OpenCode's SQLite database **read-only** and maintains its own `manager.db` for user data (tags, ideas, session metadata).

This is a **highly vibe-coded** project — the TUI has complex, interconnected state across multiple panes, tabs, overlays, and input modes. Extra caution is required to avoid regressions.

## Architecture

```
cmd/mimir/main.go         — entry point, flag parsing, DB init
internal/
  config/config.go        — JSON config loading (~/.config/mimir/config.json)
  db/
    opencode.go           — read-only queries against opencode.db
    manager.go            — CRUD for manager.db (tags, ideas, session_meta)
  export/md.go            — Markdown export renderer
  model/
    session.go            — Session, Message, Part types
    idea.go               — Idea, Tag, SessionMeta types
  tui/
    app.go                — main App model, Update/View, key handling (~1500 lines, central hub)
    input.go              — InputMode overlay (idea capture, tag editing, tag rename)
    export.go             — ExportOverlay modal
    ideas.go              — IdeasView (left-pane list for Ideas tab)
    tags.go               — TagsView (left-pane list for Tags tab, manage mode)
    keys.go               — key constants
    panes/
      session_list.go     — SessionList (left-pane list for Sessions tab)
      conversation.go     — ConversationPane (center pane, markdown rendering, search)
      metadata.go         — MetadataPane (right pane, session details)
      theme.go            — Theme definitions (Gruvbox, Default)
```

### Key design facts

- `app.go` is the **single source of truth** for all state. Panes are "dumb" — they render what they're told and emit messages upward.
- Tab switching (`[`/`]`), focus cycling (`Tab`), and overlay activation (`Ctrl+E`, `t`, `i`, `?`) are all handled in `app.go`'s `handleKey`.
- The Ideas tab uses a 3-pane layout: left list (IdeasView) + center (ConversationPane in ideaMode) + right (MetadataPane in ideaMode). `Tab` toggles the center between idea body and linked session conversation.
- All DB access is async via `tea.Cmd` functions. Never block the main goroutine.
- `opencode.db` is **read-only**. Only `manager.db` is writable.

## Critical Rules

### 1. Manager DB Safety

`manager.db` contains **irreplaceable user data** (tags, ideas, session associations). Treat it as production data.

- **NEVER** drop tables, truncate data, or run destructive DDL without explicit user request.
- **NEVER** change `runManagerSchema()` in a way that could lose existing data. All schema changes must be **additive** (new tables, new columns with defaults).
- New tables can be added directly to `runManagerSchema()` using `CREATE TABLE IF NOT EXISTS`.
- **Before** any schema change: describe what it does and ask for confirmation.
- Use transactions (`tx.Begin` / `tx.Commit` / `defer tx.Rollback()`) for all multi-statement writes.

### 2. OpenCode DB is Read-Only

- **NEVER** write to, modify, or delete anything in `opencode.db`.
- All queries against `opencode.db` must be SELECT-only.
- The DB is opened with the application's default mode — do not add write pragmas.

### 3. Test Before Commit

Run the full test suite after every change:

```bash
go test ./...
```

All tests must pass. Do not skip, delete, or weaken existing tests to make new code pass. If a test fails, fix the code, not the test (unless the test itself is genuinely wrong).

### 4. TUI Regression Awareness

The TUI has **deeply interleaved state**. A change in one area frequently breaks another. Pay special attention to:

- **`app.go` handleKey** — this is the most fragile part. Adding a new keybinding can shadow existing ones depending on context (searchMode, activeTab, focus, overlays).
- **Message routing in Update()** — new `tea.Msg` types must be handled at the right level. Check if the overlay/inputMode intercepts should come before or after your handler.
- **Tab-specific behavior** — many keys behave differently depending on `activeTab` (Sessions vs Ideas vs Tags). Test all three tabs when changing shared logic.
- **Focus state** — `FocusSessionList`, `FocusConversation`, `FocusMetadata` each have different key behaviors. Ensure your change works in all focus states.
- **ideaMode / ideaShowConv** — the Ideas tab has a complex toggle between showing idea content vs linked session conversation. Changes to `sessionLoadedMsg` or `conversation.SetMessages` can break this.

**After any TUI change, mentally walk through these scenarios:**

1. Launch → Sessions tab → navigate → open session → scroll conversation → search → exit search
2. Switch to Ideas tab → navigate ideas → `Tab` toggle idea/conversation → `Enter` jump to session
3. Switch to Tags tab → enter manage mode → navigate sessions → `d` remove → `Esc` back
4. `t` tag a session → `i` capture idea → `Ctrl+E` export → `?` help overlay
5. `/` search in each tab → `Esc` clear → `/` search in conversation pane

### 5. Code Style

- Follow existing patterns. This codebase uses:
  - Explicit `tea.Msg` types for inter-component communication (not string-based)
  - Async operations via `tea.Cmd` closures that capture DB handles (not global state)
  - `lipgloss` for all styling (no raw ANSI codes)
  - `glamour` for markdown rendering (always async, never on main goroutine)
- Use `[]rune` for string operations that may involve CJK/multi-byte characters (see existing backspace/truncation patterns).
- Keep panes as stateless as possible — push state management up to `app.go`.

### 6. File Organization

- New panes go in `internal/tui/panes/`
- New overlays/modals go in `internal/tui/` (sibling to `export.go`, `input.go`)
- New model types go in `internal/model/`
- DB queries go in `internal/db/` — `opencode.go` for read-only OpenCode queries, `manager.go` for mimir's own data
- Tests live next to the code they test (`*_test.go`)

## Existing Test Coverage

| Package | Tests | What they cover |
|---------|-------|-----------------|
| `internal/db` | `manager_test.go` | Tag CRUD, idea CRUD, settings CRUD, rename, delete, auto-cleanup |
| `internal/db` | `opencode_test.go` | Session/message loading from opencode.db |
| `internal/tui` | `ideas_test.go` | IdeasView navigation, delete confirmation, edit |
| `internal/tui/panes` | `conversation_test.go` | Rendering, tool output, truncation, session-ID guard, focus styling |
| `internal/tui/panes` | `metadata_test.go` | MetadataPane rendering |
| `internal/tui/panes` | `session_list_test.go` | SessionList rendering with tags, fork labels, time formatting |

### Gaps (known, not yet covered)

- `app.go` — no integration tests for key handling, tab switching, overlay flow
- `export/md.go` — no tests for Markdown export rendering
- `tags.go` — no tests for TagsView navigation, manage mode, delete/rename flow
- `input.go` — no tests for InputMode overlay

When adding new features, write tests for the new functionality. Prefer **in-memory SQLite** (`":memory:"`) for DB tests — see `newInMemoryDB()` in `manager_test.go` for the pattern.

## Common Pitfalls

1. **Glamour renderer panics** — always wrap in `renderMarkdownCached` which has a `recover()`. Never call `glamour.Render` directly on the main goroutine.
2. **Stale async renders** — `AsyncConvRenderMsg` carries a `SessionID` guard. If the user switches sessions before rendering completes, the stale render is discarded. Maintain this pattern for any new async rendering.
3. **`len(string)` vs `len([]rune)`** — use `[]rune` for backspace, truncation, and display-width calculations. `len(string)` counts bytes, which breaks CJK characters.
4. **Search mode key interception** — when `searchMode == true`, nearly all keys are consumed by the search handler in `handleKey`. New global keybindings added below the search block will be unreachable during search. This is intentional.
5. **Overlay priority** — `exportOverlay.IsActive()` and `inputMode.IsActive()` are checked first in `Update()`. Any new overlay must follow this pattern (check-and-return-early before the main switch).
