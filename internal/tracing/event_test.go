package tracing

import (
	"testing"
	"time"
)

func TestNewEvent(t *testing.T) {
	e := NewEvent("sess-1", EventAgentStart, "claude:start")

	if e.SessionID != "sess-1" {
		t.Errorf("expected session_id sess-1, got %s", e.SessionID)
	}
	if e.Type != EventAgentStart {
		t.Errorf("expected type agent_start, got %s", e.Type)
	}
	if e.Name != "claude:start" {
		t.Errorf("expected name claude:start, got %s", e.Name)
	}
	if e.Level != LevelInfo {
		t.Errorf("expected level info, got %s", e.Level)
	}
	if e.EventID == "" {
		t.Error("expected non-empty event_id")
	}
	if e.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestEventChaining(t *testing.T) {
	e := NewEvent("s1", EventNodeStart, "node-1").
		WithSpan("span-1", "parent-1").
		WithMeta(map[string]interface{}{"key": "val"}).
		WithDuration(500).
		WithLevel(LevelError)

	if e.SpanID != "span-1" {
		t.Errorf("expected span_id span-1, got %s", e.SpanID)
	}
	if e.ParentSpan != "parent-1" {
		t.Errorf("expected parent_span parent-1, got %s", e.ParentSpan)
	}
	if e.Metadata["key"] != "val" {
		t.Errorf("expected metadata key=val")
	}
	if e.DurationMs != 500 {
		t.Errorf("expected duration 500, got %d", e.DurationMs)
	}
	if e.Level != LevelError {
		t.Errorf("expected error level, got %s", e.Level)
	}
}

func TestNewEventDoesNotMutateOriginal(t *testing.T) {
	base := NewEvent("s1", EventLLMCall, "llm")
	_ = base.WithLevel(LevelError)

	if base.Level != LevelInfo {
		t.Error("WithLevel should not mutate the original event")
	}
}

func TestNewSpanID(t *testing.T) {
	id1 := NewSpanID()
	id2 := NewSpanID()
	if id1 == id2 {
		t.Error("expected unique span IDs")
	}
	if len(id1) == 0 {
		t.Error("expected non-empty span ID")
	}
}

func TestNewEventTimestamp(t *testing.T) {
	before := time.Now()
	e := NewEvent("s1", EventChatMessage, "msg")
	after := time.Now()

	if e.Timestamp.Before(before) || e.Timestamp.After(after) {
		t.Error("timestamp should be between before and after")
	}
}
