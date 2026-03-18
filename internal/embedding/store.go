package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Entry is a document with its embedding vector.
type Entry struct {
	ID        int64     `json:"id"`
	Type      string    `json:"type"` // "document" or "task"
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Vector    []float32 `json:"vector"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SearchResult is a scored search result.
type SearchResult struct {
	Entry *Entry
	Score float32
}

// VectorStore stores document embeddings for semantic search.
type VectorStore struct {
	mu       sync.RWMutex
	entries  map[string]*Entry // key: "type:id"
	embedder *Embedder
	filePath string
	dirty    bool
}

// NewVectorStore creates a new vector store.
func NewVectorStore(embedder *Embedder) *VectorStore {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".nlook")
	os.MkdirAll(dir, 0755)

	vs := &VectorStore{
		entries:  make(map[string]*Entry),
		embedder: embedder,
		filePath: filepath.Join(dir, "embeddings.json"),
	}
	vs.loadFromFile()

	// Auto-save every 60 seconds
	go func() {
		for {
			time.Sleep(60 * time.Second)
			vs.saveIfDirty()
		}
	}()

	return vs
}

// Upsert adds or updates an entry, generating embedding if content changed.
func (vs *VectorStore) Upsert(ctx context.Context, entryType string, id int64, title, content string) {
	key := entryKey(entryType, id)

	vs.mu.RLock()
	existing, exists := vs.entries[key]
	vs.mu.RUnlock()

	// Skip if content hasn't changed
	if exists && existing.Content == content && len(existing.Vector) > 0 {
		return
	}

	// Generate embedding
	text := title + "\n" + content
	if len(text) > 2000 {
		text = text[:2000] // Truncate for embedding
	}

	vector, err := vs.embedder.Embed(ctx, text)
	if err != nil {
		log.Printf("embedding: failed for %s: %v", key, err)
		// Store without vector (keyword search fallback)
		vector = nil
	}

	vs.mu.Lock()
	vs.entries[key] = &Entry{
		ID:        id,
		Type:      entryType,
		Title:     title,
		Content:   content,
		Vector:    vector,
		UpdatedAt: time.Now(),
	}
	vs.dirty = true
	vs.mu.Unlock()
}

// Remove deletes an entry.
func (vs *VectorStore) Remove(entryType string, id int64) {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	delete(vs.entries, entryKey(entryType, id))
	vs.dirty = true
}

// Search finds the most similar entries to the query.
func (vs *VectorStore) Search(ctx context.Context, query string, limit int) []SearchResult {
	queryVec, err := vs.embedder.Embed(ctx, query)
	if err != nil || len(queryVec) == 0 {
		return nil
	}

	vs.mu.RLock()
	defer vs.mu.RUnlock()

	var results []SearchResult
	for _, e := range vs.entries {
		if len(e.Vector) == 0 {
			continue
		}
		score := CosineSimilarity(queryVec, e.Vector)
		if score > 0.3 { // Minimum similarity threshold
			results = append(results, SearchResult{Entry: e, Score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if limit > 0 && len(results) > limit {
		return results[:limit]
	}
	return results
}

// Stats returns store statistics.
func (vs *VectorStore) Stats() (total, withVector int) {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	for _, e := range vs.entries {
		total++
		if len(e.Vector) > 0 {
			withVector++
		}
	}
	return
}

// Save persists to file.
func (vs *VectorStore) Save() {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	vs.saveToFile()
}

func (vs *VectorStore) loadFromFile() {
	data, err := os.ReadFile(vs.filePath)
	if err != nil {
		return
	}
	var entries map[string]*Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		log.Printf("embedding: load error: %v", err)
		return
	}
	vs.entries = entries
	log.Printf("embedding: loaded %d entries from %s", len(entries), vs.filePath)
}

func (vs *VectorStore) saveToFile() {
	data, err := json.Marshal(vs.entries)
	if err != nil {
		return
	}
	os.WriteFile(vs.filePath, data, 0644)
}

func (vs *VectorStore) saveIfDirty() {
	vs.mu.Lock()
	if !vs.dirty {
		vs.mu.Unlock()
		return
	}
	vs.dirty = false
	vs.saveToFile()
	vs.mu.Unlock()
}

func entryKey(entryType string, id int64) string {
	return entryType + ":" + fmt.Sprint(id)
}
