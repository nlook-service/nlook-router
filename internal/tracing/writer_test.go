package tracing

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriterWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	defer w.Close()

	e1 := NewEvent("sess-1", EventAgentStart, "start")
	e2 := NewEvent("sess-1", EventAgentComplete, "done").WithDuration(1000)

	if err := w.Write(e1); err != nil {
		t.Fatalf("write e1: %v", err)
	}
	if err := w.Write(e2); err != nil {
		t.Fatalf("write e2: %v", err)
	}

	events, err := w.ReadEvents("sess-1")
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Type != EventAgentStart {
		t.Errorf("expected agent_start, got %s", events[0].Type)
	}
	if events[1].DurationMs != 1000 {
		t.Errorf("expected duration 1000, got %d", events[1].DurationMs)
	}
}

func TestWriterMultipleSessions(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	defer w.Close()

	w.Write(NewEvent("sess-a", EventAgentStart, "a-start"))
	w.Write(NewEvent("sess-b", EventNodeStart, "b-start"))
	w.Write(NewEvent("sess-a", EventAgentComplete, "a-done"))

	eventsA, _ := w.ReadEvents("sess-a")
	eventsB, _ := w.ReadEvents("sess-b")

	if len(eventsA) != 2 {
		t.Errorf("expected 2 events for sess-a, got %d", len(eventsA))
	}
	if len(eventsB) != 1 {
		t.Errorf("expected 1 event for sess-b, got %d", len(eventsB))
	}
}

func TestWriterReadEmptySession(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	defer w.Close()

	events, err := w.ReadEvents("nonexistent")
	if err != nil {
		t.Fatalf("expected nil error for missing session, got %v", err)
	}
	if events != nil {
		t.Errorf("expected nil events, got %v", events)
	}
}

func TestWriterMetadata(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	defer w.Close()

	e := NewEvent("sess-m", EventLLMCall, "chat").
		WithMeta(map[string]interface{}{
			"model":  "qwen3:4b",
			"tokens": 150,
		})
	w.Write(e)

	events, _ := w.ReadEvents("sess-m")
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Metadata["model"] != "qwen3:4b" {
		t.Errorf("expected model qwen3:4b, got %v", events[0].Metadata["model"])
	}
}

func TestWriterCleanup(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	// Write events to two sessions
	w.Write(NewEvent("old-sess", EventAgentStart, "start"))
	w.Write(NewEvent("new-sess", EventAgentStart, "start"))
	w.Close()

	// Manually backdate the "old-sess" file
	oldPath := filepath.Join(w.Dir(), "old-sess.jsonl")
	oldTime := time.Now().Add(-8 * 24 * time.Hour) // 8 days ago
	os.Chtimes(oldPath, oldTime, oldTime)

	// Reopen writer and cleanup with 7-day retention
	w2 := NewWriter(dir)
	defer w2.Close()

	removed, err := w2.Cleanup(7 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("cleanup error: %v", err)
	}
	if removed != 1 {
		t.Errorf("expected 1 file removed, got %d", removed)
	}

	// Old session should be gone
	events, _ := w2.ReadEvents("old-sess")
	if events != nil {
		t.Error("expected old-sess to be cleaned up")
	}

	// New session should still exist
	events, _ = w2.ReadEvents("new-sess")
	if len(events) != 1 {
		t.Errorf("expected 1 event for new-sess, got %d", len(events))
	}
}

func TestWriterCleanupEmpty(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	defer w.Close()

	removed, err := w.Cleanup(7 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("cleanup error: %v", err)
	}
	if removed != 0 {
		t.Errorf("expected 0 removed, got %d", removed)
	}
}

func BenchmarkWriterWrite(b *testing.B) {
	dir := b.TempDir()
	w := NewWriter(dir)
	defer w.Close()

	e := NewEvent("bench-sess", EventAgentOutput, "output").
		WithMeta(map[string]interface{}{"chunk": "data"})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Write(e)
	}
}
