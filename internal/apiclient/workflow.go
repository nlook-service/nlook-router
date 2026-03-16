package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// ListWorkflows returns workflows from the server.
func (c *Client) ListWorkflows(ctx context.Context) ([]Workflow, error) {
	data, err := c.doRequest(ctx, http.MethodGet, "api/router/v1/workflows", nil)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Workflows []Workflow `json:"workflows"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode workflows: %w", err)
	}
	return envelope.Workflows, nil
}

// GetWorkflow returns a single workflow by ID.
func (c *Client) GetWorkflow(ctx context.Context, id int64) (*Workflow, error) {
	path := fmt.Sprintf("api/router/v1/workflows/%d", id)
	data, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var out Workflow
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("decode workflow: %w", err)
	}
	return &out, nil
}
