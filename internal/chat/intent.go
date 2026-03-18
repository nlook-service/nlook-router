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

	log.Printf("intent: ▶ action=%s query=%s", intent.Action, truncateStr(intent.Query, 80))

	switch intent.Action {
	case "list_tasks":
		log.Printf("intent: 📋 calling list_tasks")
		result, err = mcpClient.CallTool(ctx, "list_tasks", map[string]interface{}{"limit": 20})
	case "list_documents":
		log.Printf("intent: 📄 calling list_documents")
		result, err = mcpClient.CallTool(ctx, "list_documents", map[string]interface{}{"limit": 20})
	case "create_task":
		log.Printf("intent: ✅ calling create_task: %s", intent.Query)
		result, err = mcpClient.CallTool(ctx, "create_task", map[string]interface{}{
			"title": extractTitle(intent.Query),
		})
	case "create_document":
		log.Printf("intent: 📝 calling create_document: %s", intent.Query)
		result, err = mcpClient.CallTool(ctx, "create_document", map[string]interface{}{
			"title":   extractTitle(intent.Query),
			"content": intent.Query,
		})
	case "list_workspaces":
		result, err = mcpClient.CallTool(ctx, "list_workspaces", map[string]interface{}{})
	case "list_agents":
		result, err = mcpClient.CallTool(ctx, "list_agents", map[string]interface{}{})
	default:
		return ""
	}

	if err != nil {
		log.Printf("intent: ✗ MCP call failed: %v", err)
		return fmt.Sprintf("[도구 호출 오류: %v]", err)
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	log.Printf("intent: ✓ result size=%d bytes", len(data))
	return string(data)
}

// extractTitle tries to extract a meaningful title from the query.
func extractTitle(query string) string {
	// Remove common prefixes
	prefixes := []string{
		"등록해줘", "등록", "추가해줘", "추가", "만들어줘", "만들어", "생성",
		"create", "add", "register",
	}
	title := query
	for _, p := range prefixes {
		title = strings.TrimSuffix(strings.TrimSpace(title), p)
		title = strings.TrimPrefix(strings.TrimSpace(title), p)
	}
	title = strings.TrimSpace(title)

	// Limit length
	if len(title) > 100 {
		title = title[:100]
	}
	if title == "" {
		title = query
		if len(title) > 50 {
			title = title[:50]
		}
	}
	return title
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// ExtractDocumentRefs finds [@document:ID:title] patterns and fetches full content via MCP.
func ExtractDocumentRefs(ctx context.Context, query string, mcpClient *mcp.Client) string {
	if mcpClient == nil {
		return ""
	}

	// Match [@document:123:title] or [@task:456:title]
	patterns := []struct {
		prefix string
		tool   string
		idKey  string
	}{
		{"@document:", "get_document", "id"},
		{"@task:", "get_task", "id"},
	}

	var sb strings.Builder
	found := false

	for _, p := range patterns {
		idx := 0
		for {
			start := strings.Index(query[idx:], "["+p.prefix)
			if start == -1 {
				break
			}
			start += idx
			end := strings.Index(query[start:], "]")
			if end == -1 {
				break
			}
			end += start

			// Parse: [@document:123:title]
			inner := query[start+2+len(p.prefix)-1 : end] // "123:title"
			parts := strings.SplitN(inner, ":", 2)
			if len(parts) < 1 {
				idx = end + 1
				continue
			}

			// Extract ID
			var id float64
			for _, c := range parts[0] {
				if c >= '0' && c <= '9' {
					id = id*10 + float64(c-'0')
				}
			}

			if id > 0 {
				result, err := mcpClient.CallTool(ctx, p.tool, map[string]interface{}{"id": id})
				if err == nil {
					data, _ := json.MarshalIndent(result, "", "  ")
					if !found {
						sb.WriteString("\n\n[Referenced Content]\n")
						found = true
					}
					title := ""
					if len(parts) > 1 {
						title = parts[1]
					}
					sb.WriteString(fmt.Sprintf("\n## %s\n%s\n", title, string(data)))
					log.Printf("intent: fetched %s id=%.0f", p.tool, id)
				}
			}
			idx = end + 1
		}
	}

	// Also check old format: [문서: title (ID:123)]
	oldPatterns := []struct {
		label string
		tool  string
	}{
		{"문서", "get_document"},
		{"할일", "get_task"},
	}
	for _, op := range oldPatterns {
		search := "[" + op.label + ":"
		idx := 0
		for {
			start := strings.Index(query[idx:], search)
			if start == -1 {
				break
			}
			start += idx
			end := strings.Index(query[start:], "]")
			if end == -1 {
				break
			}
			end += start

			// Find (ID:123)
			idStart := strings.Index(query[start:end], "(ID:")
			if idStart != -1 {
				idStr := query[start+idStart+4 : end-1]
				var id float64
				for _, c := range idStr {
					if c >= '0' && c <= '9' {
						id = id*10 + float64(c-'0')
					}
				}
				if id > 0 {
					result, err := mcpClient.CallTool(ctx, op.tool, map[string]interface{}{"id": id})
					if err == nil {
						data, _ := json.MarshalIndent(result, "", "  ")
						if !found {
							sb.WriteString("\n\n[Referenced Content]\n")
							found = true
						}
						sb.WriteString(fmt.Sprintf("\n%s\n", string(data)))
					}
				}
			}
			idx = end + 1
		}
	}

	if found {
		sb.WriteString("[End Referenced Content]\n")
	}
	return sb.String()
}

func containsAny(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}
