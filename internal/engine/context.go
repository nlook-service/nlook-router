package engine

import "sync"

// RunContext holds state shared across all steps during a single workflow execution.
type RunContext struct {
	RunID      int64
	WorkflowID int64
	UserID     int64

	// Input is the initial user-provided data for this run.
	Input map[string]interface{}

	// nodeOutputs stores each node's output keyed by NodeID (client-generated ID).
	mu          sync.RWMutex
	nodeOutputs map[string]map[string]interface{}
}

// NewRunContext creates a new RunContext with the given initial input.
func NewRunContext(runID, workflowID, userID int64, input map[string]interface{}) *RunContext {
	if input == nil {
		input = make(map[string]interface{})
	}
	return &RunContext{
		RunID:       runID,
		WorkflowID:  workflowID,
		UserID:      userID,
		Input:       input,
		nodeOutputs: make(map[string]map[string]interface{}),
	}
}

// SetNodeOutput records the output of a completed node.
func (c *RunContext) SetNodeOutput(nodeID string, output map[string]interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nodeOutputs[nodeID] = output
}

// GetNodeOutput returns the output of a previously completed node.
func (c *RunContext) GetNodeOutput(nodeID string) map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.nodeOutputs[nodeID]
}
