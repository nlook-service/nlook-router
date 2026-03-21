package reasoning

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ClaudeReasoner handles Claude models with extended thinking.
type ClaudeReasoner struct {
	caller LLMCaller
}

func NewClaudeReasoner(caller LLMCaller) *ClaudeReasoner {
	return &ClaudeReasoner{caller: caller}
}

func (r *ClaudeReasoner) SupportsModel(model string) bool {
	return DetectProvider(model) == ProviderClaude
}

func (r *ClaudeReasoner) Reason(ctx context.Context, systemPrompt, userInput string, cfg Config) (*Result, error) {
	start := time.Now()

	raw, tokens, err := r.caller.Call(ctx, cfg.Model, systemPrompt, userInput, cfg.Temperature, cfg.MaxTokens)
	if err != nil {
		return nil, fmt.Errorf("claude reasoning: %w", err)
	}

	thinking, answer := extractClaudeThinking(raw)
	steps := thinkingToSteps(thinking)
	if answer == "" {
		answer = raw
	}

	return &Result{
		Answer:       answer,
		Steps:        steps,
		ThinkingText: thinking,
		Success:      true,
		TotalMs:      time.Since(start).Milliseconds(),
		TokensUsed:   tokens,
	}, nil
}

func (r *ClaudeReasoner) Stream(ctx context.Context, systemPrompt, userInput string, cfg Config) (<-chan Event, error) {
	ch := make(chan Event, 64)

	go func() {
		defer close(ch)
		ch <- Event{Type: EventStarted}
		start := time.Now()

		var accumulated string
		_, tokens, err := r.caller.CallStream(ctx, cfg.Model, systemPrompt, userInput, cfg.Temperature, cfg.MaxTokens, func(delta string) {
			accumulated += delta
			ch <- Event{Type: EventContentDelta, Content: delta}
		})
		if err != nil {
			ch <- Event{Type: EventError, Error: fmt.Errorf("claude reasoning stream: %w", err)}
			return
		}

		thinking, answer := extractClaudeThinking(accumulated)
		steps := thinkingToSteps(thinking)
		if answer == "" {
			answer = accumulated
		}

		for i := range steps {
			ch <- Event{Type: EventStepComplete, Step: &steps[i]}
		}

		ch <- Event{
			Type: EventCompleted,
			Result: &Result{
				Answer:       answer,
				Steps:        steps,
				ThinkingText: thinking,
				Success:      true,
				TotalMs:      time.Since(start).Milliseconds(),
				TokensUsed:   tokens,
			},
		}
	}()

	return ch, nil
}

// extractClaudeThinking handles both <thinking> and <think> tags from Claude responses.
func extractClaudeThinking(raw string) (string, string) {
	// Normalize <thinking> to <think> for shared extraction
	normalized := strings.ReplaceAll(raw, "<thinking>", "<think>")
	normalized = strings.ReplaceAll(normalized, "</thinking>", "</think>")
	thinking, answer := extractThinkTag(normalized)
	if thinking != "" {
		return thinking, answer
	}
	return "", raw
}
