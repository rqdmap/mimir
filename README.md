# mimir

> In Norse mythology, Mímir is the guardian of the well of wisdom — keeper of all memory and knowledge.

A terminal UI for browsing and managing [OpenCode](https://opencode.ai) sessions. Built with [Bubble Tea](https://github.com/charmbracelet/bubbletea).

![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat&logo=go)
![License](https://img.shields.io/badge/license-MIT-blue?style=flat)

## Features

- **Session browser** — browse all your OpenCode sessions with live search and tag filtering
- **Conversation viewer** — read full AI conversations with rendered markdown
- **Metadata pane** — view session details, attached tags, and related ideas
- **Ideas tab** — manage your ideas linked to sessions
- **Batch loading** — sessions load progressively in the background (no startup freeze)
- **Progress bar** — real `X/N` loading indicator in the status bar
- **Sub-agent filtering** — toggle visibility of sub-agent sessions with `h`

## Screenshot

```
┌─ Sessions ──────────────────────┐┌─ Conversation ───────────────────┐
│ > implement dark mode toggle    ││ You: add a dark mode toggle       │
│   fix auth middleware           ││                                   │
│   refactor pricing logic        ││ Claude: I'll help add a dark mode │
│   add batch loading progress    ││ toggle to your application...     │
│   ...                           ││                                   │
├─────────────────────────────────┤├─ Metadata ────────────────────────┤
│ Loading 42/138 [████░░░░░░░░]   ││ ID:  abc1234                      │
└─────────────────────────────────┘│ Dir: ~/projects/myapp             │
                                   │ Tags: frontend, ui                │
                                   └───────────────────────────────────┘
```

## Installation

```bash
git clone https://github.com/rqdmap/mimir
cd mimir
go build -o ocm ./cmd/ocm/
# Move to somewhere in your $PATH
mv ocm ~/.local/bin/
```

**Requirements:** Go 1.21+, [OpenCode](https://opencode.ai) installed and used at least once.

## Usage

```bash
ocm                    # Launch TUI
ocm --list-sessions    # Print all sessions to stdout and exit
```

## Keybindings

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate sessions / scroll conversation |
| `Tab` | Switch focus between panes |
| `[` / `]` | Switch left-pane tabs (Sessions ↔ Ideas) |
| `/` | Search sessions |
| `t` | Filter by tag |
| `h` | Toggle sub-agent session visibility |
| `r` | Refresh sessions |
| `?` | Show help overlay |
| `q` / `Esc` | Quit / close overlay |

## How It Works

`mimir` opens OpenCode's SQLite database (`~/.local/share/opencode/opencode.db`) **read-only** and its own manager database for tags and ideas. Sessions are loaded in batches of 100 in the background so the UI stays responsive from the first keypress.

## License

MIT
