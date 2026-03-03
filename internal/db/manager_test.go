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

func TestDeleteTag(t *testing.T) {
	memDB := newInMemoryDB(t)

	_, err := memDB.Exec(`INSERT INTO tag (name) VALUES (?)`, "qa-del")
	if err != nil {
		t.Fatalf("insert tag: %v", err)
	}
	_, err = memDB.Exec(`INSERT INTO session_meta (session_id, note, time_updated) VALUES (?, '', 0)`, "sess-del-1")
	if err != nil {
		t.Fatalf("insert session_meta 1: %v", err)
	}
	_, err = memDB.Exec(`INSERT INTO session_meta (session_id, note, time_updated) VALUES (?, '', 0)`, "sess-del-2")
	if err != nil {
		t.Fatalf("insert session_meta 2: %v", err)
	}
	_, err = memDB.Exec(`INSERT INTO session_tag (session_id, tag_name) VALUES (?, ?)`, "sess-del-1", "qa-del")
	if err != nil {
		t.Fatalf("insert session_tag 1: %v", err)
	}
	_, err = memDB.Exec(`INSERT INTO session_tag (session_id, tag_name) VALUES (?, ?)`, "sess-del-2", "qa-del")
	if err != nil {
		t.Fatalf("insert session_tag 2: %v", err)
	}
	_, err = memDB.Exec(`INSERT INTO idea (id, content, time_created, time_updated) VALUES (?, ?, 0, 0)`, "idea-del-1", "content")
	if err != nil {
		t.Fatalf("insert idea: %v", err)
	}
	_, err = memDB.Exec(`INSERT INTO idea_tag (idea_id, tag_name) VALUES (?, ?)`, "idea-del-1", "qa-del")
	if err != nil {
		t.Fatalf("insert idea_tag: %v", err)
	}

	if err := db.DeleteTag(memDB, "qa-del"); err != nil {
		t.Fatalf("DeleteTag: %v", err)
	}

	var cnt int
	memDB.QueryRow(`SELECT COUNT(*) FROM tag WHERE name = ?`, "qa-del").Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("expected tag gone, got count=%d", cnt)
	}
	memDB.QueryRow(`SELECT COUNT(*) FROM session_tag WHERE tag_name = ?`, "qa-del").Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("expected session_tag gone, got count=%d", cnt)
	}
	memDB.QueryRow(`SELECT COUNT(*) FROM idea_tag WHERE tag_name = ?`, "qa-del").Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("expected idea_tag gone, got count=%d", cnt)
	}
	t.Log("TestDeleteTag PASS")
}

func TestRenameTag(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		memDB := newInMemoryDB(t)

		_, err := memDB.Exec(`INSERT INTO tag (name, color) VALUES (?, ?)`, "old-name", "red")
		if err != nil {
			t.Fatalf("insert tag: %v", err)
		}
		_, err = memDB.Exec(`INSERT INTO session_meta (session_id, note, time_updated) VALUES (?, '', 0)`, "sess-ren-1")
		if err != nil {
			t.Fatalf("insert session_meta: %v", err)
		}
		_, err = memDB.Exec(`INSERT INTO session_tag (session_id, tag_name) VALUES (?, ?)`, "sess-ren-1", "old-name")
		if err != nil {
			t.Fatalf("insert session_tag: %v", err)
		}
		_, err = memDB.Exec(`INSERT INTO idea (id, content, time_created, time_updated) VALUES (?, ?, 0, 0)`, "idea-ren-1", "content")
		if err != nil {
			t.Fatalf("insert idea: %v", err)
		}
		_, err = memDB.Exec(`INSERT INTO idea_tag (idea_id, tag_name) VALUES (?, ?)`, "idea-ren-1", "old-name")
		if err != nil {
			t.Fatalf("insert idea_tag: %v", err)
		}

		if err := db.RenameTag(memDB, "old-name", "new-name"); err != nil {
			t.Fatalf("RenameTag: %v", err)
		}

		var cnt int
		memDB.QueryRow(`SELECT COUNT(*) FROM tag WHERE name = ?`, "old-name").Scan(&cnt)
		if cnt != 0 {
			t.Fatalf("old tag should be gone, got count=%d", cnt)
		}
		var color string
		memDB.QueryRow(`SELECT color FROM tag WHERE name = ?`, "new-name").Scan(&color)
		if color != "red" {
			t.Fatalf("color not preserved: got %q", color)
		}
		memDB.QueryRow(`SELECT COUNT(*) FROM session_tag WHERE tag_name = ? AND session_id = ?`, "new-name", "sess-ren-1").Scan(&cnt)
		if cnt != 1 {
			t.Fatalf("session_tag not updated, got count=%d", cnt)
		}
		memDB.QueryRow(`SELECT COUNT(*) FROM idea_tag WHERE tag_name = ? AND idea_id = ?`, "new-name", "idea-ren-1").Scan(&cnt)
		if cnt != 1 {
			t.Fatalf("idea_tag not updated, got count=%d", cnt)
		}
		t.Log("TestRenameTag/happy path PASS")
	})

	t.Run("conflict", func(t *testing.T) {
		memDB := newInMemoryDB(t)

		_, err := memDB.Exec(`INSERT INTO tag (name) VALUES (?)`, "foo")
		if err != nil {
			t.Fatalf("insert tag foo: %v", err)
		}
		_, err = memDB.Exec(`INSERT INTO tag (name) VALUES (?)`, "bar")
		if err != nil {
			t.Fatalf("insert tag bar: %v", err)
		}

		if err := db.RenameTag(memDB, "foo", "bar"); err == nil {
			t.Fatal("expected error renaming to existing tag, got nil")
		}

		var cnt int
		memDB.QueryRow(`SELECT COUNT(*) FROM tag WHERE name = ?`, "foo").Scan(&cnt)
		if cnt != 1 {
			t.Fatalf("foo should still exist, got count=%d", cnt)
		}
		memDB.QueryRow(`SELECT COUNT(*) FROM tag WHERE name = ?`, "bar").Scan(&cnt)
		if cnt != 1 {
			t.Fatalf("bar should still exist, got count=%d", cnt)
		}
		t.Log("TestRenameTag/conflict PASS")
	})
}

func TestRemoveSessionTagAutoCleanup(t *testing.T) {
	t.Run("single session auto-delete", func(t *testing.T) {
		memDB := newInMemoryDB(t)

		_, err := memDB.Exec(`INSERT INTO tag (name) VALUES (?)`, "qa-cleanup")
		if err != nil {
			t.Fatalf("insert tag: %v", err)
		}
		_, err = memDB.Exec(`INSERT INTO session_meta (session_id, note, time_updated) VALUES (?, '', 0)`, "sess-cleanup-1")
		if err != nil {
			t.Fatalf("insert session_meta: %v", err)
		}
		_, err = memDB.Exec(`INSERT INTO session_tag (session_id, tag_name) VALUES (?, ?)`, "sess-cleanup-1", "qa-cleanup")
		if err != nil {
			t.Fatalf("insert session_tag: %v", err)
		}

		if err := db.RemoveSessionTag(memDB, "sess-cleanup-1", "qa-cleanup"); err != nil {
			t.Fatalf("RemoveSessionTag: %v", err)
		}

		var cnt int
		memDB.QueryRow(`SELECT COUNT(*) FROM session_tag WHERE session_id = ? AND tag_name = ?`, "sess-cleanup-1", "qa-cleanup").Scan(&cnt)
		if cnt != 0 {
			t.Fatalf("session_tag should be gone, got count=%d", cnt)
		}
		memDB.QueryRow(`SELECT COUNT(*) FROM tag WHERE name = ?`, "qa-cleanup").Scan(&cnt)
		if cnt != 0 {
			t.Fatalf("tag should be auto-deleted, got count=%d", cnt)
		}
		t.Log("TestRemoveSessionTagAutoCleanup/single session PASS")
	})

	t.Run("multi-session tag preserved", func(t *testing.T) {
		memDB := newInMemoryDB(t)

		_, err := memDB.Exec(`INSERT INTO tag (name) VALUES (?)`, "qa-multi")
		if err != nil {
			t.Fatalf("insert tag: %v", err)
		}
		_, err = memDB.Exec(`INSERT INTO session_meta (session_id, note, time_updated) VALUES (?, '', 0)`, "sess-multi-1")
		if err != nil {
			t.Fatalf("insert session_meta 1: %v", err)
		}
		_, err = memDB.Exec(`INSERT INTO session_meta (session_id, note, time_updated) VALUES (?, '', 0)`, "sess-multi-2")
		if err != nil {
			t.Fatalf("insert session_meta 2: %v", err)
		}
		_, err = memDB.Exec(`INSERT INTO session_tag (session_id, tag_name) VALUES (?, ?)`, "sess-multi-1", "qa-multi")
		if err != nil {
			t.Fatalf("insert session_tag 1: %v", err)
		}
		_, err = memDB.Exec(`INSERT INTO session_tag (session_id, tag_name) VALUES (?, ?)`, "sess-multi-2", "qa-multi")
		if err != nil {
			t.Fatalf("insert session_tag 2: %v", err)
		}

		if err := db.RemoveSessionTag(memDB, "sess-multi-1", "qa-multi"); err != nil {
			t.Fatalf("RemoveSessionTag: %v", err)
		}

		var cnt int
		memDB.QueryRow(`SELECT COUNT(*) FROM session_tag WHERE session_id = ? AND tag_name = ?`, "sess-multi-1", "qa-multi").Scan(&cnt)
		if cnt != 0 {
			t.Fatalf("sess-multi-1 session_tag should be gone, got count=%d", cnt)
		}
		memDB.QueryRow(`SELECT COUNT(*) FROM tag WHERE name = ?`, "qa-multi").Scan(&cnt)
		if cnt != 1 {
			t.Fatalf("tag should be preserved (still used by sess-multi-2), got count=%d", cnt)
		}
		t.Log("TestRemoveSessionTagAutoCleanup/multi-session PASS")
	})
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
