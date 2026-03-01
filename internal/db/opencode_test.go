package db_test

import (
	"testing"

	"github.com/local/oc-manager/internal/db"
)

func TestListSessions(t *testing.T) {
	oc, err := db.OpenOpencodeDB()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer oc.Close()

	sessions, err := db.ListSessions(oc)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) == 0 {
		t.Fatal("expected non-empty session list")
	}
	t.Logf("found %d sessions, first: %q", len(sessions), sessions[0].Title)
}

func TestLoadSessionMessages(t *testing.T) {
	oc, err := db.OpenOpencodeDB()
	if err != nil {
		t.Skip("opencode db not available:", err)
	}
	defer oc.Close()
	sessions, _ := db.ListSessions(oc)
	if len(sessions) == 0 {
		t.Skip("no sessions")
	}
	msgs, err := db.LoadSessionMessages(oc, sessions[0].ID)
	if err != nil {
		t.Fatalf("load messages: %v", err)
	}
	t.Logf("loaded %d messages for session %q", len(msgs), sessions[0].Title)
}

func TestMalformedPart(t *testing.T) {
	// Test that malformed JSON in parts does not panic
	// Verify the real DB doesn't panic on any session
	oc, err := db.OpenOpencodeDB()
	if err != nil {
		t.Skip("opencode db not available:", err)
	}
	defer oc.Close()
	sessions, _ := db.ListSessions(oc)
	// Load first 10 sessions without panicking
	for i, s := range sessions {
		if i >= 10 {
			break
		}
		_, err := db.LoadSessionMessages(oc, s.ID)
		if err != nil {
			t.Logf("session %q error (ok if malformed): %v", s.Title, err)
		}
	}
	// reaching here without panic = PASS
}
