package tracing

import (
	"log"
	"time"
)

const (
	bufferSize        = 256
	DefaultRetention  = 7 * 24 * time.Hour // 7 days
	cleanupInterval   = 6 * time.Hour
)

// Collector receives trace events asynchronously and writes them via Writer.
// It also periodically cleans up old trace files.
type Collector struct {
	ch        chan TraceEvent
	writer    *Writer
	done      chan struct{}
	stopClean chan struct{}
	retention time.Duration
}

// NewCollector creates and starts a trace collector with default 7-day retention.
func NewCollector(writer *Writer) *Collector {
	return NewCollectorWithRetention(writer, DefaultRetention)
}

// NewCollectorWithRetention creates a collector with custom retention period.
func NewCollectorWithRetention(writer *Writer, retention time.Duration) *Collector {
	c := &Collector{
		ch:        make(chan TraceEvent, bufferSize),
		writer:    writer,
		done:      make(chan struct{}),
		stopClean: make(chan struct{}),
		retention: retention,
	}
	go c.loop()
	go c.cleanupLoop()
	return c
}

// Emit sends an event to the collector (non-blocking, fire-and-forget).
// If the buffer is full, the event is dropped.
func (c *Collector) Emit(event TraceEvent) {
	select {
	case c.ch <- event:
	default:
		log.Printf("tracing: buffer full, dropping event %s", event.EventID)
	}
}

// Close drains remaining events and shuts down.
func (c *Collector) Close() {
	close(c.stopClean)
	close(c.ch)
	<-c.done
	c.writer.Close()
}

func (c *Collector) loop() {
	defer close(c.done)
	for event := range c.ch {
		if err := c.writer.Write(event); err != nil {
			log.Printf("tracing: write error: %v", err)
		}
	}
}

func (c *Collector) cleanupLoop() {
	// Run initial cleanup on start
	c.runCleanup()

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopClean:
			return
		case <-ticker.C:
			c.runCleanup()
		}
	}
}

func (c *Collector) runCleanup() {
	removed, err := c.writer.Cleanup(c.retention)
	if err != nil {
		log.Printf("tracing: cleanup error: %v", err)
		return
	}
	if removed > 0 {
		log.Printf("tracing: cleaned up %d trace files older than %s", removed, c.retention)
	}
}
