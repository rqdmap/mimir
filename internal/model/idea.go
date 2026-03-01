package model

// Idea is a user-captured note/thought stored in manager.db
type Idea struct {
	ID              string
	Content         string
	SourceSessionID string
	Tags            []string
	TimeCreated     int64
	TimeUpdated     int64
}

// Tag is a label that can be applied to sessions or ideas
type Tag struct {
	Name  string
	Color string
}

// SessionMeta holds user-added metadata for an OpenCode session
type SessionMeta struct {
	SessionID string
	Note      string
	Tags      []string
}
