package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/nlook-service/nlook-router/internal/apiclient"
	"github.com/nlook-service/nlook-router/internal/tracing"
)

// StepResult holds the outcome of a single step execution.
type StepResult struct {
	Output   map[string]interface{}
	Status   string // "completed", "failed", "skipped"
	Error    string
	LogLines []string
}

// StepEvent contains all metadata about a completed step, passed to StepHook callbacks.
type StepEvent struct {
	WorkflowID int64
	RunID      int64
	NodeID     string
	NodeType   string
	StepOrder  int
	Input      map[string]interface{}
	Result     *StepResult
	Duration   time.Duration
}

// StepHook is called after each workflow step completes.
// Implementations can capture step outputs for evaluation, logging, etc.
type StepHook interface {
	OnStepComplete(ctx context.Context, event *StepEvent)
}

// StepExecutor executes individual workflow nodes and reports to cloud via apiclient.
type StepExecutor struct {
	client      apiclient.Interface
	skillRunner *SkillRunner
	skills      map[int64]*apiclient.WorkflowSkill
	agents      map[int64]*apiclient.WorkflowAgent
	hooks       []StepHook
}

// NewStepExecutor creates a new StepExecutor.
func NewStepExecutor(client apiclient.Interface, skillRunner *SkillRunner) *StepExecutor {
	return &StepExecutor{
		client:      client,
		skillRunner: skillRunner,
		skills:      make(map[int64]*apiclient.WorkflowSkill),
		agents:      make(map[int64]*apiclient.WorkflowAgent),
	}
}

// AddHook registers a StepHook to be called after each step completes.
func (e *StepExecutor) AddHook(h StepHook) {
	e.hooks = append(e.hooks, h)
}

// LoadSkillsAndAgents pre-loads skills and agents from workflow detail into lookup maps.
func (e *StepExecutor) LoadSkillsAndAgents(skills []apiclient.WorkflowSkill, agents []apiclient.WorkflowAgent) {
	e.skills = make(map[int64]*apiclient.WorkflowSkill, len(skills))
	for i := range skills {
		s := &skills[i]
		e.skills[s.ID] = s
	}
	e.agents = make(map[int64]*apiclient.WorkflowAgent, len(agents))
	for i := range agents {
		a := &agents[i]
		e.agents[a.ID] = a
	}
}

// Execute runs a single node: reports start to cloud, executes the skill/agent, and reports completion.
// parentNodeIDs are the direct predecessors in the DAG whose outputs should flow into this node's input.
func (e *StepExecutor) Execute(ctx context.Context, rctx *RunContext, workflowID int64, node *apiclient.WorkflowNode, stepOrder int, parentNodeIDs ...string) (*StepResult, error) {
	nodeType := node.NodeType

	// Report step start to cloud
	logRef, err := e.client.StartStep(ctx, workflowID, rctx.RunID, node.NodeID, nodeType)
	if err != nil {
		// Non-fatal: log the error but continue execution
		logRef = &apiclient.StepLogRef{ID: 0}
	}

	started := time.Now()

	// Trace node start
	var spanID string
	if rctx.Tracer != nil && rctx.SessionID != "" {
		spanID = tracing.NewSpanID()
		rctx.Tracer.Emit(tracing.NewEvent(rctx.SessionID, tracing.EventNodeStart, node.NodeID).
			WithSpan(spanID, "").
			WithMeta(map[string]interface{}{
				"node_type":   nodeType,
				"workflow_id": workflowID,
				"run_id":      rctx.RunID,
				"step_order":  stepOrder,
			}))
	}

	// Build input from parent node outputs
	input := e.buildInput(rctx, node, parentNodeIDs)

	// Execute based on node type
	result := e.executeNode(ctx, rctx, node, input)
	result.LogLines = append(result.LogLines,
		fmt.Sprintf("[%s] node=%s type=%s duration=%s",
			result.Status, node.NodeID, nodeType, time.Since(started).Round(time.Millisecond)),
	)

	// Trace node complete/error
	if rctx.Tracer != nil && rctx.SessionID != "" {
		eventType := tracing.EventNodeComplete
		level := tracing.LevelInfo
		if result.Status == "failed" {
			eventType = tracing.EventNodeError
			level = tracing.LevelError
		}
		rctx.Tracer.Emit(tracing.NewEvent(rctx.SessionID, eventType, node.NodeID).
			WithSpan(spanID, "").
			WithDuration(time.Since(started).Milliseconds()).
			WithLevel(level).
			WithMeta(map[string]interface{}{
				"status": result.Status,
				"error":  result.Error,
			}))
	}

	// Report step completion to cloud
	if logRef.ID != 0 {
		if err := e.client.CompleteStep(ctx, workflowID, rctx.RunID, logRef.ID, result.Status, result.Output, result.Error, result.LogLines); err != nil {
			// Non-fatal: execution continues even if reporting fails
			result.LogLines = append(result.LogLines, fmt.Sprintf("warn: complete step report failed: %v", err))
		}
	}

	// Store output in context for downstream nodes
	if result.Output != nil {
		rctx.SetNodeOutput(node.NodeID, result.Output)
	}

	// Fire step hooks
	if len(e.hooks) > 0 {
		event := &StepEvent{
			WorkflowID: workflowID,
			RunID:      rctx.RunID,
			NodeID:     node.NodeID,
			NodeType:   nodeType,
			StepOrder:  stepOrder,
			Input:      input,
			Result:     result,
			Duration:   time.Since(started),
		}
		for _, h := range e.hooks {
			h.OnStepComplete(ctx, event)
		}
	}

	return result, nil
}

// buildInput collects outputs from parent nodes as input for this node.
// Data flows: Run Input → Parent Node Outputs → Node Config Data
func (e *StepExecutor) buildInput(rctx *RunContext, node *apiclient.WorkflowNode, parentNodeIDs []string) map[string]interface{} {
	input := make(map[string]interface{})

	// 1. Original user input (always available as _run_input)
	input["_run_input"] = rctx.Input

	// 2. Collect outputs from direct parent nodes in the DAG
	parentOutputs := make([]map[string]interface{}, 0)
	for _, pid := range parentNodeIDs {
		if out := rctx.GetNodeOutput(pid); out != nil {
			parentOutputs = append(parentOutputs, out)
		}
	}

	// Merge the last parent's output at top level for easy access (A → B chain)
	if len(parentOutputs) > 0 {
		for k, v := range parentOutputs[len(parentOutputs)-1] {
			input[k] = v
		}
	}
	if len(parentOutputs) > 0 {
		input["_parent_outputs"] = parentOutputs
	}

	// 3. Node's own config data (contains skill_id, agent_id, custom settings from frontend)
	if node.Data != nil {
		for k, v := range node.Data {
			input[k] = v
		}
	}

	return input
}

// executeNode dispatches execution based on node type.
func (e *StepExecutor) executeNode(ctx context.Context, rctx *RunContext, node *apiclient.WorkflowNode, input map[string]interface{}) *StepResult {
	nodeType := node.NodeType

	switch nodeType {
	case "step", "function":
		return e.executeSkillNode(ctx, node, input)
	case "agent":
		return e.executeAgentNode(ctx, node, input)
	default:
		// For unknown types, pass through with the input as output
		return &StepResult{
			Output:   input,
			Status:   "completed",
			LogLines: []string{fmt.Sprintf("passthrough node type: %s", nodeType)},
		}
	}
}

// executeSkillNode runs a node that references a skill via RefID or cached data.
func (e *StepExecutor) executeSkillNode(ctx context.Context, node *apiclient.WorkflowNode, input map[string]interface{}) *StepResult {
	skillID := extractInt64(node.Data, "skill_id")
	if skillID == 0 && node.RefID != 0 {
		skillID = node.RefID
	}
	// Fallback: nested skill object (data.skill.id)
	if skillID == 0 {
		skillID = extractNestedInt64(node.Data, "skill", "id")
	}

	if skillID == 0 {
		return &StepResult{
			Output:   map[string]interface{}{"message": "no skill attached, passthrough"},
			Status:   "completed",
			LogLines: []string{fmt.Sprintf("no skill_id found (ref_id=%d, data_keys=%v)", node.RefID, dataKeys(node.Data))},
		}
	}

	skill, ok := e.skills[skillID]
	if !ok {
		return &StepResult{
			Status:   "failed",
			Error:    fmt.Sprintf("skill %d not found in workflow detail", skillID),
			LogLines: []string{fmt.Sprintf("skill %d not found", skillID)},
		}
	}

	// Resolve agent if agent_id is set in node data
	var agent *apiclient.WorkflowAgent
	agentID := extractInt64(node.Data, "agent_id")
	if agentID != 0 {
		if a, ok := e.agents[agentID]; ok {
			agent = a
		}
	}

	output, logs, err := e.skillRunner.RunSkill(ctx, skill, agent, input)
	if err != nil {
		return &StepResult{
			Status:   "failed",
			Error:    fmt.Sprintf("run skill '%s': %v", skill.Name, err),
			LogLines: logs,
		}
	}

	return &StepResult{
		Output:   output,
		Status:   "completed",
		LogLines: logs,
	}
}

// executeAgentNode runs a node that references an agent via RefID.
func (e *StepExecutor) executeAgentNode(ctx context.Context, node *apiclient.WorkflowNode, input map[string]interface{}) *StepResult {
	agentID := extractInt64(node.Data, "agent_id")
	if agentID == 0 && node.RefID != 0 {
		agentID = node.RefID
	}
	// Fallback: nested agent object (data.agent.id)
	if agentID == 0 {
		agentID = extractNestedInt64(node.Data, "agent", "id")
	}

	if agentID == 0 {
		return &StepResult{
			Output:   map[string]interface{}{"message": "no agent attached, passthrough"},
			Status:   "completed",
			LogLines: []string{fmt.Sprintf("no agent_id found (ref_id=%d, data_keys=%v)", node.RefID, dataKeys(node.Data))},
		}
	}

	agent, ok := e.agents[agentID]
	if !ok {
		return &StepResult{
			Status:   "failed",
			Error:    fmt.Sprintf("agent %d not found in workflow detail", agentID),
			LogLines: []string{fmt.Sprintf("agent %d not found", agentID)},
		}
	}

	// Build a synthetic prompt skill from the agent's configuration
	promptContent, _ := input["prompt"].(string)
	if promptContent == "" {
		promptContent, _ = input["content"].(string)
	}
	if promptContent == "" {
		promptContent = fmt.Sprintf("You are %s. Process the following input and respond.", agent.Name)
		if runInput, ok := input["_run_input"].(map[string]interface{}); ok {
			if msg, ok := runInput["message"].(string); ok {
				promptContent = msg
			}
		}
	}

	agentSkill := &apiclient.WorkflowSkill{
		Name:      agent.Name,
		SkillType: "prompt",
		Content:   promptContent,
	}

	output, logs, err := e.skillRunner.RunSkill(ctx, agentSkill, agent, input)
	if err != nil {
		return &StepResult{
			Status:   "failed",
			Error:    fmt.Sprintf("run agent '%s': %v", agent.Name, err),
			LogLines: logs,
		}
	}

	return &StepResult{
		Output:   output,
		Status:   "completed",
		LogLines: logs,
	}
}

// extractNestedInt64 extracts an int64 from a nested map: data[outerKey][innerKey].
func extractNestedInt64(data map[string]interface{}, outerKey, innerKey string) int64 {
	if data == nil {
		return 0
	}
	outer, ok := data[outerKey]
	if !ok {
		return 0
	}
	m, ok := outer.(map[string]interface{})
	if !ok {
		return 0
	}
	return extractInt64(m, innerKey)
}

// dataKeys returns the keys of a map for debug logging.
func dataKeys(data map[string]interface{}) []string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys
}

// extractInt64 safely extracts an int64 from a map[string]interface{}.
func extractInt64(data map[string]interface{}, key string) int64 {
	if data == nil {
		return 0
	}
	v, ok := data[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	default:
		return 0
	}
}
