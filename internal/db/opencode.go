package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/local/oc-manager/internal/model"
)

// OpenOpencodeDB opens opencode.db in READ-ONLY mode
func OpenOpencodeDB() (*sql.DB, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get home dir: %w", err)
	}
	dbPath := filepath.Join(home, ".local", "share", "opencode", "opencode.db")
	// MUST use ?mode=ro to enforce read-only
	dsn := fmt.Sprintf("file:%s?mode=ro&_journal_mode=WAL&_cache_size=-65536&mmap_size=268435456", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open opencode db: %w", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping opencode db: %w", err)
	}
	return db, nil
}

// ListSessions returns all non-archived sessions, newest first.
// The actual schema stores session data in columns (not a JSON blob).
func ListSessions(db *sql.DB) ([]model.Session, error) {
	const query = `
		SELECT id, title, slug, directory, COALESCE(parent_id, ''), time_created, time_updated
		FROM session
		WHERE time_archived IS NULL
		ORDER BY time_updated DESC
	`
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []model.Session
	for rows.Next() {
		var s model.Session
		if err := rows.Scan(&s.ID, &s.Title, &s.Slug, &s.Directory, &s.ParentID, &s.TimeCreated, &s.TimeUpdated); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return sessions, nil
}

// ListSessionsPage returns up to limit sessions starting at offset, newest first.
func ListSessionsPage(db *sql.DB, limit, offset int) ([]model.Session, error) {
	const query = `
		SELECT id, title, slug, directory, COALESCE(parent_id, ''), time_created, time_updated
		FROM session
		WHERE time_archived IS NULL
		ORDER BY time_updated DESC
		LIMIT ? OFFSET ?
	`
	rows, err := db.Query(query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query sessions page: %w", err)
	}
	defer rows.Close()
	var sessions []model.Session
	for rows.Next() {
		var s model.Session
		if err := rows.Scan(&s.ID, &s.Title, &s.Slug, &s.Directory, &s.ParentID, &s.TimeCreated, &s.TimeUpdated); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return sessions, nil
}

func CountSessions(db *sql.DB) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM session WHERE time_archived IS NULL`).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count sessions: %w", err)
	}
	return n, err
}

// messageData is the JSON structure stored in message.data
type messageData struct {
	Role string `json:"role"`
	Time struct {
		Created int64 `json:"created"`
	} `json:"time"`
}

// LoadSessionMessages loads messages + parts for ONE session.
// Skip: step-start, step-finish, agent, compaction part types.
// For file parts: parse filename/mime ONLY — do NOT read url field (base64).
// For unknown types: log.Printf warning and skip — never panic.
func LoadSessionMessages(db *sql.DB, sessionID string) ([]model.Message, error) {
	// Load messages
	const msgQuery = `
		SELECT id, data, time_created
		FROM message
		WHERE session_id = ?
		ORDER BY time_created ASC
	`
	msgRows, err := db.Query(msgQuery, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer msgRows.Close()

	var messages []model.Message
	for msgRows.Next() {
		var (
			id          string
			dataJSON    string
			timeCreated int64
		)
		if err := msgRows.Scan(&id, &dataJSON, &timeCreated); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}

		var md messageData
		if err := json.Unmarshal([]byte(dataJSON), &md); err != nil {
			log.Printf("skipping message %s: bad JSON: %v", id, err)
			continue
		}

		msg := model.Message{
			ID:          id,
			SessionID:   sessionID,
			Role:        md.Role,
			TimeCreated: timeCreated,
		}
		messages = append(messages, msg)
	}
	if err := msgRows.Err(); err != nil {
		return nil, fmt.Errorf("message rows error: %w", err)
	}

	// Load ALL parts for this session in ONE query
	const batchPartQuery = `
		SELECT id, message_id, data
		FROM part
		WHERE message_id IN (SELECT id FROM message WHERE session_id = ?)
		ORDER BY time_created ASC
	`
	partRows, err := db.Query(batchPartQuery, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query parts: %w", err)
	}
	defer partRows.Close()

	partsByMsg := make(map[string][]model.Part)
	for partRows.Next() {
		var (
			partID   string
			msgID    string
			dataJSON string
		)
		if err := partRows.Scan(&partID, &msgID, &dataJSON); err != nil {
			return nil, fmt.Errorf("scan part: %w", err)
		}

		part := parsePart(partID, dataJSON)
		if part != nil {
			partsByMsg[msgID] = append(partsByMsg[msgID], *part)
		}
	}
	if err := partRows.Err(); err != nil {
		return nil, fmt.Errorf("part rows error: %w", err)
	}

	// Assign parts to messages
	for i := range messages {
		messages[i].Parts = partsByMsg[messages[i].ID]
	}

	return messages, nil
}

// parsePart safely parses a part's JSON data into a model.Part.
// Returns nil for skipped/unknown types or on parse errors.
func parsePart(partID, dataJSON string) *model.Part {
	// First extract the type field
	var typedPart struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal([]byte(dataJSON), &typedPart); err != nil {
		log.Printf("skipping part %s: bad JSON: %v", partID, err)
		return nil
	}

	partType := typedPart.Type

	// Skip these types entirely
	switch partType {
	case "step-start", "step-finish", "agent", "compaction":
		return nil
	}

	switch partType {
	case "text":
		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(dataJSON), &p); err != nil {
			log.Printf("skipping text part %s: bad JSON: %v", partID, err)
			return nil
		}
		return &model.Part{
			Type: model.PartTypeText,
			Text: p.Text,
		}

	case "tool":
		// {"type":"tool","callID":"...","tool":"name","state":{"status":"...",...}}
		var p struct {
			Tool  string `json:"tool"`
			State struct {
				Status string `json:"status"`
			} `json:"state"`
		}
		if err := json.Unmarshal([]byte(dataJSON), &p); err != nil {
			log.Printf("skipping tool part %s: bad JSON: %v", partID, err)
			return nil
		}
		return &model.Part{
			Type:       model.PartTypeTool,
			ToolName:   p.Tool,
			ToolStatus: p.State.Status,
		}

	case "reasoning":
		// {"type":"reasoning","text":"...","time":{...}}
		var p struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(dataJSON), &p); err != nil {
			log.Printf("skipping reasoning part %s: bad JSON: %v", partID, err)
			return nil
		}
		return &model.Part{
			Type:      model.PartTypeReasoning,
			Reasoning: p.Text,
		}

	case "patch":
		// {"type":"patch","hash":"...","files":["path1","path2"]}
		var p struct {
			Files []string `json:"files"`
		}
		if err := json.Unmarshal([]byte(dataJSON), &p); err != nil {
			log.Printf("skipping patch part %s: bad JSON: %v", partID, err)
			return nil
		}
		return &model.Part{
			Type: model.PartTypePatch,
			Text: strings.Join(p.Files, ", "),
		}

	case "file":
		// {"type":"file","mime":"...","filename":"...","url":"data:..."}
		// ONLY extract filename and mime — skip url (base64, can be MBs)
		var p struct {
			Filename string `json:"filename"`
			MimeType string `json:"mime"` // actual field is "mime" not "mimeType"
		}
		if err := json.Unmarshal([]byte(dataJSON), &p); err != nil {
			log.Printf("skipping file part %s: bad JSON: %v", partID, err)
			return nil
		}
		return &model.Part{
			Type:     model.PartTypeFile,
			Filename: p.Filename,
			MimeType: p.MimeType,
		}

	default:
		log.Printf("skipping unknown part type: %s", partType)
		return nil
	}
}

// ListProjects returns all projects.
func ListProjects(db *sql.DB) ([]model.Project, error) {
	const query = `SELECT id, worktree FROM project ORDER BY id`
	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query projects: %w", err)
	}
	defer rows.Close()

	var projects []model.Project
	for rows.Next() {
		var p model.Project
		if err := rows.Scan(&p.ID, &p.Path); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		projects = append(projects, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("project rows error: %w", err)
	}
	return projects, nil
}
