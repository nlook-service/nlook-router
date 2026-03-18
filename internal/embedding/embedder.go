package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"time"
)

// Embedder generates embeddings via Ollama API.
type Embedder struct {
	baseURL    string
	model      string
	httpClient *http.Client
}

// NewEmbedder creates a new embedder.
func NewEmbedder() *Embedder {
	baseURL := os.Getenv("OLLAMA_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	model := os.Getenv("NLOOK_EMBED_MODEL")
	if model == "" {
		model = "nomic-embed-text"
	}
	return &Embedder{
		baseURL:    baseURL,
		model:      model,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Embed generates an embedding vector for the given text.
func (e *Embedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model": e.model,
		"input": text,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/api/embed", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("embed failed: status=%d body=%s", resp.StatusCode, string(respBody[:min(len(respBody), 200)]))
	}

	var result struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if len(result.Embeddings) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}
	return result.Embeddings[0], nil
}

// IsAvailable checks if the embedding model is available.
func (e *Embedder) IsAvailable(ctx context.Context) bool {
	vec, err := e.Embed(ctx, "test")
	return err == nil && len(vec) > 0
}

// CosineSimilarity computes cosine similarity between two vectors.
func CosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return float32(dot / denom)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
