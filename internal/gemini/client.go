package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const baseURL = "https://generativelanguage.googleapis.com/v1beta/models"

// Client calls the Gemini REST API.
type Client struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

// NewClient creates a Gemini client. Returns nil if no API key is available.
func NewClient() *Client {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		return nil
	}
	model := os.Getenv("NLOOK_CLOUD_MODEL")
	if model == "" {
		model = "gemini-2.0-flash"
	}
	return &Client{
		apiKey:     key,
		model:      model,
		httpClient: &http.Client{Timeout: 2 * time.Minute},
	}
}

// Model returns the configured model name.
func (c *Client) Model() string { return c.model }

type message struct {
	Role  string `json:"role"`
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text"`
}

type request struct {
	Contents         []message        `json:"contents"`
	SystemInstruction *message        `json:"systemInstruction,omitempty"`
	GenerationConfig generationConfig `json:"generationConfig"`
}

type generationConfig struct {
	MaxOutputTokens int     `json:"maxOutputTokens"`
	Temperature     float64 `json:"temperature"`
}

// ChatStream sends a streaming request and calls onDelta for each token.
// Returns full text, input tokens, output tokens, error.
func (c *Client) ChatStream(ctx context.Context, system string, messages []map[string]string, onDelta func(string)) (string, int, int, error) {
	contents := make([]message, 0, len(messages))
	for _, m := range messages {
		role := m["role"]
		if role == "assistant" {
			role = "model"
		}
		contents = append(contents, message{
			Role:  role,
			Parts: []part{{Text: m["content"]}},
		})
	}

	req := request{
		Contents: contents,
		GenerationConfig: generationConfig{
			MaxOutputTokens: 4096,
			Temperature:     0.7,
		},
	}
	if system != "" {
		req.SystemInstruction = &message{
			Role:  "user",
			Parts: []part{{Text: system}},
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", 0, 0, fmt.Errorf("marshal gemini request: %w", err)
	}

	url := fmt.Sprintf("%s/%s:streamGenerateContent?alt=sse&key=%s", baseURL, c.model, c.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return "", 0, 0, fmt.Errorf("create gemini request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return "", 0, 0, fmt.Errorf("gemini API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", 0, 0, fmt.Errorf("gemini API error: status=%d body=%s", resp.StatusCode, truncate(string(respBody), 300))
	}

	// Parse SSE stream
	var fullText strings.Builder
	var lastInputTokens, lastOutputTokens int
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var chunk struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
			UsageMetadata struct {
				PromptTokenCount     int `json:"promptTokenCount"`
				CandidatesTokenCount int `json:"candidatesTokenCount"`
			} `json:"usageMetadata"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.UsageMetadata.PromptTokenCount > 0 {
			lastInputTokens = chunk.UsageMetadata.PromptTokenCount
			lastOutputTokens = chunk.UsageMetadata.CandidatesTokenCount
		}
		for _, c := range chunk.Candidates {
			for _, p := range c.Content.Parts {
				if p.Text != "" {
					fullText.WriteString(p.Text)
					if onDelta != nil {
						onDelta(p.Text)
					}
				}
			}
		}
	}

	return fullText.String(), lastInputTokens, lastOutputTokens, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
