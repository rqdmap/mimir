package panes_test

import (
	"strings"
	"testing"

	"github.com/local/oc-manager/internal/model"
	"github.com/local/oc-manager/internal/tui/panes"
)

func TestMetadataPaneNoSession(t *testing.T) {
	mp := panes.NewMetadataPane(40, 30, panes.DefaultTheme)
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
	mp := panes.NewMetadataPane(40, 30, panes.DefaultTheme)
	mp.SetSessionMeta(model.SessionMeta{
		SessionID: "test-id",
		Note:      "This is my test note",
		Tags:      []string{"important", "work"},
	}, false)
	mp.SetSessionTitle("My Test Session Title")
	mp.SetMessageCount(42)

	view := mp.View()
	if !strings.Contains(view, "important") {
		t.Errorf("expected tag 'important' in view, got: %s", view)
	}
	if !strings.Contains(view, "work") {
		t.Errorf("expected tag 'work' in view, got: %s", view)
	}
	if !strings.Contains(view, "Session Ideas") {
		t.Errorf("expected 'Session Ideas' section in view, got: %s", view)
	}
	if !strings.Contains(view, "Usage") {
		t.Errorf("expected Usage section in view, got: %s", view)
	}
	if !strings.Contains(view, "My Test Session Title") {
		t.Errorf("expected full session title in view, got: %s", view)
	}
	if !strings.Contains(view, "test-id") {
		t.Errorf("expected session ID in view, got: %s", view)
	}
	t.Logf("metadata pane renders %d chars OK", len(view))
}

func TestMetadataPaneLongTitle(t *testing.T) {
	mp := panes.NewMetadataPane(30, 30, panes.DefaultTheme)
	longTitle := "This is a very long session title that should wrap inside the metadata pane without truncation"
	mp.SetSessionMeta(model.SessionMeta{SessionID: "ses_01ABCDEF123456789XYZ"}, false)
	mp.SetSessionTitle(longTitle)

	view := mp.View()
	if !strings.Contains(view, "This is a very long") {
		t.Errorf("expected long title content in view, got: %s", view)
	}
	if !strings.Contains(view, "ses_01ABCDEF123456789XYZ") {
		t.Errorf("expected full session ID in view, got: %s", view)
	}
	t.Logf("long-title view renders %d chars OK", len(view))
}
