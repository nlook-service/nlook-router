package usage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// TokenUsage is recorded after each LLM call.
type TokenUsage struct {
	UserID       int64
	Provider     string // "ollama", "gemini", "claude-cli"
	Model        string // "qwen3:4b", "gemini-2.0-flash-lite", "claude-haiku-4-5"
	Category     string // "chat", "workflow", "intent", "embedding"
	InputTokens  int
	OutputTokens int
	ElapsedMs    int64
}

// UsageEntry accumulates token usage for a provider/model/category key.
type UsageEntry struct {
	Provider       string `json:"provider"`
	Model          string `json:"model"`
	Category       string `json:"category"`
	InputTokens    int    `json:"input_tokens"`
	OutputTokens   int    `json:"output_tokens"`
	TotalTokens    int    `json:"total_tokens"`
	RequestCount   int    `json:"request_count"`
	TotalElapsedMs int64  `json:"total_elapsed_ms"`
}

// HourlyBucket aggregates usage per hour.
type HourlyBucket struct {
	Hour    string                 `json:"hour"`    // "2026-03-19T14"
	Entries map[string]*UsageEntry `json:"entries"` // key: "provider:model:category"
}

// Tracker records token usage and persists to disk.
type Tracker struct {
	mu       sync.Mutex
	filePath string
	buckets  map[string]*HourlyBucket // key: hour string
}

// NewTracker creates a Tracker that persists to filePath.
func NewTracker(filePath string) *Tracker {
	t := &Tracker{
		filePath: filePath,
		buckets:  make(map[string]*HourlyBucket),
	}
	_ = t.Load()
	return t
}

// Record adds a token usage entry to the current hour bucket.
func (t *Tracker) Record(u TokenUsage) {
	hour := time.Now().UTC().Format("2006-01-02T15")
	key := fmt.Sprintf("%s:%s:%s", u.Provider, u.Model, u.Category)

	t.mu.Lock()
	defer t.mu.Unlock()

	bucket, ok := t.buckets[hour]
	if !ok {
		bucket = &HourlyBucket{
			Hour:    hour,
			Entries: make(map[string]*UsageEntry),
		}
		t.buckets[hour] = bucket
	}

	entry, ok := bucket.Entries[key]
	if !ok {
		entry = &UsageEntry{
			Provider: u.Provider,
			Model:    u.Model,
			Category: u.Category,
		}
		bucket.Entries[key] = entry
	}

	entry.InputTokens += u.InputTokens
	entry.OutputTokens += u.OutputTokens
	entry.TotalTokens += u.InputTokens + u.OutputTokens
	entry.RequestCount++
	entry.TotalElapsedMs += u.ElapsedMs

	_ = t.saveLocked()
}

// Load reads persisted buckets from disk.
func (t *Tracker) Load() error {
	data, err := os.ReadFile(t.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read usage file: %w", err)
	}

	var buckets map[string]*HourlyBucket
	if err := json.Unmarshal(data, &buckets); err != nil {
		return fmt.Errorf("parse usage file: %w", err)
	}

	t.mu.Lock()
	t.buckets = buckets
	t.mu.Unlock()
	return nil
}

// Save persists all buckets to disk (thread-safe).
func (t *Tracker) Save() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.saveLocked()
}

// saveLocked persists buckets; caller must hold t.mu.
func (t *Tracker) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(t.filePath), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	data, err := json.MarshalIndent(t.buckets, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal usage: %w", err)
	}
	if err := os.WriteFile(t.filePath, data, 0o644); err != nil {
		return fmt.Errorf("write usage file: %w", err)
	}
	return nil
}

// Flush returns completed (past) hour buckets and removes them from memory.
// The current hour bucket is retained.
func (t *Tracker) Flush() []HourlyBucket {
	currentHour := time.Now().UTC().Format("2006-01-02T15")

	t.mu.Lock()
	defer t.mu.Unlock()

	var completed []HourlyBucket
	for hour, bucket := range t.buckets {
		if hour != currentHour {
			completed = append(completed, *bucket)
			delete(t.buckets, hour)
		}
	}
	if len(completed) > 0 {
		_ = t.saveLocked()
	}
	return completed
}
