package model

// Tag is a label that can be applied to sessions
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
