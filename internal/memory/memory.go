package memory

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// UserProfile stores user's role, interests, preferences.
type UserProfile struct {
	Role      string   `json:"role,omitempty"`      // e.g. "developer", "designer"
	Interests []string `json:"interests,omitempty"` // e.g. ["AI", "Go", "React"]
	Notes     string   `json:"notes,omitempty"`     // free-form notes
	Lang      string   `json:"lang,omitempty"`      // preferred language
	UpdatedAt time.Time `json:"updated_at"`
}

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
	Profile    UserProfile                    `json:"profile"`
	Summaries  map[int64]*ConversationSummary `json:"summaries"`
	Facts      []string                       `json:"facts,omitempty"` // Learned facts about user
	SavedAt    time.Time                      `json:"saved_at"`
}

// Store provides long-term memory persistence.
type Store struct {
	mu        sync.RWMutex
	profile   UserProfile
	summaries map[int64]*ConversationSummary
	facts     []string
	filePath  string
	dirty     bool
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

	// Learned Facts
	if len(s.facts) > 0 {
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
	log.Printf("memory: loaded profile + %d summaries + %d facts", len(s.summaries), len(s.facts))
}

func (s *Store) saveToFile() {
	mf := memoryFile{
		Profile:   s.profile,
		Summaries: s.summaries,
		Facts:     s.facts,
		SavedAt:   time.Now(),
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
