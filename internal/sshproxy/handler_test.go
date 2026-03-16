package sshproxy

import (
	"encoding/base64"
	"encoding/json"
	"sync"
	"testing"
)

func TestHandler_HandleMessage_UnknownType(t *testing.T) {
	proxy := NewProxy()
	h := NewHandler(proxy, func(msg []byte) {})

	handled := h.HandleMessage("unknown:type", nil)
	if handled {
		t.Error("expected false for unknown message type")
	}
}

func TestHandler_HandleMessage_SSHTypes(t *testing.T) {
	proxy := NewProxy()

	var mu sync.Mutex
	var sent []WSMessage

	sendWS := func(msg []byte) {
		var m WSMessage
		if err := json.Unmarshal(msg, &m); err == nil {
			mu.Lock()
			sent = append(sent, m)
			mu.Unlock()
		}
	}

	h := NewHandler(proxy, sendWS)

	// ssh:open with invalid config (no auth method) should send error
	openPayload, _ := json.Marshal(SSHOpenPayload{
		SessionID: "test-1",
		Host:      "127.0.0.1",
		Port:      22,
		User:      "testuser",
		Cols:      80,
		Rows:      24,
	})
	handled := h.HandleMessage("ssh:open", openPayload)
	if !handled {
		t.Error("expected ssh:open to be handled")
	}

	// ssh:data should be handled even if session doesn't exist (just logs error)
	dataPayload, _ := json.Marshal(SSHDataPayload{
		SessionID: "nonexistent",
		Data:      base64.StdEncoding.EncodeToString([]byte("hello")),
	})
	handled = h.HandleMessage("ssh:data", dataPayload)
	if !handled {
		t.Error("expected ssh:data to be handled")
	}

	// ssh:resize should be handled
	resizePayload, _ := json.Marshal(SSHResizePayload{
		SessionID: "nonexistent",
		Cols:      120,
		Rows:      40,
	})
	handled = h.HandleMessage("ssh:resize", resizePayload)
	if !handled {
		t.Error("expected ssh:resize to be handled")
	}

	// ssh:close should be handled
	closePayload, _ := json.Marshal(SSHClosePayload{
		SessionID: "nonexistent",
	})
	handled = h.HandleMessage("ssh:close", closePayload)
	if !handled {
		t.Error("expected ssh:close to be handled")
	}

	// Check that an error was sent for the failed open
	mu.Lock()
	defer mu.Unlock()
	foundError := false
	for _, m := range sent {
		if m.Type == "ssh:error" {
			foundError = true
		}
	}
	if !foundError {
		t.Error("expected ssh:error message for failed open (no auth method)")
	}
}

func TestProxy_ActiveSessions(t *testing.T) {
	proxy := NewProxy()
	if proxy.ActiveSessions() != 0 {
		t.Errorf("expected 0 active sessions, got %d", proxy.ActiveSessions())
	}
}

func TestProxy_CloseSession_Nonexistent(t *testing.T) {
	proxy := NewProxy()
	// Should not panic
	proxy.CloseSession("nonexistent")
}

func TestProxy_CloseAll(t *testing.T) {
	proxy := NewProxy()
	// Should not panic with no sessions
	proxy.CloseAll()
}

func TestProxy_WriteData_NoSession(t *testing.T) {
	proxy := NewProxy()
	err := proxy.WriteData("nonexistent", []byte("data"))
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestProxy_Resize_NoSession(t *testing.T) {
	proxy := NewProxy()
	err := proxy.Resize("nonexistent", 80, 24)
	if err == nil {
		t.Error("expected error for nonexistent session")
	}
}

func TestSSHPayload_Serialization(t *testing.T) {
	// Test that payloads round-trip correctly
	open := SSHOpenPayload{
		SessionID: "s1",
		Host:      "192.168.1.100",
		Port:      22,
		User:      "admin",
		Password:  "secret",
		Cols:      120,
		Rows:      40,
	}

	data, err := json.Marshal(open)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SSHOpenPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.SessionID != open.SessionID ||
		decoded.Host != open.Host ||
		decoded.Port != open.Port ||
		decoded.User != open.User ||
		decoded.Password != open.Password ||
		decoded.Cols != open.Cols ||
		decoded.Rows != open.Rows {
		t.Error("payload round-trip mismatch")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Security: isHostAllowed
// ──────────────────────────────────────────────────────────────────────────────

func TestProxy_IsHostAllowed_EmptyList(t *testing.T) {
	proxy := NewProxyWithConfig(ProxyConfig{AllowedHosts: nil})
	if !proxy.isHostAllowed("any-host") {
		t.Error("empty allowed list should permit all hosts")
	}
}

func TestProxy_IsHostAllowed_Match(t *testing.T) {
	proxy := NewProxyWithConfig(ProxyConfig{AllowedHosts: []string{"192.168.1.1", "10.0.0.1"}})
	if !proxy.isHostAllowed("192.168.1.1") {
		t.Error("should allow listed host")
	}
	if !proxy.isHostAllowed("10.0.0.1") {
		t.Error("should allow listed host")
	}
}

func TestProxy_IsHostAllowed_Reject(t *testing.T) {
	proxy := NewProxyWithConfig(ProxyConfig{AllowedHosts: []string{"192.168.1.1"}})
	if proxy.isHostAllowed("10.0.0.5") {
		t.Error("should reject unlisted host")
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Security: MaxSessions
// ──────────────────────────────────────────────────────────────────────────────

func TestProxy_MaxSessions_Reject(t *testing.T) {
	proxy := NewProxyWithConfig(ProxyConfig{MaxSessions: 1})
	h := NewHandler(proxy, func(msg []byte) {})

	// Open first session (will fail due to no auth, but let's test via OpenSession directly)
	cfg := Config{Host: "127.0.0.1", Port: 22, User: "test", Password: "pass"}

	// Manually add a session to simulate an active one
	proxy.mu.Lock()
	proxy.sessions["existing"] = &Session{ID: "existing"}
	proxy.mu.Unlock()

	// Now try to open via handler — should hit max sessions
	openPayload, _ := json.Marshal(SSHOpenPayload{
		SessionID: "test-2",
		Host:      "127.0.0.1",
		Port:      22,
		User:      "test",
		Password:  "pass",
		Cols:      80,
		Rows:      24,
	})

	var sent []WSMessage
	h.sendWS = func(msg []byte) {
		var m WSMessage
		if err := json.Unmarshal(msg, &m); err == nil {
			sent = append(sent, m)
		}
	}

	h.HandleMessage("ssh:open", openPayload)

	foundError := false
	for _, m := range sent {
		if m.Type == "ssh:error" {
			foundError = true
		}
	}
	if !foundError {
		t.Error("expected ssh:error when max sessions reached")
	}

	// Suppress unused variable warning
	_ = cfg
}

// ──────────────────────────────────────────────────────────────────────────────
// Security: DefaultProxyConfig
// ──────────────────────────────────────────────────────────────────────────────

func TestDefaultProxyConfig(t *testing.T) {
	cfg := DefaultProxyConfig()
	if cfg.MaxSessions != 10 {
		t.Errorf("expected MaxSessions=10, got %d", cfg.MaxSessions)
	}
	if cfg.IdleTimeout == 0 {
		t.Error("expected non-zero IdleTimeout")
	}
	if cfg.AllowedHosts != nil {
		t.Error("expected nil AllowedHosts (allow all)")
	}
}

func TestSSHOpenPayload_WithPrivateKey(t *testing.T) {
	open := SSHOpenPayload{
		SessionID:  "s1",
		Host:       "192.168.1.100",
		Port:       22,
		User:       "admin",
		PrivateKey: "-----BEGIN RSA PRIVATE KEY-----\ntest\n-----END RSA PRIVATE KEY-----",
		AuthType:   "key",
		Cols:       80,
		Rows:       24,
	}

	data, err := json.Marshal(open)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SSHOpenPayload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.PrivateKey != open.PrivateKey {
		t.Error("PrivateKey mismatch")
	}
	if decoded.AuthType != "key" {
		t.Error("AuthType mismatch")
	}
}
