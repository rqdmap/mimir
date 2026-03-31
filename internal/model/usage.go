package model

import "time"

// ModelStat represents token usage for a specific model
type ModelStat struct {
	ModelID      string
	ProviderID   string
	Turns        int
	Requests     int
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
	CacheWrite   int64
	CachePercent float64 // 0.0 when CacheRead == 0
}

// AgentStat represents token usage for a specific agent
type AgentStat struct {
	Agent        string // normalized to lowercase by DB query
	Turns        int
	Requests     int
	InputTokens  int64
	OutputTokens int64
}

// DailyPoint represents token usage on a single day
type DailyPoint struct {
	Date         time.Time // truncated to day
	Turns        int
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
	CacheWrite   int64
}

// ModelDailyPoint represents token usage for a specific model on a single day.
type ModelDailyPoint struct {
	Date         time.Time
	ModelID      string
	ProviderID   string
	Turns        int
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
	CacheWrite   int64
}

// SessionUsage represents complete token usage summary for a session
type SessionUsage struct {
	UserTurns    int
	AITurns      int
	InputTokens  int64
	OutputTokens int64
	CacheRead    int64
	CacheWrite   int64
	CachePercent float64  // 0.0 when CacheRead == 0
	Models       []string // model names ordered by usage count descending
}

// StatsPeriod is a time window selector for statistics queries
type StatsPeriod string

const (
	PeriodToday StatsPeriod = "today"
	Period7d    StatsPeriod = "7d"
	Period30d   StatsPeriod = "30d"
	PeriodAll   StatsPeriod = "all"
)
