package reasoning

import (
	"context"
	"fmt"
	"time"
)

// DeepSeekReasoner handles DeepSeek R1 models that natively produce <think> tags.
type DeepSeekReasoner struct {
	caller LLMCaller
}

func NewDeepSeekReasoner(caller LLMCaller) *DeepSeekReasoner {
	return &DeepSeekReasoner{caller: caller}
}

func (r *DeepSeekReasoner) SupportsModel(model string) bool {
	return DetectProvider(model) == ProviderDeepSeek
}

func (r *DeepSeekReasoner) Reason(ctx context.Context, systemPrompt, userInput string, cfg Config) (*Result, error) {
	start := time.Now()

	// DeepSeek R1 generates <think> tags natively without prompt augmentation.
	raw, tokens, err := r.caller.Call(ctx, cfg.Model, systemPrompt, userInput, cfg.Temperature, cfg.MaxTokens)
	if err != nil {
		return nil, fmt.Errorf("deepseek reasoning: %w", err)
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

func (r *DeepSeekReasoner) Stream(ctx context.Context, systemPrompt, userInput string, cfg Config) (<-chan Event, error) {
	ch := make(chan Event, 64)

	go func() {
		defer close(ch)
		ch <- Event{Type: EventStarted}
		start := time.Now()

		var accumulated string
		_, tokens, err := r.caller.CallStream(ctx, cfg.Model, systemPrompt, userInput, cfg.Temperature, cfg.MaxTokens, func(delta string) {
			accumulated += delta
			if inThinkingBlock(accumulated) {
				ch <- Event{Type: EventThinkingDelta, Content: delta}
			} else {
				ch <- Event{Type: EventContentDelta, Content: delta}
			}
		})
		if err != nil {
			ch <- Event{Type: EventError, Error: fmt.Errorf("deepseek reasoning stream: %w", err)}
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
