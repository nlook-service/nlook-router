package tracing

import (
	"testing"
	"time"
)

func TestCollectorEmitAndRead(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	c := NewCollector(w)

	c.Emit(NewEvent("c-sess", EventAgentStart, "start"))
	c.Emit(NewEvent("c-sess", EventAgentComplete, "done"))

	// Close drains the buffer
	c.Close()

	events, err := w.ReadEvents("c-sess")
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
}

func TestCollectorNonBlocking(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	c := NewCollector(w)
	defer c.Close()

	// Emit should not block even with many events
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			c.Emit(NewEvent("nb-sess", EventAgentOutput, "output"))
		}
		close(done)
	}()

	select {
	case <-done:
		// OK: completed without blocking
	case <-time.After(2 * time.Second):
		t.Fatal("Emit blocked for too long")
	}
}

func TestCollectorCloseWaitsForDrain(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	c := NewCollector(w)

	for i := 0; i < 50; i++ {
		c.Emit(NewEvent("drain-sess", EventNodeStart, "node"))
	}

	c.Close()

	events, _ := w.ReadEvents("drain-sess")
	if len(events) != 50 {
		t.Errorf("expected 50 events after drain, got %d", len(events))
	}
}

func BenchmarkCollectorEmit(b *testing.B) {
	dir := b.TempDir()
	w := NewWriter(dir)
	c := NewCollector(w)
	defer c.Close()

	e := NewEvent("bench", EventAgentOutput, "output")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Emit(e)
	}
}
