package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// GetWorkflowDetail fetches full workflow data needed for execution.
// GET api/workflows/{id}/detail
func (c *Client) GetWorkflowDetail(ctx context.Context, id int64) (*WorkflowDetail, error) {
	path := fmt.Sprintf("api/router/v1/workflows/%d/detail", id)
	data, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get workflow detail: %w", err)
	}
	var out WorkflowDetail
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode workflow detail: %w", err)
	}
	return &out, nil
}

// GetPendingRuns fetches pending runs for a workflow.
// GET api/workflows/{workflowID}/runs/pending
func (c *Client) GetPendingRuns(ctx context.Context, workflowID int64) ([]RunInfo, error) {
	path := fmt.Sprintf("api/router/v1/workflows/%d/runs/pending", workflowID)
	data, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get pending runs: %w", err)
	}

	var envelope struct {
		Runs []RunInfo `json:"runs"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode pending runs: %w", err)
	}
	return envelope.Runs, nil
}

// UpdateRunStatus reports a run's new status to the cloud.
// POST api/workflows/{workflowID}/runs/{runID}/status
func (c *Client) UpdateRunStatus(ctx context.Context, workflowID, runID int64, status string, output map[string]interface{}, errMsg string) error {
	path := fmt.Sprintf("api/router/v1/workflows/%d/runs/%d/status", workflowID, runID)
	body := map[string]interface{}{
		"status":        status,
		"output":        output,
		"error_message": errMsg,
	}
	_, err := c.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return fmt.Errorf("update run status: %w", err)
	}
	return nil
}

// StartStep creates a step log entry and returns its ID.
// POST api/workflows/{workflowID}/runs/{runID}/steps
func (c *Client) StartStep(ctx context.Context, workflowID, runID int64, nodeID, nodeType string) (*StepLogRef, error) {
	path := fmt.Sprintf("api/router/v1/workflows/%d/runs/%d/steps", workflowID, runID)
	body := map[string]interface{}{
		"node_id":   nodeID,
		"node_type": nodeType,
	}
	data, err := c.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, fmt.Errorf("start step: %w", err)
	}
	var ref StepLogRef
	if err := json.Unmarshal(data, &ref); err != nil {
		return nil, fmt.Errorf("decode step log ref: %w", err)
	}
	return &ref, nil
}

// CompleteStep updates a step log entry with execution results.
// PUT api/workflows/{workflowID}/runs/{runID}/logs/{logID}
func (c *Client) CompleteStep(ctx context.Context, workflowID, runID, logID int64, status string, output map[string]interface{}, errMsg string, logLines []string) error {
	path := fmt.Sprintf("api/router/v1/workflows/%d/runs/%d/logs/%d", workflowID, runID, logID)
	body := map[string]interface{}{
		"status":        status,
		"output":        output,
		"error_message": errMsg,
		"log_lines":     logLines,
	}
	_, err := c.doRequest(ctx, http.MethodPut, path, body)
	if err != nil {
		return fmt.Errorf("complete step: %w", err)
	}
	return nil
}
