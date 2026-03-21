package reasoning

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const cotSystemPrompt = `You are a reasoning engine. Solve problems step by step.

For each step, respond ONLY with a JSON object (no markdown, no extra text):
{
  "title": "concise step title",
  "action": "what you are doing in this step",
  "result": "outcome of this step",
  "reasoning": "your thought process",
  "next_action": "continue",
  "confidence": 0.85
}

Rules:
- next_action must be one of: "continue", "validate", "final_answer", "reset"
- confidence is a float from 0.0 to 1.0
- Set next_action to "final_answer" when you have a complete, validated answer
- When next_action is "final_answer", put your final answer in "result"`

// DefaultReasoner implements a generic Chain-of-Thought reasoning loop
// that works with any LLM model. Based on the agno default.py pattern.
type DefaultReasoner struct {
	caller LLMCaller
}

func NewDefaultReasoner(caller LLMCaller) *DefaultReasoner {
	return &DefaultReasoner{caller: caller}
}

func (r *DefaultReasoner) SupportsModel(_ string) bool {
	return true
}

func (r *DefaultReasoner) Reason(ctx context.Context, systemPrompt, userInput string, cfg Config) (*Result, error) {
	start := time.Now()
	var allSteps []Step
	var lastResult string

	for i := 0; i < cfg.MaxSteps; i++ {
		stepStart := time.Now()

		cotPrompt := buildCoTPrompt(systemPrompt, userInput, allSteps, i, cfg.MaxSteps)
		raw, _, err := r.caller.Call(ctx, cfg.Model, cotSystemPrompt, cotPrompt, cfg.Temperature, cfg.MaxTokens)
		if err != nil {
			return nil, fmt.Errorf("default reasoning step %d: %w", i+1, err)
		}

		step, parseErr := parseStepResponse(raw)
		if parseErr != nil {
			// JSON parse failed → graceful fallback: treat entire output as answer
			return &Result{
				Answer:  raw,
				Steps:   allSteps,
				Success: true,
				TotalMs: time.Since(start).Milliseconds(),
			}, nil
		}

		step.DurationMs = time.Since(stepStart).Milliseconds()
		allSteps = append(allSteps, *step)
		lastResult = step.Result

		if step.NextAction == ActionFinalAnswer {
			break
		}
		if step.NextAction == ActionReset {
			allSteps = nil
			continue
		}
	}

	return &Result{
		Answer:  lastResult,
		Steps:   allSteps,
		Success: true,
		TotalMs: time.Since(start).Milliseconds(),
	}, nil
}

func (r *DefaultReasoner) Stream(ctx context.Context, systemPrompt, userInput string, cfg Config) (<-chan Event, error) {
	ch := make(chan Event, 64)

	go func() {
		defer close(ch)
		ch <- Event{Type: EventStarted}

		result, err := r.Reason(ctx, systemPrompt, userInput, cfg)
		if err != nil {
			ch <- Event{Type: EventError, Error: err}
			return
		}

		for i := range result.Steps {
			ch <- Event{Type: EventStepComplete, Step: &result.Steps[i]}
		}

		if result.Answer != "" {
			ch <- Event{Type: EventContentDelta, Content: result.Answer}
		}

		ch <- Event{Type: EventCompleted, Result: result}
	}()

	return ch, nil
}

func buildCoTPrompt(systemPrompt, userInput string, prevSteps []Step, stepNum, maxSteps int) string {
	var b strings.Builder

	if systemPrompt != "" {
		b.WriteString("Context:\n")
		b.WriteString(systemPrompt)
		b.WriteString("\n\n")
	}

	b.WriteString("User Question:\n")
	b.WriteString(userInput)
	b.WriteString("\n\n")

	if len(prevSteps) > 0 {
		b.WriteString("Previous reasoning steps:\n")
		for i, s := range prevSteps {
			b.WriteString(fmt.Sprintf("%d. [%s] %s → %s\n", i+1, s.Title, s.Action, s.Result))
		}
		b.WriteString("\n")
	}

	b.WriteString(fmt.Sprintf("Now produce step %d of max %d. Respond with JSON only.", stepNum+1, maxSteps))
	return b.String()
}

func parseStepResponse(raw string) (*Step, error) {
	raw = strings.TrimSpace(raw)
	var step Step
	if err := json.Unmarshal([]byte(raw), &step); err != nil {
		extracted := extractJSON(raw)
		if extracted == "" {
			return nil, fmt.Errorf("no valid step JSON: %w", err)
		}
		if err2 := json.Unmarshal([]byte(extracted), &step); err2 != nil {
			return nil, fmt.Errorf("parse extracted JSON: %w", err2)
		}
	}
	return &step, nil
}

// extractJSON tries to find a JSON object in text, possibly wrapped in markdown code blocks.
func extractJSON(text string) string {
	// Try ```json ... ``` block
	if idx := strings.Index(text, "```json"); idx >= 0 {
		start := idx + 7
		if end := strings.Index(text[start:], "```"); end >= 0 {
			return strings.TrimSpace(text[start : start+end])
		}
	}
	// Try ``` ... ``` block
	if idx := strings.Index(text, "```"); idx >= 0 {
		start := idx + 3
		if end := strings.Index(text[start:], "```"); end >= 0 {
			candidate := strings.TrimSpace(text[start : start+end])
			if strings.HasPrefix(candidate, "{") {
				return candidate
			}
		}
	}
	// Try raw { ... }
	if start := strings.Index(text, "{"); start >= 0 {
		if end := strings.LastIndex(text, "}"); end > start {
			return text[start : end+1]
		}
	}
	return ""
}
