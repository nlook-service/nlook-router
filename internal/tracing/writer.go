package tracing

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nlook-service/nlook-router/internal/db"
)

// Writer appends trace events to a JSONL file per session.
type Writer struct {
	dir   string
	mu    sync.Mutex
	files map[string]*os.File
	db    db.DB // optional: unified DB layer (nil = file-based)
}

// NewWriter creates a trace writer. Creates the traces directory if needed.
func NewWriter(dataDir string) *Writer {
	dir := filepath.Join(dataDir, "traces")
	os.MkdirAll(dir, 0700)
	return &Writer{
		dir:   dir,
		files: make(map[string]*os.File),
	}
}

// Write appends a trace event to the session's JSONL file.
func (w *Writer) Write(event TraceEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal trace event: %w", err)
	}
	data = append(data, '\n')

	w.mu.Lock()
	defer w.mu.Unlock()

	f, ok := w.files[event.SessionID]
	if !ok {
		path := filepath.Join(w.dir, event.SessionID+".jsonl")
		f, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("open trace file: %w", err)
		}
		w.files[event.SessionID] = f
	}

	_, err = f.Write(data)
	w.syncTraceToDB(event)
	return err
}

// ReadEvents reads all trace events for a session.
func (w *Writer) ReadEvents(sessionID string) ([]TraceEvent, error) {
	path := filepath.Join(w.dir, sessionID+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read trace file: %w", err)
	}

	var events []TraceEvent
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event TraceEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	return events, nil
}

// Dir returns the traces directory path.
func (w *Writer) Dir() string {
	return w.dir
}

// Cleanup removes trace files older than maxAge.
// Returns the number of files removed.
func (w *Writer) Cleanup(maxAge time.Duration) (int, error) {
	entries, err := os.ReadDir(w.dir)
	if err != nil {
		return 0, fmt.Errorf("read traces dir: %w", err)
	}

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	w.mu.Lock()
	defer w.mu.Unlock()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
			// Close file handle if open
			if f, ok := w.files[sessionID]; ok {
				f.Close()
				delete(w.files, sessionID)
			}
			if err := os.Remove(filepath.Join(w.dir, entry.Name())); err != nil {
				log.Printf("tracing: cleanup remove %s: %v", entry.Name(), err)
				continue
			}
			removed++
		}
	}
	return removed, nil
}

// Close closes all open file handles.
func (w *Writer) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, f := range w.files {
		f.Close()
	}
	w.files = make(map[string]*os.File)
}
