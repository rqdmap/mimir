package export

import (
	"fmt"
	"regexp"
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
		created := time.Unix(0, sess.TimeCreated*int64(time.Millisecond)).Local().Format("2006-01-02 15:04:05")
		updated := time.Unix(0, sess.TimeUpdated*int64(time.Millisecond)).Local().Format("2006-01-02 15:04:05")
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
				text := cleanLLMText(part.Text)
				if opts.IncludeText && strings.TrimSpace(text) != "" {
					textParts = append(textParts, text)
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
		ts := time.Unix(0, msg.TimeCreated*int64(time.Millisecond)).Local().Format("15:04:05")
		sb.WriteString(fmt.Sprintf("## %s  <sup>%s</sup>\n\n", role, ts))

		// Reasoning (collapsible)
		if len(reasoningParts) > 0 {
			sb.WriteString("<details>\n<summary>Reasoning</summary>\n\n")
			for _, r := range reasoningParts {
				sb.WriteString(bumpHeadings(r, 2))
				if !strings.HasSuffix(r, "\n") {
					sb.WriteString("\n")
				}
			}
			sb.WriteString("\n</details>\n\n")
		}

		for _, t := range textParts {
			sb.WriteString(bumpHeadings(t, 2))
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

var headingRe = regexp.MustCompile(`(?m)^(#{1,6})\s`)
var htmlCommentRe = regexp.MustCompile(`(?s)<!--.*?-->`)

// matches both opening <tag ...> and closing </tag> for any HTML/XML element name
var genericTagRe = regexp.MustCompile(`(?i)</?([a-z][a-z0-9_-]*)(?:\s[^>]*)?>`)

// html5Elements is the complete set of standard HTML5 element names (stable since HTML5 is finalised).
// cleanLLMText escapes any tag whose name is NOT in this set to &lt;/&gt; so it renders as plain text.
var html5Elements = map[string]bool{
	"a": true, "abbr": true, "address": true, "area": true, "article": true,
	"aside": true, "audio": true, "b": true, "base": true, "bdi": true,
	"bdo": true, "blockquote": true, "body": true, "br": true, "button": true,
	"canvas": true, "caption": true, "cite": true, "code": true, "col": true,
	"colgroup": true, "data": true, "datalist": true, "dd": true, "del": true,
	"details": true, "dfn": true, "dialog": true, "div": true, "dl": true,
	"dt": true, "em": true, "embed": true, "fieldset": true, "figcaption": true,
	"figure": true, "footer": true, "form": true, "h1": true, "h2": true,
	"h3": true, "h4": true, "h5": true, "h6": true, "head": true,
	"header": true, "hgroup": true, "hr": true, "html": true, "i": true,
	"iframe": true, "img": true, "input": true, "ins": true, "kbd": true,
	"label": true, "legend": true, "li": true, "link": true, "main": true,
	"map": true, "mark": true, "menu": true, "meta": true, "meter": true,
	"nav": true, "noscript": true, "object": true, "ol": true, "optgroup": true,
	"option": true, "output": true, "p": true, "picture": true, "pre": true,
	"progress": true, "q": true, "rp": true, "rt": true, "ruby": true,
	"s": true, "samp": true, "script": true, "search": true, "section": true,
	"select": true, "slot": true, "small": true, "source": true, "span": true,
	"strong": true, "style": true, "sub": true, "summary": true, "sup": true,
	"table": true, "tbody": true, "td": true, "template": true, "textarea": true,
	"tfoot": true, "th": true, "thead": true, "time": true, "title": true,
	"tr": true, "track": true, "u": true, "ul": true, "var": true,
	"video": true, "wbr": true,
}

func cleanLLMText(text string) string {
	text = htmlCommentRe.ReplaceAllString(text, "")
	lines := strings.Split(text, "\n")
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		lines[i] = genericTagRe.ReplaceAllStringFunc(line, func(tag string) string {
			m := genericTagRe.FindStringSubmatch(tag)
			if m == nil || html5Elements[strings.ToLower(m[1])] {
				return tag
			}
			return strings.NewReplacer("<", "&lt;", ">", "&gt;").Replace(tag)
		})
	}
	return strings.Join(lines, "\n")
}

// bumpHeadings shifts all markdown headings in s down by delta levels so that
// message body content nests properly under the ## role header.
// Headings are capped at h6 (the markdown maximum).
// Fenced code blocks (``` / ~~~) are skipped to avoid mangling code.
func bumpHeadings(s string, delta int) string {
	if delta <= 0 {
		return s
	}

	lines := strings.Split(s, "\n")
	inFence := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		lines[i] = headingRe.ReplaceAllStringFunc(line, func(m string) string {
			hashes := strings.TrimRight(m, " \t")
			newLevel := len(hashes) + delta
			if newLevel > 6 {
				newLevel = 6
			}
			return strings.Repeat("#", newLevel) + " "
		})
	}
	return strings.Join(lines, "\n")
}
