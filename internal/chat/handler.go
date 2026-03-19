package chat

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/nlook-service/nlook-router/internal/apiclient"
	"github.com/nlook-service/nlook-router/internal/cache"
	"github.com/nlook-service/nlook-router/internal/embedding"
	"github.com/nlook-service/nlook-router/internal/engine"
	"github.com/nlook-service/nlook-router/internal/gemini"
	"github.com/nlook-service/nlook-router/internal/llm"
	"github.com/nlook-service/nlook-router/internal/tools"
	"github.com/nlook-service/nlook-router/internal/mcp"
	"github.com/nlook-service/nlook-router/internal/memory"
	"github.com/nlook-service/nlook-router/internal/ollama"
	"github.com/nlook-service/nlook-router/internal/usage"
)

// WSMessage mirrors the WebSocket message format.
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ChatRequestPayload is sent from cloud to request AI chat processing.
// HistoryMessage is a previous message in the conversation.
type HistoryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequestPayload struct {
	ConversationID int64            `json:"conversation_id"`
	MessageID      int64            `json:"message_id"`
	Query          string           `json:"query"`
	UserID         int64            `json:"user_id"`
	Model          string           `json:"model,omitempty"`
	Lang           string           `json:"lang,omitempty"`
	History        []HistoryMessage `json:"history,omitempty"`
}

// ChatResponsePayload is sent back to cloud with the AI response.
type ChatResponsePayload struct {
	ConversationID int64   `json:"conversation_id"`
	MessageID      int64   `json:"message_id"`
	Content        string  `json:"content"`
	Model          string  `json:"model"`
	Role           string  `json:"role"`
	ElapsedMs      int64   `json:"elapsed_ms,omitempty"`
	TokensUsed     int     `json:"tokens_used,omitempty"`
}

// ChatDeltaPayload is sent for streaming token-by-token responses.
type ChatDeltaPayload struct {
	ConversationID int64   `json:"conversation_id"`
	MessageID      int64   `json:"message_id"`
	Delta          string  `json:"delta"`
	Done           bool    `json:"done"`
	FullContent    string  `json:"full_content,omitempty"`
	Model          string  `json:"model,omitempty"`
	ElapsedMs      int64   `json:"elapsed_ms,omitempty"`
	TokensUsed     int     `json:"tokens_used,omitempty"`
}

// ChatErrorPayload is sent when chat processing fails.
type ChatErrorPayload struct {
	ConversationID int64  `json:"conversation_id"`
	MessageID      int64  `json:"message_id"`
	Error          string `json:"error"`
}

// Handler processes chat-related WebSocket messages.
type Handler struct {
	skillRunner   *engine.SkillRunner
	mcpClient     *mcp.Client
	cacheStore    *cache.Store
	vectorStore   *embedding.VectorStore
	memoryStore   *memory.Store
	llmEngine     *llm.Engine
	toolExecutor  tools.Executor
	promptBuilder *PromptBuilder
	usageTracker  *usage.Tracker
	sendWS        func(msg []byte)
}

// SetLLMEngine sets the LLM engine (vLLM or Ollama).
func (h *Handler) SetLLMEngine(e *llm.Engine) {
	h.llmEngine = e
}

// SetToolExecutor sets the built-in tool executor (web_search, code_interpreter, etc.)
func (h *Handler) SetToolExecutor(e tools.Executor) {
	h.toolExecutor = e
}

// SetCacheStore sets the data cache for AI context.
func (h *Handler) SetCacheStore(store *cache.Store) {
	h.cacheStore = store
	h.rebuildPromptBuilder()
}

// SetVectorStore sets the embedding vector store for semantic search.
func (h *Handler) SetVectorStore(vs *embedding.VectorStore) {
	h.vectorStore = vs
	h.rebuildPromptBuilder()
}

// SetMemoryStore sets the long-term memory store.
func (h *Handler) SetMemoryStore(ms *memory.Store) {
	h.memoryStore = ms
	h.rebuildPromptBuilder()
}

func (h *Handler) rebuildPromptBuilder() {
	h.promptBuilder = NewPromptBuilder(h.cacheStore, h.vectorStore, h.memoryStore)
}

// NewHandler creates a new chat message handler.
// apiKey is from config (~/.nlook/config.yaml api_key).
func NewHandler(skillRunner *engine.SkillRunner, sendWS func(msg []byte), apiKey string, usageTracker *usage.Tracker) *Handler {
	// Create MCP client: config api_key → env NLOOK_API_KEY fallback
	if apiKey == "" {
		apiKey = os.Getenv("NLOOK_API_KEY")
	}
	var mcpClient *mcp.Client
	if apiKey != "" {
		mcpClient = mcp.NewClient(apiKey)
		log.Printf("chat: MCP client created with API key (len=%d)", len(apiKey))
	} else {
		log.Printf("chat: No API key — MCP tools disabled. Set api_key in ~/.nlook/config.yaml")
	}

	return &Handler{
		skillRunner:  skillRunner,
		mcpClient:    mcpClient,
		usageTracker: usageTracker,
		sendWS:       sendWS,
	}
}

// HandleMessage processes a chat-related WebSocket message.
func (h *Handler) HandleMessage(msgType string, payload json.RawMessage) bool {
	switch msgType {
	case "chat:request":
		h.handleChatRequest(payload)
		return true
	default:
		return false
	}
}

func genTraceID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *Handler) handleChatRequest(payload json.RawMessage) {
	var req ChatRequestPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		log.Printf("chat:request unmarshal error: %v", err)
		return
	}

	traceID := genTraceID()

	log.Printf("[%s] ═══ CHAT START ═══════════════════════════", traceID)
	log.Printf("[%s] query: %s", traceID, truncateLog(req.Query, 100))
	log.Printf("[%s] conv=%d msg=%d history=%d lang=%s", traceID, req.ConversationID, req.MessageID, len(req.History), req.Lang)
	log.Printf("[%s] mcp=%v engine=%v tools=%v", traceID, h.mcpClient != nil, h.llmEngine != nil, h.toolExecutor != nil)

	go func() {
		startTime := time.Now()
		ctx := context.WithValue(context.Background(), "trace_id", traceID)
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		resp, err := h.processChat(ctx, &req)
		elapsed := time.Since(startTime)
		if err != nil {
			log.Printf("[%s] ✗ ERROR %s: %v", traceID, elapsed.Round(time.Millisecond), err)
			log.Printf("[%s] ═══ CHAT END (error) ═════════════════════", traceID)
			h.sendResponse("chat:error", ChatErrorPayload{
				ConversationID: req.ConversationID,
				MessageID:      req.MessageID,
				Error:          err.Error(),
			})
			return
		}
		resp.ElapsedMs = elapsed.Milliseconds()
		if resp.TokensUsed == 0 {
			resp.TokensUsed = len(resp.Content) / 4 // rough estimate
		}
		log.Printf("[%s] ✓ DONE %s model=%s len=%d tokens≈%d", traceID, elapsed.Round(time.Millisecond), resp.Model, len(resp.Content), resp.TokensUsed)
		log.Printf("[%s] ═══ CHAT END ═════════════════════════════", traceID)
		h.sendResponse("chat:response", resp)
	}()
}

const baseSystemPrompt = `You are nlook AI assistant. You help users manage documents, tasks, and provide analysis.

When the user wants to:
- Create a document/note → use the create_document tool
- Create a task/todo → use the create_task tool
- Search documents → use the list_documents tool
- Check tasks → use the list_tasks tool
- See workspaces → use the list_workspaces tool

Intent classification:
1. If the message looks like content to save → ask if it's a document or task, suggest workspace
2. If asking about existing content → search and show results
3. If general question → answer directly without tools

IMPORTANT - Linking format:
When mentioning documents or tasks from tool results, ALWAYS use this link format:
- Documents: [@document:ID:title] (e.g. [@document:123:회의록])
- Tasks: [@task:ID:title] (e.g. [@task:456:버그 수정])
This creates clickable links in the UI. Never use plain text for referenced items.

Always confirm before creating/modifying content. Respond concisely.`

func (h *Handler) getSystemPrompt(lang string, query string, conversationID int64) string {
	if h.promptBuilder != nil {
		return h.promptBuilder.BuildSystemPrompt(lang, query, conversationID)
	}
	// Fallback if no prompt builder
	return baseSystemPrompt
}

func (h *Handler) getHistory(req *ChatRequestPayload) []ollama.MessageEntry {
	if h.promptBuilder != nil {
		return h.promptBuilder.BuildHistory(req.History)
	}
	return h.getOllamaHistory(req)
}

// detectLang detects language from text (simple heuristic).
func detectLang(text string) string {
	for _, r := range text {
		if r >= 0xAC00 && r <= 0xD7AF { // Hangul syllables
			return "ko"
		}
		if r >= 0x3131 && r <= 0x318E { // Hangul jamo
			return "ko"
		}
		if r >= 0x4E00 && r <= 0x9FFF { // CJK (Chinese)
			return "zh"
		}
		if r >= 0x3040 && r <= 0x30FF { // Japanese
			return "ja"
		}
	}
	return "en"
}

const recentMessageCount = 6 // Keep last N messages as full context

func (h *Handler) getOllamaHistory(req *ChatRequestPayload) []ollama.MessageEntry {
	history := req.History
	if len(history) <= recentMessageCount {
		entries := make([]ollama.MessageEntry, 0, len(history))
		for _, m := range history {
			entries = append(entries, ollama.MessageEntry{Role: m.Role, Content: stripThinking(m.Content)})
		}
		return entries
	}

	// Sliding window: summarize older messages + keep recent ones
	olderMessages := history[:len(history)-recentMessageCount]
	recentMessages := history[len(history)-recentMessageCount:]

	entries := make([]ollama.MessageEntry, 0, recentMessageCount+1)

	// Compress older messages into a summary
	summary := compressHistory(olderMessages)
	entries = append(entries, ollama.MessageEntry{
		Role:    "system",
		Content: summary,
	})

	// Add recent messages as-is
	for _, m := range recentMessages {
		entries = append(entries, ollama.MessageEntry{Role: m.Role, Content: m.Content})
	}
	return entries
}

// compressHistory creates a condensed summary of older conversation messages.
func compressHistory(messages []HistoryMessage) string {
	var sb strings.Builder
	sb.WriteString("[이전 대화 요약]\n")

	for _, m := range messages {
		role := "사용자"
		if m.Role == "assistant" {
			role = "AI"
		}
		content := m.Content
		// Truncate long messages
		if len(content) > 150 {
			content = content[:150] + "..."
		}
		// Remove newlines for compactness
		content = strings.ReplaceAll(content, "\n", " ")
		sb.WriteString("- " + role + ": " + content + "\n")
	}
	sb.WriteString("[이전 대화 끝]")
	return sb.String()
}

func (h *Handler) processChat(ctx context.Context, req *ChatRequestPayload) (*ChatResponsePayload, error) {
	model := req.Model
	if model == "" {
		model = os.Getenv("NLOOK_AI_MODEL")
	}

	traceID, _ := ctx.Value("trace_id").(string)
	tlog := func(format string, args ...interface{}) {
		log.Printf("[%s] "+format, append([]interface{}{traceID}, args...)...)
	}

	// Intent detection: directly call MCP tools without relying on model
	if h.mcpClient != nil {
		// 1. Fetch referenced document/task content (highest priority)
		refContent := ExtractDocumentRefs(ctx, req.Query, h.mcpClient)
		hasRefs := refContent != ""
		if hasRefs {
			tlog("ref: found document/task references")
			req.Query = req.Query + refContent + "\n\nAbove is the referenced content. Analyze and respond based on this data."
		}

		// 2. Auto-detect intent — skip if document refs already fetched
		if !hasRefs {
		if intent := DetectIntent(req.Query); intent != nil {
			// Send immediate feedback: "데이터 조회 중..."
			h.sendResponse("chat:delta", ChatDeltaPayload{
				ConversationID: req.ConversationID,
				MessageID:      req.MessageID,
				Delta:          "🔍 ",
				Done:           false,
			})

			toolResult := ExecuteIntent(ctx, intent, h.mcpClient, h.toolExecutor)
			if toolResult != "" {
				req.Query = fmt.Sprintf("%s\n\n[Tool Result: %s]\n%s\n[End Tool Result]\n\nAbove is the data. Present it clearly and concisely to the user.", req.Query, intent.Action, toolResult)
			}
		}
		} // end !hasRefs
	}

	// qwen classifies every query (tracked in usage stats), all responses via Claude
	localModel := h.findLocalModel(ctx, model)
	complexity := h.classifyWithQwen(ctx, req.Query, localModel, tlog)

	switch complexity {
	default:
		// noop — fall through to Claude
	}

	// All responses: Claude CLI → Gemini → Ollama (qwen = classifier only, not responder)
	claudePath := findClaude()
	if claudePath != "" {
		tlog("route: Claude Code CLI (%s)", complexity)
		resp, err := h.processChatClaudeCLI(ctx, req, claudePath, complexity)
		if err == nil {
			return resp, nil
		}
		tlog("route: Claude CLI failed: %v, trying Gemini", err)
	}

	geminiClient := gemini.NewClient()
	if geminiClient != nil {
		tlog("route: Gemini Cloud (%s)", geminiClient.Model())
		resp, err := h.processChatGemini(ctx, req, geminiClient)
		if err == nil {
			return resp, nil
		}
		tlog("route: Gemini failed: %v", err)
	}

	// Last resort: local model
	if localModel != "" {
		tlog("route: local %s (last resort)", localModel)
		return h.processChatOllama(ctx, req, localModel)
	}

	return nil, fmt.Errorf("no LLM available: configure Claude Code CLI, Gemini, or Ollama")
}

// processChatWithTools uses Anthropic API with tool_use for MCP integration.
func (h *Handler) processChatWithTools(ctx context.Context, req *ChatRequestPayload, model string) (*ChatResponsePayload, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return h.processChatSimple(ctx, req, model)
	}

	tools := mcp.GetChatTools()
	anthropicTools := make([]map[string]interface{}, len(tools))
	for i, t := range tools {
		anthropicTools[i] = map[string]interface{}{
			"name":         t.Name,
			"description":  t.Description,
			"input_schema": t.InputSchema,
		}
	}

	messages := []map[string]interface{}{
		{"role": "user", "content": req.Query},
	}

	// First LLM call
	respBody, err := h.callAnthropic(ctx, apiKey, model, h.getSystemPrompt(req.Lang, req.Query, req.ConversationID), messages, anthropicTools)
	if err != nil {
		return nil, err
	}

	// Check if tool_use is requested
	var toolResults []map[string]interface{}
	var textParts []string

	for _, block := range respBody.Content {
		switch block.Type {
		case "text":
			textParts = append(textParts, block.Text)
		case "tool_use":
			log.Printf("chat: tool_use name=%s", block.Name)
			result, toolErr := h.mcpClient.CallTool(ctx, block.Name, block.Input)
			resultStr := ""
			if toolErr != nil {
				resultStr = fmt.Sprintf("Error: %v", toolErr)
			} else {
				resultBytes, _ := json.Marshal(result)
				resultStr = string(resultBytes)
			}
			toolResults = append(toolResults, map[string]interface{}{
				"type":        "tool_result",
				"tool_use_id": block.ID,
				"content":     resultStr,
			})
		}
	}

	// If tools were used, do a follow-up call with results
	if len(toolResults) > 0 {
		// Add assistant message with tool_use blocks
		assistantContent := make([]map[string]interface{}, 0)
		for _, block := range respBody.Content {
			switch block.Type {
			case "text":
				assistantContent = append(assistantContent, map[string]interface{}{
					"type": "text", "text": block.Text,
				})
			case "tool_use":
				assistantContent = append(assistantContent, map[string]interface{}{
					"type": "tool_use", "id": block.ID, "name": block.Name, "input": block.Input,
				})
			}
		}

		messages = append(messages,
			map[string]interface{}{"role": "assistant", "content": assistantContent},
			map[string]interface{}{"role": "user", "content": toolResults},
		)

		// Second call without tools (get final text response)
		finalResp, err := h.callAnthropic(ctx, apiKey, model, h.getSystemPrompt(req.Lang, req.Query, req.ConversationID), messages, nil)
		if err != nil {
			return nil, fmt.Errorf("follow-up LLM call: %w", err)
		}

		textParts = nil
		for _, block := range finalResp.Content {
			if block.Type == "text" {
				textParts = append(textParts, block.Text)
			}
		}
	}

	content := strings.Join(textParts, "\n")
	return &ChatResponsePayload{
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
		Content:        content,
		Model:          model,
		Role:           "assistant",
	}, nil
}

// processChatVLLM uses vLLM with OpenAI-compatible streaming.
func (h *Handler) processChatVLLM(ctx context.Context, req *ChatRequestPayload, model string) (*ChatResponsePayload, error) {
	systemPrompt := h.getSystemPrompt(req.Lang, req.Query, req.ConversationID)

	// Build messages with history
	messages := make([]map[string]string, 0)
	for _, m := range req.History {
		messages = append(messages, map[string]string{"role": m.Role, "content": m.Content})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.Query})

	fullText, err := h.llmEngine.ChatStream(ctx, model, systemPrompt, messages,
		func(text string) {
			h.sendResponse("chat:delta", ChatDeltaPayload{
				ConversationID: req.ConversationID,
				MessageID:      req.MessageID,
				Delta:          text,
				Done:           false,
			})
		},
	)
	if err != nil {
		return nil, fmt.Errorf("vLLM chat: %w", err)
	}

	return &ChatResponsePayload{
		ConversationID: req.ConversationID, MessageID: req.MessageID,
		Content: fullText, Model: model, Role: "assistant",
	}, nil
}

// processChatOllama uses local Ollama with tool calling + streaming.
func (h *Handler) processChatOllama(ctx context.Context, req *ChatRequestPayload, model string) (*ChatResponsePayload, error) {
	ollamaClient := ollama.NewClient()
	systemPrompt := h.getSystemPrompt(req.Lang, req.Query, req.ConversationID)

	// If MCP client available, try tool calling first (non-streaming)
	if h.mcpClient != nil {
		tools := mcp.GetChatTools()
		ollamaTools := make([]map[string]interface{}, len(tools))
		for i, t := range tools {
			ollamaTools[i] = map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.InputSchema,
				},
			}
		}

		resp, err := ollamaClient.ChatWithTools(ctx, model, systemPrompt, req.Query, ollamaTools, h.getOllamaHistory(req))
		if err == nil && len(resp.ToolCalls) > 0 {
			// Execute tool calls
			toolResults := make([]map[string]interface{}, 0)
			for _, tc := range resp.ToolCalls {
				log.Printf("chat: ollama tool_call name=%s", tc.Function.Name)
				result, toolErr := h.mcpClient.CallTool(ctx, tc.Function.Name, tc.Function.Arguments)
				content := ""
				if toolErr != nil {
					content = fmt.Sprintf("Error: %v", toolErr)
				} else {
					resultBytes, _ := json.Marshal(result)
					content = string(resultBytes)
				}
				toolResults = append(toolResults, map[string]interface{}{"content": content})
			}

			// Send tool results back to model for final streaming response
			toolStart := time.Now()
			fullText, inTok, outTok, err := ollamaClient.ChatWithToolResults(ctx, model, systemPrompt, req.Query, resp.ToolCalls, toolResults,
				func(text string) {
					h.sendResponse("chat:delta", ChatDeltaPayload{
						ConversationID: req.ConversationID,
						MessageID:      req.MessageID,
						Delta:          text,
						Done:           false,
					})
				},
			)
			if err == nil {
				fullText = stripThinking(fullText)
				if h.usageTracker != nil {
					h.usageTracker.Record(usage.TokenUsage{
						UserID: req.UserID, Provider: "ollama", Model: model, Category: "chat",
						InputTokens: inTok, OutputTokens: outTok, ElapsedMs: time.Since(toolStart).Milliseconds(),
					})
				}
				return &ChatResponsePayload{
					ConversationID: req.ConversationID, MessageID: req.MessageID,
					Content: fullText, Model: model, Role: "assistant",
					TokensUsed: inTok + outTok,
				}, nil
			}
			log.Printf("chat: tool result follow-up failed: %v, falling back", err)
		}

		// If tool calling returned text without tools, use it
		if err == nil && resp.Content != "" {
			cleaned := stripThinking(resp.Content)
			h.sendResponse("chat:response", ChatResponsePayload{
				ConversationID: req.ConversationID, MessageID: req.MessageID,
				Content: cleaned, Model: model, Role: "assistant",
			})
			return &ChatResponsePayload{
				ConversationID: req.ConversationID, MessageID: req.MessageID,
				Content: cleaned, Model: model, Role: "assistant",
			}, nil
		}
	}

	// Fallback: simple streaming chat without tools
	streamStart := time.Now()
	fullText, inTok, outTok, err := ollamaClient.ChatStream(ctx, model, systemPrompt, req.Query,
		ollama.ChatOptions{Temperature: 0.7, MaxTokens: 4096, History: h.getOllamaHistory(req)},
		func(text string) {
			h.sendResponse("chat:delta", ChatDeltaPayload{
				ConversationID: req.ConversationID,
				MessageID:      req.MessageID,
				Delta:          text,
				Done:           false,
			})
		},
	)
	if err != nil {
		return nil, fmt.Errorf("ollama chat: %w", err)
	}

	if h.usageTracker != nil {
		h.usageTracker.Record(usage.TokenUsage{
			UserID: req.UserID, Provider: "ollama", Model: model, Category: "chat",
			InputTokens: inTok, OutputTokens: outTok, ElapsedMs: time.Since(streamStart).Milliseconds(),
		})
	}

	fullText = stripThinking(fullText)

	return &ChatResponsePayload{
		ConversationID: req.ConversationID, MessageID: req.MessageID,
		Content: fullText, Model: model, Role: "assistant",
		TokensUsed: inTok + outTok,
	}, nil
}

// processChatSimple uses skill_runner for non-Claude models (no streaming).
func (h *Handler) processChatSimple(ctx context.Context, req *ChatRequestPayload, model string) (*ChatResponsePayload, error) {
	output, _, err := h.skillRunner.RunSkill(ctx,
		&apiclient.WorkflowSkill{Name: "chat", SkillType: "prompt", Content: req.Query},
		&apiclient.WorkflowAgent{Model: model, SystemPrompt: h.getSystemPrompt(req.Lang, req.Query, req.ConversationID), MaxTokens: 4096, Temperature: 0.7},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("LLM call: %w", err)
	}

	content, _ := output["text"].(string)
	usedModel, _ := output["model"].(string)
	if usedModel == "" {
		usedModel = model
	}

	return &ChatResponsePayload{
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
		Content:        content,
		Model:          usedModel,
		Role:           "assistant",
	}, nil
}

// processChatStream sends streaming deltas via WebSocket, returns final response.
func (h *Handler) processChatStream(ctx context.Context, req *ChatRequestPayload, model string) (*ChatResponsePayload, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return h.processChatSimple(ctx, req, model)
	}

	reqBody := anthropicRequest{
		Model:       model,
		MaxTokens:   4096,
		System:      h.getSystemPrompt(req.Lang, req.Query, req.ConversationID),
		Messages:    []map[string]interface{}{{"role": "user", "content": req.Query}},
		Temperature: 0.7,
		Stream:      true,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Accept", "text/event-stream")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("API call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: status=%d body=%s", resp.StatusCode, truncate(string(respBody), 500))
	}

	// Check if response is actually streaming (SSE)
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		// Non-streaming response, parse normally
		respBody, _ := io.ReadAll(resp.Body)
		var result anthropicResponse
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w", err)
		}
		text := ""
		for _, block := range result.Content {
			if block.Type == "text" {
				text += block.Text
			}
		}
		return &ChatResponsePayload{
			ConversationID: req.ConversationID,
			MessageID:      req.MessageID,
			Content:        text,
			Model:          model,
			Role:           "assistant",
		}, nil
	}

	// Parse SSE stream
	var fullContent strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var event struct {
			Type  string `json:"type"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" && event.Delta.Text != "" {
			fullContent.WriteString(event.Delta.Text)
			h.sendResponse("chat:delta", ChatDeltaPayload{
				ConversationID: req.ConversationID,
				MessageID:      req.MessageID,
				Delta:          event.Delta.Text,
				Done:           false,
			})
		}
	}

	// Send final done signal
	finalContent := fullContent.String()
	h.sendResponse("chat:delta", ChatDeltaPayload{
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
		Delta:          "",
		Done:           true,
		FullContent:    finalContent,
		Model:          model,
	})

	return &ChatResponsePayload{
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
		Content:        finalContent,
		Model:          model,
		Role:           "assistant",
	}, nil
}

// Anthropic API types

type anthropicRequest struct {
	Model       string                   `json:"model"`
	MaxTokens   int                      `json:"max_tokens"`
	System      string                   `json:"system,omitempty"`
	Messages    []map[string]interface{} `json:"messages"`
	Tools       []map[string]interface{} `json:"tools,omitempty"`
	Temperature float64                  `json:"temperature,omitempty"`
	Stream      bool                     `json:"stream,omitempty"`
}

type anthropicContentBlock struct {
	Type  string                 `json:"type"`
	Text  string                 `json:"text,omitempty"`
	ID    string                 `json:"id,omitempty"`
	Name  string                 `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
}

type anthropicResponse struct {
	Content    []anthropicContentBlock `json:"content"`
	Model      string                 `json:"model"`
	StopReason string                 `json:"stop_reason"`
}

func (h *Handler) callAnthropic(ctx context.Context, apiKey, model, system string, messages []map[string]interface{}, tools []map[string]interface{}) (*anthropicResponse, error) {
	reqBody := anthropicRequest{
		Model:       model,
		MaxTokens:   4096,
		System:      system,
		Messages:    messages,
		Tools:       tools,
		Temperature: 0.7,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("API error: status=%d body=%s", resp.StatusCode, truncate(string(respBody), 500))
	}

	var result anthropicResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	return &result, nil
}

func (h *Handler) sendResponse(msgType string, payload interface{}) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		log.Printf("chat: marshal error: %v", err)
		return
	}
	msg, err := json.Marshal(WSMessage{Type: msgType, Payload: payloadBytes})
	if err != nil {
		log.Printf("chat: marshal ws message error: %v", err)
		return
	}
	log.Printf("ws_send: type=%s len=%d payload=%s", msgType, len(msg), string(payloadBytes))
	h.sendWS(msg)
}

// stripThinking removes <think>...</think> blocks and any stray tags from LLM output.
var thinkingRe = regexp.MustCompile(`(?s)<think>.*?</think>`)
var strayThinkTagRe = regexp.MustCompile(`</?think>`)

func stripThinking(s string) string {
	s = thinkingRe.ReplaceAllString(s, "")
	s = strayThinkTagRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// findClaude returns the path to claude CLI, checking common locations.
func findClaude() string {
	// Check PATH first
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}
	// Check common install locations
	paths := []string{
		os.Getenv("HOME") + "/.local/bin/claude",
		"/usr/local/bin/claude",
		"/opt/homebrew/bin/claude",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// processChatClaudeCLI uses Claude Code CLI (for Max subscribers).
func (h *Handler) processChatClaudeCLI(ctx context.Context, req *ChatRequestPayload, claudePath string, complexity ...string) (*ChatResponsePayload, error) {
	systemPrompt := h.getSystemPrompt(req.Lang, req.Query, req.ConversationID)

	// Build prompt with system instructions + history + query
	var prompt strings.Builder
	prompt.WriteString(systemPrompt + "\n\n")
	// Language instruction
	lang := req.Lang
	if lang == "" || lang == "en" {
		// Detect from query
		for _, r := range req.Query {
			if r >= 0xAC00 && r <= 0xD7AF {
				lang = "ko"
				break
			}
		}
	}
	if lang == "ko" {
		prompt.WriteString("반드시 한국어로 응답하세요.\n\n")
	}
	// Adjust response style based on complexity
	cplx := "complex"
	if len(complexity) > 0 && complexity[0] != "" {
		cplx = complexity[0]
	}
	if cplx == "simple" {
		prompt.WriteString("응답 규칙:\n")
		prompt.WriteString("- 1-2문장으로 짧고 간결하게\n")
		prompt.WriteString("- 불필요한 설명 없이 핵심만\n")
		prompt.WriteString("- thinking/추론 과정은 절대 출력하지 마세요\n\n")
	} else {
		prompt.WriteString("응답 규칙:\n")
		prompt.WriteString("- 마크다운으로 깔끔하게 포맷팅\n")
		prompt.WriteString("- 목록은 번호/불릿 사용\n")
		prompt.WriteString("- 핵심만 간결하게\n")
		prompt.WriteString("- thinking/추론 과정은 절대 출력하지 마세요\n\n")
	}

	if len(req.History) > 0 {
		prompt.WriteString("[대화 기록]\n")
		for _, m := range req.History {
			role := "사용자"
			if m.Role == "assistant" {
				role = "AI"
			}
			content := m.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			prompt.WriteString(role + ": " + content + "\n")
		}
		prompt.WriteString("[대화 기록 끝]\n\n")
	}
	prompt.WriteString(req.Query)

	cliStart := time.Now()
	cmd := exec.CommandContext(ctx, claudePath, "-p", prompt.String(), "--model", "claude-haiku-4-5-20251001", "--output-format", "stream-json", "--verbose")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claude CLI pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claude CLI start: %w", err)
	}

	var inTok, outTok int
	var lastText string // track cumulative text to compute deltas
	model := "claude-haiku-4-5 (CLI)"

	// Stream tokens as they arrive — each "assistant" event has cumulative text
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Text()
		var event struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
			Result string `json:"result"`
			Usage  struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		switch event.Type {
		case "assistant":
			// Content is cumulative — compute delta from last seen text
			for _, c := range event.Message.Content {
				if c.Type == "text" && c.Text != "" {
					if len(c.Text) > len(lastText) {
						delta := c.Text[len(lastText):]
						// Simulate streaming: send in small chunks
						for i := 0; i < len(delta); {
							// Send ~20-40 chars per chunk (word boundary)
							end := i + 30
							if end > len(delta) {
								end = len(delta)
							} else {
								// Find next space/newline for clean break
								for end < len(delta) && delta[end] != ' ' && delta[end] != '\n' {
									end++
								}
							}
							chunk := delta[i:end]
							h.sendResponse("chat:delta", ChatDeltaPayload{
								ConversationID: req.ConversationID,
								MessageID:      req.MessageID,
								Delta:          chunk,
								Done:           false,
							})
							i = end
							time.Sleep(30 * time.Millisecond)
						}
						lastText = c.Text
					}
				}
			}
		case "result":
			inTok = event.Usage.InputTokens
			outTok = event.Usage.OutputTokens
			if event.Result != "" && lastText == "" {
				lastText = event.Result
			}
		}
	}

	_ = cmd.Wait()

	content := stripThinking(lastText)

	if h.usageTracker != nil {
		h.usageTracker.Record(usage.TokenUsage{
			UserID: req.UserID, Provider: "claude-cli", Model: "claude-haiku-4-5", Category: "chat",
			InputTokens: inTok, OutputTokens: outTok, ElapsedMs: time.Since(cliStart).Milliseconds(),
		})
	}

	return &ChatResponsePayload{
		ConversationID: req.ConversationID, MessageID: req.MessageID,
		Content: content, Model: model, Role: "assistant",
		TokensUsed: inTok + outTok,
	}, nil
}

// processChatOllamaSimple uses streaming without tool calling (for simple queries).
func (h *Handler) processChatOllamaSimple(ctx context.Context, req *ChatRequestPayload, model string) (*ChatResponsePayload, error) {
	ollamaClient := ollama.NewClient()

	// Minimal prompt for small model — short answers only
	systemPrompt := "You are nlook AI. Reply in 1-2 sentences max."
	for _, r := range req.Query {
		if r >= 0xAC00 && r <= 0xD7AF {
			systemPrompt = "nlook AI입니다. 1-2문장으로 짧게 답변하세요."
			break
		}
	}

	streamStart := time.Now()
	fullText, inTok, outTok, err := ollamaClient.ChatStream(ctx, model, systemPrompt, req.Query,
		ollama.ChatOptions{Temperature: 0.7, MaxTokens: 256, History: h.getOllamaHistory(req)},
		func(text string) {
			h.sendResponse("chat:delta", ChatDeltaPayload{
				ConversationID: req.ConversationID,
				MessageID:      req.MessageID,
				Delta:          text,
				Done:           false,
			})
		},
	)
	if err != nil {
		return nil, fmt.Errorf("ollama simple chat: %w", err)
	}

	if h.usageTracker != nil {
		h.usageTracker.Record(usage.TokenUsage{
			UserID: req.UserID, Provider: "ollama", Model: model, Category: "chat",
			InputTokens: inTok, OutputTokens: outTok, ElapsedMs: time.Since(streamStart).Milliseconds(),
		})
	}

	fullText = stripThinking(fullText)

	return &ChatResponsePayload{
		ConversationID: req.ConversationID, MessageID: req.MessageID,
		Content: fullText, Model: model, Role: "assistant",
		TokensUsed: inTok + outTok, ElapsedMs: time.Since(streamStart).Milliseconds(),
	}, nil
}

// classifyWithQwen always calls qwen for classification (for usage tracking).
func (h *Handler) classifyWithQwen(ctx context.Context, query string, localModel string, tlog func(string, ...interface{})) string {
	if localModel == "" {
		return "complex" // no local model, default to complex
	}

	ollamaClient := ollama.NewClient()
	if !ollamaClient.IsRunning(ctx) {
		return "complex"
	}

	classifyQuery := strings.TrimSpace(query)
	if idx := strings.Index(classifyQuery, "[Tool Result:"); idx >= 0 {
		classifyQuery = strings.TrimSpace(classifyQuery[:idx])
	}

	// Short messages (< 15 chars) are always simple
	if len(classifyQuery) < 15 {
		if h.usageTracker != nil {
			h.usageTracker.Record(usage.TokenUsage{
				Provider: "ollama", Model: localModel, Category: "intent",
				InputTokens: 0, OutputTokens: 0,
			})
		}
		tlog("classify: short → simple (len=%d)", len(classifyQuery))
		return "simple"
	}

	classifyPrompt := fmt.Sprintf(
		`Classify as "simple" or "complex". Reply ONE word only.

simple examples: 하이, 안녕, 뭐해, 할일 보여줘, 목록 조회, 감사합니다, hi, thanks, show tasks
complex examples: 블로그 작성해줘, 이 코드 분석해줘, SEO 전략 설명해, 보고서 만들어줘

Message: %s`, classifyQuery)

	result, inTok, outTok, err := ollamaClient.ChatStream(ctx, localModel, "", classifyPrompt,
		ollama.ChatOptions{MaxTokens: 3, Temperature: 0.0},
		nil,
	)
	if err != nil {
		tlog("classify: qwen error: %v", err)
		return "complex"
	}

	if h.usageTracker != nil {
		h.usageTracker.Record(usage.TokenUsage{
			Provider: "ollama", Model: localModel, Category: "intent",
			InputTokens: inTok, OutputTokens: outTok,
		})
	}

	result = strings.TrimSpace(strings.ToLower(result))
	if strings.Contains(result, "simple") {
		tlog("classify: qwen → simple (in=%d out=%d)", inTok, outTok)
		return "simple"
	}
	tlog("classify: qwen → complex (in=%d out=%d)", inTok, outTok)
	return "complex"
}

// classifyComplexity determines if a query is "simple" or "complex".
// Uses fast keyword check first, then qwen LLM for ambiguous cases.
func (h *Handler) classifyComplexity(ctx context.Context, query string, localModel string, tlog func(string, ...interface{})) string {
	clean := strings.TrimSpace(query)

	// Strip tool result prefix for classification
	classifyQuery := clean
	if idx := strings.Index(clean, "[Tool Result:"); idx >= 0 {
		classifyQuery = clean[:idx]
	}
	classifyQuery = strings.TrimSpace(classifyQuery)

	// 1. Very short = simple (greetings, yes/no)
	if len(classifyQuery) < 10 {
		return "simple"
	}

	// 2. Keywords → complex
	complexKeywords := []string{
		"작성해", "써줘", "만들어", "분석해", "비교해", "요약해", "번역해",
		"설명해", "자세히", "왜", "어떻게 하면",
		"SEO", "마케팅", "블로그", "리포트", "보고서", "코드",
		"write", "analyze", "summarize", "translate", "explain",
		"generate", "create", "draft", "compare", "how to",
	}
	lower := strings.ToLower(classifyQuery)
	for _, kw := range complexKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return "complex"
		}
	}

	// 3. Keywords → simple
	simpleKeywords := []string{
		"목록", "조회", "보여줘", "알려줘", "뭐야", "몇개",
		"list", "show", "what", "how many", "check",
	}
	for _, kw := range simpleKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return "simple"
		}
	}

	// 4. Tool results attached → complex (needs reasoning over data)
	if strings.Contains(clean, "[Tool Result:") {
		return "complex"
	}

	// 5. Medium length without clear keywords → use qwen to classify
	if localModel != "" {
		ollamaClient := ollama.NewClient()
		if ollamaClient.IsRunning(ctx) {
			classifyPrompt := fmt.Sprintf(
				`Classify this user message as "simple" or "complex".
Simple: greetings, status checks, listing items, short Q&A, basic CRUD.
Complex: writing, analysis, comparison, explanation, code generation, document creation.

Reply with ONLY one word: simple or complex

Message: %s`, classifyQuery)

			result, inTok, outTok, err := ollamaClient.ChatStream(ctx, localModel, "", classifyPrompt,
				ollama.ChatOptions{MaxTokens: 5, Temperature: 0.0},
				nil,
			)
			if err == nil {
				// Track classifier token usage
				if h.usageTracker != nil {
					h.usageTracker.Record(usage.TokenUsage{
						Provider: "ollama", Model: localModel, Category: "intent",
						InputTokens: inTok, OutputTokens: outTok,
					})
				}
				result = strings.TrimSpace(strings.ToLower(result))
				if strings.Contains(result, "simple") {
					tlog("classify: qwen → simple (in=%d out=%d)", inTok, outTok)
					return "simple"
				}
				if strings.Contains(result, "complex") {
					tlog("classify: qwen → complex (in=%d out=%d)", inTok, outTok)
					return "complex"
				}
			}
		}
	}

	// Default: complex (safer to use better model)
	return "complex"
}

// findLocalModel returns a usable local model name, or empty string.
func (h *Handler) findLocalModel(ctx context.Context, model string) string {
	if model != "" && isLocalModel(model) {
		return model
	}
	// Try vLLM
	if h.llmEngine != nil && h.llmEngine.Type() == llm.EngineVLLM && h.llmEngine.IsRunning(ctx) {
		return h.llmEngine.Model()
	}
	// Try Ollama
	ollamaClient := ollama.NewClient()
	if ollamaClient.IsRunning(ctx) {
		models, _ := ollamaClient.List(ctx)
		for _, m := range models {
			name := strings.ToLower(m.Name)
			if strings.Contains(name, "embed") || strings.Contains(name, "nomic") {
				continue
			}
			return m.Name
		}
	}
	return ""
}

func isClaudeModel(model string) bool {
	return strings.HasPrefix(model, "claude") || strings.HasPrefix(model, "anthropic")
}

var localModelPrefixes = []string{
	"qwen", "llama", "mistral", "codellama", "gemma", "phi",
	"deepseek", "starcoder", "vicuna", "orca", "wizardcoder",
	"ollama/", "local/",
}

func truncateLog(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func isLocalModel(model string) bool {
	lower := strings.ToLower(model)
	for _, prefix := range localModelPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// isSimpleQuery returns true for trivial queries that local model can handle.
// Everything else goes to Gemini Cloud for better reasoning.
func isSimpleQuery(query string) bool {
	// If tool results are attached, needs cloud reasoning
	if strings.Contains(query, "[Tool Result:") {
		return false
	}
	// Short greetings / simple status
	clean := strings.TrimSpace(query)
	if len(clean) < 15 {
		return true
	}
	simplePatterns := []string{
		"안녕", "하이", "헬로", "hello", "hi ", "hey",
		"ㅎㅇ", "ㅎㅎ", "ㅋㅋ", "감사", "고마워", "thanks",
		"네", "응", "아니", "ok", "yes", "no",
	}
	lower := strings.ToLower(clean)
	for _, p := range simplePatterns {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// processChatGemini uses Gemini Cloud API with streaming.
func (h *Handler) processChatGemini(ctx context.Context, req *ChatRequestPayload, client *gemini.Client) (*ChatResponsePayload, error) {
	systemPrompt := h.getSystemPrompt(req.Lang, req.Query, req.ConversationID)

	messages := make([]map[string]string, 0)
	for _, m := range req.History {
		messages = append(messages, map[string]string{"role": m.Role, "content": m.Content})
	}
	messages = append(messages, map[string]string{"role": "user", "content": req.Query})

	geminiStart := time.Now()
	fullText, inTok, outTok, err := client.ChatStream(ctx, systemPrompt, messages,
		func(text string) {
			h.sendResponse("chat:delta", ChatDeltaPayload{
				ConversationID: req.ConversationID,
				MessageID:      req.MessageID,
				Delta:          text,
				Done:           false,
			})
		},
	)
	if err != nil {
		return nil, fmt.Errorf("gemini chat: %w", err)
	}

	if h.usageTracker != nil {
		h.usageTracker.Record(usage.TokenUsage{
			UserID: req.UserID, Provider: "gemini", Model: client.Model(), Category: "chat",
			InputTokens: inTok, OutputTokens: outTok, ElapsedMs: time.Since(geminiStart).Milliseconds(),
		})
	}

	model := client.Model()
	return &ChatResponsePayload{
		ConversationID: req.ConversationID, MessageID: req.MessageID,
		Content: fullText, Model: model, Role: "assistant",
		TokensUsed: inTok + outTok,
	}, nil
}
