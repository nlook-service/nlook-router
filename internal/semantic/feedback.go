package semantic

import (
	"context"
	"log"
	"time"

	"github.com/nlook-service/nlook-router/internal/db"
)

// FeedbackManager records routing decisions and applies learning.
type FeedbackManager struct {
	storage     db.DB
	intentStore *IntentStore
	embedder    Embedder
	minSamples  int
	lastLearn   int64
}

// NewFeedbackManager creates a new feedback manager.
func NewFeedbackManager(storage db.DB, intentStore *IntentStore, embedder Embedder, minSamples int) *FeedbackManager {
	return &FeedbackManager{
		storage:     storage,
		intentStore: intentStore,
		embedder:    embedder,
		minSamples:  minSamples,
	}
}

// Record saves a routing decision (called after every chat response).
func (f *FeedbackManager) Record(ctx context.Context, rec FeedbackRecord) error {
	if f.storage == nil {
		return nil
	}
	return f.storage.InsertRoutingFeedback(ctx, &db.RoutingFeedback{
		ConversationID:  rec.ConversationID,
		MessageID:       rec.MessageID,
		QueryText:       rec.QueryText,
		MatchedIntent:   rec.MatchedIntent,
		SimilarityScore: rec.SimilarityScore,
		ModelTier:       rec.ModelTier,
		ModelUsed:       rec.ModelUsed,
		CreatedAt:       time.Now().Unix(),
	})
}

// UpdateLiked updates the liked field when user gives feedback.
func (f *FeedbackManager) UpdateLiked(ctx context.Context, messageID int64, liked bool) error {
	if f.storage == nil {
		return nil
	}
	return f.storage.UpdateRoutingFeedbackLiked(ctx, messageID, liked)
}

// Learn analyzes accumulated feedback and adjusts routing parameters.
func (f *FeedbackManager) Learn(ctx context.Context) error {
	if f.storage == nil {
		return nil
	}

	// A. Per-category like rate analysis → threshold adjustment
	stats, err := f.storage.GetRoutingFeedbackStats(ctx)
	if err != nil {
		return err
	}

	for _, stat := range stats {
		if stat.TotalCount < f.minSamples {
			continue
		}
		likeRate := float64(stat.LikedCount) / float64(stat.TotalCount)
		if likeRate < 0.5 {
			f.intentStore.RaiseMinTier(stat.Intent)
		} else if likeRate > 0.9 {
			f.intentStore.LowerMinTier(stat.Intent)
		}
	}

	// B. Add liked query embeddings to intent vectors
	likedRecords, err := f.storage.GetLikedFeedback(ctx, f.lastLearn)
	if err != nil {
		return err
	}
	for _, rec := range likedRecords {
		vec, err := f.embedder.Embed(ctx, rec.QueryText)
		if err != nil {
			continue
		}
		f.intentStore.AddVector(rec.MatchedIntent, vec)
	}

	f.lastLearn = time.Now().Unix()
	if err := f.intentStore.Save(); err != nil {
		log.Printf("semantic/feedback: save intent vectors: %v", err)
	}

	log.Printf("semantic/feedback: learn complete — %d stats, %d new vectors", len(stats), len(likedRecords))
	return nil
}

// StartLearningLoop runs Learn periodically.
func (f *FeedbackManager) StartLearningLoop(ctx context.Context, interval string) {
	dur, err := time.ParseDuration(interval)
	if err != nil {
		dur = 24 * time.Hour
	}

	ticker := time.NewTicker(dur)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := f.Learn(ctx); err != nil {
				log.Printf("semantic/feedback: learn error: %v", err)
			}
		}
	}
}
