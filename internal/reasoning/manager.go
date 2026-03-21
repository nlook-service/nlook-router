package reasoning

import (
	"context"
	"fmt"
)

// Manager orchestrates reasoning across providers.
type Manager struct {
	caller     LLMCaller
	natives    map[ProviderType]Reasoner
	defaultCoT *DefaultReasoner
}

// NewManager creates a reasoning manager with all providers registered.
func NewManager(caller LLMCaller) *Manager {
	m := &Manager{
		caller:  caller,
		natives: make(map[ProviderType]Reasoner),
	}

	m.natives[ProviderGemma] = NewGemmaReasoner(caller)
	m.natives[ProviderClaude] = NewClaudeReasoner(caller)
	m.natives[ProviderDeepSeek] = NewDeepSeekReasoner(caller)
	m.defaultCoT = NewDefaultReasoner(caller)

	return m
}

// Reason executes reasoning with automatic provider detection.
func (m *Manager) Reason(ctx context.Context, model, systemPrompt, userInput string, cfg Config) (*Result, error) {
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}
	cfg.Model = model

	provider := DetectProvider(model)
	if r, ok := m.natives[provider]; ok {
		result, err := r.Reason(ctx, systemPrompt, userInput, cfg)
		if err != nil {
			return nil, fmt.Errorf("reasoning (%s): %w", provider, err)
		}
		return result, nil
	}
	return m.defaultCoT.Reason(ctx, systemPrompt, userInput, cfg)
}

// Stream executes reasoning with streaming events.
func (m *Manager) Stream(ctx context.Context, model, systemPrompt, userInput string, cfg Config) (<-chan Event, error) {
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}
	cfg.Model = model

	provider := DetectProvider(model)
	if r, ok := m.natives[provider]; ok {
		return r.Stream(ctx, systemPrompt, userInput, cfg)
	}
	return m.defaultCoT.Stream(ctx, systemPrompt, userInput, cfg)
}

// ReasonWithData is a convenience method that returns both the answer and structured ReasoningData.
func (m *Manager) ReasonWithData(ctx context.Context, model, systemPrompt, userInput string, cfg Config) (string, *ReasoningData, error) {
	result, err := m.Reason(ctx, model, systemPrompt, userInput, cfg)
	if err != nil {
		return "", nil, err
	}
	provider := string(DetectProvider(model))
	return result.Answer, result.ToReasoningData(provider, model), nil
}
