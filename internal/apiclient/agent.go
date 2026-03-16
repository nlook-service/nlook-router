package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// GetAgentDetail fetches agent detail by ID.
func (c *Client) GetAgentDetail(ctx context.Context, agentID int64) (*WorkflowAgent, error) {
	path := fmt.Sprintf("api/router/v1/agents/%d", agentID)
	data, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("get agent detail: %w", err)
	}
	var agent WorkflowAgent
	if err := json.Unmarshal(data, &agent); err != nil {
		return nil, fmt.Errorf("decode agent detail: %w", err)
	}
	return &agent, nil
}
