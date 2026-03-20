package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s := &Store{
		summaries: make(map[int64]*ConversationSummary),
		filePath:  filepath.Join(dir, "memory.json"),
	}
	return s
}

func TestStoreAddMemory(t *testing.T) {
	s := newTestStore(t)

	s.AddMemory(UserMemory{ID: "m1", Memory: "User is a Go developer"})
	s.AddMemory(UserMemory{ID: "m2", Memory: "User prefers dark theme"})

	mems := s.GetMemories()
	if len(mems) != 2 {
		t.Fatalf("expected 2 memories, got %d", len(mems))
	}
	if mems[0].Memory != "User is a Go developer" {
		t.Errorf("expected first memory text, got %s", mems[0].Memory)
	}
}

func TestStoreAddMemoryDedup(t *testing.T) {
	s := newTestStore(t)

	s.AddMemory(UserMemory{ID: "m1", Memory: "User is a developer"})
	s.AddMemory(UserMemory{ID: "m2", Memory: "user is a developer"}) // case-insensitive dup

	mems := s.GetMemories()
	if len(mems) != 1 {
		t.Fatalf("expected 1 memory (deduped), got %d", len(mems))
	}
}

func TestStoreAddMemoryEviction(t *testing.T) {
	s := newTestStore(t)

	// Fill to max
	for i := 0; i < MaxMemories+5; i++ {
		s.AddMemory(UserMemory{
			ID:     generateMemoryID(),
			Memory: "fact " + string(rune('A'+i%26)) + string(rune('0'+i/26)),
		})
	}

	if len(s.memories) != MaxMemories {
		t.Errorf("expected %d memories after eviction, got %d", MaxMemories, len(s.memories))
	}
}

func TestStoreAddMemoryTokenCount(t *testing.T) {
	s := newTestStore(t)

	s.AddMemory(UserMemory{ID: "m1", Memory: "User works on nlook project with Go and React"})

	mems := s.GetMemories()
	if mems[0].TokenCount == 0 {
		t.Error("expected TokenCount to be auto-calculated")
	}
	if s.TotalTokens() == 0 {
		t.Error("expected TotalTokens > 0")
	}
}

func TestStoreLearnFact(t *testing.T) {
	s := newTestStore(t)

	s.LearnFact("likes Go")
	s.LearnFact("likes Go") // duplicate
	s.LearnFact("uses vim")

	facts := s.GetFacts()
	if len(facts) != 2 {
		t.Errorf("expected 2 facts, got %d", len(facts))
	}
}

func TestStoreConversationSummary(t *testing.T) {
	s := newTestStore(t)

	s.SetConversationSummary(100, "Discussed project architecture", 10)

	summary, ok := s.GetConversationSummary(100)
	if !ok {
		t.Fatal("expected summary to exist")
	}
	if summary != "Discussed project architecture" {
		t.Errorf("unexpected summary: %s", summary)
	}

	_, ok = s.GetConversationSummary(999)
	if ok {
		t.Error("expected false for missing conversation")
	}
}

func TestStoreConversationSummaryEviction(t *testing.T) {
	s := newTestStore(t)

	// Add 25 summaries (max is 20)
	for i := int64(1); i <= 25; i++ {
		s.SetConversationSummary(i, "summary", 5)
	}

	if len(s.summaries) != 20 {
		t.Errorf("expected 20 summaries, got %d", len(s.summaries))
	}
}

func TestStoreGetRecentSummaries(t *testing.T) {
	s := newTestStore(t)

	s.SetConversationSummary(1, "old", 5)
	time.Sleep(10 * time.Millisecond)
	s.SetConversationSummary(2, "new", 10)

	recent := s.GetRecentSummaries(1)
	if len(recent) != 1 {
		t.Fatalf("expected 1, got %d", len(recent))
	}
	if recent[0].Summary != "new" {
		t.Error("expected newest summary first")
	}
}

func TestStoreProfile(t *testing.T) {
	s := newTestStore(t)

	s.UpdateProfile(UserProfile{Role: "developer", Interests: []string{"AI", "Go"}})

	p := s.GetProfile()
	if p.Role != "developer" {
		t.Errorf("expected developer, got %s", p.Role)
	}
	if len(p.Interests) != 2 {
		t.Errorf("expected 2 interests, got %d", len(p.Interests))
	}
	if p.UpdatedAt.IsZero() {
		t.Error("expected UpdatedAt to be set")
	}
}

func TestStoreBuildPromptContext(t *testing.T) {
	s := newTestStore(t)

	s.UpdateProfile(UserProfile{Role: "developer"})
	s.AddMemory(UserMemory{ID: "m1", Memory: "User builds web apps with Go"})
	s.SetConversationSummary(42, "Talked about deployment", 5)

	ctx := s.BuildPromptContext(42)

	if ctx == "" {
		t.Fatal("expected non-empty prompt context")
	}
	if !contains(ctx, "developer") {
		t.Error("expected profile role in context")
	}
	if !contains(ctx, "User builds web apps") {
		t.Error("expected structured memory in context")
	}
	if !contains(ctx, "Talked about deployment") {
		t.Error("expected conversation summary in context")
	}
}

func TestStoreBuildPromptContextLegacyFacts(t *testing.T) {
	s := newTestStore(t)

	// No structured memories, only legacy facts
	s.LearnFact("prefers dark mode")

	ctx := s.BuildPromptContext(0)
	if !contains(ctx, "prefers dark mode") {
		t.Error("expected legacy fact in context when no structured memories")
	}
}

func TestStoreBuildPromptContextStructuredOverLegacy(t *testing.T) {
	s := newTestStore(t)

	s.LearnFact("legacy fact")
	s.AddMemory(UserMemory{ID: "m1", Memory: "structured memory"})

	ctx := s.BuildPromptContext(0)
	if !contains(ctx, "structured memory") {
		t.Error("expected structured memory in context")
	}
	if contains(ctx, "legacy fact") {
		t.Error("legacy facts should not appear when structured memories exist")
	}
}

func TestStorePersistence(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "memory.json")

	// Create and save
	s1 := &Store{
		summaries: make(map[int64]*ConversationSummary),
		filePath:  fp,
	}
	s1.UpdateProfile(UserProfile{Role: "dev"})
	s1.AddMemory(UserMemory{ID: "m1", Memory: "test fact", Topics: []string{"test"}})
	s1.LearnFact("legacy fact")
	s1.SetConversationSummary(1, "conv summary", 5)
	s1.saveToFile()

	// Load into new store
	s2 := &Store{
		summaries: make(map[int64]*ConversationSummary),
		filePath:  fp,
	}
	s2.loadFromFile()

	if s2.profile.Role != "dev" {
		t.Errorf("expected role dev, got %s", s2.profile.Role)
	}
	if len(s2.memories) != 1 {
		t.Errorf("expected 1 memory, got %d", len(s2.memories))
	}
	if s2.memories[0].Memory != "test fact" {
		t.Errorf("expected 'test fact', got %s", s2.memories[0].Memory)
	}
	if len(s2.facts) != 1 {
		t.Errorf("expected 1 legacy fact, got %d", len(s2.facts))
	}
	if len(s2.summaries) != 1 {
		t.Errorf("expected 1 summary, got %d", len(s2.summaries))
	}
	if s2.totalTokens == 0 {
		t.Error("expected totalTokens > 0 after load")
	}
}

func TestStorePersistenceBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "memory.json")

	// Write old-format file (no memories field)
	oldData := `{
		"profile": {"role": "designer"},
		"summaries": {},
		"facts": ["likes dark mode", "speaks Korean"],
		"saved_at": "2026-01-01T00:00:00Z"
	}`
	os.WriteFile(fp, []byte(oldData), 0644)

	s := &Store{
		summaries: make(map[int64]*ConversationSummary),
		filePath:  fp,
	}
	s.loadFromFile()

	if s.profile.Role != "designer" {
		t.Errorf("expected designer, got %s", s.profile.Role)
	}
	if len(s.facts) != 2 {
		t.Errorf("expected 2 legacy facts, got %d", len(s.facts))
	}
	if len(s.memories) != 0 {
		t.Errorf("expected 0 memories (old format), got %d", len(s.memories))
	}
}

func TestStoreOptimizeIfNeededNoOptimizer(t *testing.T) {
	s := newTestStore(t)

	// No optimizer set — should be a no-op
	err := s.OptimizeIfNeeded(nil)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestStoreOptimizeIfNeededBelowThreshold(t *testing.T) {
	s := newTestStore(t)
	s.optimizer = &mockOptimizer{called: false}

	// Add small memory (well below threshold)
	s.AddMemory(UserMemory{ID: "m1", Memory: "small fact", TokenCount: 10})

	err := s.OptimizeIfNeeded(nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if s.optimizer.(*mockOptimizer).called {
		t.Error("optimizer should not be called below threshold")
	}
}

func TestStoreSetOptimizer(t *testing.T) {
	s := newTestStore(t)
	opt := &mockOptimizer{}
	s.SetOptimizer(opt)

	if s.optimizer == nil {
		t.Error("expected optimizer to be set")
	}
}

func TestStoreSetFactExtractor(t *testing.T) {
	s := newTestStore(t)
	s.SetFactExtractor(&FactExtractor{})

	if s.factExtractor == nil {
		t.Error("expected fact extractor to be set")
	}
}

func TestRecalcTotalTokens(t *testing.T) {
	s := newTestStore(t)

	s.memories = []UserMemory{
		{Memory: "fact one", TokenCount: 10},
		{Memory: "fact two", TokenCount: 20},
	}
	s.summaries[1] = &ConversationSummary{Summary: "a summary"}
	s.facts = []string{"legacy"}

	s.recalcTotalTokens()

	if s.totalTokens <= 30 {
		t.Errorf("expected totalTokens > 30 (memories + summaries + facts), got %d", s.totalTokens)
	}
}

func TestMemoryFileJSON(t *testing.T) {
	mf := memoryFile{
		Profile: UserProfile{Role: "dev"},
		Summaries: map[int64]*ConversationSummary{
			1: {ConversationID: 1, Summary: "test"},
		},
		Facts:       []string{"fact1"},
		Memories:    []UserMemory{{ID: "m1", Memory: "structured"}},
		TotalTokens: 100,
		SavedAt:     time.Now(),
	}

	data, err := json.Marshal(mf)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var mf2 memoryFile
	if err := json.Unmarshal(data, &mf2); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if mf2.Profile.Role != "dev" {
		t.Errorf("expected dev, got %s", mf2.Profile.Role)
	}
	if len(mf2.Memories) != 1 {
		t.Errorf("expected 1 memory, got %d", len(mf2.Memories))
	}
	if mf2.TotalTokens != 100 {
		t.Errorf("expected 100, got %d", mf2.TotalTokens)
	}
}

func TestGenerateMemoryID(t *testing.T) {
	id1 := generateMemoryID()
	id2 := generateMemoryID()
	if id1 == id2 {
		t.Error("expected unique IDs")
	}
	if len(id1) < 10 {
		t.Errorf("expected reasonable ID length, got %d", len(id1))
	}
}

// mockOptimizer tracks whether Optimize was called.
type mockOptimizer struct {
	called bool
}

func (m *mockOptimizer) Optimize(_ context.Context, memories []UserMemory) ([]UserMemory, error) {
	m.called = true
	return memories, nil
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
