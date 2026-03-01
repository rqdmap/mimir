package db_test

import (
	"database/sql"
	"os"
	"testing"

	"github.com/local/oc-manager/internal/db"
	_ "modernc.org/sqlite"
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

func newInMemoryDB(t *testing.T) *sql.DB {
	t.Helper()
	memDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	if err := db.RunSchema(memDB); err != nil {
		t.Fatalf("run schema: %v", err)
	}
	t.Cleanup(func() { memDB.Close() })
	return memDB
}

func TestRunMigrations(t *testing.T) {
	memDB := newInMemoryDB(t)

	// Insert a session_meta row with a note
	_, err := memDB.Exec(`INSERT INTO session_meta (session_id, note, time_updated) VALUES (?, ?, ?)`,
		"session-migrate-1", "my note", 1700000000)
	if err != nil {
		t.Fatalf("insert session_meta: %v", err)
	}

	// Run migrations
	if err := db.RunMigrations(memDB); err != nil {
		t.Fatalf("RunMigrations: %v", err)
	}

	// Assert idea row was created
	var count int
	err = memDB.QueryRow(`SELECT COUNT(*) FROM idea WHERE source_session_id = ?`, "session-migrate-1").Scan(&count)
	if err != nil {
		t.Fatalf("count ideas: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 idea, got %d", count)
	}

	// Assert note was NULLed
	var note *string
	err = memDB.QueryRow(`SELECT note FROM session_meta WHERE session_id = ?`, "session-migrate-1").Scan(&note)
	if err != nil {
		t.Fatalf("scan note: %v", err)
	}
	if note != nil {
		t.Fatalf("expected note to be NULL, got %q", *note)
	}
	t.Log("TestRunMigrations PASS")
}

func TestRunMigrationsIdempotent(t *testing.T) {
	memDB := newInMemoryDB(t)

	// Insert a session_meta row with a note
	_, err := memDB.Exec(`INSERT INTO session_meta (session_id, note, time_updated) VALUES (?, ?, ?)`,
		"session-migrate-2", "idempotent note", 1700000001)
	if err != nil {
		t.Fatalf("insert session_meta: %v", err)
	}

	// Run migrations twice
	if err := db.RunMigrations(memDB); err != nil {
		t.Fatalf("RunMigrations first: %v", err)
	}
	if err := db.RunMigrations(memDB); err != nil {
		t.Fatalf("RunMigrations second: %v", err)
	}

	// Assert idea count = 1 (not doubled)
	var count int
	err = memDB.QueryRow(`SELECT COUNT(*) FROM idea WHERE source_session_id = ?`, "session-migrate-2").Scan(&count)
	if err != nil {
		t.Fatalf("count ideas: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 idea after idempotent run, got %d", count)
	}
	t.Log("TestRunMigrationsIdempotent PASS")
}

func TestGetIdeasForSession(t *testing.T) {
	memDB := newInMemoryDB(t)

	// Insert an idea with source_session_id
	now := int64(1700000000000)
	_, err := memDB.Exec(`INSERT INTO idea (id, content, source_session_id, time_created, time_updated) VALUES (?, ?, ?, ?, ?)`,
		"idea-test-1", "test content", "session-xyz", now, now)
	if err != nil {
		t.Fatalf("insert idea: %v", err)
	}

	// Call GetIdeasForSession
	ideas, err := db.GetIdeasForSession(memDB, "session-xyz")
	if err != nil {
		t.Fatalf("GetIdeasForSession: %v", err)
	}
	if len(ideas) != 1 {
		t.Fatalf("expected 1 idea, got %d", len(ideas))
	}
	if ideas[0].ID != "idea-test-1" {
		t.Fatalf("wrong idea ID: %q", ideas[0].ID)
	}
	if ideas[0].Content != "test content" {
		t.Fatalf("wrong content: %q", ideas[0].Content)
	}
	if ideas[0].SourceSessionID != "session-xyz" {
		t.Fatalf("wrong source session: %q", ideas[0].SourceSessionID)
	}
	t.Log("TestGetIdeasForSession PASS")
}