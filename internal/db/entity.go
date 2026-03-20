package db

import "time"

// Session represents a chat/agent/workflow session.
type Session struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`  // chat, agent, workflow, composite
	State     string    `json:"state"` // active, completed, expired
	UserID    int64     `json:"user_id"`
	AgentIDs  []string  `json:"agent_ids,omitempty"`
	RunIDs    []int64   `json:"run_ids,omitempty"`
	Context   []byte    `json:"context,omitempty"` // JSON-encoded context
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// UserProfile stores user role, interests, and preferences.
type UserProfile struct {
	UserID    int64    `json:"user_id"`
	Role      string   `json:"role,omitempty"`
	Interests []string `json:"interests,omitempty"`
	Notes     string   `json:"notes,omitempty"`
	Lang      string   `json:"lang,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UserMemory is a structured memory unit with metadata.
type UserMemory struct {
	ID         string    `json:"id"`
	UserID     int64     `json:"user_id,omitempty"`
	Memory     string    `json:"memory"`
	Topics     []string  `json:"topics,omitempty"`
	TokenCount int       `json:"token_count,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ConversationSummary stores a compressed summary of a conversation.
type ConversationSummary struct {
	ConversationID int64     `json:"conversation_id"`
	UserID         int64     `json:"user_id,omitempty"`
	Summary        string    `json:"summary"`
	MessageCount   int       `json:"message_count"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// CachedDocument is a document synced from Cloud server.
type CachedDocument struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id,omitempty"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CachedTask is a task synced from Cloud server.
type CachedTask struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id,omitempty"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Priority  string    `json:"priority,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	DueDate   string    `json:"due_date,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TraceEvent is a single traceable event.
type TraceEvent struct {
	EventID    string                 `json:"event_id"`
	SessionID  string                 `json:"session_id"`
	SpanID     string                 `json:"span_id,omitempty"`
	ParentSpan string                 `json:"parent_span,omitempty"`
	Type       string                 `json:"type"`
	Name       string                 `json:"name"`
	Level      string                 `json:"level"`
	Timestamp  time.Time              `json:"timestamp"`
	DurationMs int64                  `json:"duration_ms,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// ChatMessage is a local AI conversation history entry.
type ChatMessage struct {
	ID             int64     `json:"id"`
	ConversationID int64     `json:"conversation_id"`
	UserID         int64     `json:"user_id,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
	Role           string    `json:"role"`
	Content        string    `json:"content"`
	Model          string    `json:"model,omitempty"`
	TokenCount     int       `json:"token_count,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}
