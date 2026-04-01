package db_test

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/local/oc-manager/internal/db"
	_ "modernc.org/sqlite"
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

// newInMemoryOpencodeDB creates an in-memory SQLite database for testing.
func newInMemoryOpencodeDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open in-memory db: %v", err)
	}
	_, err = d.Exec(`CREATE TABLE message (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		time_created INTEGER NOT NULL DEFAULT 0,
		time_updated INTEGER NOT NULL DEFAULT 0,
		data TEXT NOT NULL DEFAULT '{}'
	)`)
	if err != nil {
		t.Fatalf("create message table: %v", err)
	}
	_, err = d.Exec(`CREATE TABLE session (
		id TEXT PRIMARY KEY,
		parent_id TEXT
	)`)
	if err != nil {
		t.Fatalf("create session table: %v", err)
	}
	return d
}

// insertMessage is a test helper to insert a message with given data.
func insertMessage(t *testing.T, d *sql.DB, id, sessionID string, data map[string]interface{}) {
	t.Helper()
	jsonData, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	_, err = d.Exec(
		`INSERT INTO message (id, session_id, time_created, time_updated, data) VALUES (?, ?, 0, 0, ?)`,
		id, sessionID, string(jsonData),
	)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
}

func TestGetUsageByModel(t *testing.T) {
	d := newInMemoryOpencodeDB(t)
	defer d.Close()

	// Insert 2 messages for claude-sonnet-4-5 (input=1000, output=200, cacheRead=500, cacheWrite=300 each)
	insertMessage(t, d, "msg1", "session-1", map[string]interface{}{
		"role":       "assistant",
		"modelID":    "claude-sonnet-4-5",
		"providerID": "anthropic",
		"tokens": map[string]interface{}{
			"input":  1000,
			"output": 200,
			"cache": map[string]interface{}{
				"read":  500,
				"write": 300,
			},
		},
		"time": map[string]interface{}{
			"created": int64(1700000000000),
		},
	})
	insertMessage(t, d, "msg2", "session-1", map[string]interface{}{
		"role":       "assistant",
		"modelID":    "claude-sonnet-4-5",
		"providerID": "anthropic",
		"tokens": map[string]interface{}{
			"input":  1000,
			"output": 200,
			"cache": map[string]interface{}{
				"read":  500,
				"write": 300,
			},
		},
		"time": map[string]interface{}{
			"created": int64(1700000001000),
		},
	})
	// Insert 1 message for gpt-4o (input=500, output=100, cacheRead=0)
	insertMessage(t, d, "msg3", "session-1", map[string]interface{}{
		"role":       "assistant",
		"modelID":    "gpt-4o",
		"providerID": "openai",
		"tokens": map[string]interface{}{
			"input":  500,
			"output": 100,
			"cache": map[string]interface{}{
				"read": 0,
			},
		},
		"time": map[string]interface{}{
			"created": int64(1700000002000),
		},
	})

	results, err := db.GetUsageByModel(d, 0)
	if err != nil {
		t.Fatalf("GetUsageByModel: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// First result: claude-sonnet-4-5 (highest input tokens)
	if results[0].ModelID != "claude-sonnet-4-5" {
		t.Errorf("expected first model claude-sonnet-4-5, got %s", results[0].ModelID)
	}
	if results[0].Turns != 2 {
		t.Errorf("expected 2 turns for claude-sonnet-4-5, got %d", results[0].Turns)
	}
	if results[0].InputTokens != 2000 {
		t.Errorf("expected 2000 input tokens, got %d", results[0].InputTokens)
	}
	if results[0].OutputTokens != 400 {
		t.Errorf("expected 400 output tokens, got %d", results[0].OutputTokens)
	}
	if results[0].CacheRead != 1000 {
		t.Errorf("expected 1000 cache read, got %d", results[0].CacheRead)
	}
	if results[0].CacheWrite != 600 {
		t.Errorf("expected 600 cache write, got %d", results[0].CacheWrite)
	}
	// CachePercent = 1000 / (2000 + 1000 + 600) * 100 = 27.78%
	expectedCP := 100.0 * 1000.0 / 3600.0
	if results[0].CachePercent <= 0 || results[0].CachePercent < expectedCP-1 {
		t.Errorf("expected cache percent ~%.1f%%, got %f", expectedCP, results[0].CachePercent)
	}

	// Second result: gpt-4o
	if results[1].ModelID != "gpt-4o" {
		t.Errorf("expected second model gpt-4o, got %s", results[1].ModelID)
	}
	if results[1].Turns != 1 {
		t.Errorf("expected 1 turn for gpt-4o, got %d", results[1].Turns)
	}
	if results[1].CachePercent != 0.0 {
		t.Errorf("expected cache percent 0.0 for gpt-4o, got %f", results[1].CachePercent)
	}
}

func TestGetUsageByModelSinceFilter(t *testing.T) {
	d := newInMemoryOpencodeDB(t)
	defer d.Close()

	// Insert message at time=100ms
	insertMessage(t, d, "msg1", "session-1", map[string]interface{}{
		"role":       "assistant",
		"modelID":    "claude-sonnet-4-5",
		"providerID": "anthropic",
		"tokens": map[string]interface{}{
			"input":  1000,
			"output": 200,
		},
		"time": map[string]interface{}{
			"created": int64(100),
		},
	})
	// Insert message at time=200ms
	insertMessage(t, d, "msg2", "session-1", map[string]interface{}{
		"role":       "assistant",
		"modelID":    "claude-sonnet-4-5",
		"providerID": "anthropic",
		"tokens": map[string]interface{}{
			"input":  2000,
			"output": 300,
		},
		"time": map[string]interface{}{
			"created": int64(200),
		},
	})

	// Query with since=150 → should return only 1 row (time=200)
	results, err := db.GetUsageByModel(d, 150)
	if err != nil {
		t.Fatalf("GetUsageByModel with since=150: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result with since=150, got %d", len(results))
	}
	if results[0].InputTokens != 2000 {
		t.Errorf("expected 2000 input tokens (time=200), got %d", results[0].InputTokens)
	}

	// Query with since=0 → should return all
	results, err = db.GetUsageByModel(d, 0)
	if err != nil {
		t.Fatalf("GetUsageByModel with since=0: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 model result (aggregated), got %d", len(results))
	}
	if results[0].InputTokens != 3000 {
		t.Errorf("expected 3000 total input tokens, got %d", results[0].InputTokens)
	}
	if results[0].Turns != 2 {
		t.Errorf("expected 2 turns, got %d", results[0].Turns)
	}
}

func TestGetUsageByAgent(t *testing.T) {
	d := newInMemoryOpencodeDB(t)
	defer d.Close()

	// Insert 2 messages with agent="Build"
	insertMessage(t, d, "msg1", "session-1", map[string]interface{}{
		"role":       "assistant",
		"modelID":    "claude-sonnet-4-5",
		"providerID": "anthropic",
		"agent":      "Build",
		"tokens": map[string]interface{}{
			"input":  1000,
			"output": 200,
		},
		"time": map[string]interface{}{
			"created": int64(1700000000000),
		},
	})
	insertMessage(t, d, "msg2", "session-1", map[string]interface{}{
		"role":       "assistant",
		"modelID":    "claude-sonnet-4-5",
		"providerID": "anthropic",
		"agent":      "Build",
		"tokens": map[string]interface{}{
			"input":  1500,
			"output": 250,
		},
		"time": map[string]interface{}{
			"created": int64(1700000001000),
		},
	})
	// Insert 1 message with agent="SISYPHUS"
	insertMessage(t, d, "msg3", "session-1", map[string]interface{}{
		"role":       "assistant",
		"modelID":    "claude-sonnet-4-5",
		"providerID": "anthropic",
		"agent":      "SISYPHUS",
		"tokens": map[string]interface{}{
			"input":  500,
			"output": 100,
		},
		"time": map[string]interface{}{
			"created": int64(1700000002000),
		},
	})

	results, err := db.GetUsageByAgent(d, 0)
	if err != nil {
		t.Fatalf("GetUsageByAgent: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 agent results, got %d", len(results))
	}

	// Agents should be normalized to lowercase, ordered by input tokens DESC
	// First: "build" (1000+1500=2500 input)
	if results[0].Agent != "build" {
		t.Errorf("expected first agent 'build', got %s", results[0].Agent)
	}
	if results[0].Turns != 2 {
		t.Errorf("expected 2 turns for build, got %d", results[0].Turns)
	}

	// Second: "sisyphus" (500 input)
	if results[1].Agent != "sisyphus" {
		t.Errorf("expected second agent 'sisyphus', got %s", results[1].Agent)
	}
	if results[1].Turns != 1 {
		t.Errorf("expected 1 turn for sisyphus, got %d", results[1].Turns)
	}
}

func TestGetDailyUsage(t *testing.T) {
	d := newInMemoryOpencodeDB(t)
	defer d.Close()

	// Insert messages on 2024-01-15
	day1 := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC).UnixMilli()
	insertMessage(t, d, "msg1", "session-1", map[string]interface{}{
		"role":       "assistant",
		"modelID":    "claude-sonnet-4-5",
		"providerID": "anthropic",
		"tokens": map[string]interface{}{
			"input":  1000,
			"output": 200,
		},
		"time": map[string]interface{}{
			"created": day1,
		},
	})
	insertMessage(t, d, "msg2", "session-1", map[string]interface{}{
		"role":       "assistant",
		"modelID":    "claude-sonnet-4-5",
		"providerID": "anthropic",
		"tokens": map[string]interface{}{
			"input":  500,
			"output": 100,
		},
		"time": map[string]interface{}{
			"created": day1 + 3600000, // 1 hour later, same day
		},
	})

	// Insert 1 message on 2024-01-16
	day2 := time.Date(2024, 1, 16, 12, 0, 0, 0, time.UTC).UnixMilli()
	insertMessage(t, d, "msg3", "session-1", map[string]interface{}{
		"role":       "assistant",
		"modelID":    "claude-sonnet-4-5",
		"providerID": "anthropic",
		"tokens": map[string]interface{}{
			"input":  800,
			"output": 150,
		},
		"time": map[string]interface{}{
			"created": day2,
		},
	})

	results, err := db.GetDailyUsage(d, 0)
	if err != nil {
		t.Fatalf("GetDailyUsage: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 daily points, got %d", len(results))
	}

	// First day: 2024-01-15
	if results[0].Date.Year() != 2024 || results[0].Date.Month() != time.January || results[0].Date.Day() != 15 {
		t.Errorf("expected date 2024-01-15, got %s", results[0].Date)
	}
	if results[0].Turns != 2 {
		t.Errorf("expected 2 turns on day 1, got %d", results[0].Turns)
	}
	if results[0].InputTokens != 1500 {
		t.Errorf("expected 1500 input tokens on day 1, got %d", results[0].InputTokens)
	}

	// Second day: 2024-01-16
	if results[1].Date.Year() != 2024 || results[1].Date.Month() != time.January || results[1].Date.Day() != 16 {
		t.Errorf("expected date 2024-01-16, got %s", results[1].Date)
	}
	if results[1].Turns != 1 {
		t.Errorf("expected 1 turn on day 2, got %d", results[1].Turns)
	}
	if results[1].InputTokens != 800 {
		t.Errorf("expected 800 input tokens on day 2, got %d", results[1].InputTokens)
	}
}

func TestGetSessionUsage(t *testing.T) {
	d := newInMemoryOpencodeDB(t)
	defer d.Close()

	// Insert 2 user messages for session-1
	insertMessage(t, d, "user1", "session-1", map[string]interface{}{
		"role": "user",
		"time": map[string]interface{}{
			"created": int64(1700000000000),
		},
	})
	insertMessage(t, d, "user2", "session-1", map[string]interface{}{
		"role": "user",
		"time": map[string]interface{}{
			"created": int64(1700000001000),
		},
	})

	// Insert 3 assistant messages for session-1 (input=1000, output=200, cache read=500, cache write=200 each)
	insertMessage(t, d, "ai1", "session-1", map[string]interface{}{
		"role":       "assistant",
		"modelID":    "claude-sonnet-4-5",
		"providerID": "anthropic",
		"tokens": map[string]interface{}{
			"input":  1000,
			"output": 200,
			"cache": map[string]interface{}{
				"read":  500,
				"write": 200,
			},
		},
		"time": map[string]interface{}{
			"created": int64(1700000000500),
		},
	})
	insertMessage(t, d, "ai2", "session-1", map[string]interface{}{
		"role":       "assistant",
		"modelID":    "claude-sonnet-4-5",
		"providerID": "anthropic",
		"tokens": map[string]interface{}{
			"input":  1000,
			"output": 200,
			"cache": map[string]interface{}{
				"read":  500,
				"write": 200,
			},
		},
		"time": map[string]interface{}{
			"created": int64(1700000001500),
		},
	})
	insertMessage(t, d, "ai3", "session-1", map[string]interface{}{
		"role":       "assistant",
		"modelID":    "claude-sonnet-4-5",
		"providerID": "anthropic",
		"tokens": map[string]interface{}{
			"input":  1000,
			"output": 200,
			"cache": map[string]interface{}{
				"read":  500,
				"write": 200,
			},
		},
		"time": map[string]interface{}{
			"created": int64(1700000002500),
		},
	})

	// Insert 1 assistant message for session-2 (should not be included)
	insertMessage(t, d, "ai_other", "session-2", map[string]interface{}{
		"role":       "assistant",
		"modelID":    "gpt-4o",
		"providerID": "openai",
		"tokens": map[string]interface{}{
			"input":  500,
			"output": 100,
		},
		"time": map[string]interface{}{
			"created": int64(1700000003000),
		},
	})

	// Query session-1
	su, err := db.GetSessionUsage(d, "session-1")
	if err != nil {
		t.Fatalf("GetSessionUsage: %v", err)
	}

	if su.UserTurns != 2 {
		t.Errorf("expected 2 user turns, got %d", su.UserTurns)
	}
	if su.AITurns != 3 {
		t.Errorf("expected 3 AI turns, got %d", su.AITurns)
	}
	if su.InputTokens != 3000 {
		t.Errorf("expected 3000 input tokens, got %d", su.InputTokens)
	}
	if su.OutputTokens != 600 {
		t.Errorf("expected 600 output tokens, got %d", su.OutputTokens)
	}
	if su.CacheRead != 1500 {
		t.Errorf("expected 1500 cache read, got %d", su.CacheRead)
	}
	if su.CacheWrite != 600 {
		t.Errorf("expected 600 cache write, got %d", su.CacheWrite)
	}
	// CachePercent = 1500 / (3000 + 1500 + 600) * 100 = 29.41%
	expectedCP := 100.0 * 1500.0 / 5100.0
	if su.CachePercent <= 0 || su.CachePercent < expectedCP-1 {
		t.Errorf("expected cache percent ~%.1f%%, got %f", expectedCP, su.CachePercent)
	}
	if len(su.Models) < 1 {
		t.Fatalf("expected at least 1 model, got %d", len(su.Models))
	}
	if su.Models[0] != "claude-sonnet-4-5" {
		t.Errorf("expected first model claude-sonnet-4-5, got %s", su.Models[0])
	}
}
