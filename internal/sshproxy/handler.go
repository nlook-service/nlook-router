package sshproxy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
)

// WSMessage mirrors the WebSocket message format used by the ws.Client.
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// SSH message payloads

// SSHOpenPayload is sent from cloud to request a new SSH session.
type SSHOpenPayload struct {
	SessionID  string `json:"session_id"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	AuthType   string `json:"auth_type,omitempty"` // "password" or "key"
	Cols       int    `json:"cols"`
	Rows       int    `json:"rows"`
}

// SSHDataPayload carries terminal input/output data.
type SSHDataPayload struct {
	SessionID string `json:"session_id"`
	Data      string `json:"data"` // base64 encoded
}

// SSHResizePayload requests terminal resize.
type SSHResizePayload struct {
	SessionID string `json:"session_id"`
	Cols      int    `json:"cols"`
	Rows      int    `json:"rows"`
}

// SSHClosePayload requests session termination.
type SSHClosePayload struct {
	SessionID string `json:"session_id"`
}

// Handler processes SSH-related WebSocket messages.
type Handler struct {
	proxy  *Proxy
	sendWS func(msg []byte) // function to send a message via WebSocket
}

// NewHandler creates a new SSH message handler.
func NewHandler(proxy *Proxy, sendWS func(msg []byte)) *Handler {
	return &Handler{
		proxy:  proxy,
		sendWS: sendWS,
	}
}

// HandleMessage processes an SSH-related WebSocket message.
// Returns true if the message type was handled.
func (h *Handler) HandleMessage(msgType string, payload json.RawMessage) bool {
	switch msgType {
	case "ssh:open":
		h.handleOpen(payload)
		return true
	case "ssh:data":
		h.handleData(payload)
		return true
	case "ssh:resize":
		h.handleResize(payload)
		return true
	case "ssh:close":
		h.handleClose(payload)
		return true
	default:
		return false
	}
}

func (h *Handler) handleOpen(payload json.RawMessage) {
	var p SSHOpenPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		log.Printf("ssh:open unmarshal error: %v", err)
		h.sendError(p.SessionID, fmt.Sprintf("invalid payload: %v", err))
		return
	}

	cfg := Config{
		Host:       p.Host,
		Port:       p.Port,
		User:       p.User,
		Password:   p.Password,
		PrivateKey: p.PrivateKey,
		AuthType:   p.AuthType,
	}

	onOutput := func(data []byte) {
		h.sendSSHData(p.SessionID, data)
	}

	onClose := func() {
		h.sendSSHClose(p.SessionID)
	}

	// Audit log: host/user only — password and private_key are never logged.
	log.Printf("ssh: opening session %s to %s@%s:%d (auth=%s)", p.SessionID, p.User, p.Host, p.Port, p.AuthType)

	if err := h.proxy.OpenSession(p.SessionID, cfg, p.Cols, p.Rows, onOutput, onClose); err != nil {
		log.Printf("ssh:open error: %v", err)
		h.sendError(p.SessionID, err.Error())
		return
	}
}

func (h *Handler) handleData(payload json.RawMessage) {
	var p SSHDataPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		log.Printf("ssh:data unmarshal error: %v", err)
		return
	}

	data, err := base64.StdEncoding.DecodeString(p.Data)
	if err != nil {
		log.Printf("ssh:data base64 decode error: %v", err)
		return
	}

	if err := h.proxy.WriteData(p.SessionID, data); err != nil {
		log.Printf("ssh:data write error: %v", err)
	}
}

func (h *Handler) handleResize(payload json.RawMessage) {
	var p SSHResizePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		log.Printf("ssh:resize unmarshal error: %v", err)
		return
	}

	if err := h.proxy.Resize(p.SessionID, p.Cols, p.Rows); err != nil {
		log.Printf("ssh:resize error: %v", err)
	}
}

func (h *Handler) handleClose(payload json.RawMessage) {
	var p SSHClosePayload
	if err := json.Unmarshal(payload, &p); err != nil {
		log.Printf("ssh:close unmarshal error: %v", err)
		return
	}

	h.proxy.CloseSession(p.SessionID)
}

// sendSSHData sends terminal output back to the cloud via WebSocket.
func (h *Handler) sendSSHData(sessionID string, data []byte) {
	payload := SSHDataPayload{
		SessionID: sessionID,
		Data:      base64.StdEncoding.EncodeToString(data),
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return
	}

	msg := WSMessage{
		Type:    "ssh:data",
		Payload: payloadBytes,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.sendWS(msgBytes)
}

// sendSSHClose notifies the cloud that a session has ended.
func (h *Handler) sendSSHClose(sessionID string) {
	payload := SSHClosePayload{
		SessionID: sessionID,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return
	}

	msg := WSMessage{
		Type:    "ssh:close",
		Payload: payloadBytes,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.sendWS(msgBytes)
}

// sendError sends an error message to the cloud for a session.
func (h *Handler) sendError(sessionID string, errMsg string) {
	payload := map[string]string{
		"session_id": sessionID,
		"error":      errMsg,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return
	}

	msg := WSMessage{
		Type:    "ssh:error",
		Payload: payloadBytes,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return
	}

	h.sendWS(msgBytes)
}
