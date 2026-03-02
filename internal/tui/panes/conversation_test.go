package panes_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/local/oc-manager/internal/model"
	"github.com/local/oc-manager/internal/tui/panes"
)

func applySetMessages(cp panes.ConversationPane, messages []model.Message) panes.ConversationPane {
	cmd := cp.SetMessages(messages, "test-session")
	if cmd != nil {
		msg := cmd()
		cp, _ = cp.Update(msg)
	}
	return cp
}

func TestConversationRenderNoPanic(t *testing.T) {
	messages := []model.Message{
		{
			ID:   "m1",
			Role: "user",
			Parts: []model.Part{
				{Type: model.PartTypeText, Text: "Hello, world!"},
			},
		},
		{
			ID:   "m2",
			Role: "assistant",
			Parts: []model.Part{
				{Type: model.PartTypeText, Text: "# Response\nThis is a **markdown** response."},
				{Type: model.PartTypeTool, ToolName: "bash", ToolStatus: "completed"},
				{Type: model.PartTypeFile, Filename: "screen.png", MimeType: "image/png"},
				{Type: model.PartTypePatch, Text: "src/main.go, internal/db/opencode.go"},
				{Type: model.PartTypeReasoning, Reasoning: "I think therefore I am"},
			},
		},
	}

	cp := panes.NewConversationPane(120, 40, "dark")
	cp = applySetMessages(cp, messages)
	view := cp.View()

	if strings.Contains(view, "data:image") {
		t.Fatal("should not contain base64 image data")
	}
	t.Logf("conversation renders %d chars OK", len(view))
}

func TestConversationEmptyState(t *testing.T) {
	cp := panes.NewConversationPane(80, 24, "dark")
	view := cp.View()
	if !strings.Contains(view, "Select a session") {
		t.Errorf("empty pane should show 'Select a session' message, got: %q", view)
	}
}

func TestConversationToolOnlySession(t *testing.T) {
	messages := []model.Message{
		{
			ID:   "m1",
			Role: "assistant",
			Parts: []model.Part{
				{Type: model.PartTypeTool, ToolName: "list_files", ToolStatus: "completed"},
				{Type: model.PartTypeReasoning, Reasoning: "thinking..."},
			},
		},
	}

	cp := panes.NewConversationPane(100, 30, "dark")
	cp = applySetMessages(cp, messages)
	view := cp.View()

	if strings.Contains(view, "data:image") {
		t.Fatal("should not contain base64 image data")
	}
	if !strings.Contains(view, "no readable text") {
		t.Errorf("tool-only session should mention no readable text, got: %q", view)
	}
}

func TestConversationFocusStyleChange(t *testing.T) {
	cp := panes.NewConversationPane(80, 24, "dark")
	cp.SetFocused(false)
	unfocused := cp.View()
	if unfocused == "" {
		t.Error("unfocused view must not be empty")
	}
	cp.SetFocused(true)
	focused := cp.View()
	if focused == "" {
		t.Error("focused view must not be empty")
	}
	t.Logf("unfocused len=%d focused len=%d", len(unfocused), len(focused))
}

func TestConversationSetSize(t *testing.T) {
	cp := panes.NewConversationPane(80, 24, "dark")
	cp.SetSize(120, 40)
	view := cp.View()
	if view == "" {
		t.Error("view should not be empty after resize")
	}
}

func TestConversationFilePartNoBase64(t *testing.T) {
	messages := []model.Message{
		{
			ID:   "m1",
			Role: "assistant",
			Parts: []model.Part{
				{Type: model.PartTypeFile, Filename: "photo.jpg", MimeType: "image/jpeg"},
			},
		},
	}

	cp := panes.NewConversationPane(100, 30, "dark")
	cp = applySetMessages(cp, messages)
	view := cp.View()

	if strings.Contains(view, "data:") {
		t.Fatal("file parts must never emit data URIs or base64")
	}
	if !strings.Contains(view, "photo.jpg") {
		t.Error("file part should display filename")
	}
}

func TestConversationToolOutputRendered(t *testing.T) {
	messages := []model.Message{
		{
			ID:   "m1",
			Role: "assistant",
			Parts: []model.Part{
				{
					Type:       model.PartTypeTool,
					ToolName:   "bash",
					ToolStatus: "completed",
					ToolOutput: "total 42\ndrwxr-xr-x  5 user staff 160B Jan 01 00:00 .",
				},
			},
		},
	}
	cp := panes.NewConversationPane(120, 40, "dark")
	cp = applySetMessages(cp, messages)
	view := cp.View()
	if !strings.Contains(view, "total 42") {
		t.Errorf("tool output should be rendered in view, got: %q", view)
	}
	if !strings.Contains(view, "bash") {
		t.Errorf("tool name should still appear in view, got: %q", view)
	}
}

func TestConversationToolOutputTruncated(t *testing.T) {
	longOutput := strings.Repeat("a", 3000)
	messages := []model.Message{
		{
			ID:   "m1",
			Role: "assistant",
			Parts: []model.Part{
				{
					Type:       model.PartTypeTool,
					ToolName:   "bash",
					ToolStatus: "completed",
					ToolOutput: longOutput,
				},
			},
		},
	}
	cp := panes.NewConversationPane(120, 40, "dark")
	cp = applySetMessages(cp, messages)
	view := cp.View()
	if !strings.Contains(view, "[truncated]") {
		t.Errorf("output over 2000 chars should be truncated, got len=%d", len(view))
	}
}

func TestConversationSessionIDGuard(t *testing.T) {
	messages := []model.Message{
		{ID: "m1", Role: "user", Parts: []model.Part{{Type: model.PartTypeText, Text: "hello"}}},
	}

	cp := panes.NewConversationPane(120, 40, "dark")
	cmd := cp.SetMessages(messages, "session-A")
	if cmd == nil {
		t.Fatal("SetMessages with non-empty messages must return a Cmd")
	}
	asyncMsg := cmd()

	cp.SetMessages(nil, "session-B")

	cp, _ = cp.Update(asyncMsg)
	view := cp.View()

	if strings.Contains(view, "hello") {
		t.Error("stale render from session-A must not overwrite session-B content")
	}
	if !strings.Contains(view, "Select a session") {
		t.Errorf("after switching to nil session, should show 'Select a session', got: %q", view)
	}
}

func TestConversationSetMessagesNilReturnsNilCmd(t *testing.T) {
	cp := panes.NewConversationPane(80, 24, "dark")
	var cmd tea.Cmd = cp.SetMessages(nil, "")
	if cmd != nil {
		t.Error("SetMessages(nil) must return nil Cmd")
	}
}
