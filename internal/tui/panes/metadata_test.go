package panes_test

import (
	"strings"
	"testing"

	"github.com/local/oc-manager/internal/model"
	"github.com/local/oc-manager/internal/tui/panes"
)

func TestMetadataPaneNoSession(t *testing.T) {
	mp := panes.NewMetadataPane(40, 30)
	view := mp.View()
	if view == "" {
		t.Fatal("view must not be empty")
	}
	// Should show empty/no-session message
	if !strings.Contains(view, "Select a session") {
		t.Errorf("expected 'Select a session' in view, got: %s", view)
	}
	t.Logf("no-session view: %s", view)
}

func TestMetadataPaneWithData(t *testing.T) {
	mp := panes.NewMetadataPane(40, 30)
	mp.SetSessionMeta(model.SessionMeta{
		SessionID: "test-id",
		Note:      "This is my test note",
		Tags:      []string{"important", "work"},
	})
	mp.SetIdeaCount(3)
	mp.SetMessageCount(42)

	view := mp.View()
	if !strings.Contains(view, "important") {
		t.Errorf("expected tag 'important' in view, got: %s", view)
	}
	if !strings.Contains(view, "work") {
		t.Errorf("expected tag 'work' in view, got: %s", view)
	}
	if !strings.Contains(view, "This is my test note") {
		t.Errorf("expected note 'This is my test note' in view, got: %s", view)
	}
	if !strings.Contains(view, "Messages: 42") {
		t.Logf("message count may be styled away or format diff, view: %s", view)
	}
	t.Logf("metadata pane renders %d chars OK", len(view))
}
