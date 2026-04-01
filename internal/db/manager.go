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
	return openManagerDBAt(filepath.Join(home, ".local", "share", "oc-manager"))
}

func OpenManagerDBAt(dbDir string) (*sql.DB, error) {
	return openManagerDBAt(dbDir)
}

func openManagerDBAt(dbDir string) (*sql.DB, error) {
	dbPath := filepath.Join(dbDir, "manager.db")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dbDir, err)
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
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
		`CREATE TABLE IF NOT EXISTS tag (
			name  TEXT PRIMARY KEY,
			color TEXT NOT NULL DEFAULT '#7D56F4'
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
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

// DeleteTag removes a tag and all its session associations.
func DeleteTag(db *sql.DB, tagName string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

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

// GetSetting retrieves a value from the settings table. Returns "" if not found.
func GetSetting(d *sql.DB, key string) (string, error) {
	var value string
	err := d.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get setting %q: %w", key, err)
	}
	return value, nil
}

// SetSetting upserts a key-value pair in the settings table.
func SetSetting(d *sql.DB, key, value string) error {
	_, err := d.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
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
