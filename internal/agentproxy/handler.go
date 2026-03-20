package agentproxy

import (
	"encoding/json"
	"log"
)

// WSMessage mirrors the WebSocket message envelope.
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Incoming payloads (Cloud → Router)

// StartPayload requests a new agent session.
type StartPayload struct {
	SessionID string   `json:"session_id"`
	Workspace string   `json:"workspace"`
	Prompt    string   `json:"prompt"`
	Args      []string `json:"args,omitempty"`
	TaskID    int64    `json:"task_id,omitempty"`
	Cols      int      `json:"cols,omitempty"`
	Rows      int      `json:"rows,omitempty"`
}

// InputPayload forwards user input to a running session.
type InputPayload struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"` // user message text
}

// StopPayload requests session termination.
type StopPayload struct {
	SessionID string `json:"session_id"`
}

// ListPayload requests active session list (no fields needed).
type ListPayload struct{}

// Outgoing payloads (Router → Cloud)

// StartedPayload confirms session creation.
type StartedPayload struct {
	SessionID string `json:"session_id"`
	Workspace string `json:"workspace"`
}

// OutputPayload streams claude output events.
type OutputPayload struct {
	SessionID string            `json:"session_id"`
	Event     ClaudeStreamEvent `json:"event"`
}

// CompletedPayload notifies session completion with usage stats.
type CompletedPayload struct {
	SessionID  string       `json:"session_id"`
	Result     string       `json:"result"`
	IsError    bool         `json:"is_error"`
	ExitCode   int          `json:"exit_code"`
	DurationMs int64        `json:"duration_ms"`
	TotalCost  float64      `json:"total_cost_usd"`
	Usage      *ClaudeUsage `json:"usage,omitempty"`
	StopReason string       `json:"stop_reason,omitempty"`
}

// ErrorPayload reports an error for a session.
type ErrorPayload struct {
	SessionID string `json:"session_id"`
	Error     string `json:"error"`
}

// ListResponsePayload returns active sessions.
type ListResponsePayload struct {
	Sessions []SessionInfo `json:"sessions"`
}

// UsageRecorder records token usage. Matches usage.Tracker.Record() signature.
type UsageRecorder interface {
	Record(u UsageRecord)
}

// UsageRecord holds token usage for a single agent session.
type UsageRecord struct {
	UserID       int64
	Provider     string
	Model        string
	Category     string
	InputTokens  int
	OutputTokens int
	ElapsedMs    int64
}

// Handler processes agent-related WebSocket messages.
type Handler struct {
	manager       *SessionManager
	sendWS        func(msg []byte)
	usageRecorder UsageRecorder
}

// NewHandler creates a new agent message handler.
func NewHandler(manager *SessionManager, sendWS func(msg []byte)) *Handler {
	return &Handler{
		manager: manager,
		sendWS:  sendWS,
	}
}

// SetUsageRecorder sets the usage tracker for recording claude-cli token usage.
func (h *Handler) SetUsageRecorder(r UsageRecorder) {
	h.usageRecorder = r
}

// HandleMessage processes an agent:* WebSocket message.
// Returns true if the message type was handled.
func (h *Handler) HandleMessage(msgType string, payload json.RawMessage) bool {
	switch msgType {
	case "agent:start":
		h.handleStart(payload)
		return true
	case "agent:input":
		h.handleInput(payload)
		return true
	case "agent:stop":
		h.handleStop(payload)
		return true
	case "agent:list":
		h.handleList()
		return true
	default:
		return false
	}
}

func (h *Handler) handleStart(payload json.RawMessage) {
	var p StartPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		log.Printf("agent:start unmarshal error: %v", err)
		h.sendError(p.SessionID, "invalid payload")
		return
	}

	if p.SessionID == "" || p.Workspace == "" || p.Prompt == "" {
		h.sendError(p.SessionID, "session_id, workspace, and prompt are required")
		return
	}

	log.Printf("agent: starting session %s in %s (task=%d)", p.SessionID, p.Workspace, p.TaskID)

	onEvent := func(event ClaudeStreamEvent) {
		h.sendOutput(p.SessionID, event)
	}

	onDone := func(result ClaudeResult) {
		h.sendCompleted(p.SessionID, result)
	}

	if err := h.manager.StartSession(p.SessionID, p.Workspace, p.Prompt, p.Args, p.TaskID, onEvent, onDone); err != nil {
		log.Printf("agent:start error: %v", err)
		h.sendError(p.SessionID, err.Error())
		return
	}

	h.sendStarted(p.SessionID, p.Workspace)
}

func (h *Handler) handleInput(payload json.RawMessage) {
	var p InputPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		log.Printf("agent:input unmarshal error: %v", err)
		return
	}
	if p.SessionID == "" || p.Content == "" {
		return
	}
	// For now, agent:input starts a new session with the follow-up prompt in the same workspace.
	// Full stdin piping requires --input-format stream-json which is not yet stable.
	log.Printf("agent: input received for session %s (content length=%d) — queued for future multi-turn support", p.SessionID, len(p.Content))
	// TODO: When claude --input-format stream-json is stable, pipe content to stdin.
}

func (h *Handler) handleStop(payload json.RawMessage) {
	var p StopPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		log.Printf("agent:stop unmarshal error: %v", err)
		return
	}
	h.manager.StopSession(p.SessionID)
}

func (h *Handler) handleList() {
	sessions := h.manager.ListSessions()
	h.sendMessage("agent:list", ListResponsePayload{Sessions: sessions})
}

// Send helpers

func (h *Handler) sendStarted(sessionID, workspace string) {
	h.sendMessage("agent:started", StartedPayload{
		SessionID: sessionID,
		Workspace: workspace,
	})
}

func (h *Handler) sendOutput(sessionID string, event ClaudeStreamEvent) {
	h.sendMessage("agent:output", OutputPayload{
		SessionID: sessionID,
		Event:     event,
	})
}

func (h *Handler) sendCompleted(sessionID string, result ClaudeResult) {
	h.sendMessage("agent:completed", CompletedPayload{
		SessionID:  sessionID,
		Result:     result.Result,
		IsError:    result.IsError,
		ExitCode:   result.ExitCode,
		DurationMs: result.DurationMs,
		TotalCost:  result.TotalCost,
		Usage:      result.Usage,
		StopReason: result.StopReason,
	})

	// Record token usage to tracker
	if h.usageRecorder != nil && result.Usage != nil {
		h.usageRecorder.Record(UsageRecord{
			Provider:     "claude-cli",
			Model:        "claude-code",
			Category:     "agent",
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
			ElapsedMs:    result.DurationMs,
		})
		log.Printf("agent: usage recorded: in=%d out=%d cost=$%.4f",
			result.Usage.InputTokens, result.Usage.OutputTokens, result.TotalCost)
	}
}

func (h *Handler) sendError(sessionID string, errMsg string) {
	h.sendMessage("agent:error", ErrorPayload{
		SessionID: sessionID,
		Error:     errMsg,
	})
}

func (h *Handler) sendMessage(msgType string, payload interface{}) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return
	}
	msg, err := json.Marshal(WSMessage{Type: msgType, Payload: payloadBytes})
	if err != nil {
		return
	}
	h.sendWS(msg)
}
