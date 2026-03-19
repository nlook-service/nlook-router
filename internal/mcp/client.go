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

// helper to build a simple object schema
func objSchema(props map[string]interface{}, required ...string) map[string]interface{} {
	s := map[string]interface{}{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func strProp(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": desc}
}

func numProp(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "number", "description": desc}
}

func boolProp(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "boolean", "description": desc}
}

func enumProp(desc string, values []string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": desc, "enum": values}
}

func arrProp(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": desc}
}

// GetChatTools returns all available tools for AI chat.
func GetChatTools() []Tool {
	return []Tool{
		// ── Documents ──
		{Name: "create_document", Description: "Create a new document/note", InputSchema: objSchema(map[string]interface{}{
			"title": strProp("Document title"), "content": strProp("Content in Markdown"),
			"tags": arrProp("Tags"), "is_public": boolProp("Public visibility"),
		}, "title", "content")},
		{Name: "list_documents", Description: "Search and list documents", InputSchema: objSchema(map[string]interface{}{
			"search": strProp("Search query"), "limit": numProp("Max results (default 10)"), "tags": strProp("Filter by tag"),
		})},
		{Name: "get_document", Description: "Get a document by ID", InputSchema: objSchema(map[string]interface{}{
			"id": numProp("Document ID"),
		}, "id")},
		{Name: "update_document", Description: "Update a document", InputSchema: objSchema(map[string]interface{}{
			"id": numProp("Document ID"), "title": strProp("New title"), "content": strProp("New content"),
		}, "id")},
		{Name: "delete_document", Description: "Delete a document", InputSchema: objSchema(map[string]interface{}{
			"id": numProp("Document ID"),
		}, "id")},
		{Name: "append_to_document", Description: "Append content to a document", InputSchema: objSchema(map[string]interface{}{
			"document_id": numProp("Document ID"), "title": strProp("Entry title"), "content": strProp("Content to append"),
		}, "document_id", "content")},

		// ── Tasks ──
		{Name: "create_task", Description: "Create a new task/todo", InputSchema: objSchema(map[string]interface{}{
			"title": strProp("Task title"), "notes": strProp("Task notes in Markdown"),
			"priority": enumProp("Priority", []string{"none", "low", "medium", "high"}),
			"due_date": strProp("Due date in ISO format"),
		}, "title")},
		{Name: "list_tasks", Description: "List tasks/todos with optional filters", InputSchema: objSchema(map[string]interface{}{
			"status": enumProp("Filter by status", []string{"pending", "in_progress", "completed"}),
			"limit":  numProp("Max results (default 10)"),
		})},
		{Name: "get_task", Description: "Get task details by ID", InputSchema: objSchema(map[string]interface{}{
			"id": numProp("Task ID"),
		}, "id")},
		{Name: "update_task", Description: "Update a task", InputSchema: objSchema(map[string]interface{}{
			"id": numProp("Task ID"), "title": strProp("New title"),
			"status":   enumProp("Status", []string{"pending", "in_progress", "completed"}),
			"priority": enumProp("Priority", []string{"none", "low", "medium", "high"}),
		}, "id")},
		{Name: "complete_task", Description: "Mark a task as completed", InputSchema: objSchema(map[string]interface{}{
			"id": numProp("Task ID"),
		}, "id")},
		{Name: "delete_task", Description: "Delete a task", InputSchema: objSchema(map[string]interface{}{
			"id": numProp("Task ID"),
		}, "id")},
		{Name: "append_to_task", Description: "Add a note/entry to a task", InputSchema: objSchema(map[string]interface{}{
			"task_id": numProp("Task ID"), "title": strProp("Entry title"), "content": strProp("Content"),
		}, "task_id", "content")},

		// ── Task Lists ──
		{Name: "list_task_lists", Description: "List all task lists", InputSchema: objSchema(map[string]interface{}{})},
		{Name: "create_task_list", Description: "Create a new task list", InputSchema: objSchema(map[string]interface{}{
			"name": strProp("Task list name"),
		}, "name")},

		// ── Workspaces ──
		{Name: "list_workspaces", Description: "List all workspaces/collections", InputSchema: objSchema(map[string]interface{}{})},
		{Name: "get_workspace", Description: "Get workspace details", InputSchema: objSchema(map[string]interface{}{
			"id": numProp("Workspace ID"),
		}, "id")},
		{Name: "add_document_to_workspace", Description: "Add a document to a workspace/collection", InputSchema: objSchema(map[string]interface{}{
			"workspace_id": numProp("Workspace/Collection ID"),
			"document_id":  numProp("Document ID to add"),
		}, "workspace_id", "document_id")},

		// ── Notifications ──
		{Name: "send_notification", Description: "Send a notification to yourself", InputSchema: objSchema(map[string]interface{}{
			"title":   strProp("Notification title"),
			"message": strProp("Notification message"),
		}, "title", "message")},

		// ── Workflows ──
		{Name: "list_workflows", Description: "List all workflows", InputSchema: objSchema(map[string]interface{}{})},
		{Name: "get_workflow", Description: "Get workflow details", InputSchema: objSchema(map[string]interface{}{
			"id": numProp("Workflow ID"),
		}, "id")},

		// ── Agents ──
		{Name: "list_agents", Description: "List all AI agents", InputSchema: objSchema(map[string]interface{}{})},
		{Name: "get_agent", Description: "Get agent details", InputSchema: objSchema(map[string]interface{}{
			"id": numProp("Agent ID"),
		}, "id")},

		// ── Skills ──
		{Name: "list_skills", Description: "List all skills", InputSchema: objSchema(map[string]interface{}{})},

		// ── Runs ──
		{Name: "list_runs", Description: "List workflow runs", InputSchema: objSchema(map[string]interface{}{
			"limit": numProp("Max results"),
		})},

		// ── Schedules ──
		{Name: "list_schedules", Description: "List workflow schedules", InputSchema: objSchema(map[string]interface{}{})},

		// ── Admin / Stats ──
		{Name: "admin_get_summary", Description: "Get admin summary statistics (documents, tasks, users counts)", InputSchema: objSchema(map[string]interface{}{})},
	}
}

// CallTool executes a tool by calling the nlook REST API.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	// Documents
	case "create_document":
		return c.post(ctx, "/api/v1/public/documents", args)
	case "list_documents":
		return c.get(ctx, "/api/v1/public/documents"+buildQuery(args, "search", "limit", "tags"))
	case "get_document":
		return c.get(ctx, fmt.Sprintf("/api/v1/public/documents/%v", args["id"]))
	case "update_document":
		id := args["id"]
		delete(args, "id")
		return c.put(ctx, fmt.Sprintf("/api/v1/public/documents/%v", id), args)
	case "delete_document":
		return c.del(ctx, fmt.Sprintf("/api/v1/public/documents/%v", args["id"]))
	case "append_to_document":
		docID := args["document_id"]
		delete(args, "document_id")
		return c.post(ctx, fmt.Sprintf("/api/v1/public/documents/%v/entries", docID), args)

	// Tasks
	case "create_task":
		return c.post(ctx, "/api/v1/public/tasks", args)
	case "list_tasks":
		return c.get(ctx, "/api/v1/public/tasks"+buildQuery(args, "status", "limit"))
	case "get_task":
		return c.get(ctx, fmt.Sprintf("/api/v1/public/tasks/%v", args["id"]))
	case "update_task":
		id := args["id"]
		delete(args, "id")
		return c.put(ctx, fmt.Sprintf("/api/v1/public/tasks/%v", id), args)
	case "complete_task":
		return c.put(ctx, fmt.Sprintf("/api/v1/public/tasks/%v", args["id"]), map[string]interface{}{"status": "completed"})
	case "delete_task":
		return c.del(ctx, fmt.Sprintf("/api/v1/public/tasks/%v", args["id"]))
	case "append_to_task":
		taskID := args["task_id"]
		delete(args, "task_id")
		return c.post(ctx, fmt.Sprintf("/api/v1/public/tasks/%v/entries", taskID), args)

	// Task Lists
	case "list_task_lists":
		return c.get(ctx, "/api/v1/public/task-lists")
	case "create_task_list":
		return c.post(ctx, "/api/v1/public/task-lists", args)

	// Workspaces
	case "list_workspaces":
		return c.get(ctx, "/api/v1/public/collections")
	case "get_workspace":
		return c.get(ctx, fmt.Sprintf("/api/v1/public/collections/%v", args["id"]))
	case "add_document_to_workspace":
		wsID := args["workspace_id"]
		delete(args, "workspace_id")
		return c.post(ctx, fmt.Sprintf("/api/v1/public/collections/%v/documents", wsID), args)

	// Notifications
	case "send_notification":
		return c.post(ctx, "/api/v1/public/notifications/self", args)

	// Workflows
	case "list_workflows":
		return c.get(ctx, "/api/v1/public/workflows")
	case "get_workflow":
		return c.get(ctx, fmt.Sprintf("/api/v1/public/workflows/%v", args["id"]))

	// Agents
	case "list_agents":
		return c.get(ctx, "/api/v1/public/agents")
	case "get_agent":
		return c.get(ctx, fmt.Sprintf("/api/v1/public/agents/%v", args["id"]))

	// Skills
	case "list_skills":
		return c.get(ctx, "/api/v1/public/skills")

	// Runs
	case "list_runs":
		return c.get(ctx, "/api/v1/public/workflow-runs"+buildQuery(args, "limit"))

	// Schedules
	case "list_schedules":
		return c.get(ctx, "/api/v1/public/workflow-schedules")

	// Admin
	case "admin_get_summary":
		return c.get(ctx, "/api/v1/public/admin/summary")

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

func (c *Client) put(ctx context.Context, path string, body interface{}) (interface{}, error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+path, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.doRequest(req)
}

func (c *Client) del(ctx context.Context, path string) (interface{}, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
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
