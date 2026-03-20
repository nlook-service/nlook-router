package session

import (
	"testing"
	"time"
)

func TestBindAgent(t *testing.T) {
	sess := &Session{
		ID:      "test-sess",
		Type:    TypeChat,
		State:   StateActive,
		Context: NewContext(),
	}

	sess.BindAgent("agent-1")
	sess.BindAgent("agent-2")

	if len(sess.AgentIDs) != 2 {
		t.Fatalf("expected 2 agent IDs, got %d", len(sess.AgentIDs))
	}
	if sess.AgentIDs[0] != "agent-1" {
		t.Errorf("expected agent-1, got %s", sess.AgentIDs[0])
	}
	if sess.Type != TypeComposite {
		t.Errorf("expected composite type after binding agent, got %s", sess.Type)
	}
}

func TestBindRun(t *testing.T) {
	sess := &Session{
		ID:      "test-sess",
		Type:    TypeChat,
		State:   StateActive,
		Context: NewContext(),
	}

	sess.BindRun(42)

	if len(sess.RunIDs) != 1 {
		t.Fatalf("expected 1 run ID, got %d", len(sess.RunIDs))
	}
	if sess.RunIDs[0] != 42 {
		t.Errorf("expected run ID 42, got %d", sess.RunIDs[0])
	}
	if sess.Type != TypeComposite {
		t.Errorf("expected composite type, got %s", sess.Type)
	}
}

func TestBindAgentKeepsNonChatType(t *testing.T) {
	sess := &Session{
		ID:      "test-sess",
		Type:    TypeWorkflow,
		State:   StateActive,
		Context: NewContext(),
	}

	sess.BindAgent("agent-1")

	if sess.Type != TypeWorkflow {
		t.Errorf("expected workflow type to be preserved, got %s", sess.Type)
	}
}

func TestComplete(t *testing.T) {
	sess := &Session{ID: "test", State: StateActive}
	sess.Complete()
	if sess.State != StateCompleted {
		t.Errorf("expected completed, got %s", sess.State)
	}
}

func TestIsActive(t *testing.T) {
	active := &Session{
		State:     StateActive,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if !active.IsActive() {
		t.Error("expected active session to return true")
	}

	expired := &Session{
		State:     StateActive,
		ExpiresAt: time.Now().Add(-time.Hour),
	}
	if expired.IsActive() {
		t.Error("expected expired session to return false")
	}

	completed := &Session{
		State:     StateCompleted,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if completed.IsActive() {
		t.Error("expected completed session to return false")
	}
}

func TestContextAddMessage(t *testing.T) {
	ctx := NewContext()

	for i := 0; i < 60; i++ {
		ctx.AddMessage("user", "msg")
	}

	if len(ctx.Messages) != MaxMessages {
		t.Errorf("expected %d messages (sliding window), got %d", MaxMessages, len(ctx.Messages))
	}
}

func TestContextAddAgentResult(t *testing.T) {
	ctx := NewContext()
	ctx.AddAgentResult(AgentResult{
		AgentSessionID: "a-1",
		Result:         "done",
		DurationMs:     1000,
	})

	if len(ctx.AgentResults) != 1 {
		t.Fatalf("expected 1 agent result, got %d", len(ctx.AgentResults))
	}
	if ctx.AgentResults[0].Result != "done" {
		t.Errorf("expected 'done', got %s", ctx.AgentResults[0].Result)
	}
}

func TestContextVariables(t *testing.T) {
	ctx := NewContext()
	ctx.SetVariable("key", "value")

	v, ok := ctx.GetVariable("key")
	if !ok {
		t.Fatal("expected variable to exist")
	}
	if v != "value" {
		t.Errorf("expected 'value', got %v", v)
	}

	_, ok = ctx.GetVariable("missing")
	if ok {
		t.Error("expected missing variable to return false")
	}
}

func TestGenerateID(t *testing.T) {
	id1 := generateID()
	id2 := generateID()
	if id1 == id2 {
		t.Error("expected unique IDs")
	}
	if len(id1) != 32 {
		t.Errorf("expected 32 char hex, got %d chars", len(id1))
	}
}
