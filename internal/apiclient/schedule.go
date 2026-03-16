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

// CreateRun creates a new run for a workflow with the given input and trigger type.
func (c *Client) CreateRun(ctx context.Context, workflowID int64, input map[string]interface{}, triggerType string, scheduleID int64) (*RunInfo, error) {
	path := fmt.Sprintf("api/router/v1/workflows/%d/runs", workflowID)
	body := map[string]interface{}{
		"trigger_type": triggerType,
		"input":        input,
	}
	if scheduleID > 0 {
		body["schedule_id"] = scheduleID
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
