package reasoning

import (
	"context"
	"fmt"
	"testing"
)

// mockCaller implements LLMCaller for testing.
type mockCaller struct {
	response string
	tokens   int
}

func (m *mockCaller) Call(_ context.Context, _, _, _ string, _ float64, _ int) (string, int, error) {
	return m.response, m.tokens, nil
}

func (m *mockCaller) CallStream(_ context.Context, _, _, _ string, _ float64, _ int, onDelta func(string)) (string, int, error) {
	if onDelta != nil {
		onDelta(m.response)
	}
	return m.response, m.tokens, nil
}

func TestExtractThinkTag(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		thinking  string
		answer    string
	}{
		{"no tag", "Hello world", "", "Hello world"},
		{"with tag", "<think>step 1\n\nstep 2</think>The answer", "step 1\n\nstep 2", "The answer"},
		{"unclosed tag", "<think>thinking...", "thinking...", ""},
		{"empty think", "<think></think>Answer", "", "Answer"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			thinking, answer := extractThinkTag(tt.input)
			if thinking != tt.thinking {
				t.Errorf("thinking: got %q, want %q", thinking, tt.thinking)
			}
			if answer != tt.answer {
				t.Errorf("answer: got %q, want %q", answer, tt.answer)
			}
		})
	}
}

func TestThinkingToSteps(t *testing.T) {
	steps := thinkingToSteps("first block\n\nsecond block\n\nthird block")
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	if steps[0].NextAction != ActionContinue {
		t.Errorf("step 0: expected continue, got %s", steps[0].NextAction)
	}
	if steps[2].NextAction != ActionFinalAnswer {
		t.Errorf("step 2: expected final_answer, got %s", steps[2].NextAction)
	}
}

func TestInThinkingBlock(t *testing.T) {
	if !inThinkingBlock("Hello <think>thinking") {
		t.Error("should be in thinking block")
	}
	if inThinkingBlock("Hello <think>thinking</think>answer") {
		t.Error("should NOT be in thinking block")
	}
}

func TestDetectProvider(t *testing.T) {
	tests := []struct {
		model    string
		expected ProviderType
	}{
		{"gemma3:12b", ProviderGemma},
		{"gemma3:27b", ProviderGemma},
		{"claude-sonnet-4-20250514", ProviderClaude},
		{"anthropic/claude", ProviderClaude},
		{"deepseek-r1:7b", ProviderDeepSeek},
		{"gpt-4", ProviderDefault},
		{"qwen2:7b", ProviderDefault},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := DetectProvider(tt.model)
			if got != tt.expected {
				t.Errorf("DetectProvider(%q) = %s, want %s", tt.model, got, tt.expected)
			}
		})
	}
}

func TestGemmaReasoner(t *testing.T) {
	caller := &mockCaller{
		response: "<think>Let me analyze this\n\nThe answer is clear</think>42 is the answer.",
		tokens:   100,
	}
	r := NewGemmaReasoner(caller)

	result, err := r.Reason(context.Background(), "system", "What is 42?", Config{Model: "gemma3:12b", MaxSteps: 5, MaxTokens: 4096})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Answer != "42 is the answer." {
		t.Errorf("answer: got %q, want %q", result.Answer, "42 is the answer.")
	}
	if len(result.Steps) != 2 {
		t.Errorf("steps: got %d, want 2", len(result.Steps))
	}
	if result.ThinkingText == "" {
		t.Error("thinking_text should not be empty")
	}
}

func TestGemmaReasonerNoThinkTag(t *testing.T) {
	caller := &mockCaller{response: "Just a direct answer", tokens: 50}
	r := NewGemmaReasoner(caller)

	result, err := r.Reason(context.Background(), "", "Hello", Config{Model: "gemma3:4b", MaxSteps: 5, MaxTokens: 4096})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Answer != "Just a direct answer" {
		t.Errorf("answer: got %q", result.Answer)
	}
	if len(result.Steps) != 0 {
		t.Errorf("should have 0 steps for non-thinking response")
	}
}

func TestDefaultReasonerCoT(t *testing.T) {
	callCount := 0
	caller := &mockCaller{}
	// Override with a sequencing mock
	seqCaller := &sequenceMockCaller{
		responses: []string{
			`{"title":"Analyze","action":"analyzing","result":"found X","reasoning":"because Y","next_action":"continue","confidence":0.8}`,
			`{"title":"Conclude","action":"concluding","result":"final answer","reasoning":"verified","next_action":"final_answer","confidence":0.95}`,
		},
	}
	_ = caller

	r := NewDefaultReasoner(seqCaller)
	result, err := r.Reason(context.Background(), "system", "test question", Config{Model: "gpt-4", MaxSteps: 5, MaxTokens: 4096, Temperature: 0.7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = callCount
	if len(result.Steps) != 2 {
		t.Errorf("steps: got %d, want 2", len(result.Steps))
	}
	if result.Answer != "final answer" {
		t.Errorf("answer: got %q, want 'final answer'", result.Answer)
	}
	if result.Steps[1].Confidence != 0.95 {
		t.Errorf("confidence: got %f, want 0.95", result.Steps[1].Confidence)
	}
}

func TestReasoningData(t *testing.T) {
	result := &Result{
		Answer:       "test",
		Steps:        []Step{{Confidence: 0.8}, {Confidence: 0.9}},
		ThinkingText: "raw thinking",
		Success:      true,
		TotalMs:      100,
		TokensUsed:   50,
	}

	data := result.ToReasoningData("gemma", "gemma3:12b")
	if !data.Enabled {
		t.Error("should be enabled")
	}
	if data.StepCount != 2 {
		t.Errorf("step_count: got %d, want 2", data.StepCount)
	}
	if data.AvgConfidence < 0.849 || data.AvgConfidence > 0.851 {
		t.Errorf("avg_confidence: got %f, want ~0.85", data.AvgConfidence)
	}
}

func TestDisabledReasoningData(t *testing.T) {
	data := DisabledReasoningData()
	if data.Enabled {
		t.Error("should be disabled")
	}
}

func TestNilResultToReasoningData(t *testing.T) {
	var result *Result
	data := result.ToReasoningData("test", "test")
	if data.Enabled {
		t.Error("nil result should produce disabled data")
	}
}

func TestManagerRouting(t *testing.T) {
	caller := &mockCaller{
		response: "<think>thinking</think>answer",
		tokens:   100,
	}
	mgr := NewManager(caller)

	// Gemma → GemmaReasoner
	result, err := mgr.Reason(context.Background(), "gemma3:12b", "sys", "input", DefaultConfig())
	if err != nil {
		t.Fatalf("gemma: %v", err)
	}
	if result.Answer != "answer" {
		t.Errorf("gemma answer: got %q", result.Answer)
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{"key":"val"}`, `{"key":"val"}`},
		{"```json\n{\"key\":\"val\"}\n```", `{"key":"val"}`},
		{"Some text {\"a\":1} more text", `{"a":1}`},
		{"no json here", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input[:min(len(tt.input), 20)], func(t *testing.T) {
			got := extractJSON(tt.input)
			if got != tt.expected {
				t.Errorf("extractJSON: got %q, want %q", got, tt.expected)
			}
		})
	}
}

// sequenceMockCaller returns different responses for each call.
type sequenceMockCaller struct {
	responses []string
	idx       int
}

func (m *sequenceMockCaller) Call(_ context.Context, _, _, _ string, _ float64, _ int) (string, int, error) {
	if m.idx >= len(m.responses) {
		return "", 0, fmt.Errorf("no more mock responses")
	}
	resp := m.responses[m.idx]
	m.idx++
	return resp, 0, nil
}

func (m *sequenceMockCaller) CallStream(_ context.Context, _, _, _ string, _ float64, _ int, onDelta func(string)) (string, int, error) {
	text, tokens, err := m.Call(nil, "", "", "", 0, 0)
	if err != nil {
		return "", 0, err
	}
	if onDelta != nil {
		onDelta(text)
	}
	return text, tokens, nil
}
