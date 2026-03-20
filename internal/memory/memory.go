package memory

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nlook-service/nlook-router/internal/db"
	"github.com/nlook-service/nlook-router/internal/tokenizer"
)

// UserProfile stores user's role, interests, preferences.
type UserProfile struct {
	Role      string   `json:"role,omitempty"`      // e.g. "developer", "designer"
	Interests []string `json:"interests,omitempty"` // e.g. ["AI", "Go", "React"]
	Notes     string   `json:"notes,omitempty"`     // free-form notes
	Lang      string   `json:"lang,omitempty"`      // preferred language
	UpdatedAt time.Time `json:"updated_at"`
}

// UserMemory is a structured memory unit with metadata.
type UserMemory struct {
	ID         string    `json:"id"`
	Memory     string    `json:"memory"`
	Topics     []string  `json:"topics,omitempty"`
	UserID     int64     `json:"user_id,omitempty"`
	TokenCount int       `json:"token_count,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// OptimizeTokenThreshold is the total token count that triggers memory optimization.
const OptimizeTokenThreshold = 4000

// MaxMemories is the maximum number of structured memories to retain.
const MaxMemories = 100

// ConversationSummary stores a compressed summary of a conversation.
type ConversationSummary struct {
	ConversationID int64     `json:"conversation_id"`
	Summary        string    `json:"summary"`
	MessageCount   int       `json:"message_count"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// memoryFile is the persistence format.
type memoryFile struct {
	Profile     UserProfile                    `json:"profile"`
	Summaries   map[int64]*ConversationSummary `json:"summaries"`
	Facts       []string                       `json:"facts,omitempty"`       // legacy: kept for compat
	Memories    []UserMemory                   `json:"memories,omitempty"`    // structured memories
	SavedAt     time.Time                      `json:"saved_at"`
	TotalTokens int                            `json:"total_tokens,omitempty"`
}

// Store provides long-term memory persistence.
type Store struct {
	mu             sync.RWMutex
	profile        UserProfile
	summaries      map[int64]*ConversationSummary
	facts          []string
	memories       []UserMemory
	totalTokens    int
	filePath       string
	dirty          bool
	optimizer      MemoryOptimizer
	factExtractor  *FactExtractor
	optimizing     sync.Mutex // prevents concurrent optimization
	db             db.DB      // optional: unified DB layer (nil = file-based)
}

// NewStore creates a new memory store.
func NewStore() *Store {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".nlook")
	os.MkdirAll(dir, 0755)

	s := &Store{
		summaries: make(map[int64]*ConversationSummary),
		filePath:  filepath.Join(dir, "memory.json"),
	}
	s.loadFromFile()

	go func() {
		for {
			time.Sleep(30 * time.Second)
			s.saveIfDirty()
		}
	}()

	return s
}

// GetProfile returns the user profile.
func (s *Store) GetProfile() UserProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.profile
}

// UpdateProfile updates the user profile.
func (s *Store) UpdateProfile(p UserProfile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.UpdatedAt = time.Now()
	s.profile = p
	s.dirty = true
	s.syncProfileToDB()
}

// LearnFact adds a learned fact about the user.
func (s *Store) LearnFact(fact string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Avoid duplicates
	for _, f := range s.facts {
		if f == fact {
			return
		}
	}
	s.facts = append(s.facts, fact)
	if len(s.facts) > 50 {
		s.facts = s.facts[len(s.facts)-50:] // Keep last 50
	}
	s.dirty = true
	s.syncFactToDB(fact)
}

// GetFacts returns learned facts.
func (s *Store) GetFacts() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string{}, s.facts...)
}

// SetConversationSummary stores a conversation summary.
func (s *Store) SetConversationSummary(convID int64, summary string, msgCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.summaries[convID] = &ConversationSummary{
		ConversationID: convID,
		Summary:        summary,
		MessageCount:   msgCount,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	// Keep only last 20 summaries
	if len(s.summaries) > 20 {
		var oldest int64
		var oldestTime time.Time
		for id, sum := range s.summaries {
			if oldest == 0 || sum.CreatedAt.Before(oldestTime) {
				oldest = id
				oldestTime = sum.CreatedAt
			}
		}
		delete(s.summaries, oldest)
	}
	s.dirty = true
	s.syncSummaryToDB(s.summaries[convID])
}

// GetConversationSummary returns summary for a conversation.
func (s *Store) GetConversationSummary(convID int64) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sum, ok := s.summaries[convID]; ok {
		return sum.Summary, true
	}
	return "", false
}

// GetRecentSummaries returns recent conversation summaries.
func (s *Store) GetRecentSummaries(limit int) []*ConversationSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*ConversationSummary, 0, len(s.summaries))
	for _, sum := range s.summaries {
		result = append(result, sum)
	}
	// Sort by UpdatedAt desc
	for i := 0; i < len(result); i++ {
		for j := i + 1; j < len(result); j++ {
			if result[j].UpdatedAt.After(result[i].UpdatedAt) {
				result[i], result[j] = result[j], result[i]
			}
		}
	}
	if limit > 0 && len(result) > limit {
		return result[:limit]
	}
	return result
}

// BuildPromptContext generates the memory section for system prompt.
func (s *Store) BuildPromptContext(convID int64) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var sb strings.Builder

	// User Profile
	if s.profile.Role != "" || len(s.profile.Interests) > 0 {
		sb.WriteString("\n\n[User Profile]\n")
		if s.profile.Role != "" {
			sb.WriteString("- Role: " + s.profile.Role + "\n")
		}
		if len(s.profile.Interests) > 0 {
			sb.WriteString("- Interests: " + strings.Join(s.profile.Interests, ", ") + "\n")
		}
		if s.profile.Notes != "" {
			sb.WriteString("- Notes: " + s.profile.Notes + "\n")
		}
	}

	// Structured Memories (preferred over legacy facts)
	if len(s.memories) > 0 {
		sb.WriteString("\n[Known Context About User]\n")
		for _, m := range s.memories {
			sb.WriteString("- " + m.Memory + "\n")
		}
	}

	// Legacy facts (only if no structured memories)
	if len(s.memories) == 0 && len(s.facts) > 0 {
		sb.WriteString("\n[Known Facts About User]\n")
		for _, f := range s.facts {
			sb.WriteString("- " + f + "\n")
		}
	}

	// Current conversation summary (if exists)
	if convID > 0 {
		if sum, ok := s.summaries[convID]; ok {
			sb.WriteString("\n[Previous Conversation Summary]\n")
			sb.WriteString(sum.Summary + "\n")
		}
	}

	return sb.String()
}

// SetOptimizer sets the memory optimization strategy.
func (s *Store) SetOptimizer(opt MemoryOptimizer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.optimizer = opt
}

// SetFactExtractor sets the LLM-based fact extractor.
func (s *Store) SetFactExtractor(fe *FactExtractor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.factExtractor = fe
}

// AddMemory adds a structured memory, deduplicating by content similarity.
func (s *Store) AddMemory(m UserMemory) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Simple dedup: skip if memory text already exists
	lower := strings.ToLower(m.Memory)
	for _, existing := range s.memories {
		if strings.ToLower(existing.Memory) == lower {
			return
		}
	}
	if m.TokenCount == 0 {
		m.TokenCount = tokenizer.EstimateTokens(m.Memory)
	}
	s.memories = append(s.memories, m)
	s.totalTokens += m.TokenCount
	// Evict oldest if over limit
	if len(s.memories) > MaxMemories {
		evicted := s.memories[0]
		s.memories = s.memories[1:]
		s.totalTokens -= evicted.TokenCount
	}
	s.dirty = true
	s.syncMemoryToDB(m)
}

// GetMemories returns all structured memories.
func (s *Store) GetMemories() []UserMemory {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]UserMemory, len(s.memories))
	copy(result, s.memories)
	return result
}

// TotalTokens returns the cached total token count of all memories + summaries.
func (s *Store) TotalTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalTokens
}

// LearnFromConversation extracts facts from conversation using LLM and stores them.
func (s *Store) LearnFromConversation(ctx context.Context, messages []HistoryMessage) error {
	s.mu.RLock()
	fe := s.factExtractor
	s.mu.RUnlock()

	if fe == nil {
		return nil
	}

	facts, err := fe.Extract(ctx, messages)
	if err != nil {
		return err
	}
	for _, f := range facts {
		s.AddMemory(f)
	}
	return nil
}

// OptimizeIfNeeded checks token threshold and runs optimizer if exceeded.
func (s *Store) OptimizeIfNeeded(ctx context.Context) error {
	s.mu.RLock()
	opt := s.optimizer
	total := s.totalTokens
	memCount := len(s.memories)
	s.mu.RUnlock()

	if opt == nil || total < OptimizeTokenThreshold || memCount < 2 {
		return nil
	}

	// Prevent concurrent optimization
	if !s.optimizing.TryLock() {
		return nil
	}
	defer s.optimizing.Unlock()

	// Re-check under lock
	s.mu.RLock()
	memories := make([]UserMemory, len(s.memories))
	copy(memories, s.memories)
	s.mu.RUnlock()

	optimized, err := opt.Optimize(ctx, memories)
	if err != nil {
		return err
	}

	// Calculate new total
	newTotal := 0
	for _, m := range optimized {
		if m.TokenCount == 0 {
			m.TokenCount = tokenizer.EstimateTokens(m.Memory)
		}
		newTotal += m.TokenCount
	}

	s.mu.Lock()
	s.memories = optimized
	s.totalTokens = newTotal
	s.dirty = true
	s.mu.Unlock()

	s.syncReplaceAllMemoriesToDB(optimized)

	return nil
}

// recalcTotalTokens recalculates the total token count from all sources.
func (s *Store) recalcTotalTokens() {
	total := 0
	for i := range s.memories {
		if s.memories[i].TokenCount == 0 {
			s.memories[i].TokenCount = tokenizer.EstimateTokens(s.memories[i].Memory)
		}
		total += s.memories[i].TokenCount
	}
	for _, sum := range s.summaries {
		total += tokenizer.EstimateTokens(sum.Summary)
	}
	for _, f := range s.facts {
		total += tokenizer.EstimateTokens(f)
	}
	s.totalTokens = total
}

// Save persists to file.
func (s *Store) Save() {
	s.mu.RLock()
	defer s.mu.RUnlock()
	s.saveToFile()
}

func (s *Store) loadFromFile() {
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return
	}
	var mf memoryFile
	if err := json.Unmarshal(data, &mf); err != nil {
		log.Printf("memory: load error: %v", err)
		return
	}
	s.profile = mf.Profile
	if mf.Summaries != nil {
		s.summaries = mf.Summaries
	}
	s.facts = mf.Facts
	s.memories = mf.Memories
	s.recalcTotalTokens()
	log.Printf("memory: loaded profile + %d summaries + %d facts + %d memories (%d tokens)",
		len(s.summaries), len(s.facts), len(s.memories), s.totalTokens)
}

func (s *Store) saveToFile() {
	mf := memoryFile{
		Profile:     s.profile,
		Summaries:   s.summaries,
		Facts:       s.facts,
		Memories:    s.memories,
		SavedAt:     time.Now(),
		TotalTokens: s.totalTokens,
	}
	data, err := json.MarshalIndent(mf, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(s.filePath, data, 0644)
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
