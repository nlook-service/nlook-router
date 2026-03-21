package reasoning

import (
	"context"
	"fmt"
)

// CallFunc is the signature for a synchronous LLM call.
// Matches the pattern of SkillRunner.callOllama / callClaudeCLI / callOpenAI:
// returns (map with "text" key, logs, error).
type CallFunc func(ctx context.Context, model, system, prompt string, temp float64, maxTokens int) (text string, tokens int, err error)

// StreamFunc is the signature for a streaming LLM call.
type StreamFunc func(ctx context.Context, model, system, prompt string, temp float64, maxTokens int, onDelta func(string)) (text string, tokens int, err error)

// FuncCaller adapts function references to the LLMCaller interface.
// This allows the reasoning package to reuse existing LLM call functions
// without depending on concrete SkillRunner or LLM Engine types.
type FuncCaller struct {
	callFn   CallFunc
	streamFn StreamFunc
}

// NewFuncCaller creates an LLMCaller from call/stream functions.
func NewFuncCaller(callFn CallFunc, streamFn StreamFunc) *FuncCaller {
	return &FuncCaller{
		callFn:   callFn,
		streamFn: streamFn,
	}
}

func (c *FuncCaller) Call(ctx context.Context, model, systemPrompt, userPrompt string, temp float64, maxTokens int) (string, int, error) {
	if c.callFn == nil {
		return "", 0, fmt.Errorf("reasoning: call function not configured")
	}
	return c.callFn(ctx, model, systemPrompt, userPrompt, temp, maxTokens)
}

func (c *FuncCaller) CallStream(ctx context.Context, model, systemPrompt, userPrompt string, temp float64, maxTokens int, onDelta func(string)) (string, int, error) {
	if c.streamFn != nil {
		return c.streamFn(ctx, model, systemPrompt, userPrompt, temp, maxTokens, onDelta)
	}
	// Fallback: use non-streaming call if no stream function provided
	text, tokens, err := c.Call(ctx, model, systemPrompt, userPrompt, temp, maxTokens)
	if err != nil {
		return "", 0, err
	}
	if onDelta != nil {
		onDelta(text)
	}
	return text, tokens, nil
}
