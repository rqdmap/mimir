package panes

import (
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type Theme struct {
	Name            string
	BorderFocused   lipgloss.Color
	BorderUnfocused lipgloss.Color
	Accent          lipgloss.Color
	AccentBg        lipgloss.Color
	AccentFg        lipgloss.Color
	TextNormal      lipgloss.Color
	TextMuted       lipgloss.Color
	ErrorText       lipgloss.Color
}

func (t Theme) GlamourOption() glamour.TermRendererOption {
	if t.Name == "gruvbox" {
		return glamour.WithStylesFromJSONBytes(gruvboxStyleJSON)
	}
	return glamour.WithStylePath("dark")
}

var DefaultTheme = Theme{
	Name:            "default",
	BorderFocused:   "#7D56F4",
	BorderUnfocused: "240",
	Accent:          "205",
	AccentBg:        "62",
	AccentFg:        "230",
	TextNormal:      "252",
	TextMuted:       "240",
	ErrorText:       "196",
}

var GruvboxTheme = Theme{
	Name:            "gruvbox",
	BorderFocused:   "#83a598", // aqua
	BorderUnfocused: "#665c54", // bg3
	Accent:          "#fabd2f", // bright yellow
	AccentBg:        "#458588", // blue
	AccentFg:        "#282828",
	TextNormal:      "#ebdbb2", // fg1
	TextMuted:       "#928374", // gray
	ErrorText:       "#fb4934", // bright red
}

func ThemeByName(name string) Theme {
	switch name {
	case "default":
		return DefaultTheme
	default:
		return GruvboxTheme
	}
}

var gruvboxStyleJSON = []byte(`{
  "document": {
    "block_prefix": "\n",
    "block_suffix": "\n",
    "color": "#ebdbb2",
    "margin": 2
  },
  "block_quote": {
    "color": "#d3869b",
    "indent": 1,
    "indent_token": "│ "
  },
  "paragraph": {},
  "list": {
    "level_indent": 2
  },
  "heading": {
    "block_suffix": "\n",
    "color": "#fabd2f",
    "bold": true
  },
  "h1": {
    "prefix": " ",
    "suffix": " ",
    "color": "#282828",
    "background_color": "#d79921",
    "bold": true
  },
  "h2": {
    "prefix": "## ",
    "color": "#fabd2f"
  },
  "h3": {
    "prefix": "### ",
    "color": "#fe8019"
  },
  "h4": {
    "prefix": "#### ",
    "color": "#fb4934"
  },
  "h5": {
    "prefix": "##### ",
    "color": "#fbf1c7"
  },
  "h6": {
    "prefix": "###### ",
    "color": "#a89984",
    "bold": false
  },
  "text": {},
  "strikethrough": {
    "crossed_out": true
  },
  "emph": {
    "color": "#d5c4a1",
    "italic": true
  },
  "strong": {
    "color": "#fbf1c7",
    "bold": true
  },
  "hr": {
    "color": "#7c6f64",
    "format": "\n────────\n"
  },
  "item": {
    "block_prefix": "• "
  },
  "enumeration": {
    "block_prefix": ". "
  },
  "task": {
    "ticked": "[✓] ",
    "unticked": "[ ] "
  },
  "link": {
    "color": "#83a598",
    "underline": true
  },
  "link_text": {
    "color": "#8ec07c",
    "bold": true
  },
  "image": {
    "color": "#b8bb26",
    "underline": true
  },
  "image_text": {
    "color": "#928374",
    "format": "Image: {{.text}} →"
  },
  "code": {
    "prefix": " ",
    "suffix": " ",
    "color": "#fe8019",
    "background_color": "#3c3836"
  },
  "code_block": {
    "color": "#ebdbb2",
    "margin": 2,
    "theme": "gruvbox"
  },
  "table": {},
  "definition_list": {},
  "definition_term": {},
  "definition_description": {
    "block_prefix": "\n🠶 "
  },
  "html_block": {},
  "html_span": {}
}`)
