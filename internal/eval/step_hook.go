package eval

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// StepCompleteData holds step metadata passed from the engine layer.
// Defined here (not in engine) to avoid import cycles.
type StepCompleteData struct {
	NodeID   string
	NodeType string
	Order    int
	Input    map[string]interface{}
	Output   map[string]interface{}
	Status   string
	Duration time.Duration
}

// StepCapture records a single step's input/output during workflow evaluation.
type StepCapture struct {
	NodeID   string
	NodeType string
	Order    int
	Input    map[string]interface{}
	Output   map[string]interface{}
	Status   string
	Duration time.Duration
}

// StepEvalHook captures workflow step outputs and optionally evaluates them
// against per-step expected outputs. It is called by engine.StepExecutor
// via an adapter that converts engine.StepEvent → eval.StepCompleteData.
type StepEvalHook struct {
	mu       sync.Mutex
	captures []StepCapture

	// Per-step evaluation (optional). Key: NodeID → expected output.
	expectations map[string]*EvalCase
	evaluator    *AccuracyEvaluator
	results      []*EvalResult
	evalRunID    string
}

// NewStepEvalHook creates a hook that captures step outputs.
// Pass expectations to also score each step; pass nil for capture-only mode.
func NewStepEvalHook(expectations map[string]*EvalCase, evaluator *AccuracyEvaluator, evalRunID string) *StepEvalHook {
	if expectations == nil {
		expectations = make(map[string]*EvalCase)
	}
	return &StepEvalHook{
		expectations: expectations,
		evaluator:    evaluator,
		evalRunID:    evalRunID,
	}
}

// HandleStepComplete processes a completed step. Called by the adapter in the integration layer.
func (h *StepEvalHook) HandleStepComplete(ctx context.Context, data *StepCompleteData) {
	h.mu.Lock()
	defer h.mu.Unlock()

	capture := StepCapture{
		NodeID:   data.NodeID,
		NodeType: data.NodeType,
		Order:    data.Order,
		Status:   data.Status,
		Input:    data.Input,
		Output:   data.Output,
		Duration: data.Duration,
	}
	h.captures = append(h.captures, capture)

	// If there's a per-step expectation, evaluate it.
	ec, ok := h.expectations[data.NodeID]
	if !ok || h.evaluator == nil {
		return
	}

	actualOutput := flattenOutput(data.Output)
	scoreResult, err := h.evaluator.Score(ctx, ec.Input, ec.ExpectedOutput, actualOutput)
	if err != nil {
		log.Printf("eval: step %s scoring failed: %v", data.NodeID, err)
		return
	}

	result := &EvalResult{
		ID:             uuid.New().String(),
		EvalRunID:      h.evalRunID,
		EvalCaseID:     ec.ID,
		Iteration:      1,
		ActualOutput:   actualOutput,
		AccuracyScore:  scoreResult.Score,
		AccuracyReason: scoreResult.Reason,
		LatencyMs:      data.Duration.Milliseconds(),
		CreatedAt:      time.Now(),
	}
	h.results = append(h.results, result)
}

// Captures returns all captured step data.
func (h *StepEvalHook) Captures() []StepCapture {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]StepCapture, len(h.captures))
	copy(out, h.captures)
	return out
}

// Results returns all per-step evaluation results.
func (h *StepEvalHook) Results() []*EvalResult {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*EvalResult, len(h.results))
	copy(out, h.results)
	return out
}

// flattenOutput converts a step output map to a string for evaluation.
func flattenOutput(output map[string]interface{}) string {
	if output == nil {
		return ""
	}
	// Try "text" field first (common for prompt skills)
	if text, ok := output["text"].(string); ok {
		return text
	}
	// Try "content" field
	if content, ok := output["content"].(string); ok {
		return content
	}
	// Fallback to JSON
	b, _ := json.Marshal(output)
	return string(b)
}
