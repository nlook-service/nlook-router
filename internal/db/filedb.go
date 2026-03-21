package db

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nlook-service/nlook-router/internal/eval"
)

// FileDB implements DB using existing JSON file format.
// This preserves full backward compatibility with the current file-based storage.
type FileDB struct {
	dataDir string
	mu      sync.RWMutex

	// In-memory state mirrors existing file formats
	sessions  map[string]*Session
	memory    memoryData
	cacheData cacheFileData
	traces    traceData
	chatMsgs  []ChatMessage

	dirty map[string]bool // tracks which files need saving
}

type memoryData struct {
	Profile   UserProfile                     `json:"profile"`
	Summaries map[int64]*ConversationSummary  `json:"summaries"`
	Facts     []string                        `json:"facts,omitempty"`
	Memories  []UserMemory                    `json:"memories,omitempty"`
	SavedAt   time.Time                       `json:"saved_at"`
}

type cacheFileData struct {
	Documents map[int64]*CachedDocument `json:"documents"`
	Tasks     map[int64]*CachedTask     `json:"tasks"`
	SavedAt   time.Time                 `json:"saved_at"`
}

type traceData struct {
	dir   string
	mu    sync.Mutex
	files map[string]*os.File
}

func newFileDB(dataDir string) (*FileDB, error) {
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".nlook")
	}
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	fdb := &FileDB{
		dataDir:  dataDir,
		sessions: make(map[string]*Session),
		memory: memoryData{
			Summaries: make(map[int64]*ConversationSummary),
		},
		cacheData: cacheFileData{
			Documents: make(map[int64]*CachedDocument),
			Tasks:     make(map[int64]*CachedTask),
		},
		traces: traceData{
			dir:   filepath.Join(dataDir, "traces"),
			files: make(map[string]*os.File),
		},
		dirty: make(map[string]bool),
	}
	os.MkdirAll(fdb.traces.dir, 0700)

	fdb.loadSessions()
	fdb.loadMemory()
	fdb.loadCache()
	fdb.loadChatMessages()

	return fdb, nil
}

// --- Session ---

func (f *FileDB) UpsertSession(ctx context.Context, s *Session) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[s.ID] = s
	f.dirty["sessions"] = true
	return nil
}

func (f *FileDB) GetSession(ctx context.Context, id string) (*Session, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	s, ok := f.sessions[id]
	if !ok {
		return nil, nil
	}
	return s, nil
}

func (f *FileDB) ListSessions(ctx context.Context, filter SessionFilter) ([]*Session, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var result []*Session
	for _, s := range f.sessions {
		if filter.UserID != nil && s.UserID != *filter.UserID {
			continue
		}
		if filter.Type != nil && s.Type != *filter.Type {
			continue
		}
		if filter.State != nil && s.State != *filter.State {
			continue
		}
		result = append(result, s)
	}
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

func (f *FileDB) DeleteSession(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, id)
	f.dirty["sessions"] = true
	return nil
}

func (f *FileDB) DeleteExpiredSessions(ctx context.Context, before time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for id, s := range f.sessions {
		if s.ExpiresAt.Before(before) {
			delete(f.sessions, id)
			count++
		}
	}
	if count > 0 {
		f.dirty["sessions"] = true
	}
	return count, nil
}

// --- User Profile ---

func (f *FileDB) UpsertUserProfile(ctx context.Context, p *UserProfile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.memory.Profile = *p
	f.dirty["memory"] = true
	return nil
}

func (f *FileDB) GetUserProfile(ctx context.Context, userID int64) (*UserProfile, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	p := f.memory.Profile
	return &p, nil
}

// --- User Memory ---

func (f *FileDB) UpsertMemory(ctx context.Context, m *UserMemory) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Dedup by content
	lower := strings.ToLower(m.Memory)
	for i, existing := range f.memory.Memories {
		if strings.ToLower(existing.Memory) == lower {
			f.memory.Memories[i] = *m
			f.dirty["memory"] = true
			return nil
		}
	}
	f.memory.Memories = append(f.memory.Memories, *m)
	f.dirty["memory"] = true
	return nil
}

func (f *FileDB) ListMemories(ctx context.Context, userID int64, limit int) ([]*UserMemory, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	result := make([]*UserMemory, 0, len(f.memory.Memories))
	for i := range f.memory.Memories {
		result = append(result, &f.memory.Memories[i])
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (f *FileDB) DeleteMemory(ctx context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, m := range f.memory.Memories {
		if m.ID == id {
			f.memory.Memories = append(f.memory.Memories[:i], f.memory.Memories[i+1:]...)
			f.dirty["memory"] = true
			return nil
		}
	}
	return nil
}

func (f *FileDB) CountMemories(ctx context.Context, userID int64) (int, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.memory.Memories), nil
}

func (f *FileDB) TotalMemoryTokens(ctx context.Context, userID int64) (int, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	total := 0
	for _, m := range f.memory.Memories {
		total += m.TokenCount
	}
	return total, nil
}

func (f *FileDB) ReplaceAllMemories(ctx context.Context, userID int64, memories []*UserMemory) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.memory.Memories = make([]UserMemory, len(memories))
	for i, m := range memories {
		f.memory.Memories[i] = *m
	}
	f.dirty["memory"] = true
	return nil
}

// --- Conversation Summary ---

func (f *FileDB) UpsertSummary(ctx context.Context, s *ConversationSummary) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.memory.Summaries[s.ConversationID] = s
	f.dirty["memory"] = true
	return nil
}

func (f *FileDB) GetSummary(ctx context.Context, convID int64) (*ConversationSummary, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	s, ok := f.memory.Summaries[convID]
	if !ok {
		return nil, nil
	}
	return s, nil
}

func (f *FileDB) ListSummaries(ctx context.Context, userID int64, limit int) ([]*ConversationSummary, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	result := make([]*ConversationSummary, 0, len(f.memory.Summaries))
	for _, s := range f.memory.Summaries {
		result = append(result, s)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (f *FileDB) DeleteOldestSummary(ctx context.Context, userID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.memory.Summaries) == 0 {
		return nil
	}
	var oldestID int64
	var oldestTime time.Time
	for id, s := range f.memory.Summaries {
		if oldestID == 0 || s.CreatedAt.Before(oldestTime) {
			oldestID = id
			oldestTime = s.CreatedAt
		}
	}
	delete(f.memory.Summaries, oldestID)
	f.dirty["memory"] = true
	return nil
}

// --- Legacy Facts ---

func (f *FileDB) ListFacts(ctx context.Context, userID int64) ([]string, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	result := make([]string, len(f.memory.Facts))
	copy(result, f.memory.Facts)
	return result, nil
}

func (f *FileDB) AddFact(ctx context.Context, userID int64, fact string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.memory.Facts {
		if existing == fact {
			return nil
		}
	}
	f.memory.Facts = append(f.memory.Facts, fact)
	if len(f.memory.Facts) > 50 {
		f.memory.Facts = f.memory.Facts[len(f.memory.Facts)-50:]
	}
	f.dirty["memory"] = true
	return nil
}

// --- Cached Documents ---

func (f *FileDB) UpsertDocument(ctx context.Context, doc *CachedDocument) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cacheData.Documents[doc.ID] = doc
	f.dirty["cache"] = true
	return nil
}

func (f *FileDB) GetDocument(ctx context.Context, id int64) (*CachedDocument, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	d, ok := f.cacheData.Documents[id]
	if !ok {
		return nil, nil
	}
	return d, nil
}

func (f *FileDB) ListDocuments(ctx context.Context, filter DocumentFilter) ([]*CachedDocument, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	result := make([]*CachedDocument, 0, len(f.cacheData.Documents))
	for _, d := range f.cacheData.Documents {
		if filter.UserID != nil && d.UserID != *filter.UserID {
			continue
		}
		if len(filter.Tags) > 0 {
			matched := false
			for _, ft := range filter.Tags {
				for _, dt := range d.Tags {
					if strings.EqualFold(ft, dt) {
						matched = true
						break
					}
				}
				if matched {
					break
				}
			}
			if !matched {
				continue
			}
		}
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

func (f *FileDB) DeleteDocument(ctx context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.cacheData.Documents, id)
	f.dirty["cache"] = true
	return nil
}

func (f *FileDB) SearchDocuments(ctx context.Context, query string, limit int) ([]*CachedDocument, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	keywords := strings.Fields(strings.ToLower(query))
	if len(keywords) == 0 {
		return f.recentDocs(limit), nil
	}

	type scored struct {
		doc   *CachedDocument
		score int
	}
	var results []scored
	for _, d := range f.cacheData.Documents {
		score := 0
		titleLower := strings.ToLower(d.Title)
		contentLower := strings.ToLower(d.Content)
		for _, kw := range keywords {
			if strings.Contains(titleLower, kw) {
				score += 3
			}
			if strings.Contains(contentLower, kw) {
				score++
			}
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
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})
	docs := make([]*CachedDocument, 0, limit)
	for i, r := range results {
		if limit > 0 && i >= limit {
			break
		}
		docs = append(docs, r.doc)
	}
	return docs, nil
}

func (f *FileDB) recentDocs(limit int) []*CachedDocument {
	docs := make([]*CachedDocument, 0, len(f.cacheData.Documents))
	for _, d := range f.cacheData.Documents {
		docs = append(docs, d)
	}
	sort.Slice(docs, func(i, j int) bool {
		return docs[i].UpdatedAt.After(docs[j].UpdatedAt)
	})
	if limit > 0 && len(docs) > limit {
		return docs[:limit]
	}
	return docs
}

// --- Cached Tasks ---

func (f *FileDB) UpsertTask(ctx context.Context, task *CachedTask) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cacheData.Tasks[task.ID] = task
	f.dirty["cache"] = true
	return nil
}

func (f *FileDB) GetTask(ctx context.Context, id int64) (*CachedTask, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	t, ok := f.cacheData.Tasks[id]
	if !ok {
		return nil, nil
	}
	return t, nil
}

func (f *FileDB) ListTasks(ctx context.Context, filter TaskFilter) ([]*CachedTask, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	result := make([]*CachedTask, 0, len(f.cacheData.Tasks))
	for _, t := range f.cacheData.Tasks {
		if filter.UserID != nil && t.UserID != *filter.UserID {
			continue
		}
		if filter.Status != nil && t.Status != *filter.Status {
			continue
		}
		if filter.Priority != nil && t.Priority != *filter.Priority {
			continue
		}
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

func (f *FileDB) DeleteTask(ctx context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.cacheData.Tasks, id)
	f.dirty["cache"] = true
	return nil
}

// --- Trace Events ---

func (f *FileDB) WriteTrace(ctx context.Context, event *TraceEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal trace event: %w", err)
	}
	data = append(data, '\n')

	f.traces.mu.Lock()
	defer f.traces.mu.Unlock()

	file, ok := f.traces.files[event.SessionID]
	if !ok {
		path := filepath.Join(f.traces.dir, event.SessionID+".jsonl")
		file, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("open trace file: %w", err)
		}
		f.traces.files[event.SessionID] = file
	}
	_, err = file.Write(data)
	return err
}

func (f *FileDB) ListTraces(ctx context.Context, filter TraceFilter) ([]*TraceEvent, error) {
	sessionID := ""
	if filter.SessionID != nil {
		sessionID = *filter.SessionID
	}
	if sessionID == "" {
		return nil, nil
	}

	path := filepath.Join(f.traces.dir, sessionID+".jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read trace file: %w", err)
	}

	var result []*TraceEvent
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var event TraceEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if filter.EventType != nil && event.Type != *filter.EventType {
			continue
		}
		if filter.After != nil && event.Timestamp.Before(*filter.After) {
			continue
		}
		if filter.Before != nil && event.Timestamp.After(*filter.Before) {
			continue
		}
		result = append(result, &event)
	}
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

// --- Chat Messages ---

func (f *FileDB) InsertChatMessage(ctx context.Context, msg *ChatMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	msg.ID = int64(len(f.chatMsgs) + 1)
	f.chatMsgs = append(f.chatMsgs, *msg)
	f.dirty["chat"] = true
	return nil
}

func (f *FileDB) ListChatMessages(ctx context.Context, convID int64, limit int) ([]*ChatMessage, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var result []*ChatMessage
	for i := range f.chatMsgs {
		if f.chatMsgs[i].ConversationID == convID {
			result = append(result, &f.chatMsgs[i])
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[len(result)-limit:]
	}
	return result, nil
}

// --- Eval (not supported in file mode) ---

func (f *FileDB) UpsertEvalSet(_ context.Context, _ *eval.EvalSet) error {
	return fmt.Errorf("eval: not supported in file mode")
}

func (f *FileDB) GetEvalSet(_ context.Context, _ string) (*eval.EvalSet, error) {
	return nil, fmt.Errorf("eval: not supported in file mode")
}

func (f *FileDB) ListEvalSets(_ context.Context) ([]*eval.EvalSet, error) {
	return nil, fmt.Errorf("eval: not supported in file mode")
}

func (f *FileDB) DeleteEvalSet(_ context.Context, _ string) error {
	return fmt.Errorf("eval: not supported in file mode")
}

func (f *FileDB) InsertEvalCase(_ context.Context, _ *eval.EvalCase) error {
	return fmt.Errorf("eval: not supported in file mode")
}

func (f *FileDB) ListEvalCases(_ context.Context, _ string) ([]*eval.EvalCase, error) {
	return nil, fmt.Errorf("eval: not supported in file mode")
}

func (f *FileDB) DeleteEvalCase(_ context.Context, _ string) error {
	return fmt.Errorf("eval: not supported in file mode")
}

func (f *FileDB) InsertEvalRun(_ context.Context, _ *eval.EvalRun) error {
	return fmt.Errorf("eval: not supported in file mode")
}

func (f *FileDB) UpdateEvalRun(_ context.Context, _ *eval.EvalRun) error {
	return fmt.Errorf("eval: not supported in file mode")
}

func (f *FileDB) GetEvalRun(_ context.Context, _ string) (*eval.EvalRun, error) {
	return nil, fmt.Errorf("eval: not supported in file mode")
}

func (f *FileDB) ListEvalRuns(_ context.Context, _ string) ([]*eval.EvalRun, error) {
	return nil, fmt.Errorf("eval: not supported in file mode")
}

func (f *FileDB) InsertEvalResult(_ context.Context, _ *eval.EvalResult) error {
	return fmt.Errorf("eval: not supported in file mode")
}

func (f *FileDB) ListEvalResults(_ context.Context, _ string) ([]*eval.EvalResult, error) {
	return nil, fmt.Errorf("eval: not supported in file mode")
}

// --- Lifecycle ---

// --- Routing Feedback (no-op for FileDB, requires SQLite) ---

func (f *FileDB) InsertRoutingFeedback(ctx context.Context, fb *RoutingFeedback) error { return nil }
func (f *FileDB) UpdateRoutingFeedbackLiked(ctx context.Context, messageID int64, liked bool) error {
	return nil
}
func (f *FileDB) GetRoutingFeedbackStats(ctx context.Context) ([]*RoutingFeedbackStats, error) {
	return nil, nil
}
func (f *FileDB) GetLikedFeedback(ctx context.Context, since int64) ([]*RoutingFeedback, error) {
	return nil, nil
}

func (f *FileDB) Migrate(ctx context.Context) error {
	return nil // FileDB has no schema to migrate
}

func (f *FileDB) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saveAllLocked()
	f.traces.mu.Lock()
	for _, file := range f.traces.files {
		file.Close()
	}
	f.traces.files = make(map[string]*os.File)
	f.traces.mu.Unlock()
	return nil
}

// Save persists all dirty data to files.
func (f *FileDB) Save() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saveAllLocked()
}

func (f *FileDB) saveAllLocked() {
	if f.dirty["sessions"] {
		f.saveSessions()
		f.dirty["sessions"] = false
	}
	if f.dirty["memory"] {
		f.saveMemory()
		f.dirty["memory"] = false
	}
	if f.dirty["cache"] {
		f.saveCache()
		f.dirty["cache"] = false
	}
	if f.dirty["chat"] {
		f.saveChatMessages()
		f.dirty["chat"] = false
	}
}

// --- File I/O (existing JSON formats) ---

func (f *FileDB) sessionPath() string {
	return filepath.Join(f.dataDir, "sessions.json")
}

func (f *FileDB) memoryPath() string {
	return filepath.Join(f.dataDir, "memory.json")
}

func (f *FileDB) cachePath() string {
	return filepath.Join(f.dataDir, "cache.json")
}

func (f *FileDB) chatPath() string {
	return filepath.Join(f.dataDir, "chat_messages.json")
}

func (f *FileDB) loadSessions() {
	data, err := os.ReadFile(f.sessionPath())
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &f.sessions); err != nil {
		log.Printf("db/file: load sessions error: %v", err)
		f.sessions = make(map[string]*Session)
	}
}

func (f *FileDB) saveSessions() {
	data, err := json.MarshalIndent(f.sessions, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(f.sessionPath(), data, 0600)
}

func (f *FileDB) loadMemory() {
	data, err := os.ReadFile(f.memoryPath())
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &f.memory); err != nil {
		log.Printf("db/file: load memory error: %v", err)
	}
	if f.memory.Summaries == nil {
		f.memory.Summaries = make(map[int64]*ConversationSummary)
	}
}

func (f *FileDB) saveMemory() {
	f.memory.SavedAt = time.Now()
	data, err := json.MarshalIndent(f.memory, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(f.memoryPath(), data, 0644)
}

func (f *FileDB) loadCache() {
	data, err := os.ReadFile(f.cachePath())
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &f.cacheData); err != nil {
		log.Printf("db/file: load cache error: %v", err)
	}
	if f.cacheData.Documents == nil {
		f.cacheData.Documents = make(map[int64]*CachedDocument)
	}
	if f.cacheData.Tasks == nil {
		f.cacheData.Tasks = make(map[int64]*CachedTask)
	}
}

func (f *FileDB) saveCache() {
	f.cacheData.SavedAt = time.Now()
	data, err := json.MarshalIndent(f.cacheData, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(f.cachePath(), data, 0644)
}

func (f *FileDB) loadChatMessages() {
	data, err := os.ReadFile(f.chatPath())
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &f.chatMsgs); err != nil {
		log.Printf("db/file: load chat messages error: %v", err)
	}
}

func (f *FileDB) saveChatMessages() {
	data, err := json.MarshalIndent(f.chatMsgs, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(f.chatPath(), data, 0644)
}
