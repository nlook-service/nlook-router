package chat

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nlook-service/nlook-router/internal/apiclient"
	"github.com/nlook-service/nlook-router/internal/engine"
	"github.com/nlook-service/nlook-router/internal/mcp"
	"github.com/nlook-service/nlook-router/internal/ollama"
)

// WSMessage mirrors the WebSocket message format.
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// ChatRequestPayload is sent from cloud to request AI chat processing.
type ChatRequestPayload struct {
	ConversationID int64  `json:"conversation_id"`
	MessageID      int64  `json:"message_id"`
	Query          string `json:"query"`
	UserID         int64  `json:"user_id"`
	Model          string `json:"model,omitempty"`
	Lang           string `json:"lang,omitempty"`
}

// ChatResponsePayload is sent back to cloud with the AI response.
type ChatResponsePayload struct {
	ConversationID int64  `json:"conversation_id"`
	MessageID      int64  `json:"message_id"`
	Content        string `json:"content"`
	Model          string `json:"model"`
	Role           string `json:"role"`
}

// ChatDeltaPayload is sent for streaming token-by-token responses.
type ChatDeltaPayload struct {
	ConversationID int64  `json:"conversation_id"`
	MessageID      int64  `json:"message_id"`
	Delta          string `json:"delta"`
	Done           bool   `json:"done"`
	FullContent    string `json:"full_content,omitempty"`
	Model          string `json:"model,omitempty"`
}

// ChatErrorPayload is sent when chat processing fails.
type ChatErrorPayload struct {
	ConversationID int64  `json:"conversation_id"`
	MessageID      int64  `json:"message_id"`
	Error          string `json:"error"`
}

// Handler processes chat-related WebSocket messages.
type Handler struct {
	skillRunner *engine.SkillRunner
	mcpClient   *mcp.Client
	sendWS      func(msg []byte)
}

// NewHandler creates a new chat message handler.
func NewHandler(skillRunner *engine.SkillRunner, sendWS func(msg []byte)) *Handler {
	// Create MCP client if API key is available
	apiKey := os.Getenv("NLOOK_API_KEY")
	var mcpClient *mcp.Client
	if apiKey != "" {
		mcpClient = mcp.NewClient(apiKey)
	}

	return &Handler{
		skillRunner: skillRunner,
		mcpClient:   mcpClient,
		sendWS:      sendWS,
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

func (h *Handler) handleChatRequest(payload json.RawMessage) {
	var req ChatRequestPayload
	if err := json.Unmarshal(payload, &req); err != nil {
		log.Printf("chat:request unmarshal error: %v", err)
		return
	}

	log.Printf("chat: received request conv=%d msg=%d query_len=%d", req.ConversationID, req.MessageID, len(req.Query))

	go func() {
		ctx := context.Background()
		resp, err := h.processChat(ctx, &req)
		if err != nil {
			log.Printf("chat: processing error: %v", err)
			h.sendResponse("chat:error", ChatErrorPayload{
				ConversationID: req.ConversationID,
				MessageID:      req.MessageID,
				Error:          err.Error(),
			})
			return
		}
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

Always confirm before creating/modifying content. Respond concisely.`

func getSystemPrompt(lang string) string {
	switch lang {
	case "ko":
		return baseSystemPrompt + "\n\nIMPORTANT: You MUST respond entirely in Korean (한국어). 절대 다른 언어를 섞지 마세요."
	case "en":
		return baseSystemPrompt + "\n\nIMPORTANT: You MUST respond entirely in English."
	default:
		return baseSystemPrompt + "\n\nIMPORTANT: Always respond in the SAME language the user writes in. Never mix languages."
	}
}

func (h *Handler) processChat(ctx context.Context, req *ChatRequestPayload) (*ChatResponsePayload, error) {
	model := req.Model
	if model == "" {
		model = os.Getenv("NLOOK_AI_MODEL")
	}

	// Auto-detect: if no model specified, try Ollama first
	if model == "" {
		ollamaClient := ollama.NewClient()
		if ollamaClient.IsRunning(ctx) {
			models, _ := ollamaClient.List(ctx)
			if len(models) > 0 {
				model = models[0].Name
				log.Printf("chat: using local model %s", model)
			}
		}
	}

	// Fallback to Claude
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	// Local model → stream via Ollama
	if isLocalModel(model) {
		return h.processChatOllama(ctx, req, model)
	}

	// Claude with MCP tools
	if h.mcpClient != nil && isClaudeModel(model) {
		return h.processChatWithTools(ctx, req, model)
	}

	// Claude without MCP: use streaming
	if isClaudeModel(model) {
		return h.processChatStream(ctx, req, model)
	}

	// Other models: simple LLM call
	return h.processChatSimple(ctx, req, model)
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
	respBody, err := h.callAnthropic(ctx, apiKey, model, getSystemPrompt(req.Lang), messages, anthropicTools)
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
		finalResp, err := h.callAnthropic(ctx, apiKey, model, getSystemPrompt(req.Lang), messages, nil)
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

// processChatOllama streams via local Ollama with chat:delta events.
func (h *Handler) processChatOllama(ctx context.Context, req *ChatRequestPayload, model string) (*ChatResponsePayload, error) {
	ollamaClient := ollama.NewClient()

	fullText, err := ollamaClient.ChatStream(ctx, model, getSystemPrompt(req.Lang), req.Query,
		ollama.ChatOptions{Temperature: 0.7, MaxTokens: 4096},
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

	// Send done signal
	h.sendResponse("chat:delta", ChatDeltaPayload{
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
		Delta:          "",
		Done:           true,
		FullContent:    fullText,
		Model:          model,
	})

	return &ChatResponsePayload{
		ConversationID: req.ConversationID,
		MessageID:      req.MessageID,
		Content:        fullText,
		Model:          model,
		Role:           "assistant",
	}, nil
}

// processChatSimple uses skill_runner for non-Claude models (no streaming).
func (h *Handler) processChatSimple(ctx context.Context, req *ChatRequestPayload, model string) (*ChatResponsePayload, error) {
	output, _, err := h.skillRunner.RunSkill(ctx,
		&apiclient.WorkflowSkill{Name: "chat", SkillType: "prompt", Content: req.Query},
		&apiclient.WorkflowAgent{Model: model, SystemPrompt: getSystemPrompt(req.Lang), MaxTokens: 4096, Temperature: 0.7},
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
		System:      getSystemPrompt(req.Lang),
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
	h.sendWS(msg)
}

func isClaudeModel(model string) bool {
	return strings.HasPrefix(model, "claude") || strings.HasPrefix(model, "anthropic")
}

var localModelPrefixes = []string{
	"qwen", "llama", "mistral", "codellama", "gemma", "phi",
	"deepseek", "starcoder", "vicuna", "orca", "wizardcoder",
	"ollama/", "local/",
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
