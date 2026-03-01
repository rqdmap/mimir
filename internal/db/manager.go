package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
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
		`CREATE TABLE IF NOT EXISTS tag (
			name  TEXT PRIMARY KEY,
			color TEXT NOT NULL DEFAULT '#7D56F4'
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:min(len(stmt), 40)], err)
		}
	}
	return nil
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

// RemoveSessionTag removes a tag from a session.
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
	now := time.Now().Unix()

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
	for rows.Next() {
		var idea model.Idea
		var sourceID sql.NullString
		if err := rows.Scan(&idea.ID, &idea.Content, &sourceID, &idea.TimeCreated, &idea.TimeUpdated); err != nil {
			return nil, fmt.Errorf("scan idea: %w", err)
		}
		idea.SourceSessionID = sourceID.String
		ideas = append(ideas, idea)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fetch tags for each idea.
	for i := range ideas {
		tags, err := getIdeaTags(db, ideas[i].ID)
		if err != nil {
			return nil, err
		}
		ideas[i].Tags = tags
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

	now := time.Now().Unix()
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
