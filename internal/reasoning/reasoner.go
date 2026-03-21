package reasoning

import "context"

// Reasoner performs reasoning on a given input.
type Reasoner interface {
	Reason(ctx context.Context, systemPrompt, userInput string, cfg Config) (*Result, error)
	Stream(ctx context.Context, systemPrompt, userInput string, cfg Config) (<-chan Event, error)
	SupportsModel(model string) bool
}

// LLMCaller abstracts the actual LLM invocation, reusing existing infrastructure.
type LLMCaller interface {
	Call(ctx context.Context, model, systemPrompt, userPrompt string, temp float64, maxTokens int) (text string, tokens int, err error)
	CallStream(ctx context.Context, model, systemPrompt, userPrompt string, temp float64, maxTokens int, onDelta func(string)) (text string, tokens int, err error)
}
