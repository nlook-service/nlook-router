package tracing

import (
	"crypto/rand"
	"fmt"
	"time"
)

// EventType categorizes trace events.
type EventType string

const (
	EventAgentStart    EventType = "agent_start"
	EventAgentOutput   EventType = "agent_output"
	EventAgentComplete EventType = "agent_complete"
	EventAgentError    EventType = "agent_error"
	EventNodeStart     EventType = "node_start"
	EventNodeComplete  EventType = "node_complete"
	EventNodeError     EventType = "node_error"
	EventLLMCall       EventType = "llm_call"
	EventLLMResponse   EventType = "llm_response"
	EventChatMessage   EventType = "chat_message"
	EventSessionEnd    EventType = "session_end"
	EventError         EventType = "error"
)

// TraceLevel indicates severity.
type TraceLevel string

const (
	LevelInfo  TraceLevel = "info"
	LevelWarn  TraceLevel = "warn"
	LevelError TraceLevel = "error"
)

// TraceEvent is a single traceable event.
type TraceEvent struct {
	EventID    string                 `json:"event_id"`
	SessionID  string                 `json:"session_id"`
	SpanID     string                 `json:"span_id,omitempty"`
	ParentSpan string                 `json:"parent_span,omitempty"`
	Type       EventType              `json:"type"`
	Name       string                 `json:"name"`
	Level      TraceLevel             `json:"level"`
	Timestamp  time.Time              `json:"timestamp"`
	DurationMs int64                  `json:"duration_ms,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
}

// NewEvent creates a TraceEvent with generated ID and current timestamp.
func NewEvent(sessionID string, eventType EventType, name string) TraceEvent {
	return TraceEvent{
		EventID:   newID(),
		SessionID: sessionID,
		Type:      eventType,
		Name:      name,
		Level:     LevelInfo,
		Timestamp: time.Now(),
	}
}

// WithSpan sets span IDs for grouping related events.
func (e TraceEvent) WithSpan(spanID, parentSpan string) TraceEvent {
	e.SpanID = spanID
	e.ParentSpan = parentSpan
	return e
}

// WithMeta sets metadata.
func (e TraceEvent) WithMeta(meta map[string]interface{}) TraceEvent {
	e.Metadata = meta
	return e
}

// WithDuration sets the duration in milliseconds.
func (e TraceEvent) WithDuration(ms int64) TraceEvent {
	e.DurationMs = ms
	return e
}

// WithLevel sets the trace level.
func (e TraceEvent) WithLevel(level TraceLevel) TraceEvent {
	e.Level = level
	return e
}

// NewSpanID generates a new span ID.
func NewSpanID() string {
	return newID()
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
