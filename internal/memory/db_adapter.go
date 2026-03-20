package memory

import (
	"context"
	"log"
	"time"

	"github.com/nlook-service/nlook-router/internal/db"
)

// NewStoreWithDB creates a memory store backed by the unified DB layer.
// The returned Store has the same public API as NewStore() but delegates
// persistence to the DB interface instead of a JSON file.
func NewStoreWithDB(storage db.DB) *Store {
	s := &Store{
		summaries: make(map[int64]*ConversationSummary),
		db:        storage,
	}
	s.loadFromDB()
	return s
}

func (s *Store) loadFromDB() {
	if s.db == nil {
		return
	}
	ctx := context.Background()

	// Load profile
	p, err := s.db.GetUserProfile(ctx, 0)
	if err != nil {
		log.Printf("memory/db: load profile error: %v", err)
	} else if p != nil {
		s.profile = UserProfile{
			Role:      p.Role,
			Interests: p.Interests,
			Notes:     p.Notes,
			Lang:      p.Lang,
			UpdatedAt: p.UpdatedAt,
		}
	}

	// Load memories
	memories, err := s.db.ListMemories(ctx, 0, MaxMemories)
	if err != nil {
		log.Printf("memory/db: load memories error: %v", err)
	} else {
		for _, m := range memories {
			s.memories = append(s.memories, UserMemory{
				ID:         m.ID,
				Memory:     m.Memory,
				Topics:     m.Topics,
				UserID:     m.UserID,
				TokenCount: m.TokenCount,
				CreatedAt:  m.CreatedAt,
				UpdatedAt:  m.UpdatedAt,
			})
		}
	}

	// Load summaries
	summaries, err := s.db.ListSummaries(ctx, 0, 20)
	if err != nil {
		log.Printf("memory/db: load summaries error: %v", err)
	} else {
		for _, cs := range summaries {
			s.summaries[cs.ConversationID] = &ConversationSummary{
				ConversationID: cs.ConversationID,
				Summary:        cs.Summary,
				MessageCount:   cs.MessageCount,
				CreatedAt:      cs.CreatedAt,
				UpdatedAt:      cs.UpdatedAt,
			}
		}
	}

	// Load facts
	facts, err := s.db.ListFacts(ctx, 0)
	if err != nil {
		log.Printf("memory/db: load facts error: %v", err)
	} else {
		s.facts = facts
	}

	s.recalcTotalTokens()
	log.Printf("memory/db: loaded profile + %d summaries + %d facts + %d memories (%d tokens)",
		len(s.summaries), len(s.facts), len(s.memories), s.totalTokens)
}

// syncProfileToDB persists profile to DB (called after in-memory update).
func (s *Store) syncProfileToDB() {
	if s.db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.mu.RLock()
		p := s.profile
		s.mu.RUnlock()
		s.db.UpsertUserProfile(ctx, &db.UserProfile{
			UserID:    0,
			Role:      p.Role,
			Interests: p.Interests,
			Notes:     p.Notes,
			Lang:      p.Lang,
			UpdatedAt: p.UpdatedAt,
		})
	}()
}

// syncMemoryToDB persists a single memory to DB.
func (s *Store) syncMemoryToDB(m UserMemory) {
	if s.db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.db.UpsertMemory(ctx, &db.UserMemory{
			ID:         m.ID,
			UserID:     m.UserID,
			Memory:     m.Memory,
			Topics:     m.Topics,
			TokenCount: m.TokenCount,
			CreatedAt:  m.CreatedAt,
			UpdatedAt:  m.UpdatedAt,
		})
	}()
}

// syncSummaryToDB persists a summary to DB.
func (s *Store) syncSummaryToDB(cs *ConversationSummary) {
	if s.db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.db.UpsertSummary(ctx, &db.ConversationSummary{
			ConversationID: cs.ConversationID,
			UserID:         0,
			Summary:        cs.Summary,
			MessageCount:   cs.MessageCount,
			CreatedAt:      cs.CreatedAt,
			UpdatedAt:      cs.UpdatedAt,
		})
	}()
}

// syncFactToDB persists a fact to DB.
func (s *Store) syncFactToDB(fact string) {
	if s.db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.db.AddFact(ctx, 0, fact)
	}()
}

// syncReplaceAllMemoriesToDB replaces all memories in DB after optimization.
func (s *Store) syncReplaceAllMemoriesToDB(memories []UserMemory) {
	if s.db == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		dbMems := make([]*db.UserMemory, len(memories))
		for i, m := range memories {
			dbMems[i] = &db.UserMemory{
				ID:         m.ID,
				UserID:     m.UserID,
				Memory:     m.Memory,
				Topics:     m.Topics,
				TokenCount: m.TokenCount,
				CreatedAt:  m.CreatedAt,
				UpdatedAt:  m.UpdatedAt,
			}
		}
		s.db.ReplaceAllMemories(ctx, 0, dbMems)
	}()
}

