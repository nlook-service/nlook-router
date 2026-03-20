package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/nlook-service/nlook-router/internal/llm"
)

// AccuracyEvaluator uses an LLM to score how well actual output matches expected output.
type AccuracyEvaluator struct {
	engine *llm.Engine
	model  string
}

// ScoreResult holds the evaluator's score and reasoning.
type ScoreResult struct {
	Score  int    `json:"score"`
	Reason string `json:"reason"`
}

// NewAccuracyEvaluator creates an evaluator that uses the given model.
func NewAccuracyEvaluator(engine *llm.Engine, model string) *AccuracyEvaluator {
	return &AccuracyEvaluator{engine: engine, model: model}
}

// chatResponse is the OpenAI-compatible response structure.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Score evaluates accuracy of actual output against expected output for a given input.
func (e *AccuracyEvaluator) Score(ctx context.Context, input, expected, actual string) (*ScoreResult, error) {
	userPrompt := buildAccuracyUserPrompt(input, expected, actual)

	resp, err := e.engine.ChatCompletion(ctx, e.model, accuracySystemPrompt, []map[string]string{
		{"role": "user", "content": userPrompt},
	}, nil, false)
	if err != nil {
		return nil, fmt.Errorf("evaluator llm call: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read evaluator response: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("evaluator API error: status=%d body=%s", resp.StatusCode, truncate(string(body), 300))
	}

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("parse evaluator API response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return nil, fmt.Errorf("evaluator returned no choices")
	}

	content := strings.TrimSpace(cr.Choices[0].Message.Content)
	// Strip markdown code fences if present
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var result ScoreResult
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("parse evaluator response %q: %w", truncate(content, 200), err)
	}
	if result.Score < 1 || result.Score > 10 {
		return nil, fmt.Errorf("invalid score %d: must be 1-10", result.Score)
	}
	return &result, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
