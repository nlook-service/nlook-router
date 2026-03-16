package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// GroupExecutor handles execution of group-type nodes (condition, loop, parallel, router).
type GroupExecutor struct {
	stepExecutor *StepExecutor
}

// NewGroupExecutor creates a new GroupExecutor.
func NewGroupExecutor(stepExecutor *StepExecutor) *GroupExecutor {
	return &GroupExecutor{stepExecutor: stepExecutor}
}

// Execute dispatches to the appropriate group handler based on node type.
func (g *GroupExecutor) Execute(ctx context.Context, rctx *RunContext, dag *DAG, groupNode *DAGNode, stepOrder *int, workflowID int64) (*StepResult, error) {
	nodeType := groupNode.Node.NodeType

	switch nodeType {
	case "condition":
		return g.executeCondition(ctx, rctx, dag, groupNode, stepOrder, workflowID)
	case "loop":
		return g.executeLoop(ctx, rctx, dag, groupNode, stepOrder, workflowID)
	case "parallel":
		return g.executeParallel(ctx, rctx, dag, groupNode, stepOrder, workflowID)
	case "router":
		return g.executeRouter(ctx, rctx, dag, groupNode, stepOrder, workflowID)
	default:
		return &StepResult{
			Output:   map[string]interface{}{},
			Status:   "completed",
			LogLines: []string{fmt.Sprintf("unknown group type: %s, skipping", nodeType)},
		}, nil
	}
}

// getChildNodes returns DAG nodes whose parent_id matches the group node's NodeID.
func (g *GroupExecutor) getChildNodes(dag *DAG, parentNodeID string) []*DAGNode {
	var children []*DAGNode
	for _, dn := range dag.Nodes {
		if dn.Node.ParentID == parentNodeID {
			children = append(children, dn)
		}
	}
	return children
}

// findLinkLabel searches for the label on a connection between source and target.
// Convention: condition nodes' children use node.Data["branch"] = "true"/"false".
func (g *GroupExecutor) findLinkLabel(dag *DAG, sourceNodeID, targetNodeID string) string {
	targetNode := dag.Nodes[targetNodeID]
	if targetNode != nil && targetNode.Node.Data != nil {
		if branch, ok := targetNode.Node.Data["branch"].(string); ok {
			return branch
		}
	}
	return ""
}

// executeCondition evaluates a condition and executes the matching branch.
// Node Data expected: { "condition": "{{key}} == value" } or { "condition_field": "key", "condition_value": "value" }
// Children are connected with label "true" or "false" (via link Label or child node Data["branch"]).
func (g *GroupExecutor) executeCondition(ctx context.Context, rctx *RunContext, dag *DAG, groupNode *DAGNode, stepOrder *int, workflowID int64) (*StepResult, error) {
	logs := []string{"condition node evaluation started"}

	condResult := g.evaluateCondition(rctx, groupNode)
	logs = append(logs, fmt.Sprintf("condition evaluated: %v", condResult))

	// Find branches by label
	branchLabel := "false"
	if condResult {
		branchLabel = "true"
	}

	// Execute child nodes of the matching branch
	branchChildren := g.findBranchChildren(dag, groupNode, branchLabel)
	if len(branchChildren) == 0 {
		logs = append(logs, fmt.Sprintf("no children for branch '%s', skipping", branchLabel))
		return &StepResult{
			Output:   map[string]interface{}{"condition_result": condResult},
			Status:   "completed",
			LogLines: logs,
		}, nil
	}

	var lastOutput map[string]interface{}
	for _, child := range branchChildren {
		if ctx.Err() != nil {
			return &StepResult{Status: "failed", Error: "cancelled", LogLines: logs}, ctx.Err()
		}

		childType := child.Node.NodeType
		if childType == "start" || childType == "end" {
			continue
		}

		*stepOrder++
		result, err := g.stepExecutor.Execute(ctx, rctx, workflowID, child.Node, *stepOrder)
		if err != nil {
			return nil, fmt.Errorf("condition branch child %s: %w", child.Node.NodeID, err)
		}
		if result.Status == "failed" {
			return result, nil
		}
		logs = append(logs, result.LogLines...)
		lastOutput = result.Output
	}

	return &StepResult{
		Output:   lastOutput,
		Status:   "completed",
		LogLines: logs,
	}, nil
}

// evaluateCondition checks the condition in the node's Data.
func (g *GroupExecutor) evaluateCondition(rctx *RunContext, groupNode *DAGNode) bool {
	node := groupNode.Node
	if node.Data == nil {
		return false
	}

	// Simple field-value comparison
	field, _ := node.Data["condition_field"].(string)
	value, _ := node.Data["condition_value"].(string)
	operator, _ := node.Data["condition_operator"].(string)
	if operator == "" {
		operator = "eq"
	}

	if field == "" {
		// Try expression-based: { "condition": "some_key == some_value" }
		expr, _ := node.Data["condition"].(string)
		return g.evaluateExpression(rctx, expr)
	}

	// Get actual value from run context
	actual := g.resolveFieldValue(rctx, field)

	switch operator {
	case "eq", "==", "equals":
		return fmt.Sprintf("%v", actual) == value
	case "neq", "!=", "not_equals":
		return fmt.Sprintf("%v", actual) != value
	case "contains":
		return strings.Contains(fmt.Sprintf("%v", actual), value)
	case "not_empty":
		return fmt.Sprintf("%v", actual) != "" && actual != nil
	case "empty":
		return fmt.Sprintf("%v", actual) == "" || actual == nil
	default:
		return fmt.Sprintf("%v", actual) == value
	}
}

// evaluateExpression handles simple "key == value" expressions.
func (g *GroupExecutor) evaluateExpression(rctx *RunContext, expr string) bool {
	if expr == "" {
		return false
	}

	// Support: "key == value", "key != value"
	for _, op := range []string{"!=", "=="} {
		parts := strings.SplitN(expr, op, 2)
		if len(parts) == 2 {
			field := strings.TrimSpace(parts[0])
			expected := strings.TrimSpace(parts[1])
			actual := fmt.Sprintf("%v", g.resolveFieldValue(rctx, field))

			if op == "==" {
				return actual == expected
			}
			return actual != expected
		}
	}

	// If expression is just a field name, check if it's truthy
	val := g.resolveFieldValue(rctx, strings.TrimSpace(expr))
	return val != nil && val != "" && val != false && val != 0
}

// resolveFieldValue looks up a field value from run input or node outputs.
func (g *GroupExecutor) resolveFieldValue(rctx *RunContext, field string) interface{} {
	// Check node outputs first (format: "nodeId.fieldName")
	parts := strings.SplitN(field, ".", 2)
	if len(parts) == 2 {
		nodeOutput := rctx.GetNodeOutput(parts[0])
		if nodeOutput != nil {
			return nodeOutput[parts[1]]
		}
	}

	// Check run input
	if rctx.Input != nil {
		if v, ok := rctx.Input[field]; ok {
			return v
		}
	}

	return nil
}

// findBranchChildren returns children connected to a group node for a specific branch.
func (g *GroupExecutor) findBranchChildren(dag *DAG, groupNode *DAGNode, branchLabel string) []*DAGNode {
	var result []*DAGNode
	for _, child := range groupNode.Children {
		// Check if child's data marks it for this branch
		if child.Node.Data != nil {
			if branch, ok := child.Node.Data["branch"].(string); ok {
				if branch == branchLabel {
					result = append(result, child)
				}
				continue
			}
		}
		// If no branch marking and looking for "true", include all unmarked children
		if branchLabel == "true" {
			result = append(result, child)
		}
	}
	return result
}

// executeLoop repeats child node execution based on loop configuration.
// Node Data expected: { "loop_count": 3 } or { "loop_field": "items" }
func (g *GroupExecutor) executeLoop(ctx context.Context, rctx *RunContext, dag *DAG, groupNode *DAGNode, stepOrder *int, workflowID int64) (*StepResult, error) {
	logs := []string{"loop node execution started"}

	loopCount := g.getLoopCount(rctx, groupNode)
	if loopCount <= 0 {
		logs = append(logs, "loop_count is 0 or negative, skipping")
		return &StepResult{
			Output:   map[string]interface{}{"iterations": 0},
			Status:   "completed",
			LogLines: logs,
		}, nil
	}

	children := g.getChildNodes(dag, groupNode.Node.NodeID)
	if len(children) == 0 {
		children = g.getDirectChildren(groupNode)
	}

	logs = append(logs, fmt.Sprintf("loop_count=%d children=%d", loopCount, len(children)))

	iterationResults := make([]map[string]interface{}, 0, loopCount)

	for i := 0; i < loopCount; i++ {
		if ctx.Err() != nil {
			return &StepResult{Status: "failed", Error: "cancelled", LogLines: logs}, ctx.Err()
		}

		// Set loop index in context for child nodes
		rctx.SetNodeOutput(groupNode.Node.NodeID, map[string]interface{}{
			"loop_index": i,
			"loop_count": loopCount,
		})

		var iterOutput map[string]interface{}
		for _, child := range children {
			childType := child.Node.NodeType
			if childType == "start" || childType == "end" {
				continue
			}

			*stepOrder++
			result, err := g.stepExecutor.Execute(ctx, rctx, workflowID, child.Node, *stepOrder)
			if err != nil {
				return nil, fmt.Errorf("loop iteration %d child %s: %w", i, child.Node.NodeID, err)
			}
			if result.Status == "failed" {
				return result, nil
			}
			iterOutput = result.Output
		}

		if iterOutput != nil {
			iterationResults = append(iterationResults, iterOutput)
		}

		logs = append(logs, fmt.Sprintf("iteration %d/%d completed", i+1, loopCount))
	}

	return &StepResult{
		Output: map[string]interface{}{
			"iterations": len(iterationResults),
			"results":    iterationResults,
		},
		Status:   "completed",
		LogLines: logs,
	}, nil
}

// getLoopCount determines how many iterations to run.
func (g *GroupExecutor) getLoopCount(rctx *RunContext, groupNode *DAGNode) int {
	node := groupNode.Node
	if node.Data == nil {
		return 0
	}

	// Direct count
	if count, ok := node.Data["loop_count"]; ok {
		switch v := count.(type) {
		case float64:
			return int(v)
		case int:
			return v
		case json.Number:
			n, _ := v.Int64()
			return int(n)
		}
	}

	// Count from array field
	if field, ok := node.Data["loop_field"].(string); ok {
		val := g.resolveFieldValue(rctx, field)
		if arr, ok := val.([]interface{}); ok {
			return len(arr)
		}
	}

	return 0
}

// getDirectChildren returns children via DAG edges (not parent_id).
func (g *GroupExecutor) getDirectChildren(groupNode *DAGNode) []*DAGNode {
	return groupNode.Children
}

// executeParallel runs child nodes concurrently using goroutines.
func (g *GroupExecutor) executeParallel(ctx context.Context, rctx *RunContext, dag *DAG, groupNode *DAGNode, stepOrder *int, workflowID int64) (*StepResult, error) {
	logs := []string{"parallel node execution started"}

	children := g.getChildNodes(dag, groupNode.Node.NodeID)
	if len(children) == 0 {
		children = g.getDirectChildren(groupNode)
	}

	if len(children) == 0 {
		return &StepResult{
			Output:   map[string]interface{}{},
			Status:   "completed",
			LogLines: append(logs, "no children to execute"),
		}, nil
	}

	logs = append(logs, fmt.Sprintf("executing %d children in parallel", len(children)))

	type parallelResult struct {
		nodeID string
		result *StepResult
		err    error
	}

	resultsCh := make(chan parallelResult, len(children))
	var wg sync.WaitGroup

	// Assign step orders upfront so they're deterministic
	childOrders := make(map[string]int, len(children))
	for _, child := range children {
		childType := child.Node.NodeType
		if childType == "start" || childType == "end" {
			continue
		}
		*stepOrder++
		childOrders[child.Node.NodeID] = *stepOrder
	}

	for _, child := range children {
		childType := child.Node.NodeType
		if childType == "start" || childType == "end" {
			continue
		}

		wg.Add(1)
		go func(c *DAGNode, order int) {
			defer wg.Done()
			result, err := g.stepExecutor.Execute(ctx, rctx, workflowID, c.Node, order)
			resultsCh <- parallelResult{nodeID: c.Node.NodeID, result: result, err: err}
		}(child, childOrders[child.Node.NodeID])
	}

	wg.Wait()
	close(resultsCh)

	mergedOutput := make(map[string]interface{})
	for pr := range resultsCh {
		if pr.err != nil {
			return nil, fmt.Errorf("parallel child %s: %w", pr.nodeID, pr.err)
		}
		if pr.result.Status == "failed" {
			return pr.result, nil
		}
		logs = append(logs, pr.result.LogLines...)
		mergedOutput[pr.nodeID] = pr.result.Output
	}

	return &StepResult{
		Output:   mergedOutput,
		Status:   "completed",
		LogLines: logs,
	}, nil
}

// executeRouter matches input against route rules and executes the first matching route's children.
// Node Data expected: { "routes": [{ "match_field": "key", "match_value": "val", "target_node_id": "..." }] }
func (g *GroupExecutor) executeRouter(ctx context.Context, rctx *RunContext, dag *DAG, groupNode *DAGNode, stepOrder *int, workflowID int64) (*StepResult, error) {
	logs := []string{"router node evaluation started"}

	routes, _ := groupNode.Node.Data["routes"].([]interface{})
	if len(routes) == 0 {
		// No routes defined — execute all children as fallback
		logs = append(logs, "no routes defined, executing all children")
		return g.executeAllChildren(ctx, rctx, dag, groupNode, stepOrder, logs, workflowID)
	}

	for i, route := range routes {
		routeMap, ok := route.(map[string]interface{})
		if !ok {
			continue
		}

		matchField, _ := routeMap["match_field"].(string)
		matchValue, _ := routeMap["match_value"].(string)
		targetNodeID, _ := routeMap["target_node_id"].(string)

		actual := fmt.Sprintf("%v", g.resolveFieldValue(rctx, matchField))
		if actual == matchValue {
			logs = append(logs, fmt.Sprintf("route %d matched: %s == %s → %s", i, matchField, matchValue, targetNodeID))

			if targetNodeID != "" {
				targetNode, ok := dag.Nodes[targetNodeID]
				if !ok {
					return &StepResult{
						Status:   "failed",
						Error:    fmt.Sprintf("router target node not found: %s", targetNodeID),
						LogLines: logs,
					}, nil
				}

				*stepOrder++
				result, err := g.stepExecutor.Execute(ctx, rctx, workflowID, targetNode.Node, *stepOrder)
				if err != nil {
					return nil, fmt.Errorf("router target %s: %w", targetNodeID, err)
				}
				logs = append(logs, result.LogLines...)
				return &StepResult{
					Output:   result.Output,
					Status:   result.Status,
					LogLines: logs,
				}, nil
			}
		}
	}

	// No route matched — check for default
	logs = append(logs, "no route matched, checking default")
	defaultNodeID, _ := groupNode.Node.Data["default_target"].(string)
	if defaultNodeID != "" {
		if targetNode, ok := dag.Nodes[defaultNodeID]; ok {
			*stepOrder++
			result, err := g.stepExecutor.Execute(ctx, rctx, workflowID, targetNode.Node, *stepOrder)
			if err != nil {
				return nil, fmt.Errorf("router default target %s: %w", defaultNodeID, err)
			}
			logs = append(logs, result.LogLines...)
			return &StepResult{
				Output:   result.Output,
				Status:   result.Status,
				LogLines: logs,
			}, nil
		}
	}

	return &StepResult{
		Output:   map[string]interface{}{"matched": false},
		Status:   "completed",
		LogLines: append(logs, "no route matched, no default target"),
	}, nil
}

// executeAllChildren runs all children of a group node sequentially.
func (g *GroupExecutor) executeAllChildren(ctx context.Context, rctx *RunContext, dag *DAG, groupNode *DAGNode, stepOrder *int, logs []string, workflowID int64) (*StepResult, error) {
	children := g.getChildNodes(dag, groupNode.Node.NodeID)
	if len(children) == 0 {
		children = g.getDirectChildren(groupNode)
	}

	var lastOutput map[string]interface{}
	for _, child := range children {
		childType := child.Node.NodeType
		if childType == "start" || childType == "end" {
			continue
		}

		if ctx.Err() != nil {
			return &StepResult{Status: "failed", Error: "cancelled", LogLines: logs}, ctx.Err()
		}

		*stepOrder++
		result, err := g.stepExecutor.Execute(ctx, rctx, workflowID, child.Node, *stepOrder)
		if err != nil {
			return nil, fmt.Errorf("child %s: %w", child.Node.NodeID, err)
		}
		if result.Status == "failed" {
			return result, nil
		}
		logs = append(logs, result.LogLines...)
		lastOutput = result.Output
	}

	return &StepResult{
		Output:   lastOutput,
		Status:   "completed",
		LogLines: logs,
	}, nil
}
