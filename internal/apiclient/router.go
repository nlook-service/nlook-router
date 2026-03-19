package apiclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// RegisterRouter registers this router with the server.
func (c *Client) RegisterRouter(ctx context.Context, payload *RegisterPayload) error {
	_, err := c.doRequest(ctx, http.MethodPost, "api/routers/register", payload)
	return err
}

// Heartbeat sends a heartbeat to the server.
func (c *Client) Heartbeat(ctx context.Context, payload *RegisterPayload) error {
	data, err := c.doRequest(ctx, http.MethodPost, "api/routers/heartbeat", payload)
	if err != nil {
		return err
	}
	var resp struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("decode heartbeat response: %w", err)
	}
	return nil
}

// ReportUsage sends hourly token usage buckets to the server.
func (c *Client) ReportUsage(ctx context.Context, buckets interface{}) error {
	_, err := c.doRequest(ctx, http.MethodPost, "api/routers/usage", buckets)
	return err
}
