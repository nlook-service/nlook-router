package engine

import (
	"context"
	"testing"

	"github.com/nlook-service/nlook-router/internal/apiclient"
)

// ──────────────────────────────────────────────────────────────────────────────
// helpers
// ──────────────────────────────────────────────────────────────────────────────

func buildEngine(mc *mockClient) *WorkflowEngine {
	skillRunner := NewSkillRunner()
	stepExec := NewStepExecutor(mc, skillRunner)
	return NewWorkflowEngine(stepExec)
}

func simpleLinearDetail() *apiclient.WorkflowDetail {
	return &apiclient.WorkflowDetail{
		ID:    1,
		Title: "linear workflow",
		Nodes: []apiclient.WorkflowNode{
			{NodeID: "start-1", NodeType: "start"},
			{NodeID: "step-1", NodeType: "step"},
			{NodeID: "end-1", NodeType: "end"},
		},
		Edges: []apiclient.WorkflowEdge{
			{EdgeID: "e1", SourceNodeID: "start-1", TargetNodeID: "step-1"},
			{EdgeID: "e2", SourceNodeID: "step-1", TargetNodeID: "end-1"},
		},
	}
}

func simpleRun(workflowID int64) apiclient.RunInfo {
	return apiclient.RunInfo{
		ID:         1,
		WorkflowID: workflowID,
		UserID:     99,
		Input:      map[string]interface{}{"greeting": "hello"},
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Basic execution
// ──────────────────────────────────────────────────────────────────────────────

func TestWorkflowEngine_Execute_linearChain(t *testing.T) {
	mc := newMockClient()
	eng := buildEngine(mc)

	detail := simpleLinearDetail()
	run := simpleRun(1)

	output, err := eng.Execute(context.Background(), detail, run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only step-1 actually executes; it's a passthrough with no skill so output is the built input.
	// We mainly verify no error and that StartStep was called for step-1.
	if mc.startStepCount() == 0 {
		t.Error("expected at least one StartStep call")
	}
	_ = output // output may be nil if step-1 passthrough returns input map
}

func TestWorkflowEngine_Execute_emptyNodes(t *testing.T) {
	mc := newMockClient()
	eng := buildEngine(mc)

	detail := &apiclient.WorkflowDetail{ID: 2}
	run := simpleRun(2)

	_, err := eng.Execute(context.Background(), detail, run)
	if err == nil {
		t.Fatal("expected error for empty nodes, got nil")
	}
}

func TestWorkflowEngine_Execute_noStartNode(t *testing.T) {
	mc := newMockClient()
	eng := buildEngine(mc)

	detail := &apiclient.WorkflowDetail{
		ID: 3,
		Nodes: []apiclient.WorkflowNode{
			{NodeID: "step-1", NodeType: "step"},
		},
	}
	run := simpleRun(3)

	_, err := eng.Execute(context.Background(), detail, run)
	if err == nil {
		t.Fatal("expected error for missing start node, got nil")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// start → tool-step → end  (step with loaded skill)
// ──────────────────────────────────────────────────────────────────────────────

func TestWorkflowEngine_Execute_withToolSkill(t *testing.T) {
	mc := newMockClient()
	eng := buildEngine(mc)

	detail := &apiclient.WorkflowDetail{
		ID:    10,
		Title: "tool workflow",
		Nodes: []apiclient.WorkflowNode{
			{NodeID: "start-1", NodeType: "start"},
			{NodeID: "step-1", NodeType: "step", RefID: 1},
			{NodeID: "end-1", NodeType: "end"},
		},
		Edges: []apiclient.WorkflowEdge{
			{EdgeID: "e1", SourceNodeID: "start-1", TargetNodeID: "step-1"},
			{EdgeID: "e2", SourceNodeID: "step-1", TargetNodeID: "end-1"},
		},
		Skills: []apiclient.WorkflowSkill{
			{
				ID:        1,
				Name:      "greeter",
				SkillType: "tool",
				Config:    map[string]interface{}{"tool_name": "greeter"},
			},
		},
	}
	run := simpleRun(10)

	output, err := eng.Execute(context.Background(), detail, run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if output == nil {
		t.Fatal("expected non-nil output")
	}
	if output["tool"] != "greeter" {
		t.Errorf("output[tool]: got %v, want 'greeter'", output["tool"])
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Condition branching
// ──────────────────────────────────────────────────────────────────────────────

func TestWorkflowEngine_Execute_conditionTrue(t *testing.T) {
	mc := newMockClient()
	eng := buildEngine(mc)

	// Workflow: start → condition(check flag==yes) → [true-branch: step-true] → end
	// Run input has flag=yes, so the true branch should execute.
	detail := &apiclient.WorkflowDetail{
		ID:    20,
		Title: "condition workflow",
		Nodes: []apiclient.WorkflowNode{
			{NodeID: "start-1", NodeType: "start"},
			{
				NodeID:   "cond-1",
				NodeType: "condition",
				Data: map[string]interface{}{
					"condition_field":    "flag",
					"condition_value":    "yes",
					"condition_operator": "eq",
				},
			},
			{
				NodeID:   "step-true",
				NodeType: "step",
				Data:     map[string]interface{}{"branch": "true"},
			},
			{
				NodeID:   "step-false",
				NodeType: "step",
				Data:     map[string]interface{}{"branch": "false"},
			},
			{NodeID: "end-1", NodeType: "end"},
		},
		Edges: []apiclient.WorkflowEdge{
			{EdgeID: "e1", SourceNodeID: "start-1", TargetNodeID: "cond-1"},
			{EdgeID: "e2", SourceNodeID: "cond-1", TargetNodeID: "step-true"},
			{EdgeID: "e3", SourceNodeID: "cond-1", TargetNodeID: "step-false"},
			{EdgeID: "e4", SourceNodeID: "step-true", TargetNodeID: "end-1"},
			{EdgeID: "e5", SourceNodeID: "step-false", TargetNodeID: "end-1"},
		},
	}

	run := apiclient.RunInfo{
		ID:         1,
		WorkflowID: 20,
		UserID:     1,
		Input:      map[string]interface{}{"flag": "yes"},
	}

	_, err := eng.Execute(context.Background(), detail, run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWorkflowEngine_Execute_conditionFalse(t *testing.T) {
	mc := newMockClient()
	eng := buildEngine(mc)

	detail := &apiclient.WorkflowDetail{
		ID: 21,
		Nodes: []apiclient.WorkflowNode{
			{NodeID: "start-1", NodeType: "start"},
			{
				NodeID:   "cond-1",
				NodeType: "condition",
				Data: map[string]interface{}{
					"condition_field":    "flag",
					"condition_value":    "yes",
					"condition_operator": "eq",
				},
			},
			{
				NodeID:   "step-false",
				NodeType: "step",
				Data:     map[string]interface{}{"branch": "false"},
			},
			{NodeID: "end-1", NodeType: "end"},
		},
		Edges: []apiclient.WorkflowEdge{
			{EdgeID: "e1", SourceNodeID: "start-1", TargetNodeID: "cond-1"},
			{EdgeID: "e2", SourceNodeID: "cond-1", TargetNodeID: "step-false"},
			{EdgeID: "e3", SourceNodeID: "step-false", TargetNodeID: "end-1"},
		},
	}

	run := apiclient.RunInfo{
		ID:         1,
		WorkflowID: 21,
		UserID:     1,
		Input:      map[string]interface{}{"flag": "no"}, // does not equal "yes"
	}

	_, err := eng.Execute(context.Background(), detail, run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Loop execution
// ──────────────────────────────────────────────────────────────────────────────

func TestWorkflowEngine_Execute_loopNode(t *testing.T) {
	mc := newMockClient()
	eng := buildEngine(mc)

	const loopIterations = 3

	// Workflow: start → loop(count=3) → end
	// The loop has a direct child step via edges (no parent_id grouping)
	detail := &apiclient.WorkflowDetail{
		ID: 30,
		Nodes: []apiclient.WorkflowNode{
			{NodeID: "start-1", NodeType: "start"},
			{
				NodeID:   "loop-1",
				NodeType: "loop",
				Data:     map[string]interface{}{"loop_count": float64(loopIterations)},
			},
			{NodeID: "step-body", NodeType: "step"},
			{NodeID: "end-1", NodeType: "end"},
		},
		Edges: []apiclient.WorkflowEdge{
			{EdgeID: "e1", SourceNodeID: "start-1", TargetNodeID: "loop-1"},
			{EdgeID: "e2", SourceNodeID: "loop-1", TargetNodeID: "step-body"},
			{EdgeID: "e3", SourceNodeID: "step-body", TargetNodeID: "end-1"},
		},
	}

	run := simpleRun(30)

	output, err := eng.Execute(context.Background(), detail, run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = output
}

func TestWorkflowEngine_Execute_loopZeroCount(t *testing.T) {
	mc := newMockClient()
	eng := buildEngine(mc)

	detail := &apiclient.WorkflowDetail{
		ID: 31,
		Nodes: []apiclient.WorkflowNode{
			{NodeID: "start-1", NodeType: "start"},
			{
				NodeID:   "loop-1",
				NodeType: "loop",
				Data:     map[string]interface{}{"loop_count": float64(0)},
			},
			{NodeID: "end-1", NodeType: "end"},
		},
		Edges: []apiclient.WorkflowEdge{
			{EdgeID: "e1", SourceNodeID: "start-1", TargetNodeID: "loop-1"},
			{EdgeID: "e2", SourceNodeID: "loop-1", TargetNodeID: "end-1"},
		},
	}

	_, err := eng.Execute(context.Background(), detail, run31())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func run31() apiclient.RunInfo {
	return apiclient.RunInfo{ID: 1, WorkflowID: 31, UserID: 1, Input: nil}
}

// ──────────────────────────────────────────────────────────────────────────────
// Context cancellation
// ──────────────────────────────────────────────────────────────────────────────

func TestWorkflowEngine_Execute_cancelledContext(t *testing.T) {
	mc := newMockClient()
	eng := buildEngine(mc)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	detail := simpleLinearDetail()
	run := simpleRun(1)

	_, err := eng.Execute(ctx, detail, run)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Failed node stops the workflow
// ──────────────────────────────────────────────────────────────────────────────

func TestWorkflowEngine_Execute_failedNodeStopsWorkflow(t *testing.T) {
	mc := newMockClient()
	eng := buildEngine(mc)

	// step-1 has a non-existent skill ref — will return Status="failed"
	detail := &apiclient.WorkflowDetail{
		ID: 40,
		Nodes: []apiclient.WorkflowNode{
			{NodeID: "start-1", NodeType: "start"},
			{NodeID: "step-1", NodeType: "step", RefID: 999}, // skill 999 not loaded
			{NodeID: "end-1", NodeType: "end"},
		},
		Edges: []apiclient.WorkflowEdge{
			{EdgeID: "e1", SourceNodeID: "start-1", TargetNodeID: "step-1"},
			{EdgeID: "e2", SourceNodeID: "step-1", TargetNodeID: "end-1"},
		},
		// No skills loaded — skill 999 will be "not found"
	}

	run := simpleRun(40)

	_, err := eng.Execute(context.Background(), detail, run)
	if err == nil {
		t.Fatal("expected error when node fails, got nil")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Output propagation — final output comes from last executed node
// ──────────────────────────────────────────────────────────────────────────────

func TestWorkflowEngine_Execute_outputPropagation(t *testing.T) {
	mc := newMockClient()
	eng := buildEngine(mc)

	detail := &apiclient.WorkflowDetail{
		ID: 50,
		Nodes: []apiclient.WorkflowNode{
			{NodeID: "start-1", NodeType: "start"},
			{NodeID: "step-1", NodeType: "step", RefID: 1},
			{NodeID: "end-1", NodeType: "end"},
		},
		Edges: []apiclient.WorkflowEdge{
			{EdgeID: "e1", SourceNodeID: "start-1", TargetNodeID: "step-1"},
			{EdgeID: "e2", SourceNodeID: "step-1", TargetNodeID: "end-1"},
		},
		Skills: []apiclient.WorkflowSkill{
			{ID: 1, Name: "printer", SkillType: "tool", Config: map[string]interface{}{"tool_name": "printer"}},
		},
	}

	run := apiclient.RunInfo{ID: 1, WorkflowID: 50, UserID: 1, Input: map[string]interface{}{"msg": "test"}}

	output, err := eng.Execute(context.Background(), detail, run)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output == nil {
		t.Fatal("expected non-nil final output")
	}
	// Tool skill returns { "tool": ..., "message": ..., "input": ... }
	if _, ok := output["tool"]; !ok {
		t.Errorf("expected 'tool' key in output, got: %v", output)
	}
}
