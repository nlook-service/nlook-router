package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestNewClient_URLConstruction(t *testing.T) {
	tests := []struct {
		apiURL   string
		wantPfx  string
	}{
		{"https://nlook.me", "wss://nlook.me/api/routers/ws?"},
		{"http://localhost:8080", "ws://localhost:8080/api/routers/ws?"},
	}

	for _, tt := range tests {
		c := NewClient(tt.apiURL, "test-key", "r1")
		if !strings.HasPrefix(c.url, tt.wantPfx) {
			t.Errorf("NewClient(%q).url = %q, want prefix %q", tt.apiURL, c.url, tt.wantPfx)
		}
		if !strings.Contains(c.url, "token=test-key") {
			t.Errorf("URL should contain token")
		}
		if !strings.Contains(c.url, "router_id=r1") {
			t.Errorf("URL should contain router_id")
		}
	}
}

func TestClient_IsConnected(t *testing.T) {
	c := NewClient("https://example.com", "key", "r1")
	if c.IsConnected() {
		t.Error("expected not connected initially")
	}
	c.setConnected(true)
	if !c.IsConnected() {
		t.Error("expected connected after setConnected(true)")
	}
}

func TestClient_SendMessage(t *testing.T) {
	c := NewClient("https://example.com", "key", "r1")

	err := c.SendMessage("test:msg", map[string]string{"foo": "bar"})
	if err != nil {
		t.Fatalf("SendMessage error: %v", err)
	}

	// Read from send channel
	select {
	case msg := <-c.sendCh:
		var wsMsg WSMessage
		if err := json.Unmarshal(msg, &wsMsg); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if wsMsg.Type != "test:msg" {
			t.Errorf("expected type test:msg, got %s", wsMsg.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for message")
	}
}

func TestClient_RunDispatchIntegration(t *testing.T) {
	// Create a test WebSocket server
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	var mu sync.Mutex
	var dispatched bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		// Send a run:dispatch message
		payload, _ := json.Marshal(RunDispatchPayload{
			RunID:      42,
			WorkflowID: 1,
			UserID:     100,
		})
		msg, _ := json.Marshal(WSMessage{
			Type:    "run:dispatch",
			Payload: payload,
		})
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}

		// Keep connection alive for a moment
		time.Sleep(500 * time.Millisecond)
	}))
	defer server.Close()

	// Create client pointing to test server
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/routers/ws?token=test&router_id=r1"
	c := &Client{
		url:          wsURL,
		routerID:     "r1",
		sendCh:       make(chan []byte, 64),
		done:         make(chan struct{}),
		maxReconnect: time.Second,
	}

	c.OnRunDispatch = func(p RunDispatchPayload) {
		mu.Lock()
		dispatched = true
		mu.Unlock()
		if p.RunID != 42 {
			t.Errorf("expected RunID 42, got %d", p.RunID)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	c.Start(ctx)

	// Wait for dispatch
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		done := dispatched
		mu.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting for run dispatch")
		case <-time.After(50 * time.Millisecond):
		}
	}

	c.Stop()
}

func TestClient_Stop(t *testing.T) {
	c := NewClient("https://example.com", "key", "r1")
	c.Stop()
	// Double stop should not panic
	c.Stop()
}
