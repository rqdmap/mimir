package panes_test

import (
	"testing"
	"time"

	"github.com/local/oc-manager/internal/model"
	"github.com/local/oc-manager/internal/tui/panes"
)

func TestSessionListRender(t *testing.T) {
	// Create some dummy sessions
	now := time.Now().UnixMilli()
	sessions := []model.Session{
		{ID: "1", Title: "Normal session", TimeUpdated: now},
		{ID: "2", Title: "Fork session", ParentID: "1", TimeUpdated: now - 3600000}, // 1 hour ago
		{ID: "3", Title: "Tagged session", TimeUpdated: now - 86400000},             // 1 day ago
	}

	// Initialize the component
	sl := panes.NewSessionList(80, 30, panes.DefaultTheme)

	// Set sessions with some tags
	sl.SetSessions(sessions, map[string][]string{
		"3": {"important", "review"},
	})

	// Focus the component
	sl.SetFocused(true)

	// Render the view
	view := sl.View()

	// Basic validation
	if view == "" {
		t.Fatal("view should not be empty")
	}

	// Check for expected content
	// Note: lipgloss adds ANSI codes, so exact string matching is hard.
	// We check for key content strings.

	expectedStrings := []string{
		"Normal session",
		"Fork session",
		"(fork)",
		"Tagged session",
		"1h ago", // for session 2
	}

	for _, s := range expectedStrings {
		if !containsStr(view, s) {
			t.Errorf("view missing expected string: %q", s)
		}
	}

	// Verify selected session (defaults to first item)
	selected := sl.SelectedSession()
	if selected == nil {
		t.Fatal("expected a selected session, got nil")
	}
	if selected.ID != "1" {
		t.Errorf("expected selected session ID '1', got %s", selected.ID)
	}

	t.Logf("Session List View Output length: %d", len(view))
}

func containsStr(s, sub string) bool {
	// Simple containment check
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
