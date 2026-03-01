package model

// Session represents an OpenCode chat session (read from opencode.db)
type Session struct {
	ID          string
	Title       string
	Slug        string
	Directory   string
	ParentID    string // non-empty means this is a forked session
	TimeCreated int64
	TimeUpdated int64
}

// Message is a single user or assistant turn in a session
type Message struct {
	ID          string
	SessionID   string
	Role        string // "user" | "assistant"
	TimeCreated int64
	Parts       []Part
}

// Part is a renderable unit within a message
type Part struct {
	Type       PartType
	Text       string
	ToolName   string
	ToolStatus string
	Filename   string
	MimeType   string
	Reasoning  string
}

// PartType identifies what kind of content a Part holds
type PartType string

const (
	PartTypeText      PartType = "text"
	PartTypeTool      PartType = "tool"
	PartTypeReasoning PartType = "reasoning"
	PartTypePatch     PartType = "patch"
	PartTypeFile      PartType = "file"
	PartTypeUnknown   PartType = "unknown"
)

// Project represents an OpenCode project directory
type Project struct {
	ID   string
	Path string
}
