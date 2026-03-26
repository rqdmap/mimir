package export

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/local/oc-manager/internal/model"
)

// Options controls what gets included in the exported Markdown.
type Options struct {
	IncludeMetadata  bool
	IncludeText      bool
	IncludeTool      bool
	IncludeReasoning bool
}

// DefaultOptions returns sensible defaults (everything except reasoning).
func DefaultOptions() Options {
	return Options{
		IncludeMetadata:  true,
		IncludeText:      true,
		IncludeTool:      false,
		IncludeReasoning: false,
	}
}

// RenderMarkdown converts session messages into a Markdown string.
func RenderMarkdown(sess model.Session, messages []model.Message, tags []string, opts Options) string {
	var sb strings.Builder

	if opts.IncludeMetadata {
		sb.WriteString("# ")
		sb.WriteString(escapeTitle(sess.Title))
		sb.WriteString("\n\n")

		sb.WriteString("| Field | Value |\n")
		sb.WriteString("|-------|-------|\n")
		sb.WriteString(fmt.Sprintf("| **ID** | `%s` |\n", sess.ID))
		sb.WriteString(fmt.Sprintf("| **Directory** | `%s` |\n", sess.Directory))
		if len(tags) > 0 {
			tagStr := strings.Join(tags, ", ")
			sb.WriteString(fmt.Sprintf("| **Tags** | %s |\n", tagStr))
		}
		created := time.Unix(0, sess.TimeCreated*int64(time.Millisecond)).UTC().Format("2006-01-02 15:04:05 UTC")
		updated := time.Unix(0, sess.TimeUpdated*int64(time.Millisecond)).UTC().Format("2006-01-02 15:04:05 UTC")
		sb.WriteString(fmt.Sprintf("| **Created** | %s |\n", created))
		sb.WriteString(fmt.Sprintf("| **Updated** | %s |\n", updated))
		sb.WriteString(fmt.Sprintf("| **Messages** | %d |\n", len(messages)))
		sb.WriteString("\n---\n\n")
	} else {
		// Even without metadata, write a title so the file is readable
		sb.WriteString("# ")
		sb.WriteString(escapeTitle(sess.Title))
		sb.WriteString("\n\n")
	}

	for _, msg := range messages {
		hasContent := false

		// Collect parts we'll render for this message
		var textParts []string
		var toolBlocks []string
		var reasoningParts []string

		for _, part := range msg.Parts {
			switch part.Type {
			case model.PartTypeText:
				if opts.IncludeText && strings.TrimSpace(part.Text) != "" {
					textParts = append(textParts, part.Text)
				}
			case model.PartTypeReasoning:
				if opts.IncludeReasoning && strings.TrimSpace(part.Reasoning) != "" {
					reasoningParts = append(reasoningParts, part.Reasoning)
				}
			case model.PartTypeTool:
				if opts.IncludeTool {
					block := renderToolBlock(part)
					if block != "" {
						toolBlocks = append(toolBlocks, block)
					}
				}
			case model.PartTypePatch:
				if opts.IncludeTool && strings.TrimSpace(part.Text) != "" {
					toolBlocks = append(toolBlocks, fmt.Sprintf("**Patch:** `%s`", part.Text))
				}
			case model.PartTypeFile:
				if opts.IncludeTool {
					toolBlocks = append(toolBlocks, fmt.Sprintf("**File:** `%s` (%s)", part.Filename, part.MimeType))
				}
			}
		}

		hasContent = len(textParts) > 0 || len(toolBlocks) > 0 || len(reasoningParts) > 0
		if !hasContent {
			continue
		}

		// Message header
		role := strings.ToUpper(msg.Role[:1]) + msg.Role[1:]
		ts := time.Unix(0, msg.TimeCreated*int64(time.Millisecond)).UTC().Format("15:04:05")
		sb.WriteString(fmt.Sprintf("## %s  <sup>%s</sup>\n\n", role, ts))

		// Reasoning (collapsible)
		if len(reasoningParts) > 0 {
			sb.WriteString("<details>\n<summary>Reasoning</summary>\n\n")
			for _, r := range reasoningParts {
				sb.WriteString(r)
				if !strings.HasSuffix(r, "\n") {
					sb.WriteString("\n")
				}
			}
			sb.WriteString("\n</details>\n\n")
		}

		for _, t := range textParts {
			sb.WriteString(shiftHeadings(t, 2))
			if !strings.HasSuffix(t, "\n") {
				sb.WriteString("\n")
			}
			sb.WriteString("\n")
		}

		// Tool calls
		for _, block := range toolBlocks {
			sb.WriteString(block)
			sb.WriteString("\n\n")
		}

		sb.WriteString("\n")
	}

	return sb.String()
}

// Slugify converts a session title into a safe filename (no extension).
func Slugify(title string) string {
	var sb strings.Builder
	prev := '_'
	for _, r := range strings.ToLower(title) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
			prev = r
		} else if prev != '_' {
			sb.WriteRune('_')
			prev = '_'
		}
	}
	s := strings.Trim(sb.String(), "_")
	if s == "" {
		s = "session"
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

func shiftHeadings(text string, levels int) string {
	lines := strings.Split(text, "\n")
	extra := strings.Repeat("#", levels)
	for i, line := range lines {
		if strings.HasPrefix(line, "#") && (len(line) == 1 || line[1] == '#' || line[1] == ' ') {
			lines[i] = extra + line
		}
	}
	return strings.Join(lines, "\n")
}

func renderToolBlock(part model.Part) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**Tool:** `%s`", part.ToolName))
	if part.ToolStatus != "" {
		sb.WriteString(fmt.Sprintf(" [%s]", part.ToolStatus))
	}
	if strings.TrimSpace(part.ToolInput) != "" {
		input := truncate(part.ToolInput, 500)
		sb.WriteString("\n```\n")
		sb.WriteString(input)
		sb.WriteString("\n```")
	}
	if strings.TrimSpace(part.ToolOutput) != "" {
		output := truncate(part.ToolOutput, 500)
		sb.WriteString("\n\n<details><summary>Output</summary>\n\n```\n")
		sb.WriteString(output)
		sb.WriteString("\n```\n</details>")
	}
	return sb.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n… (truncated)"
}

func escapeTitle(t string) string {
	// Remove leading # chars that would break Markdown heading levels
	return strings.TrimLeft(t, "#")
}
