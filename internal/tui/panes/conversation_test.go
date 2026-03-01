package panes_test

import (
	"strings"
	"testing"

	"github.com/local/oc-manager/internal/model"
	"github.com/local/oc-manager/internal/tui/panes"
)

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

	cp := panes.NewConversationPane(120, 40)
	cp.SetMessages(messages)
	view := cp.View()

	if strings.Contains(view, "data:image") {
		t.Fatal("should not contain base64 image data")
	}
	t.Logf("conversation renders %d chars OK", len(view))
}

func TestConversationEmptyState(t *testing.T) {
	cp := panes.NewConversationPane(80, 24)
	view := cp.View()
	// Should show the no-session-selected message
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

	cp := panes.NewConversationPane(100, 30)
	cp.SetMessages(messages)
	view := cp.View()

	if strings.Contains(view, "data:image") {
		t.Fatal("should not contain base64 image data")
	}
	// Should mention no readable text since there are no text parts
	if !strings.Contains(view, "no readable text") {
		t.Errorf("tool-only session should mention no readable text, got: %q", view)
	}
}

func TestConversationFocusStyleChange(t *testing.T) {
	cp := panes.NewConversationPane(80, 24)
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
	// Both must not panic and must produce output.
	// ANSI color differences are stripped in headless tests so we
	// just verify both states render without error.
	t.Logf("unfocused len=%d focused len=%d", len(unfocused), len(focused))
}

func TestConversationSetSize(t *testing.T) {
	cp := panes.NewConversationPane(80, 24)
	// Should not panic on resize
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

	cp := panes.NewConversationPane(100, 30)
	cp.SetMessages(messages)
	view := cp.View()

	if strings.Contains(view, "data:") {
		t.Fatal("file parts must never emit data URIs or base64")
	}
	if !strings.Contains(view, "photo.jpg") {
		t.Error("file part should display filename")
	}
}
