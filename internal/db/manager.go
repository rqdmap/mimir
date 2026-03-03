package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/local/oc-manager/internal/model"
	_ "modernc.org/sqlite"
)

// OpenManagerDB creates ~/.local/share/oc-manager/manager.db and runs schema migrations.
func OpenManagerDB() (*sql.DB, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}
	dbDir := filepath.Join(home, ".local", "share", "oc-manager")
	dbPath := filepath.Join(dbDir, "manager.db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dbDir, err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open manager.db: %w", err)
	}

	if err := runManagerSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema migration: %w", err)
	}

	return db, nil
}

func runManagerSchema(db *sql.DB) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS session_meta (
			session_id   TEXT PRIMARY KEY,
			note         TEXT,
			time_updated INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS session_tag (
			session_id TEXT NOT NULL,
			tag_name   TEXT NOT NULL,
			PRIMARY KEY (session_id, tag_name)
		)`,
		`CREATE TABLE IF NOT EXISTS idea (
			id                TEXT PRIMARY KEY,
			content           TEXT NOT NULL,
			source_session_id TEXT,
			time_created      INTEGER NOT NULL,
			time_updated      INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS idea_tag (
			idea_id  TEXT NOT NULL,
			tag_name TEXT NOT NULL,
			PRIMARY KEY (idea_id, tag_name)
		)`,
		`CREATE TABLE IF NOT EXISTS idea_tag (
			idea_id  TEXT NOT NULL,
			tag_name TEXT NOT NULL,
			PRIMARY KEY (idea_id, tag_name)
		)`,
		`CREATE TABLE IF NOT EXISTS tag (
			name  TEXT PRIMARY KEY,
			color TEXT NOT NULL DEFAULT '#7D56F4'
		)`,
		`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER PRIMARY KEY, applied_at INTEGER NOT NULL)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:min(len(stmt), 40)], err)
		}
	}
	return nil
}

// RunSchema is an exported wrapper around runManagerSchema for use in tests.
func RunSchema(db *sql.DB) error {
	return runManagerSchema(db)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetSessionMeta retrieves note + tags for a session. Returns empty SessionMeta if not found.
func GetSessionMeta(db *sql.DB, sessionID string) (model.SessionMeta, error) {
	meta := model.SessionMeta{SessionID: sessionID}

	row := db.QueryRow(`SELECT note FROM session_meta WHERE session_id = ?`, sessionID)
	var note sql.NullString
	if err := row.Scan(&note); err != nil && err != sql.ErrNoRows {
		return meta, fmt.Errorf("get session_meta: %w", err)
	}
	meta.Note = note.String

	tags, err := GetSessionTags(db, sessionID)
	if err != nil {
		return meta, err
	}
	meta.Tags = tags

	return meta, nil
}

// UpsertSessionNote saves or updates a note for a session.
func UpsertSessionNote(db *sql.DB, sessionID, note string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	_, err = tx.Exec(`
		INSERT INTO session_meta (session_id, note, time_updated)
		VALUES (?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET note = excluded.note, time_updated = excluded.time_updated
	`, sessionID, note, now)
	if err != nil {
		return fmt.Errorf("upsert session note: %w", err)
	}

	return tx.Commit()
}

// AddSessionTag adds a tag to a session; also inserts the tag into the tag table if not exists.
func AddSessionTag(db *sql.DB, sessionID, tag string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Ensure tag exists in tag table.
	_, err = tx.Exec(`INSERT INTO tag (name) VALUES (?) ON CONFLICT(name) DO NOTHING`, tag)
	if err != nil {
		return fmt.Errorf("insert tag: %w", err)
	}

	// Ensure session_meta row exists (so FK-style relationship is consistent).
	now := time.Now().Unix()
	_, err = tx.Exec(`
		INSERT INTO session_meta (session_id, note, time_updated)
		VALUES (?, '', ?)
		ON CONFLICT(session_id) DO NOTHING
	`, sessionID, now)
	if err != nil {
		return fmt.Errorf("ensure session_meta: %w", err)
	}

	_, err = tx.Exec(`
		INSERT INTO session_tag (session_id, tag_name) VALUES (?, ?)
		ON CONFLICT(session_id, tag_name) DO NOTHING
	`, sessionID, tag)
	if err != nil {
		return fmt.Errorf("insert session_tag: %w", err)
	}

	return tx.Commit()
}

// RemoveSessionTag removes a tag from a session. If no other sessions reference
// the tag after removal, the tag row is auto-deleted from the tag table.
func RemoveSessionTag(db *sql.DB, sessionID, tag string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM session_tag WHERE session_id = ? AND tag_name = ?`, sessionID, tag)
	if err != nil {
		return fmt.Errorf("delete session_tag: %w", err)
	}

	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM session_tag WHERE tag_name = ?`, tag).Scan(&count); err != nil {
		return fmt.Errorf("count session_tag: %w", err)
	}
	if count == 0 {
		_, err = tx.Exec(`DELETE FROM tag WHERE name = ?`, tag)
		if err != nil {
			return fmt.Errorf("auto-delete tag: %w", err)
		}
	}

	return tx.Commit()
}

// DeleteTag removes a tag and all its associations from idea_tag and session_tag.
func DeleteTag(db *sql.DB, tagName string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM idea_tag WHERE tag_name = ?`, tagName)
	if err != nil {
		return fmt.Errorf("delete idea_tag: %w", err)
	}

	_, err = tx.Exec(`DELETE FROM session_tag WHERE tag_name = ?`, tagName)
	if err != nil {
		return fmt.Errorf("delete session_tag: %w", err)
	}

	_, err = tx.Exec(`DELETE FROM tag WHERE name = ?`, tagName)
	if err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}

	return tx.Commit()
}

// RenameTag renames a tag, preserving its color and updating all associations.
// Returns an error if newName already exists in the tag table.
func RenameTag(db *sql.DB, oldName, newName string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var existing int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM tag WHERE name = ?`, newName).Scan(&existing); err != nil {
		return fmt.Errorf("check tag exists: %w", err)
	}
	if existing > 0 {
		return fmt.Errorf("tag %q already exists", newName)
	}

	_, err = tx.Exec(`INSERT INTO tag (name, color) SELECT ?, color FROM tag WHERE name = ?`, newName, oldName)
	if err != nil {
		return fmt.Errorf("insert renamed tag: %w", err)
	}

	_, err = tx.Exec(`UPDATE session_tag SET tag_name = ? WHERE tag_name = ?`, newName, oldName)
	if err != nil {
		return fmt.Errorf("update session_tag: %w", err)
	}

	_, err = tx.Exec(`UPDATE idea_tag SET tag_name = ? WHERE tag_name = ?`, newName, oldName)
	if err != nil {
		return fmt.Errorf("update idea_tag: %w", err)
	}

	_, err = tx.Exec(`DELETE FROM tag WHERE name = ?`, oldName)
	if err != nil {
		return fmt.Errorf("delete old tag: %w", err)
	}

	return tx.Commit()
}

// GetSessionTags returns all tag names for a session.
func GetSessionTags(db *sql.DB, sessionID string) ([]string, error) {
	rows, err := db.Query(`SELECT tag_name FROM session_tag WHERE session_id = ? ORDER BY tag_name`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query session_tag: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// ListAllSessionTags returns ALL session tags in ONE query as map[sessionID][]tagName.
// Use this instead of calling GetSessionTags per-session to avoid N+1 queries.
func ListAllSessionTags(db *sql.DB) (map[string][]string, error) {
	if db == nil {
		return make(map[string][]string), nil
	}
	rows, err := db.Query(`SELECT session_id, tag_name FROM session_tag ORDER BY session_id, tag_name`)
	if err != nil {
		return nil, fmt.Errorf("query all session_tag: %w", err)
	}
	defer rows.Close()

	tags := make(map[string][]string)
	for rows.Next() {
		var sessionID, tagName string
		if err := rows.Scan(&sessionID, &tagName); err != nil {
			return nil, fmt.Errorf("scan session_tag: %w", err)
		}
		tags[sessionID] = append(tags[sessionID], tagName)
	}
	return tags, rows.Err()
}

// ListAllTags returns all tags from the tag table.
func ListAllTags(db *sql.DB) ([]model.Tag, error) {
	rows, err := db.Query(`SELECT name, color FROM tag ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("query tags: %w", err)
	}
	defer rows.Close()

	var tags []model.Tag
	for rows.Next() {
		var t model.Tag
		if err := rows.Scan(&t.Name, &t.Color); err != nil {
			return nil, fmt.Errorf("scan tag row: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// ListSessionsByTag returns session IDs that have a given tag.
func ListSessionsByTag(db *sql.DB, tag string) ([]string, error) {
	rows, err := db.Query(`SELECT session_id FROM session_tag WHERE tag_name = ? ORDER BY session_id`, tag)
	if err != nil {
		return nil, fmt.Errorf("query sessions by tag: %w", err)
	}
	defer rows.Close()

	var sessions []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("scan session_id: %w", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// AddIdea creates a new idea linked to a session and returns the new idea ID.
func AddIdea(db *sql.DB, content, sourceSessionID string) (string, error) {
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	id := fmt.Sprintf("idea_%d", time.Now().UnixNano())
	now := time.Now().UnixMilli()

	_, err = tx.Exec(`
		INSERT INTO idea (id, content, source_session_id, time_created, time_updated)
		VALUES (?, ?, ?, ?, ?)
	`, id, content, sourceSessionID, now, now)
	if err != nil {
		return "", fmt.Errorf("insert idea: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

// ListIdeas returns all ideas ordered by time_created DESC, with their tags.
func ListIdeas(db *sql.DB) ([]model.Idea, error) {
	rows, err := db.Query(`
		SELECT id, content, source_session_id, time_created, time_updated
		FROM idea
		ORDER BY time_created DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query ideas: %w", err)
	}
	defer rows.Close()

	var ideas []model.Idea
	var ideaIDs []string
	for rows.Next() {
		var idea model.Idea
		var sourceID sql.NullString
		if err := rows.Scan(&idea.ID, &idea.Content, &sourceID, &idea.TimeCreated, &idea.TimeUpdated); err != nil {
			return nil, fmt.Errorf("scan idea: %w", err)
		}
		idea.SourceSessionID = sourceID.String
		ideas = append(ideas, idea)
		ideaIDs = append(ideaIDs, idea.ID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(ideaIDs) == 0 {
		return ideas, nil
	}

	// Fetch all tags in a single query (avoids N+1).
	placeholders := strings.Repeat("?,", len(ideaIDs))
	placeholders = placeholders[:len(placeholders)-1] // trim trailing comma
	args := make([]any, len(ideaIDs))
	for i, id := range ideaIDs {
		args[i] = id
	}
	tagRows, err := db.Query(
		"SELECT idea_id, tag_name FROM idea_tag WHERE idea_id IN ("+placeholders+") ORDER BY idea_id, tag_name",
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query idea_tags: %w", err)
	}
	defer tagRows.Close()

	tagMap := make(map[string][]string, len(ideaIDs))
	for tagRows.Next() {
		var ideaID, tagName string
		if err := tagRows.Scan(&ideaID, &tagName); err != nil {
			return nil, fmt.Errorf("scan idea_tag: %w", err)
		}
		tagMap[ideaID] = append(tagMap[ideaID], tagName)
	}
	if err := tagRows.Err(); err != nil {
		return nil, err
	}

	for i := range ideas {
		ideas[i].Tags = tagMap[ideas[i].ID]
	}

	return ideas, nil
}

func getIdeaTags(db *sql.DB, ideaID string) ([]string, error) {
	rows, err := db.Query(`SELECT tag_name FROM idea_tag WHERE idea_id = ? ORDER BY tag_name`, ideaID)
	if err != nil {
		return nil, fmt.Errorf("query idea_tag: %w", err)
	}
	defer rows.Close()

	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scan idea tag: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// UpdateIdea updates an idea's content.
func UpdateIdea(db *sql.DB, id, content string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UnixMilli()
	_, err = tx.Exec(`UPDATE idea SET content = ?, time_updated = ? WHERE id = ?`, content, now, id)
	if err != nil {
		return fmt.Errorf("update idea: %w", err)
	}

	return tx.Commit()
}

// DeleteIdea deletes an idea and its tags.
func DeleteIdea(db *sql.DB, id string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`DELETE FROM idea_tag WHERE idea_id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete idea_tag: %w", err)
	}

	_, err = tx.Exec(`DELETE FROM idea WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete idea: %w", err)
	}

	return tx.Commit()
}

// RunMigrations checks the schema_version table and runs any pending migrations.
// It is idempotent: calling it multiple times is safe.
func RunMigrations(db *sql.DB) error {
	var currentVersion int
	row := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`)
	if err := row.Scan(&currentVersion); err != nil {
		return fmt.Errorf("check schema version: %w", err)
	}

	if currentVersion < 1 {
		if err := migrateV1(db); err != nil {
			return fmt.Errorf("migration v1: %w", err)
		}
	}
	return nil
}

func migrateV1(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Fetch all sessions with non-empty notes
	rows, err := tx.Query(`SELECT session_id, note, time_updated FROM session_meta WHERE note IS NOT NULL AND note != ''`)
	if err != nil {
		return fmt.Errorf("query notes: %w", err)
	}

	type noteRow struct {
		sessionID   string
		note        string
		timeUpdated int64
	}
	var notes []noteRow
	for rows.Next() {
		var n noteRow
		if err := rows.Scan(&n.sessionID, &n.note, &n.timeUpdated); err != nil {
			rows.Close()
			return err
		}
		notes = append(notes, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	now := time.Now().UnixMilli()
	for _, n := range notes {
		id := fmt.Sprintf("migrated_%s", n.sessionID)
		ts := n.timeUpdated
		if ts < 1e10 {
			ts = ts * 1000 // convert seconds -> ms if needed
		}
		_, err = tx.Exec(`
			INSERT INTO idea (id, content, source_session_id, time_created, time_updated)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(id) DO NOTHING
		`, id, n.note, n.sessionID, ts, now)
		if err != nil {
			return fmt.Errorf("insert migrated idea: %w", err)
		}
		_, err = tx.Exec(`UPDATE session_meta SET note = NULL WHERE session_id = ?`, n.sessionID)
		if err != nil {
			return fmt.Errorf("null note: %w", err)
		}
	}

	// Record migration as complete
	_, err = tx.Exec(`INSERT INTO schema_version (version, applied_at) VALUES (1, ?)`, now)
	if err != nil {
		return fmt.Errorf("record migration v1: %w", err)
	}

	return tx.Commit()
}

func ListTagsWithSessionCounts(db *sql.DB) ([]model.Tag, map[string]int, error) {
	tags, err := ListAllTags(db)
	if err != nil {
		return nil, nil, err
	}

	rows, err := db.Query(`SELECT tag_name, COUNT(DISTINCT session_id) FROM session_tag GROUP BY tag_name`)
	if err != nil {
		return nil, nil, fmt.Errorf("count sessions per tag: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			return nil, nil, fmt.Errorf("scan tag count: %w", err)
		}
		counts[name] = n
	}
	return tags, counts, rows.Err()
}

func GetIdeasForSession(db *sql.DB, sessionID string) ([]model.Idea, error) {
	rows, err := db.Query(`
		SELECT id, content, source_session_id, time_created, time_updated
		FROM idea
		WHERE source_session_id = ?
		ORDER BY time_created DESC
	`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query ideas for session: %w", err)
	}
	defer rows.Close()

	var ideas []model.Idea
	for rows.Next() {
		var idea model.Idea
		var sourceID sql.NullString
		if err := rows.Scan(&idea.ID, &idea.Content, &sourceID, &idea.TimeCreated, &idea.TimeUpdated); err != nil {
			return nil, fmt.Errorf("scan idea: %w", err)
		}
		idea.SourceSessionID = sourceID.String
		ideas = append(ideas, idea)
	}
	return ideas, rows.Err()
}
