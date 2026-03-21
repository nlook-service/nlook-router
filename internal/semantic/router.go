package semantic

import (
	"context"
	"log"
)

// TierThresholds defines score boundaries for model tier selection.
type TierThresholds struct {
	Tier1 float64 // >= this → local model (default: 0.85)
	Tier2 float64 // >= this → fast API model (default: 0.65)
	Tier3 float64 // >= this → balanced API model (default: 0.45)
	// below Tier3 → Tier 4 (deep/reasoning)
}

// TierModels maps tier numbers to model names.
type TierModels struct {
	Tier1 string // e.g. "gemma3:4b"
	Tier2 string // e.g. "claude-haiku-4-5-20251001"
	Tier3 string // e.g. "claude-sonnet-4-6-20260320"
	Tier4 string // e.g. "claude-opus-4-6-20260318"
}

// Router classifies queries using embedding similarity.
type Router struct {
	intentStore *IntentStore
	embedder    Embedder
	thresholds  TierThresholds
	models      TierModels
	fallbackFn  func(ctx context.Context, query string) ClassifyResult
}

// NewRouter creates and initializes the semantic router.
func NewRouter(intentStore *IntentStore, embedder Embedder, thresholds TierThresholds, models TierModels, fallback func(ctx context.Context, query string) ClassifyResult) *Router {
	return &Router{
		intentStore: intentStore,
		embedder:    embedder,
		thresholds:  thresholds,
		models:      models,
		fallbackFn:  fallback,
	}
}

// Classify returns intent, score, and model tier for a query.
func (r *Router) Classify(ctx context.Context, query string) ClassifyResult {
	// 1. Embed query
	vec, err := r.embedder.Embed(ctx, query)
	if err != nil {
		log.Printf("semantic/router: embed error: %v, falling back", err)
		if r.fallbackFn != nil {
			return r.fallbackFn(ctx, query)
		}
		return ClassifyResult{Intent: "general", Complexity: "complex", ModelTier: 2, Model: r.models.Tier2}
	}

	// 2. Match against intent centroids
	intent, score := r.intentStore.Match(vec)

	// 3. Determine model tier from score + per-category overrides
	tier := r.scoreTier(score, intent)

	// 4. Resolve model name from tier
	model := r.tierModel(tier)

	// 5. Get complexity from intent store
	complexity := r.intentStore.GetComplexity(intent)

	return ClassifyResult{
		Intent:          intent,
		Complexity:      complexity,
		SimilarityScore: score,
		ModelTier:       tier,
		Model:           model,
		Confident:       score >= r.thresholds.Tier2,
	}
}

// IntentStore returns the underlying intent store (for feedback manager).
func (r *Router) IntentStore() *IntentStore {
	return r.intentStore
}

// scoreTier maps similarity score to model tier, respecting per-category minimums.
func (r *Router) scoreTier(score float64, intent string) int {
	// Base tier from score
	tier := 4
	switch {
	case score >= r.thresholds.Tier1:
		tier = 1
	case score >= r.thresholds.Tier2:
		tier = 2
	case score >= r.thresholds.Tier3:
		tier = 3
	}

	// Enforce per-category minimum (learned from feedback)
	cat := r.intentStore.Get(intent)
	if cat != nil && tier < cat.MinTier {
		tier = cat.MinTier
	}

	return tier
}

// tierModel resolves a tier number to a model name.
func (r *Router) tierModel(tier int) string {
	switch tier {
	case 1:
		return r.models.Tier1
	case 2:
		return r.models.Tier2
	case 3:
		return r.models.Tier3
	default:
		return r.models.Tier4
	}
}
