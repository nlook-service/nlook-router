package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/nlook-service/nlook-router/internal/mcp"
)

// Intent represents a detected user intent.
type Intent struct {
	Action string // "list_tasks", "list_documents", "create_task", etc.
	Query  string // original query for context
}

// DetectIntent analyzes user message and returns an actionable intent.
// Returns nil if no clear intent is detected (general conversation).
func DetectIntent(query string) *Intent {
	q := strings.ToLower(query)

	// Task-related
	taskKeywords := []string{"할일", "할 일", "todo", "task", "일정", "작업", "해야 할"}
	listKeywords := []string{"뭐", "목록", "리스트", "보여", "확인", "조회", "알려", "있어", "있지", "list", "show", "what"}
	createKeywords := []string{"등록", "추가", "만들", "생성", "create", "add", "new"}

	isTask := containsAny(q, taskKeywords)
	isList := containsAny(q, listKeywords)
	isCreate := containsAny(q, createKeywords)

	// Document-related
	docKeywords := []string{"문서", "글", "노트", "메모", "document", "doc", "note", "작성한"}
	isDoc := containsAny(q, docKeywords)

	// Workspace
	wsKeywords := []string{"워크스페이스", "workspace", "컬렉션", "프로젝트"}
	isWS := containsAny(q, wsKeywords)

	// Agent/Workflow
	agentKeywords := []string{"에이전트", "agent", "워크플로우", "workflow"}
	isAgent := containsAny(q, agentKeywords)

	// Determine intent
	if isTask && (isList || !isCreate) {
		return &Intent{Action: "list_tasks", Query: query}
	}
	if isTask && isCreate {
		return &Intent{Action: "create_task", Query: query}
	}
	if isDoc && (isList || !isCreate) {
		return &Intent{Action: "list_documents", Query: query}
	}
	if isDoc && isCreate {
		return &Intent{Action: "create_document", Query: query}
	}
	if isWS {
		return &Intent{Action: "list_workspaces", Query: query}
	}
	if isAgent {
		return &Intent{Action: "list_agents", Query: query}
	}

	return nil
}

// ExecuteIntent calls MCP tool directly based on detected intent.
// Returns formatted result string, or empty if intent is nil or execution fails.
func ExecuteIntent(ctx context.Context, intent *Intent, mcpClient *mcp.Client) string {
	if intent == nil || mcpClient == nil {
		return ""
	}

	log.Printf("intent: executing %s", intent.Action)

	var result interface{}
	var err error

	switch intent.Action {
	case "list_tasks":
		result, err = mcpClient.CallTool(ctx, "list_tasks", map[string]interface{}{"limit": 20})
	case "list_documents":
		result, err = mcpClient.CallTool(ctx, "list_documents", map[string]interface{}{"limit": 20})
	case "list_workspaces":
		result, err = mcpClient.CallTool(ctx, "list_workspaces", map[string]interface{}{})
	case "list_agents":
		result, err = mcpClient.CallTool(ctx, "list_agents", map[string]interface{}{})
	default:
		return ""
	}

	if err != nil {
		log.Printf("intent: MCP call failed: %v", err)
		return fmt.Sprintf("[도구 호출 오류: %v]", err)
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	return string(data)
}

func containsAny(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}
