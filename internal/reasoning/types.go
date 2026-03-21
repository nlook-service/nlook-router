package reasoning

import (
	"strings"
	"time"
)

// NextAction controls the reasoning flow between steps.
type NextAction string

const (
	ActionContinue    NextAction = "continue"
	ActionValidate    NextAction = "validate"
	ActionFinalAnswer NextAction = "final_answer"
	ActionReset       NextAction = "reset"
)

// Step represents a single reasoning step.
type Step struct {
	Title      string     `json:"title"`
	Action     string     `json:"action,omitempty"`
	Result     string     `json:"result,omitempty"`
	Reasoning  string     `json:"reasoning"`
	NextAction NextAction `json:"next_action"`
	Confidence float64    `json:"confidence"`
	DurationMs int64      `json:"duration_ms,omitempty"`
}

// Result is the final output of a reasoning process.
type Result struct {
	Answer       string `json:"answer"`
	Steps        []Step `json:"steps"`
	ThinkingText string `json:"thinking_text,omitempty"`
	Success      bool   `json:"success"`
	TotalMs      int64  `json:"total_ms"`
	TokensUsed   int    `json:"tokens_used"`
}

// ReasoningData is the structured reasoning payload included in all responses.
type ReasoningData struct {
	Enabled       bool    `json:"enabled"`
	Provider      string  `json:"provider"`
	Model         string  `json:"model"`
	Steps         []Step  `json:"steps,omitempty"`
	ThinkingText  string  `json:"thinking_text,omitempty"`
	TotalMs       int64   `json:"total_ms"`
	TokensUsed    int     `json:"tokens_used"`
	StepCount     int     `json:"step_count"`
	AvgConfidence float64 `json:"avg_confidence,omitempty"`
}

// ToReasoningData converts a Result to a structured ReasoningData payload.
func (r *Result) ToReasoningData(provider, model string) *ReasoningData {
	if r == nil {
		return &ReasoningData{Enabled: false}
	}
	var avgConf float64
	if len(r.Steps) > 0 {
		var sum float64
		for _, s := range r.Steps {
			sum += s.Confidence
		}
		avgConf = sum / float64(len(r.Steps))
	}
	return &ReasoningData{
		Enabled:       true,
		Provider:      provider,
		Model:         model,
		Steps:         r.Steps,
		ThinkingText:  r.ThinkingText,
		TotalMs:       r.TotalMs,
		TokensUsed:    r.TokensUsed,
		StepCount:     len(r.Steps),
		AvgConfidence: avgConf,
	}
}

// DisabledReasoningData returns a ReasoningData indicating reasoning was not used.
func DisabledReasoningData() *ReasoningData {
	return &ReasoningData{Enabled: false}
}

// Config configures a reasoning session.
type Config struct {
	Model       string
	MinSteps    int
	MaxSteps    int
	Temperature float64
	MaxTokens   int
	Timeout     time.Duration
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		MinSteps:    1,
		MaxSteps:    5,
		Temperature: 0.7,
		MaxTokens:   4096,
		Timeout:     120 * time.Second,
	}
}

// EventType categorizes reasoning events for streaming.
type EventType string

const (
	EventStarted       EventType = "started"
	EventThinkingDelta EventType = "thinking_delta"
	EventStepComplete  EventType = "step_complete"
	EventContentDelta  EventType = "content_delta"
	EventCompleted     EventType = "completed"
	EventError         EventType = "error"
)

// Event is emitted during reasoning for streaming.
type Event struct {
	Type    EventType
	Step    *Step
	Content string
	Result  *Result
	Error   error
}

// ProviderType identifies a native reasoning provider.
type ProviderType string

const (
	ProviderGemma    ProviderType = "gemma"
	ProviderClaude   ProviderType = "claude"
	ProviderDeepSeek ProviderType = "deepseek"
	ProviderDefault  ProviderType = "default"
)

// DetectProvider determines the native reasoning provider from model name.
func DetectProvider(model string) ProviderType {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "gemma"):
		return ProviderGemma
	case strings.HasPrefix(m, "claude") || strings.HasPrefix(m, "anthropic"):
		return ProviderClaude
	case strings.Contains(m, "deepseek"):
		return ProviderDeepSeek
	default:
		return ProviderDefault
	}
}
