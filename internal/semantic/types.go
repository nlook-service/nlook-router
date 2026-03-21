package semantic

import "context"

// Embedder generates embedding vectors for text.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	IsAvailable(ctx context.Context) bool
}

// IntentCategory defines an intent with representative embeddings.
type IntentCategory struct {
	Name          string      `json:"name"`
	Complexity    string      `json:"complexity"` // simple, complex, reasoning
	Texts         []string    `json:"texts"`
	Vectors       [][]float32 `json:"vectors,omitempty"`
	Centroid      []float32   `json:"-"`
	MinTier       int         `json:"min_tier"`
	PreferredTier int         `json:"preferred_tier"`
}

// ClassifyResult is returned by the Semantic Router.
type ClassifyResult struct {
	Intent          string  `json:"intent"`
	Complexity      string  `json:"complexity"`
	SimilarityScore float64 `json:"similarity_score"`
	ModelTier       int     `json:"model_tier"`
	Model           string  `json:"model"`
	Confident       bool    `json:"confident"`
}

// RoutingInfo is included in chat responses for frontend display.
type RoutingInfo struct {
	Intent          string  `json:"matched_intent"`
	SimilarityScore float64 `json:"similarity_score"`
	ModelTier       int     `json:"model_tier"`
	Model           string  `json:"model"`
}

// FeedbackRecord stores a routing decision + user feedback.
type FeedbackRecord struct {
	ID              int64  `json:"id"`
	ConversationID  int64  `json:"conversation_id"`
	MessageID       int64  `json:"message_id"`
	QueryText       string `json:"query_text"`
	MatchedIntent   string `json:"matched_intent"`
	SimilarityScore float64 `json:"similarity_score"`
	ModelTier       int    `json:"model_tier"`
	ModelUsed       string `json:"model_used"`
	Liked           *bool  `json:"liked"`
	CreatedAt       int64  `json:"created_at"`
}

// FeedbackStats is an aggregated stat per intent+tier.
type FeedbackStats struct {
	Intent     string
	ModelTier  int
	TotalCount int
	LikedCount int
}
