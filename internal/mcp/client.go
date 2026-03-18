package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Client calls nlook REST API directly (same endpoints as MCP server).
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new nlook API client for MCP-equivalent operations.
func NewClient(apiKey string) *Client {
	baseURL := os.Getenv("NLOOK_API_URL")
	if baseURL == "" {
		baseURL = "https://nlook.me"
	}

	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Tool represents an available tool for LLM tool_use.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// GetChatTools returns the subset of tools useful for chat interactions.
func GetChatTools() []Tool {
	return []Tool{
		{
			Name:        "create_document",
			Description: "Create a new document/note in nlook",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"title":   map[string]interface{}{"type": "string", "description": "Document title"},
					"content": map[string]interface{}{"type": "string", "description": "Document content in Markdown"},
					"tags":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Tags for categorization"},
				},
				"required": []string{"title", "content"},
			},
		},
		{
			Name:        "create_task",
			Description: "Create a new task/todo item",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"title":    map[string]interface{}{"type": "string", "description": "Task title"},
					"notes":    map[string]interface{}{"type": "string", "description": "Task notes in Markdown"},
					"priority": map[string]interface{}{"type": "string", "enum": []string{"none", "low", "medium", "high"}},
					"due_date": map[string]interface{}{"type": "string", "description": "Due date in ISO format"},
				},
				"required": []string{"title"},
			},
		},
		{
			Name:        "list_documents",
			Description: "Search and list documents",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"search": map[string]interface{}{"type": "string", "description": "Search query"},
					"limit":  map[string]interface{}{"type": "number", "description": "Max results (default 10)"},
				},
			},
		},
		{
			Name:        "list_tasks",
			Description: "List tasks/todos",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"status": map[string]interface{}{"type": "string", "enum": []string{"pending", "in_progress", "completed"}},
					"limit":  map[string]interface{}{"type": "number", "description": "Max results (default 10)"},
				},
			},
		},
		{
			Name:        "list_workspaces",
			Description: "List available workspaces/collections",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{},
			},
		},
	}
}

// CallTool executes a tool by calling the nlook REST API.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "create_document":
		return c.post(ctx, "/api/v1/public/documents", args)
	case "create_task":
		return c.post(ctx, "/api/v1/public/tasks", args)
	case "list_documents":
		query := buildQuery(args, "search", "limit")
		return c.get(ctx, "/api/v1/public/documents"+query)
	case "list_tasks":
		query := buildQuery(args, "status", "limit")
		return c.get(ctx, "/api/v1/public/tasks"+query)
	case "list_workspaces":
		return c.get(ctx, "/api/v1/public/workspaces")
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func (c *Client) get(ctx context.Context, path string) (interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	return c.doRequest(req)
}

func (c *Client) post(ctx context.Context, path string, body interface{}) (interface{}, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doRequest(req)
}

func (c *Client) doRequest(req *http.Request) (interface{}, error) {
	req.Header.Set("X-API-Key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: status=%d body=%s", resp.StatusCode, truncate(string(body), 200))
	}

	if resp.StatusCode == 204 {
		return map[string]interface{}{"status": "success"}, nil
	}

	var result interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return map[string]interface{}{"text": string(body)}, nil
	}
	return result, nil
}

func buildQuery(args map[string]interface{}, keys ...string) string {
	query := ""
	sep := "?"
	for _, k := range keys {
		if v, ok := args[k]; ok && v != nil {
			query += fmt.Sprintf("%s%s=%v", sep, k, v)
			sep = "&"
		}
	}
	return query
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
