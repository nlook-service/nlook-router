package agentproxy

import (
	"encoding/json"
	"testing"
)

func TestHandleMessage_Routing(t *testing.T) {
	var sentMsgs []string
	sendWS := func(msg []byte) {
		sentMsgs = append(sentMsgs, string(msg))
	}

	// Empty config — no workspaces allowed, so start will fail
	mgr := NewSessionManager(nil, SessionConfig{
		MaxSessions:     5,
		AllowedCommands: []string{"claude"},
	})
	h := NewHandler(mgr, sendWS)

	// agent:start should be handled (returns true)
	payload, _ := json.Marshal(StartPayload{
		SessionID: "test-1",
		Workspace: "/nonexistent",
		Prompt:    "hello",
	})
	if !h.HandleMessage("agent:start", payload) {
		t.Error("expected agent:start to be handled")
	}

	// Should have sent an error (no workspaces configured)
	if len(sentMsgs) == 0 {
		t.Fatal("expected error message to be sent")
	}
	var msg WSMessage
	json.Unmarshal([]byte(sentMsgs[0]), &msg)
	if msg.Type != "agent:error" {
		t.Errorf("expected agent:error, got %s", msg.Type)
	}

	// agent:list should be handled
	sentMsgs = nil
	if !h.HandleMessage("agent:list", nil) {
		t.Error("expected agent:list to be handled")
	}
	if len(sentMsgs) == 0 {
		t.Fatal("expected list response")
	}
	json.Unmarshal([]byte(sentMsgs[0]), &msg)
	if msg.Type != "agent:list" {
		t.Errorf("expected agent:list response, got %s", msg.Type)
	}

	// agent:stop should be handled (no-op for nonexistent session)
	stopPayload, _ := json.Marshal(StopPayload{SessionID: "nonexistent"})
	if !h.HandleMessage("agent:stop", stopPayload) {
		t.Error("expected agent:stop to be handled")
	}

	// agent:input should be handled
	inputPayload, _ := json.Marshal(InputPayload{SessionID: "test-1", Content: "fix the bug"})
	if !h.HandleMessage("agent:input", inputPayload) {
		t.Error("expected agent:input to be handled")
	}

	// Unknown type should not be handled
	if h.HandleMessage("ssh:open", nil) {
		t.Error("expected ssh:open to NOT be handled by agent handler")
	}
}

func TestUsageRecording(t *testing.T) {
	var recorded []UsageRecord
	mockRecorder := &mockUsageRecorder{records: &recorded}

	mgr := NewSessionManager(nil, SessionConfig{
		MaxSessions:     5,
		AllowedCommands: []string{"claude"},
	})
	var sentMsgs []string
	h := NewHandler(mgr, func(msg []byte) {
		sentMsgs = append(sentMsgs, string(msg))
	})
	h.SetUsageRecorder(mockRecorder)

	// Simulate a completed session
	result := ClaudeResult{
		SessionID:  "test-session",
		Result:     "done",
		DurationMs: 5000,
		TotalCost:  0.05,
		Usage:      &ClaudeUsage{InputTokens: 100, OutputTokens: 50},
	}
	h.sendCompleted("test-session", result)

	if len(recorded) != 1 {
		t.Fatalf("expected 1 usage record, got %d", len(recorded))
	}
	r := recorded[0]
	if r.Provider != "claude-cli" {
		t.Errorf("expected provider=claude-cli, got %s", r.Provider)
	}
	if r.InputTokens != 100 || r.OutputTokens != 50 {
		t.Errorf("expected tokens 100/50, got %d/%d", r.InputTokens, r.OutputTokens)
	}
	if r.Category != "agent" {
		t.Errorf("expected category=agent, got %s", r.Category)
	}
}

type mockUsageRecorder struct {
	records *[]UsageRecord
}

func (m *mockUsageRecorder) Record(u UsageRecord) {
	*m.records = append(*m.records, u)
}
