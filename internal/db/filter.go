package db

import "time"

// SessionFilter filters sessions.
type SessionFilter struct {
	UserID *int64
	Type   *string // chat, agent, workflow
	State  *string // active, completed, expired
	Before *time.Time
	After  *time.Time
	Limit  int
}

// DocumentFilter filters cached documents.
type DocumentFilter struct {
	UserID  *int64
	Tags    []string // any match
	Limit   int
	OrderBy string // "updated_at" (default)
}

// TaskFilter filters cached tasks.
type TaskFilter struct {
	UserID   *int64
	Status   *string // pending, in_progress, completed
	Priority *string
	Limit    int
	OrderBy  string
}

// TraceFilter filters trace events.
type TraceFilter struct {
	SessionID *string
	EventType *string
	After     *time.Time
	Before    *time.Time
	Limit     int
}
