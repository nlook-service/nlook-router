package semantic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// OpenAIEmbedder calls OpenAI embedding API.
type OpenAIEmbedder struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewOpenAIEmbedder creates an OpenAI embedding client.
func NewOpenAIEmbedder(apiKey, model string) *OpenAIEmbedder {
	if model == "" {
		model = "text-embedding-3-small"
	}
	return &OpenAIEmbedder{
		apiKey:     apiKey,
		model:      model,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

type openaiEmbedRequest struct {
	Model string      `json:"model"`
	Input interface{} `json:"input"` // string or []string
}

type openaiEmbedResponse struct {
	Data  []openaiEmbedData `json:"data"`
	Error *openaiError      `json:"error,omitempty"`
}

type openaiEmbedData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

type openaiError struct {
	Message string `json:"message"`
}

// Embed returns embedding vector for a single text.
func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := e.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}
	return vecs[0], nil
}

// EmbedBatch returns embedding vectors for multiple texts in a single API call.
func (e *OpenAIEmbedder) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(openaiEmbedRequest{Model: e.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("openai embed: status=%d body=%s", resp.StatusCode, truncate(string(respBody), 200))
	}

	var result openaiEmbedResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("openai error: %s", result.Error.Message)
	}

	vecs := make([][]float32, len(result.Data))
	for _, d := range result.Data {
		if d.Index < len(vecs) {
			vecs[d.Index] = d.Embedding
		}
	}
	return vecs, nil
}

// IsAvailable checks if the API key is set and the API responds.
func (e *OpenAIEmbedder) IsAvailable(ctx context.Context) bool {
	if e.apiKey == "" {
		return false
	}
	_, err := e.Embed(ctx, "test")
	return err == nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
