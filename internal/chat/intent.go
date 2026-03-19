package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/nlook-service/nlook-router/internal/mcp"
)

// Executor is a tool executor interface (matches tools.Executor).
type Executor interface {
	Execute(ctx context.Context, name string, args map[string]interface{}) ([]byte, error)
}

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
	taskKeywords := []string{"할일", "할 일", "todo", "task", "일정", "해야 할"}
	createKeywords := []string{"추가해줘", "만들어줘", "생성해줘", "등록해줘", "create ", "add ", "새로운"}

	isTask := containsAny(q, taskKeywords)
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

	// Question markers → always list, never create
	questionMarkers := []string{"뭐", "있지", "있어", "알려", "보여", "어떤", "몇", "what", "show", "which", "?"}
	isQuestion := containsAny(q, questionMarkers)

	// Determine intent (order matters: specific first)
	if isWS {
		return &Intent{Action: "list_workspaces", Query: query}
	}
	if isAgent {
		return &Intent{Action: "list_agents", Query: query}
	}
	if isTask && isCreate && !isQuestion {
		// Don't create directly — fetch workspaces and ask user to confirm
		return &Intent{Action: "confirm_create_task", Query: query}
	}
	if isTask {
		return &Intent{Action: "list_tasks", Query: query}
	}
	if isDoc && isCreate && !isQuestion {
		return &Intent{Action: "confirm_create_document", Query: query}
	}
	if isDoc {
		return &Intent{Action: "list_documents", Query: query}
	}

	// Web search
	searchKeywords := []string{"날씨", "검색", "찾아", "뉴스", "weather", "search", "find", "google", "최신", "현재"}
	if containsAny(q, searchKeywords) {
		return &Intent{Action: "web_search", Query: query}
	}

	// Calculator
	calcKeywords := []string{"계산", "calculate", "몇", "합계", "평균"}
	if containsAny(q, calcKeywords) {
		return &Intent{Action: "calculator", Query: query}
	}

	return nil
}

// ExecuteIntent calls MCP tool or built-in tool based on detected intent.
func ExecuteIntent(ctx context.Context, intent *Intent, mcpClient *mcp.Client, toolExec Executor) string {
	if intent == nil {
		return ""
	}

	traceID, _ := ctx.Value("trace_id").(string)
	tlog := func(format string, args ...interface{}) {
		log.Printf("[%s] "+format, append([]interface{}{traceID}, args...)...)
	}

	// Built-in tool execution (web_search, calculator, etc.)
	if intent.Action == "web_search" || intent.Action == "calculator" {
		if toolExec == nil {
			tlog("intent: ⚠ no tool executor for %s", intent.Action)
			return ""
		}
		tlog("intent: 🔧 calling built-in tool: %s", intent.Action)
		result, err := toolExec.Execute(ctx, intent.Action, map[string]interface{}{"query": intent.Query})
		if err != nil {
			tlog("intent: ✗ tool exec failed: %v", err)
			return fmt.Sprintf("[도구 오류: %v]", err)
		}
		tlog("intent: ✓ tool result size=%d bytes", len(result))
		resultStr := string(result)
		if len(resultStr) > 3000 {
			resultStr = resultStr[:3000] + "..."
		}
		return resultStr
	}

	if mcpClient == nil {
		return ""
	}

	var result interface{}
	var err error

	tlog("intent: %s → %s", intent.Action, truncateStr(intent.Query, 60))

	switch intent.Action {
	case "list_tasks":
		tlog("intent: 📋 calling list_tasks (pending, limit 10)")
		result, err = mcpClient.CallTool(ctx, "list_tasks", map[string]interface{}{"status": "pending", "limit": 10})
	case "list_documents":
		tlog("intent: 📄 calling list_documents (limit 10)")
		result, err = mcpClient.CallTool(ctx, "list_documents", map[string]interface{}{"limit": 10})
	case "confirm_create_task", "confirm_create_document":
		tlog("intent: 📋 fetching workspaces for confirmation")
		wsResult, wsErr := mcpClient.CallTool(ctx, "list_workspaces", map[string]interface{}{})
		if wsErr != nil {
			tlog("intent: ✗ workspace fetch failed: %v", wsErr)
		}
		wsData, _ := json.Marshal(wsResult)
		itemType := "할일"
		if intent.Action == "confirm_create_document" {
			itemType = "문서"
		}
		return fmt.Sprintf("[사용자가 %s 생성을 요청함. 아래 workspace 목록을 보여주고, 어떤 workspace에 저장할지 물어보세요. 직접 생성하지 마세요.]\n\nWorkspace 목록:\n%s", itemType, truncateStr(string(wsData), 2000))
	case "list_workspaces":
		result, err = mcpClient.CallTool(ctx, "list_workspaces", map[string]interface{}{})
	case "list_agents":
		result, err = mcpClient.CallTool(ctx, "list_agents", map[string]interface{}{})
	default:
		return ""
	}

	if err != nil {
		tlog("intent: ✗ MCP call failed: %v", err)
		return fmt.Sprintf("[도구 호출 오류: %v]", err)
	}

	data, _ := json.MarshalIndent(result, "", "  ")
	tlog("intent: ✓ result size=%d bytes", len(data))

	// Truncate large results to prevent slow LLM processing
	const maxResultSize = 1500
	resultStr := string(data)
	if len(resultStr) > maxResultSize {
		resultStr = resultStr[:maxResultSize] + "\n... (truncated, showing first items)"
		tlog("intent: ⚠ truncated from %d to %d bytes", len(data), maxResultSize)
	}
	return resultStr
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
	traceID, _ := ctx.Value("trace_id").(string)
	refLog := func(format string, args ...interface{}) {
		log.Printf("[%s] "+format, append([]interface{}{traceID}, args...)...)
	}
	_ = refLog

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
		searchStr := "[" + p.prefix
		refLog("ref: searching for %s in query (len=%d)", searchStr, len(query))
		idx := 0
		for {
			start := strings.Index(query[idx:], searchStr)
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
			inner := query[start+len(searchStr) : end] // "123:title"
			refLog("ref: parsed inner=%s", truncateStr(inner, 50))
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

			refLog("ref: extracted id=%.0f", id)
			if id > 0 {
				result, err := mcpClient.CallTool(ctx, p.tool, map[string]interface{}{"id": id})
				if err != nil {
					refLog("ref: ✗ %s failed: %v", p.tool, err)
				}
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
					refLog("ref: fetched %s id=%.0f", p.tool, id)
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

	// Also match #ID pattern (e.g. "#124554052835 문서 분석해줘")
	{
		re := regexp.MustCompile(`#(\d{6,})`)
		matches := re.FindAllStringSubmatch(query, -1)
		for _, m := range matches {
			var id float64
			for _, c := range m[1] {
				if c >= '0' && c <= '9' {
					id = id*10 + float64(c-'0')
				}
			}
			if id > 0 {
				refLog("ref: trying #%.0f as document", id)
				// Try document first
				result, err := mcpClient.CallTool(ctx, "get_document", map[string]interface{}{"id": id})
				if err == nil {
					data, _ := json.MarshalIndent(result, "", "  ")
					if !found {
						sb.WriteString("\n\n[Referenced Content]\n")
						found = true
					}
					sb.WriteString(fmt.Sprintf("\n## Document #%.0f\n%s\n", id, string(data)))
					refLog("ref: fetched document #%.0f", id)
					continue
				}
				// Try task
				result, err = mcpClient.CallTool(ctx, "get_task", map[string]interface{}{"id": id})
				if err == nil {
					data, _ := json.MarshalIndent(result, "", "  ")
					if !found {
						sb.WriteString("\n\n[Referenced Content]\n")
						found = true
					}
					sb.WriteString(fmt.Sprintf("\n## Task #%.0f\n%s\n", id, string(data)))
					refLog("ref: fetched task #%.0f", id)
				}
			}
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
