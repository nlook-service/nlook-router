package tracing

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/nlook-service/nlook-router/internal/db"
)

// NewWriterWithDB creates a trace writer backed by the unified DB layer.
func NewWriterWithDB(storage db.DB) *Writer {
	return &Writer{
		files: make(map[string]*os.File),
		db:    storage,
	}
}

func (w *Writer) syncTraceToDB(event TraceEvent) {
	if w.db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := w.db.WriteTrace(ctx, &db.TraceEvent{
			EventID:    event.EventID,
			SessionID:  event.SessionID,
			SpanID:     event.SpanID,
			ParentSpan: event.ParentSpan,
			Type:       string(event.Type),
			Name:       event.Name,
			Level:      string(event.Level),
			Timestamp:  event.Timestamp,
			DurationMs: event.DurationMs,
			Metadata:   event.Metadata,
		}); err != nil {
			log.Printf("tracing/db: write error: %v", err)
		}
	}()
}
