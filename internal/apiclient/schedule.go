package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// GetSchedules returns all schedules for a workflow.
func (c *Client) GetSchedules(ctx context.Context, workflowID int64) ([]Schedule, error) {
	path := fmt.Sprintf("api/router/v1/workflows/%d/schedules", workflowID)
	data, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Schedules []Schedule `json:"schedules"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode schedules: %w", err)
	}
	return envelope.Schedules, nil
}

// GetAllSchedules returns all enabled schedules across all workflows.
func (c *Client) GetAllSchedules(ctx context.Context) ([]Schedule, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "api/router/v1/workflow-schedules", nil)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Schedules []Schedule `json:"schedules"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode schedules: %w", err)
	}
	return envelope.Schedules, nil
}

// CreateRunParams holds parameters for creating a run.
type CreateRunParams struct {
	WorkflowID  int64
	Input       map[string]interface{}
	TriggerType string
	RunType     string
	AgentID     int64
	ScheduleID  int64
}

// CreateRun creates a new run for a workflow with the given input and trigger type.
func (c *Client) CreateRun(ctx context.Context, workflowID int64, input map[string]interface{}, triggerType string, scheduleID int64) (*RunInfo, error) {
	return c.CreateRunWithParams(ctx, CreateRunParams{
		WorkflowID:  workflowID,
		Input:       input,
		TriggerType: triggerType,
		ScheduleID:  scheduleID,
		RunType:     "workflow",
	})
}

// CreateRunWithParams creates a new run with full parameter control.
func (c *Client) CreateRunWithParams(ctx context.Context, params CreateRunParams) (*RunInfo, error) {
	path := fmt.Sprintf("api/router/v1/workflows/%d/runs", params.WorkflowID)
	body := map[string]interface{}{
		"trigger_type": params.TriggerType,
		"input":        params.Input,
	}
	if params.RunType != "" {
		body["run_type"] = params.RunType
	}
	if params.AgentID > 0 {
		body["agent_id"] = params.AgentID
	}
	if params.ScheduleID > 0 {
		body["schedule_id"] = params.ScheduleID
	}
	data, err := c.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}
	var run RunInfo
	if err := json.Unmarshal(data, &run); err != nil {
		return nil, fmt.Errorf("decode run: %w", err)
	}
	return &run, nil
}
