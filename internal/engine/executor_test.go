package engine

import (
	"context"
	"testing"

	"github.com/nlook-service/nlook-router/internal/apiclient"
)

// buildExecutor returns a StepExecutor wired with a mockClient.
func buildExecutor(mc *mockClient) *StepExecutor {
	return NewStepExecutor(mc, NewSkillRunner())
}

// ──────────────────────────────────────────────────────────────────────────────
// Passthrough node types
// ──────────────────────────────────────────────────────────────────────────────

func TestStepExecutor_passthrough_startNode(t *testing.T) {
	mc := newMockClient()
	exec := buildExecutor(mc)
	rctx := NewRunContext(1, 1, 1, nil)

	node := &apiclient.WorkflowNode{NodeID: "start-1", NodeType: "start"}
	result, err := exec.Execute(context.Background(), rctx, 1, node, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status: got %q, want 'completed'", result.Status)
	}
}

func TestStepExecutor_passthrough_unknownType(t *testing.T) {
	mc := newMockClient()
	exec := buildExecutor(mc)
	rctx := NewRunContext(1, 1, 1, nil)

	node := &apiclient.WorkflowNode{NodeID: "n1", NodeType: "mystery_type"}
	result, err := exec.Execute(context.Background(), rctx, 1, node, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status: got %q, want 'completed'", result.Status)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Skill node — no skill attached (passthrough)
// ──────────────────────────────────────────────────────────────────────────────

func TestStepExecutor_skillNode_noSkillID(t *testing.T) {
	mc := newMockClient()
	exec := buildExecutor(mc)
	rctx := NewRunContext(1, 1, 1, nil)

	node := &apiclient.WorkflowNode{
		NodeID:   "step-1",
		NodeType: "step",
		RefID:    0,
		Data:     map[string]interface{}{},
	}

	result, err := exec.Execute(context.Background(), rctx, 1, node, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status: got %q, want 'completed'", result.Status)
	}
	if result.Output["message"] != "no skill attached, passthrough" {
		t.Errorf("unexpected output: %v", result.Output)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Skill node — skill not found in loaded skills
// ──────────────────────────────────────────────────────────────────────────────

func TestStepExecutor_skillNode_skillNotFound(t *testing.T) {
	mc := newMockClient()
	exec := buildExecutor(mc)
	// Load no skills
	exec.LoadSkillsAndAgents(nil, nil)

	rctx := NewRunContext(1, 1, 1, nil)

	node := &apiclient.WorkflowNode{
		NodeID:   "step-1",
		NodeType: "step",
		RefID:    99, // non-existent skill ID
	}

	result, err := exec.Execute(context.Background(), rctx, 1, node, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("status: got %q, want 'failed'", result.Status)
	}
	if result.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Skill node — skill found, tool type (no external call)
// ──────────────────────────────────────────────────────────────────────────────

func TestStepExecutor_skillNode_toolSkill(t *testing.T) {
	mc := newMockClient()
	exec := buildExecutor(mc)

	skill := apiclient.WorkflowSkill{
		ID:        42,
		Name:      "my-tool",
		SkillType: "tool",
		Config:    map[string]interface{}{"tool_name": "calculator"},
	}
	exec.LoadSkillsAndAgents([]apiclient.WorkflowSkill{skill}, nil)

	rctx := NewRunContext(1, 1, 1, nil)

	node := &apiclient.WorkflowNode{
		NodeID:   "step-1",
		NodeType: "step",
		RefID:    42,
	}

	result, err := exec.Execute(context.Background(), rctx, 1, node, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status: got %q, want 'completed'", result.Status)
	}
	if result.Output["tool"] != "calculator" {
		t.Errorf("output[tool]: got %v, want 'calculator'", result.Output["tool"])
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Skill node — skill_id from node.Data (float64 from JSON)
// ──────────────────────────────────────────────────────────────────────────────

func TestStepExecutor_skillNode_skillIDFromData(t *testing.T) {
	mc := newMockClient()
	exec := buildExecutor(mc)

	skill := apiclient.WorkflowSkill{
		ID:        7,
		Name:      "data-tool",
		SkillType: "tool",
		Config:    map[string]interface{}{"tool_name": "data-tool"},
	}
	exec.LoadSkillsAndAgents([]apiclient.WorkflowSkill{skill}, nil)

	rctx := NewRunContext(1, 1, 1, nil)

	node := &apiclient.WorkflowNode{
		NodeID:   "step-1",
		NodeType: "step",
		Data:     map[string]interface{}{"skill_id": float64(7)}, // JSON-decoded float
	}

	result, err := exec.Execute(context.Background(), rctx, 1, node, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status: got %q, want 'completed'", result.Status)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Agent node — no agent attached (passthrough)
// ──────────────────────────────────────────────────────────────────────────────

func TestStepExecutor_agentNode_noAgentID(t *testing.T) {
	mc := newMockClient()
	exec := buildExecutor(mc)
	rctx := NewRunContext(1, 1, 1, nil)

	node := &apiclient.WorkflowNode{
		NodeID:   "agent-1",
		NodeType: "agent",
		RefID:    0,
		Data:     map[string]interface{}{},
	}

	result, err := exec.Execute(context.Background(), rctx, 1, node, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status: got %q, want 'completed'", result.Status)
	}
	if result.Output["message"] != "no agent attached, passthrough" {
		t.Errorf("unexpected output: %v", result.Output)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Agent node — agent not found
// ──────────────────────────────────────────────────────────────────────────────

func TestStepExecutor_agentNode_agentNotFound(t *testing.T) {
	mc := newMockClient()
	exec := buildExecutor(mc)
	exec.LoadSkillsAndAgents(nil, nil)
	rctx := NewRunContext(1, 1, 1, nil)

	node := &apiclient.WorkflowNode{
		NodeID:   "agent-1",
		NodeType: "agent",
		RefID:    55,
	}

	result, err := exec.Execute(context.Background(), rctx, 1, node, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("status: got %q, want 'failed'", result.Status)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// StepExecutor reports to apiclient
// ──────────────────────────────────────────────────────────────────────────────

func TestStepExecutor_reportsStartAndComplete(t *testing.T) {
	mc := newMockClient()
	exec := buildExecutor(mc)
	rctx := NewRunContext(10, 20, 1, nil)

	node := &apiclient.WorkflowNode{NodeID: "step-x", NodeType: "start"}
	_, err := exec.Execute(context.Background(), rctx, 20, node, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if mc.startStepCount() != 1 {
		t.Errorf("StartStep should have been called once, got %d", mc.startStepCount())
	}
	if mc.completeStepCount() != 1 {
		t.Errorf("CompleteStep should have been called once, got %d", mc.completeStepCount())
	}
}

func TestStepExecutor_startStepError_continuesExecution(t *testing.T) {
	mc := newMockClient()
	mc.startStepFn = func(_, _ int64, _, _ string) (*apiclient.StepLogRef, error) {
		return nil, &testError{"simulated StartStep failure"}
	}

	exec := buildExecutor(mc)
	rctx := NewRunContext(1, 1, 1, nil)

	node := &apiclient.WorkflowNode{NodeID: "step-1", NodeType: "start"}
	result, err := exec.Execute(context.Background(), rctx, 1, node, 0)
	// Execution should still succeed even when StartStep fails
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status: got %q, want 'completed'", result.Status)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// buildInput
// ──────────────────────────────────────────────────────────────────────────────

func TestStepExecutor_buildInput_includesRunInput(t *testing.T) {
	mc := newMockClient()
	exec := buildExecutor(mc)
	rctx := NewRunContext(1, 1, 1, map[string]interface{}{"user_msg": "hi"})

	node := &apiclient.WorkflowNode{
		NodeID:   "step-1",
		NodeType: "step",
		Data:     map[string]interface{}{"extra": "data"},
	}

	input := exec.buildInput(rctx, node, nil)

	if _, ok := input["_run_input"]; !ok {
		t.Error("expected '_run_input' key in built input")
	}
	if input["extra"] != "data" {
		t.Errorf("expected 'extra'='data' from node.Data")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// extractInt64 helper
// ──────────────────────────────────────────────────────────────────────────────

func TestExtractInt64(t *testing.T) {
	cases := []struct {
		name  string
		data  map[string]interface{}
		key   string
		want  int64
	}{
		{"float64", map[string]interface{}{"id": float64(42)}, "id", 42},
		{"int64", map[string]interface{}{"id": int64(100)}, "id", 100},
		{"int", map[string]interface{}{"id": int(7)}, "id", 7},
		{"missing key", map[string]interface{}{"other": 1}, "id", 0},
		{"nil map", nil, "id", 0},
		{"wrong type", map[string]interface{}{"id": "not-a-number"}, "id", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractInt64(tc.data, tc.key)
			if got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

// testError is a simple error type used in tests.
type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
