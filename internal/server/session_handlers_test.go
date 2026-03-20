package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nlook-service/nlook-router/internal/session"
	"github.com/nlook-service/nlook-router/internal/tracing"
)

func TestSessionsHandler(t *testing.T) {
	s := New(":0", &Status{RouterID: "test"})
	store := session.NewStore("", time.Hour)
	defer store.Close()
	s.SetSessionStore(store)

	store.Register("sess-1", session.TypeChat, 1)
	store.Register("sess-2", session.TypeAgent, 2)

	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	w := httptest.NewRecorder()
	s.sessionsHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Sessions []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(resp.Sessions))
	}
}

func TestSessionsHandlerNoStore(t *testing.T) {
	s := New(":0", &Status{RouterID: "test"})

	req := httptest.NewRequest(http.MethodGet, "/sessions", nil)
	w := httptest.NewRecorder()
	s.sessionsHandler(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestSessionDetailHandler(t *testing.T) {
	s := New(":0", &Status{RouterID: "test"})
	store := session.NewStore("", time.Hour)
	defer store.Close()
	s.SetSessionStore(store)

	sess := store.Register("detail-1", session.TypeChat, 1)
	sess.BindAgent("agent-x")

	req := httptest.NewRequest(http.MethodGet, "/sessions/detail-1", nil)
	w := httptest.NewRecorder()
	s.sessionDetailHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["id"] != "detail-1" {
		t.Errorf("expected id detail-1, got %v", resp["id"])
	}
}

func TestSessionDetailNotFound(t *testing.T) {
	s := New(":0", &Status{RouterID: "test"})
	store := session.NewStore("", time.Hour)
	defer store.Close()
	s.SetSessionStore(store)

	req := httptest.NewRequest(http.MethodGet, "/sessions/nonexistent", nil)
	w := httptest.NewRecorder()
	s.sessionDetailHandler(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestSessionTracesHandler(t *testing.T) {
	dir := t.TempDir()
	s := New(":0", &Status{RouterID: "test"})
	tw := tracing.NewWriter(dir)
	defer tw.Close()
	s.SetTraceWriter(tw)

	// Write some trace events
	tw.Write(tracing.NewEvent("trace-sess", tracing.EventAgentStart, "start"))
	tw.Write(tracing.NewEvent("trace-sess", tracing.EventAgentComplete, "done").WithDuration(5000))

	req := httptest.NewRequest(http.MethodGet, "/sessions/trace-sess/traces", nil)
	w := httptest.NewRecorder()
	s.sessionDetailHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		SessionID string                `json:"session_id"`
		Events    []tracing.TraceEvent  `json:"events"`
		Count     int                   `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Count != 2 {
		t.Errorf("expected 2 events, got %d", resp.Count)
	}
	if resp.SessionID != "trace-sess" {
		t.Errorf("expected session_id trace-sess, got %s", resp.SessionID)
	}
}

func TestSessionTracesEmpty(t *testing.T) {
	dir := t.TempDir()
	s := New(":0", &Status{RouterID: "test"})
	tw := tracing.NewWriter(dir)
	defer tw.Close()
	s.SetTraceWriter(tw)

	req := httptest.NewRequest(http.MethodGet, "/sessions/empty-sess/traces", nil)
	w := httptest.NewRecorder()
	s.sessionDetailHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Count int `json:"count"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Count != 0 {
		t.Errorf("expected 0 events, got %d", resp.Count)
	}
}
