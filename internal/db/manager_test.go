package db_test

import (
	"os"
	"testing"

	"github.com/local/oc-manager/internal/db"
)

func TestOpenManagerDB(t *testing.T) {
	// Remove existing db to test auto-creation
	home, _ := os.UserHomeDir()
	dbPath := home + "/.local/share/oc-manager/manager.db"
	os.Remove(dbPath) // ok if not exists

	mgr, err := db.OpenManagerDB()
	if err != nil {
		t.Fatalf("open manager db: %v", err)
	}
	defer mgr.Close()

	// Verify file was created
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("manager.db not created: %v", err)
	}
	t.Log("manager.db auto-created successfully")
}

func TestTagRoundTrip(t *testing.T) {
	mgr, err := db.OpenManagerDB()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer mgr.Close()

	sessionID := "test-session-001"
	tag := "important"

	// Add tag
	if err := db.AddSessionTag(mgr, sessionID, tag); err != nil {
		t.Fatalf("add tag: %v", err)
	}

	// Get tags
	tags, err := db.GetSessionTags(mgr, sessionID)
	if err != nil {
		t.Fatalf("get tags: %v", err)
	}
	found := false
	for _, tg := range tags {
		if tg == tag {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("tag %q not found in %v", tag, tags)
	}

	// Remove tag
	if err := db.RemoveSessionTag(mgr, sessionID, tag); err != nil {
		t.Fatalf("remove tag: %v", err)
	}

	tags2, _ := db.GetSessionTags(mgr, sessionID)
	for _, tg := range tags2 {
		if tg == tag {
			t.Fatal("tag should have been removed")
		}
	}
	t.Log("tag round-trip PASS")
}

func TestManagerDB(t *testing.T) {
	mgr, err := db.OpenManagerDB()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer mgr.Close()

	// Note upsert
	if err := db.UpsertSessionNote(mgr, "session-abc", "test note"); err != nil {
		t.Fatalf("upsert note: %v", err)
	}
	meta, err := db.GetSessionMeta(mgr, "session-abc")
	if err != nil {
		t.Fatalf("get meta: %v", err)
	}
	if meta.Note != "test note" {
		t.Fatalf("note mismatch: got %q", meta.Note)
	}

	// Idea round-trip
	ideaID, err := db.AddIdea(mgr, "test idea content", "session-abc")
	if err != nil {
		t.Fatalf("add idea: %v", err)
	}
	ideas, err := db.ListIdeas(mgr)
	if err != nil {
		t.Fatalf("list ideas: %v", err)
	}
	found := false
	for _, idea := range ideas {
		if idea.ID == ideaID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("added idea not found in list")
	}

	// Delete
	if err := db.DeleteIdea(mgr, ideaID); err != nil {
		t.Fatalf("delete idea: %v", err)
	}
	t.Log("manager DB full round-trip PASS")
}
