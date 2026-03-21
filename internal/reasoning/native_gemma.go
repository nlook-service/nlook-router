package reasoning

import (
	"context"
	"fmt"
	"time"
)

const gemmaThinkingInstruction = "\n\nWhen solving complex problems, think step by step inside <think>...</think> tags before giving your final answer."

// GemmaReasoner handles Gemma 3 models that support <think> tag reasoning.
type GemmaReasoner struct {
	caller LLMCaller
}

// NewGemmaReasoner creates a reasoner for Gemma models.
func NewGemmaReasoner(caller LLMCaller) *GemmaReasoner {
	return &GemmaReasoner{caller: caller}
}

func (r *GemmaReasoner) SupportsModel(model string) bool {
	return DetectProvider(model) == ProviderGemma
}

func (r *GemmaReasoner) Reason(ctx context.Context, systemPrompt, userInput string, cfg Config) (*Result, error) {
	start := time.Now()

	enhancedSystem := systemPrompt + gemmaThinkingInstruction
	raw, tokens, err := r.caller.Call(ctx, cfg.Model, enhancedSystem, userInput, cfg.Temperature, cfg.MaxTokens)
	if err != nil {
		return nil, fmt.Errorf("gemma reasoning: %w", err)
	}

	thinking, answer := extractThinkTag(raw)
	steps := thinkingToSteps(thinking)
	if answer == "" && thinking == "" {
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

func (r *GemmaReasoner) Stream(ctx context.Context, systemPrompt, userInput string, cfg Config) (<-chan Event, error) {
	ch := make(chan Event, 64)

	go func() {
		defer close(ch)
		ch <- Event{Type: EventStarted}
		start := time.Now()

		enhancedSystem := systemPrompt + gemmaThinkingInstruction
		var accumulated string

		_, tokens, err := r.caller.CallStream(ctx, cfg.Model, enhancedSystem, userInput, cfg.Temperature, cfg.MaxTokens, func(delta string) {
			accumulated += delta
			if inThinkingBlock(accumulated) {
				ch <- Event{Type: EventThinkingDelta, Content: delta}
			} else {
				ch <- Event{Type: EventContentDelta, Content: delta}
			}
		})
		if err != nil {
			ch <- Event{Type: EventError, Error: fmt.Errorf("gemma reasoning stream: %w", err)}
			return
		}

		thinking, answer := extractThinkTag(accumulated)
		steps := thinkingToSteps(thinking)
		if answer == "" && thinking == "" {
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
