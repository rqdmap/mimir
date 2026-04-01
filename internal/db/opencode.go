package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	// MUST use ?mode=ro to enforce read-only.
	// Do NOT set journal_mode on a read-only connection — it would attempt
	// a write and fail with SQLITE_READONLY. WAL mode is managed by opencode.
	//
	// IMPORTANT: modernc.org/sqlite uses _pragma=NAME(VALUE) syntax, NOT
	// _busy_timeout= or _journal_mode= (those are mattn/go-sqlite3 format
	// and are silently ignored by modernc).
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)&_pragma=cache_size(-65536)&_pragma=mmap_size(536870912)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open opencode db: %w", err)
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

	// Load ALL parts for this session in ONE query using JSON_EXTRACT to avoid
	// reading the base64 url field of file-type parts into memory.
	const batchPartQuery = `
		SELECT
		    p.id,
		    p.message_id,
		    JSON_EXTRACT(p.data, '$.type')  AS part_type,
		    CASE JSON_EXTRACT(p.data, '$.type')
		        WHEN 'text'      THEN JSON_EXTRACT(p.data, '$.text')
		        WHEN 'reasoning' THEN JSON_EXTRACT(p.data, '$.text')
		        WHEN 'subtask'   THEN JSON_EXTRACT(p.data, '$.description')
		        ELSE NULL
		    END AS text_content,
		    CASE JSON_EXTRACT(p.data, '$.type')
		        WHEN 'tool' THEN json_object(
		            'tool', JSON_EXTRACT(p.data, '$.tool'),
		            'state', json_object(
		                'status', JSON_EXTRACT(p.data, '$.state.status'),
		                'input', SUBSTR(COALESCE(JSON_EXTRACT(p.data, '$.state.input'), ''), 1, 2000),
		                'output', SUBSTR(COALESCE(JSON_EXTRACT(p.data, '$.state.output'), ''), 1, 4000)
		            )
		        )
		        WHEN 'subtask' THEN JSON_EXTRACT(p.data, '$.agent')
		        ELSE NULL
		    END AS tool_data,
		    CASE JSON_EXTRACT(p.data, '$.type')
		        WHEN 'file' THEN JSON_EXTRACT(p.data, '$.filename')
		        ELSE NULL
		    END AS filename,
		    CASE JSON_EXTRACT(p.data, '$.type')
		        WHEN 'file' THEN JSON_EXTRACT(p.data, '$.mime')
		        ELSE NULL
		    END AS mime_type,
		    CASE JSON_EXTRACT(p.data, '$.type')
		        WHEN 'patch' THEN JSON_EXTRACT(p.data, '$.files')
		        ELSE NULL
		    END AS patch_files
		FROM part p
		WHERE p.message_id IN (SELECT id FROM message WHERE session_id = ?)
		  AND JSON_EXTRACT(p.data, '$.type') NOT IN ('step-start', 'step-finish', 'agent', 'compaction')
		ORDER BY p.time_created ASC
	`
	partRows, err := db.Query(batchPartQuery, sessionID)
	if err != nil {
		return nil, fmt.Errorf("query parts: %w", err)
	}
	defer partRows.Close()

	partsByMsg := make(map[string][]model.Part)
	for partRows.Next() {
		var partID, msgID string
		var partType, textContent, toolData, filename, mimeType, patchFiles sql.NullString
		if err := partRows.Scan(&partID, &msgID, &partType, &textContent, &toolData, &filename, &mimeType, &patchFiles); err != nil {
			return nil, fmt.Errorf("scan part: %w", err)
		}

		part := parsePartFromFields(partID, partType, textContent, toolData, filename, mimeType, patchFiles)
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

// parsePartFromFields builds a model.Part from pre-extracted SQL columns.
// file parts never have their url field loaded — only filename and mime.
func parsePartFromFields(id string, partType, textContent, toolData, filename, mimeType, patchFiles sql.NullString) *model.Part {
	switch partType.String {
	case "text":
		return &model.Part{Type: model.PartTypeText, Text: textContent.String}
	case "reasoning":
		return &model.Part{Type: model.PartTypeReasoning, Reasoning: textContent.String}
	case "tool":
		var p struct {
			Tool  string `json:"tool"`
			State struct {
				Status string `json:"status"`
				Input  string `json:"input"`
				Output string `json:"output"`
			} `json:"state"`
		}
		if err := json.Unmarshal([]byte(toolData.String), &p); err != nil {
			log.Printf("skipping tool part %s: bad JSON: %v", id, err)
			return nil
		}
		return &model.Part{Type: model.PartTypeTool, ToolName: p.Tool, ToolStatus: p.State.Status, ToolInput: p.State.Input, ToolOutput: p.State.Output}
	case "file":
		return &model.Part{Type: model.PartTypeFile, Filename: filename.String, MimeType: mimeType.String}
	case "patch":
		var files []string
		if patchFiles.Valid && patchFiles.String != "" {
			_ = json.Unmarshal([]byte(patchFiles.String), &files)
		}
		return &model.Part{Type: model.PartTypePatch, Text: strings.Join(files, ", ")}
	case "subtask":
		return &model.Part{Type: model.PartTypeSubtask, Text: textContent.String, ToolName: toolData.String}
	default:
		log.Printf("skipping unknown part type: %s", partType.String)
		return nil
	}
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
				Input  string `json:"input"`
				Output string `json:"output"`
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
			ToolInput:  p.State.Input,
			ToolOutput: p.State.Output,
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

	case "subtask":
		var p struct {
			Description string `json:"description"`
			Agent       string `json:"agent"`
		}
		if err := json.Unmarshal([]byte(dataJSON), &p); err != nil {
			log.Printf("skipping subtask part %s: bad JSON: %v", partID, err)
			return nil
		}
		return &model.Part{
			Type:     model.PartTypeSubtask,
			Text:     p.Description,
			ToolName: p.Agent,
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

// GetUsageByModel returns token usage aggregated by model, optionally filtered
// by a Unix millisecond timestamp (since). Pass since=0 to return all data.
func GetUsageByModel(db *sql.DB, since int64) ([]model.ModelStat, error) {
	var (
		aiCTESince   string
		userCTESince string
		mainSince    string
		args         []interface{}
	)
	if since > 0 {
		aiCTESince = "AND CAST(JSON_EXTRACT(data, '$.time.created') AS INTEGER) >= ?"
		userCTESince = "AND CAST(JSON_EXTRACT(u.data, '$.time.created') AS INTEGER) >= ?"
		mainSince = "AND CAST(JSON_EXTRACT(m.data, '$.time.created') AS INTEGER) >= ?"
		args = append(args, since, since, since)
	}

	query := fmt.Sprintf(`
		WITH model_sessions AS (
			SELECT DISTINCT
				JSON_EXTRACT(data, '$.modelID')    AS modelID,
				JSON_EXTRACT(data, '$.providerID') AS providerID,
				session_id
			FROM message
			WHERE JSON_EXTRACT(data, '$.role') = 'assistant'
			  AND JSON_EXTRACT(data, '$.modelID') IS NOT NULL
			  %s
		),
		user_reqs AS (
			SELECT
				ms.modelID,
				ms.providerID,
				COUNT(*)                                         AS user_requests,
				COUNT(CASE WHEN s.parent_id IS NULL THEN 1 END) AS human_requests
			FROM message u
			JOIN session s ON u.session_id = s.id
			JOIN model_sessions ms ON u.session_id = ms.session_id
			WHERE JSON_EXTRACT(u.data, '$.role') = 'user'
			  %s
			GROUP BY ms.modelID, ms.providerID
		)
		SELECT
			JSON_EXTRACT(m.data, '$.modelID')    AS modelID,
			JSON_EXTRACT(m.data, '$.providerID') AS providerID,
			COUNT(*)                             AS turns,
			COUNT(DISTINCT m.session_id)         AS sessions,
			COALESCE(ur.user_requests,  0)       AS user_requests,
			COALESCE(ur.human_requests, 0)       AS human_requests,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.input')       AS INTEGER)), 0) AS inputTokens,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.output')      AS INTEGER)), 0) AS outputTokens,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.cache.read')  AS INTEGER)), 0) AS cacheRead,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.cache.write') AS INTEGER)), 0) AS cacheWrite
		FROM message m
		LEFT JOIN user_reqs ur
			ON JSON_EXTRACT(m.data, '$.modelID')    = ur.modelID
		   AND JSON_EXTRACT(m.data, '$.providerID') = ur.providerID
		WHERE JSON_EXTRACT(m.data, '$.role') = 'assistant'
		  AND JSON_EXTRACT(m.data, '$.modelID') IS NOT NULL
		  %s
		GROUP BY JSON_EXTRACT(m.data, '$.modelID'), JSON_EXTRACT(m.data, '$.providerID')
		ORDER BY inputTokens DESC`, aiCTESince, userCTESince, mainSince)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query usage by model: %w", err)
	}
	defer rows.Close()

	var results []model.ModelStat
	for rows.Next() {
		var s model.ModelStat
		if err := rows.Scan(&s.ModelID, &s.ProviderID, &s.Turns, &s.Sessions,
			&s.UserRequests, &s.HumanRequests,
			&s.InputTokens, &s.OutputTokens, &s.CacheRead, &s.CacheWrite); err != nil {
			return nil, fmt.Errorf("scan model stat: %w", err)
		}
		total := s.InputTokens + s.CacheRead + s.CacheWrite
		if total > 0 {
			s.CachePercent = float64(s.CacheRead) / float64(total) * 100.0
		}
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error (usage by model): %w", err)
	}
	return results, nil
}

func GetUsageByAgent(db *sql.DB, since int64) ([]model.AgentStat, error) {
	var (
		aiCTESince   string
		userCTESince string
		mainSince    string
		args         []interface{}
	)
	if since > 0 {
		aiCTESince = "AND CAST(JSON_EXTRACT(data, '$.time.created') AS INTEGER) >= ?"
		userCTESince = "AND CAST(JSON_EXTRACT(u.data, '$.time.created') AS INTEGER) >= ?"
		mainSince = "AND CAST(JSON_EXTRACT(m.data, '$.time.created') AS INTEGER) >= ?"
		args = append(args, since, since, since)
	}

	query := fmt.Sprintf(`
		WITH agent_sessions AS (
			SELECT DISTINCT
				LOWER(TRIM(COALESCE(JSON_EXTRACT(data, '$.agent'), ''))) AS agent,
				session_id
			FROM message
			WHERE JSON_EXTRACT(data, '$.role') = 'assistant'
			  AND LOWER(TRIM(COALESCE(JSON_EXTRACT(data, '$.agent'), ''))) != ''
			  AND LOWER(TRIM(COALESCE(JSON_EXTRACT(data, '$.agent'), ''))) != 'compaction'
			  %s
		),
		user_reqs AS (
			SELECT
				ag.agent,
				COUNT(*)                                         AS user_requests,
				COUNT(CASE WHEN s.parent_id IS NULL THEN 1 END) AS human_requests
			FROM message u
			JOIN session s ON u.session_id = s.id
			JOIN agent_sessions ag ON u.session_id = ag.session_id
			WHERE JSON_EXTRACT(u.data, '$.role') = 'user'
			  %s
			GROUP BY ag.agent
		)
		SELECT
			LOWER(TRIM(COALESCE(JSON_EXTRACT(m.data, '$.agent'), ''))) AS agent,
			COUNT(*)                                                   AS turns,
			COUNT(DISTINCT m.session_id)                               AS sessions,
			COALESCE(ur.user_requests,  0)                             AS user_requests,
			COALESCE(ur.human_requests, 0)                             AS human_requests,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.input')       AS INTEGER)), 0) AS inputTokens,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.output')      AS INTEGER)), 0) AS outputTokens,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.cache.read')  AS INTEGER)), 0) AS cacheRead,
			COALESCE(SUM(CAST(JSON_EXTRACT(m.data, '$.tokens.cache.write') AS INTEGER)), 0) AS cacheWrite
		FROM message m
		LEFT JOIN user_reqs ur
			ON LOWER(TRIM(COALESCE(JSON_EXTRACT(m.data, '$.agent'), ''))) = ur.agent
		WHERE JSON_EXTRACT(m.data, '$.role') = 'assistant'
		  AND LOWER(TRIM(COALESCE(JSON_EXTRACT(m.data, '$.agent'), ''))) != ''
		  AND LOWER(TRIM(COALESCE(JSON_EXTRACT(m.data, '$.agent'), ''))) != 'compaction'
		  %s
		GROUP BY LOWER(TRIM(COALESCE(JSON_EXTRACT(m.data, '$.agent'), '')))
		ORDER BY inputTokens DESC`, aiCTESince, userCTESince, mainSince)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query usage by agent: %w", err)
	}
	defer rows.Close()

	var results []model.AgentStat
	for rows.Next() {
		var s model.AgentStat
		if err := rows.Scan(&s.Agent, &s.Turns, &s.Sessions,
			&s.UserRequests, &s.HumanRequests,
			&s.InputTokens, &s.OutputTokens, &s.CacheRead, &s.CacheWrite); err != nil {
			return nil, fmt.Errorf("scan agent stat: %w", err)
		}
		total := s.InputTokens + s.CacheRead + s.CacheWrite
		if total > 0 {
			s.CachePercent = float64(s.CacheRead) / float64(total) * 100.0
		}
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error (usage by agent): %w", err)
	}
	return results, nil
}

// GetDailyUsage returns token usage aggregated by calendar day (local time),
// ordered chronologically. Pass since=0 for all data.
func GetDailyUsage(db *sql.DB, since int64) ([]model.DailyPoint, error) {
	sinceClause := ""
	var args []interface{}
	if since > 0 {
		sinceClause = "AND CAST(JSON_EXTRACT(data, '$.time.created') AS INTEGER) >= ?"
		args = append(args, since, since)
	}

	query := fmt.Sprintf(`
		WITH daily_ai AS (
			SELECT
				date(CAST(JSON_EXTRACT(data, '$.time.created') AS INTEGER)/1000, 'unixepoch', 'localtime') AS day,
				COUNT(*)                   AS turns,
				COUNT(DISTINCT session_id) AS sessions,
				COALESCE(SUM(CAST(JSON_EXTRACT(data, '$.tokens.input')       AS INTEGER)), 0) AS inputTokens,
				COALESCE(SUM(CAST(JSON_EXTRACT(data, '$.tokens.output')      AS INTEGER)), 0) AS outputTokens,
				COALESCE(SUM(CAST(JSON_EXTRACT(data, '$.tokens.cache.read')  AS INTEGER)), 0) AS cacheRead,
				COALESCE(SUM(CAST(JSON_EXTRACT(data, '$.tokens.cache.write') AS INTEGER)), 0) AS cacheWrite
			FROM message
			WHERE JSON_EXTRACT(data, '$.role') = 'assistant' %s
			GROUP BY day
		),
		daily_user AS (
			SELECT
				date(CAST(JSON_EXTRACT(data, '$.time.created') AS INTEGER)/1000, 'unixepoch', 'localtime') AS day,
				COUNT(*)                                                                AS user_requests,
				COUNT(CASE WHEN s.parent_id IS NULL THEN 1 END)                        AS human_requests
			FROM message m
			JOIN session s ON m.session_id = s.id
			WHERE JSON_EXTRACT(m.data, '$.role') = 'user' %s
			GROUP BY day
		)
		SELECT a.day, a.turns, a.inputTokens, a.outputTokens, a.cacheRead, a.cacheWrite,
		       a.sessions, COALESCE(u.user_requests, 0), COALESCE(u.human_requests, 0)
		FROM daily_ai a
		LEFT JOIN daily_user u ON a.day = u.day
		ORDER BY a.day ASC`, sinceClause, sinceClause)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query daily usage: %w", err)
	}
	defer rows.Close()

	var results []model.DailyPoint
	for rows.Next() {
		var dayStr sql.NullString
		var dp model.DailyPoint
		if err := rows.Scan(&dayStr, &dp.Turns, &dp.InputTokens, &dp.OutputTokens, &dp.CacheRead, &dp.CacheWrite, &dp.Sessions, &dp.UserRequests, &dp.HumanRequests); err != nil {
			return nil, fmt.Errorf("scan daily point: %w", err)
		}
		if !dayStr.Valid || dayStr.String == "" {
			continue
		}
		t, err := time.Parse("2006-01-02", dayStr.String)
		if err != nil {
			continue
		}
		dp.Date = t
		results = append(results, dp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error (daily usage): %w", err)
	}
	return results, nil
}

func GetDailyUserRequestsByProvider(db *sql.DB, since int64) ([]model.UserDailyPoint, error) {
	sinceClause := ""
	var args []interface{}
	if since > 0 {
		sinceClause = "AND CAST(JSON_EXTRACT(m.data, '$.time.created') AS INTEGER) >= ?"
		args = append(args, since)
	}
	query := fmt.Sprintf(`
		SELECT
			date(CAST(JSON_EXTRACT(m.data, '$.time.created') AS INTEGER)/1000, 'unixepoch', 'localtime') AS day,
			COALESCE(JSON_EXTRACT(m.data, '$.model.providerID'), '') AS provider_id,
			COUNT(*) AS user_requests,
			COUNT(CASE WHEN s.parent_id IS NULL THEN 1 END) AS human_requests
		FROM message m
		JOIN session s ON m.session_id = s.id
		WHERE JSON_EXTRACT(m.data, '$.role') = 'user' %s
		GROUP BY day, provider_id
		ORDER BY day ASC`, sinceClause)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query daily user requests by provider: %w", err)
	}
	defer rows.Close()

	var results []model.UserDailyPoint
	for rows.Next() {
		var dayStr sql.NullString
		var dp model.UserDailyPoint
		if err := rows.Scan(&dayStr, &dp.ProviderID, &dp.UserRequests, &dp.HumanRequests); err != nil {
			return nil, fmt.Errorf("scan user daily point: %w", err)
		}
		if !dayStr.Valid || dayStr.String == "" {
			continue
		}
		t, err := time.Parse("2006-01-02", dayStr.String)
		if err != nil {
			continue
		}
		dp.Date = t
		results = append(results, dp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error (daily user requests by provider): %w", err)
	}
	return results, nil
}

func GetDailyUsageByModel(db *sql.DB, since int64) ([]model.ModelDailyPoint, error) {
	query := `
		SELECT
			date(CAST(JSON_EXTRACT(data, '$.time.created') AS INTEGER)/1000, 'unixepoch', 'localtime') AS day,
			COALESCE(JSON_EXTRACT(data, '$.modelID'),    '') AS model_id,
			COALESCE(JSON_EXTRACT(data, '$.providerID'), '') AS provider_id,
			COUNT(*)                                                                                    AS turns,
			COALESCE(SUM(CAST(JSON_EXTRACT(data, '$.tokens.input')       AS INTEGER)), 0) AS inputTokens,
			COALESCE(SUM(CAST(JSON_EXTRACT(data, '$.tokens.output')      AS INTEGER)), 0) AS outputTokens,
			COALESCE(SUM(CAST(JSON_EXTRACT(data, '$.tokens.cache.read')  AS INTEGER)), 0) AS cacheRead,
			COALESCE(SUM(CAST(JSON_EXTRACT(data, '$.tokens.cache.write') AS INTEGER)), 0) AS cacheWrite
		FROM message
		WHERE JSON_EXTRACT(data, '$.role') = 'assistant'
		  AND JSON_EXTRACT(data, '$.modelID') IS NOT NULL`
	args := []interface{}{}
	if since > 0 {
		query += `
		  AND CAST(JSON_EXTRACT(data, '$.time.created') AS INTEGER) >= ?`
		args = append(args, since)
	}
	query += `
		GROUP BY day, model_id, provider_id
		ORDER BY day ASC`

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query daily usage by model: %w", err)
	}
	defer rows.Close()

	var results []model.ModelDailyPoint
	for rows.Next() {
		var dayStr sql.NullString
		var dp model.ModelDailyPoint
		if err := rows.Scan(&dayStr, &dp.ModelID, &dp.ProviderID, &dp.Turns, &dp.InputTokens, &dp.OutputTokens, &dp.CacheRead, &dp.CacheWrite); err != nil {
			return nil, fmt.Errorf("scan model daily point: %w", err)
		}
		if !dayStr.Valid || dayStr.String == "" {
			continue
		}
		t, err := time.Parse("2006-01-02", dayStr.String)
		if err != nil {
			continue
		}
		dp.Date = t
		results = append(results, dp)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error (daily usage by model): %w", err)
	}
	return results, nil
}

// GetDistinctSessionCount returns the number of distinct sessions that had at
// least one assistant message in the given period. Pass since=0 for all data.
func GetDistinctSessionCount(db *sql.DB, since int64) (int, error) {
	query := `SELECT COUNT(DISTINCT session_id) FROM message WHERE JSON_EXTRACT(data, '$.role') = 'assistant'`
	args := []interface{}{}
	if since > 0 {
		query += ` AND CAST(JSON_EXTRACT(data, '$.time.created') AS INTEGER) >= ?`
		args = append(args, since)
	}
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("query distinct session count: %w", err)
	}
	return n, nil
}

func GetUserRequestCount(db *sql.DB, since int64) (int, error) {
	query := `SELECT COUNT(*) FROM message WHERE JSON_EXTRACT(data, '$.role') = 'user'`
	args := []interface{}{}
	if since > 0 {
		query += ` AND CAST(JSON_EXTRACT(data, '$.time.created') AS INTEGER) >= ?`
		args = append(args, since)
	}
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("query user request count: %w", err)
	}
	return n, nil
}

func GetHumanRequestCount(db *sql.DB, since int64) (int, error) {
	query := `
		SELECT COUNT(*) FROM message m
		JOIN session s ON m.session_id = s.id
		WHERE JSON_EXTRACT(m.data, '$.role') = 'user'
		  AND s.parent_id IS NULL`
	args := []interface{}{}
	if since > 0 {
		query += ` AND CAST(JSON_EXTRACT(m.data, '$.time.created') AS INTEGER) >= ?`
		args = append(args, since)
	}
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("query human request count: %w", err)
	}
	return n, nil
}

// GetSessionUsage returns a complete token usage summary for a single session.
func GetSessionUsage(db *sql.DB, sessionID string) (model.SessionUsage, error) {
	var su model.SessionUsage

	if err := db.QueryRow(
		`SELECT COUNT(*) FROM message WHERE session_id=? AND JSON_EXTRACT(data,'$.role')='user'`,
		sessionID,
	).Scan(&su.UserTurns); err != nil {
		return model.SessionUsage{}, fmt.Errorf("query user turns: %w", err)
	}

	if err := db.QueryRow(
		`SELECT COUNT(*),
			COALESCE(SUM(CAST(JSON_EXTRACT(data,'$.tokens.input')       AS INTEGER)), 0),
			COALESCE(SUM(CAST(JSON_EXTRACT(data,'$.tokens.output')      AS INTEGER)), 0),
			COALESCE(SUM(CAST(JSON_EXTRACT(data,'$.tokens.cache.read')  AS INTEGER)), 0),
			COALESCE(SUM(CAST(JSON_EXTRACT(data,'$.tokens.cache.write') AS INTEGER)), 0)
		FROM message
		WHERE session_id=? AND JSON_EXTRACT(data,'$.role')='assistant'`,
		sessionID,
	).Scan(&su.AITurns, &su.InputTokens, &su.OutputTokens, &su.CacheRead, &su.CacheWrite); err != nil {
		return model.SessionUsage{}, fmt.Errorf("query ai turns: %w", err)
	}

	total := su.InputTokens + su.CacheRead + su.CacheWrite
	if total > 0 {
		su.CachePercent = float64(su.CacheRead) / float64(total) * 100.0
	}

	rows, err := db.Query(
		`SELECT JSON_EXTRACT(data,'$.modelID'), COUNT(*) AS cnt
		FROM message
		WHERE session_id=?
		  AND JSON_EXTRACT(data,'$.role')='assistant'
		  AND JSON_EXTRACT(data,'$.modelID') IS NOT NULL
		GROUP BY JSON_EXTRACT(data,'$.modelID')
		ORDER BY cnt DESC`,
		sessionID,
	)
	if err != nil {
		return model.SessionUsage{}, fmt.Errorf("query session models: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var modelID string
		var cnt int
		if err := rows.Scan(&modelID, &cnt); err != nil {
			return model.SessionUsage{}, fmt.Errorf("scan session model: %w", err)
		}
		su.Models = append(su.Models, modelID)
	}
	if err := rows.Err(); err != nil {
		return model.SessionUsage{}, fmt.Errorf("rows error (session models): %w", err)
	}

	return su, nil
}
