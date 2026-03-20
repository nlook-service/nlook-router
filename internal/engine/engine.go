package engine

import (
	"context"
	"fmt"
	"log"

	"github.com/nlook-service/nlook-router/internal/apiclient"
	"github.com/nlook-service/nlook-router/internal/tracing"
)

// WorkflowEngine orchestrates the execution of a workflow by traversing its DAG.
type WorkflowEngine struct {
	executor  *StepExecutor
	groupExec *GroupExecutor
	tracer    *tracing.Collector
}

// NewWorkflowEngine creates a new WorkflowEngine.
func NewWorkflowEngine(executor *StepExecutor) *WorkflowEngine {
	return &WorkflowEngine{
		executor:  executor,
		groupExec: NewGroupExecutor(executor),
	}
}

// SetTracer sets the trace collector for workflow execution tracing.
func (e *WorkflowEngine) SetTracer(t *tracing.Collector) {
	e.tracer = t
}

// SkillRunner returns the underlying skill runner for direct agent execution.
func (e *WorkflowEngine) SkillRunner() *SkillRunner {
	return e.executor.skillRunner
}

// Execute runs the workflow synchronously and returns the final output.
// Status updates (running/completed/failed) are reported via apiclient.
func (e *WorkflowEngine) Execute(ctx context.Context, detail *apiclient.WorkflowDetail, run apiclient.RunInfo) (map[string]interface{}, error) {
	// Pre-load skills and agents into the step executor
	e.executor.LoadSkillsAndAgents(detail.Skills, detail.Agents)

	if len(detail.Nodes) == 0 {
		return nil, fmt.Errorf("workflow has no nodes")
	}

	// Parse DAG
	dag, err := ParseDAG(detail.Nodes, detail.Edges)
	if err != nil {
		return nil, fmt.Errorf("parse DAG: %w", err)
	}

	// Topological sort
	sorted, err := dag.TopologicalSort()
	if err != nil {
		return nil, fmt.Errorf("topological sort: %w", err)
	}

	log.Printf("workflow execution started: workflow_id=%d run_id=%d node_count=%d",
		run.WorkflowID, run.ID, len(sorted))

	// Create RunContext
	rctx := NewRunContext(run.ID, run.WorkflowID, run.UserID, run.Input)
	rctx.SessionID = run.TraceID
	rctx.Tracer = e.tracer

	// Execute nodes in order
	stepOrder := 0
	for _, dagNode := range sorted {
		// Check for cancellation
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		nodeType := dagNode.Node.NodeType

		// Skip start/end nodes (they don't execute logic)
		if nodeType == "start" || nodeType == "end" {
			continue
		}

		var result *StepResult

		if isGroupType(nodeType) {
			result, err = e.groupExec.Execute(ctx, rctx, dag, dagNode, &stepOrder, run.WorkflowID)
			if err != nil {
				return nil, fmt.Errorf("execute group %s: %w", dagNode.Node.NodeID, err)
			}
		} else {
			// Collect parent node IDs from DAG edges
			parentIDs := make([]string, 0, len(dagNode.Parents))
			for _, p := range dagNode.Parents {
				parentIDs = append(parentIDs, p.Node.NodeID)
			}
			result, err = e.executor.Execute(ctx, rctx, run.WorkflowID, dagNode.Node, stepOrder, parentIDs...)
			if err != nil {
				return nil, fmt.Errorf("execute node %s: %w", dagNode.Node.NodeID, err)
			}
			stepOrder++
		}

		// Store group output in context
		if result.Output != nil {
			rctx.SetNodeOutput(dagNode.Node.NodeID, result.Output)
		}

		// If step failed, fail the entire run
		if result.Status == "failed" {
			return nil, fmt.Errorf("node %s failed: %s", dagNode.Node.NodeID, result.Error)
		}
	}

	// Collect final output from the last executed node
	var finalOutput map[string]interface{}
	for i := len(sorted) - 1; i >= 0; i-- {
		nodeID := sorted[i].Node.NodeID
		if out := rctx.GetNodeOutput(nodeID); out != nil {
			finalOutput = out
			break
		}
	}

	log.Printf("workflow execution completed: workflow_id=%d run_id=%d steps_executed=%d",
		run.WorkflowID, run.ID, stepOrder)

	return finalOutput, nil
}

// isGroupType returns true for group container node types.
func isGroupType(nodeType string) bool {
	switch nodeType {
	case "condition", "loop", "parallel", "router":
		return true
	}
	return false
}
