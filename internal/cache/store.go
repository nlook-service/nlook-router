package cache

import (
	"sync"
	"time"
)

// Document represents a cached document.
type Document struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Tags      []string  `json:"tags,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Task represents a cached task.
type Task struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	Priority  string    `json:"priority,omitempty"`
	Notes     string    `json:"notes,omitempty"`
	DueDate   string    `json:"due_date,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Store holds cached user data for AI context.
type Store struct {
	mu        sync.RWMutex
	documents map[int64]*Document
	tasks     map[int64]*Task
}

// NewStore creates a new cache store.
func NewStore() *Store {
	return &Store{
		documents: make(map[int64]*Document),
		tasks:     make(map[int64]*Task),
	}
}

// SetDocument adds or updates a document in cache.
func (s *Store) SetDocument(doc *Document) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.documents[doc.ID] = doc
}

// RemoveDocument removes a document from cache.
func (s *Store) RemoveDocument(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.documents, id)
}

// SetTask adds or updates a task in cache.
func (s *Store) SetTask(task *Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = task
}

// RemoveTask removes a task from cache.
func (s *Store) RemoveTask(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, id)
}

// GetRecentDocuments returns the most recent N documents.
func (s *Store) GetRecentDocuments(limit int) []*Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	docs := make([]*Document, 0, len(s.documents))
	for _, d := range s.documents {
		docs = append(docs, d)
	}
	// Sort by UpdatedAt desc
	for i := 0; i < len(docs); i++ {
		for j := i + 1; j < len(docs); j++ {
			if docs[j].UpdatedAt.After(docs[i].UpdatedAt) {
				docs[i], docs[j] = docs[j], docs[i]
			}
		}
	}
	if limit > 0 && len(docs) > limit {
		return docs[:limit]
	}
	return docs
}

// GetPendingTasks returns tasks that are not completed.
func (s *Store) GetPendingTasks(limit int) []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tasks := make([]*Task, 0)
	for _, t := range s.tasks {
		if t.Status != "completed" {
			tasks = append(tasks, t)
		}
	}
	// Sort by UpdatedAt desc
	for i := 0; i < len(tasks); i++ {
		for j := i + 1; j < len(tasks); j++ {
			if tasks[j].UpdatedAt.After(tasks[i].UpdatedAt) {
				tasks[i], tasks[j] = tasks[j], tasks[i]
			}
		}
	}
	if limit > 0 && len(tasks) > limit {
		return tasks[:limit]
	}
	return tasks
}

// GetAllTasks returns all cached tasks.
func (s *Store) GetAllTasks() []*Task {
	return s.GetPendingTasks(0)
}

// Summary returns a text summary of cached data for AI system prompt.
func (s *Store) Summary() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.documents) == 0 && len(s.tasks) == 0 {
		return ""
	}

	summary := "\n\n--- User's Data Context ---\n"

	if len(s.tasks) > 0 {
		summary += "\nTasks:\n"
		count := 0
		for _, t := range s.tasks {
			if count >= 20 {
				break
			}
			status := t.Status
			if status == "" {
				status = "pending"
			}
			line := "- [" + status + "] " + t.Title
			if t.Priority != "" && t.Priority != "none" {
				line += " (" + t.Priority + ")"
			}
			if t.DueDate != "" {
				line += " due:" + t.DueDate
			}
			summary += line + "\n"
			count++
		}
	}

	if len(s.documents) > 0 {
		summary += "\nRecent Documents:\n"
		count := 0
		for _, d := range s.documents {
			if count >= 15 {
				break
			}
			line := "- " + d.Title
			if len(d.Tags) > 0 {
				line += " [" + joinStrings(d.Tags, ", ") + "]"
			}
			summary += line + "\n"
			count++
		}
	}

	summary += "--- End Context ---"
	return summary
}

// Stats returns cache statistics.
func (s *Store) Stats() (docCount, taskCount int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.documents), len(s.tasks)
}

func joinStrings(strs []string, sep string) string {
	result := ""
	for i, s := range strs {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
