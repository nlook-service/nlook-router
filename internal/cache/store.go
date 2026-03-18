package cache

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
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

// cacheFile is the serialization format.
type cacheFile struct {
	Documents map[int64]*Document `json:"documents"`
	Tasks     map[int64]*Task     `json:"tasks"`
	SavedAt   time.Time           `json:"saved_at"`
}

// Store holds cached user data for AI context.
type Store struct {
	mu        sync.RWMutex
	documents map[int64]*Document
	tasks     map[int64]*Task
	filePath  string
	dirty     bool
}

// NewStore creates a new cache store with file persistence.
func NewStore() *Store {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".nlook")
	os.MkdirAll(dir, 0755)

	s := &Store{
		documents: make(map[int64]*Document),
		tasks:     make(map[int64]*Task),
		filePath:  filepath.Join(dir, "cache.json"),
	}
	s.loadFromFile()

	// Auto-save every 30 seconds
	go func() {
		for {
			time.Sleep(30 * time.Second)
			s.saveIfDirty()
		}
	}()

	return s
}

// SetDocument adds or updates a document in cache.
func (s *Store) SetDocument(doc *Document) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.documents[doc.ID] = doc
	s.dirty = true
}

// RemoveDocument removes a document from cache.
func (s *Store) RemoveDocument(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.documents, id)
	s.dirty = true
}

// SetTask adds or updates a task in cache.
func (s *Store) SetTask(task *Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = task
	s.dirty = true
}

// RemoveTask removes a task from cache.
func (s *Store) RemoveTask(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, id)
	s.dirty = true
}

// SearchDocuments finds documents matching the query by keyword.
func (s *Store) SearchDocuments(query string, limit int) []*Document {
	s.mu.RLock()
	defer s.mu.RUnlock()

	query = strings.ToLower(query)
	keywords := strings.Fields(query)
	if len(keywords) == 0 {
		return s.GetRecentDocuments(limit)
	}

	type scored struct {
		doc   *Document
		score int
	}
	var results []scored

	for _, d := range s.documents {
		score := 0
		titleLower := strings.ToLower(d.Title)
		contentLower := strings.ToLower(d.Content)

		for _, kw := range keywords {
			if strings.Contains(titleLower, kw) {
				score += 3 // Title match = higher weight
			}
			if strings.Contains(contentLower, kw) {
				score += 1
			}
			// Tag match
			for _, tag := range d.Tags {
				if strings.Contains(strings.ToLower(tag), kw) {
					score += 2
				}
			}
		}
		if score > 0 {
			results = append(results, scored{doc: d, score: score})
		}
	}

	// Sort by score desc
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[i].score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	docs := make([]*Document, 0, limit)
	for i, r := range results {
		if limit > 0 && i >= limit {
			break
		}
		docs = append(docs, r.doc)
	}
	return docs
}

// GetRecentDocuments returns the most recent N documents.
func (s *Store) GetRecentDocuments(limit int) []*Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	docs := make([]*Document, 0, len(s.documents))
	for _, d := range s.documents {
		docs = append(docs, d)
	}
	sortDocsByDate(docs)
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
	sortTasksByDate(tasks)
	if limit > 0 && len(tasks) > limit {
		return tasks[:limit]
	}
	return tasks
}

// GetAllTasks returns all cached tasks.
func (s *Store) GetAllTasks() []*Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tasks := make([]*Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		tasks = append(tasks, t)
	}
	sortTasksByDate(tasks)
	return tasks
}

// Summary returns a short overview for system prompt.
func (s *Store) Summary() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.documents) == 0 && len(s.tasks) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n--- User's Data Context ---\n")

	// Tasks summary
	if len(s.tasks) > 0 {
		pending, inProgress, completed := 0, 0, 0
		for _, t := range s.tasks {
			switch t.Status {
			case "in_progress":
				inProgress++
			case "completed":
				completed++
			default:
				pending++
			}
		}
		sb.WriteString("\nTasks Overview:\n")
		sb.WriteString("- Total: " + itoa(len(s.tasks)) + " (pending: " + itoa(pending) + ", in_progress: " + itoa(inProgress) + ", completed: " + itoa(completed) + ")\n")

		// Show pending tasks
		count := 0
		for _, t := range s.tasks {
			if t.Status == "completed" {
				continue
			}
			if count >= 15 {
				sb.WriteString("- ... and more\n")
				break
			}
			line := "- [" + t.Status + "] " + t.Title
			if t.Priority != "" && t.Priority != "none" {
				line += " (" + t.Priority + ")"
			}
			if t.DueDate != "" {
				line += " due:" + t.DueDate
			}
			sb.WriteString(line + "\n")
			count++
		}
	}

	// Documents summary
	if len(s.documents) > 0 {
		sb.WriteString("\nDocuments: " + itoa(len(s.documents)) + " total\n")
		count := 0
		for _, d := range s.documents {
			if count >= 10 {
				sb.WriteString("- ... and more\n")
				break
			}
			line := "- " + d.Title
			if len(d.Tags) > 0 {
				line += " [" + strings.Join(d.Tags, ", ") + "]"
			}
			sb.WriteString(line + "\n")
			count++
		}
	}

	sb.WriteString("--- End Context ---")
	return sb.String()
}

// BuildContextForQuery returns detailed content for documents/tasks relevant to the query.
// This is the NotebookLM-style feature: inject full document content into AI context.
func (s *Store) BuildContextForQuery(query string) string {
	var sb strings.Builder

	// Find relevant documents
	relevantDocs := s.SearchDocuments(query, 5)
	if len(relevantDocs) > 0 {
		sb.WriteString("\n\n--- Relevant Documents ---\n")
		for _, d := range relevantDocs {
			sb.WriteString("\n## " + d.Title + "\n")
			content := d.Content
			// Truncate very long documents
			if len(content) > 3000 {
				content = content[:3000] + "\n... (truncated)"
			}
			sb.WriteString(content + "\n")
		}
		sb.WriteString("--- End Documents ---\n")
	}

	// Include pending tasks if query seems task-related
	queryLower := strings.ToLower(query)
	taskKeywords := []string{"할일", "할 일", "task", "todo", "일정", "진행", "완료", "등록", "오늘"}
	isTaskQuery := false
	for _, kw := range taskKeywords {
		if strings.Contains(queryLower, kw) {
			isTaskQuery = true
			break
		}
	}

	if isTaskQuery {
		tasks := s.GetAllTasks()
		if len(tasks) > 0 {
			sb.WriteString("\n--- All Tasks ---\n")
			for _, t := range tasks {
				line := "- [" + t.Status + "] " + t.Title
				if t.Priority != "" && t.Priority != "none" {
					line += " (priority: " + t.Priority + ")"
				}
				if t.DueDate != "" {
					line += " (due: " + t.DueDate + ")"
				}
				if t.Notes != "" {
					notes := t.Notes
					if len(notes) > 200 {
						notes = notes[:200] + "..."
					}
					line += "\n  Notes: " + notes
				}
				sb.WriteString(line + "\n")
			}
			sb.WriteString("--- End Tasks ---\n")
		}
	}

	return sb.String()
}

// Stats returns cache statistics.
func (s *Store) Stats() (docCount, taskCount int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.documents), len(s.tasks)
}

// Save persists cache to file.
func (s *Store) Save() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	s.saveToFile()
}

func (s *Store) loadFromFile() {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return // No cache file yet
	}
	var cf cacheFile
	if err := json.Unmarshal(data, &cf); err != nil {
		log.Printf("cache: failed to load %s: %v", s.filePath, err)
		return
	}
	if cf.Documents != nil {
		s.documents = cf.Documents
	}
	if cf.Tasks != nil {
		s.tasks = cf.Tasks
	}
	log.Printf("cache: loaded %d documents, %d tasks from %s", len(s.documents), len(s.tasks), s.filePath)
}

func (s *Store) saveToFile() {
	cf := cacheFile{
		Documents: s.documents,
		Tasks:     s.tasks,
		SavedAt:   time.Now(),
	}
	data, err := json.MarshalIndent(cf, "", "  ")
	if err != nil {
		log.Printf("cache: marshal error: %v", err)
		return
	}
	if err := os.WriteFile(s.filePath, data, 0644); err != nil {
		log.Printf("cache: write error: %v", err)
	}
}

func (s *Store) saveIfDirty() {
	s.mu.Lock()
	if !s.dirty {
		s.mu.Unlock()
		return
	}
	s.dirty = false
	s.saveToFile()
	s.mu.Unlock()
}

func sortDocsByDate(docs []*Document) {
	for i := 0; i < len(docs); i++ {
		for j := i + 1; j < len(docs); j++ {
			if docs[j].UpdatedAt.After(docs[i].UpdatedAt) {
				docs[i], docs[j] = docs[j], docs[i]
			}
		}
	}
}

func sortTasksByDate(tasks []*Task) {
	for i := 0; i < len(tasks); i++ {
		for j := i + 1; j < len(tasks); j++ {
			if tasks[j].UpdatedAt.After(tasks[i].UpdatedAt) {
				tasks[i], tasks[j] = tasks[j], tasks[i]
			}
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
