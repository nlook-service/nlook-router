package compression

import (
	"context"
	"fmt"
	"time"

	"github.com/nlook-service/nlook-router/internal/ollama"
	"github.com/nlook-service/nlook-router/internal/tokenizer"
)

const compressionPrompt = `You are a data compression assistant. Extract ONLY the essential information from the tool result below.

RULES:
- KEEP: numbers, statistics, dates, names, IDs, URLs, status values, error messages
- KEEP: the first/main item in full detail
- REMOVE: formatting (markdown, HTML, JSON structure), boilerplate, repeated patterns
- REMOVE: introductions, conclusions, meta-commentary
- OUTPUT: plain text, concise bullet points, no formatting

Tool result:
%s

Compressed (essential facts only):`

// llmCompressor uses a local LLM to intelligently compress tool results.
type llmCompressor struct {
	client *ollama.Client
	model  string
}

func (l *llmCompressor) Compress(ctx context.Context, text string, maxTokens int) Result {
	original := tokenizer.EstimateTokens(text)

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	model := l.model
	if model == "" {
		return Result{Text: text, Original: original, Compressed: original, Method: "llm-error"}
	}

	prompt := fmt.Sprintf(compressionPrompt, text)
	result, _, _, err := l.client.ChatStream(ctx, model, "", prompt,
		ollama.ChatOptions{Temperature: 0.0, MaxTokens: maxTokens}, nil)
	if err != nil {
		return Result{Text: text, Original: original, Compressed: original, Method: "llm-error"}
	}

	compressed := tokenizer.EstimateTokens(result)
	return Result{Text: result, Original: original, Compressed: compressed, Method: "llm"}
}
