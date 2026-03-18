package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/nlook-service/nlook-router/internal/engine"
	"github.com/nlook-service/nlook-router/internal/apiclient"
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
}

// ChatResponsePayload is sent back to cloud with the AI response.
type ChatResponsePayload struct {
	ConversationID int64  `json:"conversation_id"`
	MessageID      int64  `json:"message_id"`
	Content        string `json:"content"`
	Model          string `json:"model"`
	Role           string `json:"role"`
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
	sendWS      func(msg []byte)
}

// NewHandler creates a new chat message handler.
func NewHandler(skillRunner *engine.SkillRunner, sendWS func(msg []byte)) *Handler {
	return &Handler{
		skillRunner: skillRunner,
		sendWS:      sendWS,
	}
}

// HandleMessage processes a chat-related WebSocket message.
// Returns true if the message type was handled.
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

const defaultSystemPrompt = `You are nlook AI assistant. You help users manage documents, tasks, and provide analysis.
Respond concisely in the user's language. Be helpful and direct.`

func (h *Handler) processChat(ctx context.Context, req *ChatRequestPayload) (*ChatResponsePayload, error) {
	model := req.Model
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	skill := &apiclient.WorkflowSkill{
		Name:      "chat",
		SkillType: "prompt",
		Content:   req.Query,
	}

	agent := &apiclient.WorkflowAgent{
		Name:         "nlook-chat",
		Model:        model,
		SystemPrompt: defaultSystemPrompt,
		MaxTokens:    4096,
		Temperature:  0.7,
	}

	output, _, err := h.skillRunner.RunSkill(ctx, skill, agent, nil)
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
