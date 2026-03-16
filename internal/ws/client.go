package ws

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSMessage is the envelope for all WebSocket messages.
type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// RunDispatchPayload is received from cloud when a run is ready for execution.
type RunDispatchPayload struct {
	RunID      int64 `json:"run_id"`
	WorkflowID int64 `json:"workflow_id"`
	UserID     int64 `json:"user_id"`
}

// RunCancelPayload is received from cloud when a run should be cancelled.
type RunCancelPayload struct {
	RunID int64 `json:"run_id"`
}

// sanitizeURL masks sensitive query parameters (token) in URLs for safe logging.
func sanitizeURL(rawURL string) string {
	if idx := strings.Index(rawURL, "token="); idx != -1 {
		end := strings.Index(rawURL[idx:], "&")
		if end == -1 {
			return rawURL[:idx] + "token=***"
		}
		return rawURL[:idx] + "token=***" + rawURL[idx+end:]
	}
	return rawURL
}

// Client manages a WebSocket connection to the nlook cloud server.
type Client struct {
	url      string // ws(s)://host/api/routers/ws?token=xxx&router_id=xxx
	routerID string

	conn   *websocket.Conn
	mu     sync.Mutex
	closed bool

	sendCh chan []byte
	done   chan struct{}

	// Callback for incoming run dispatch messages
	OnRunDispatch func(payload RunDispatchPayload)
	// Callback for incoming run cancel messages
	OnRunCancel func(runID int64)
	// Callback for incoming messages (generic — for SSH etc.)
	OnMessage func(msgType string, payload json.RawMessage)

	connected     bool
	connectedMu   sync.RWMutex
	maxReconnect  time.Duration
}

// NewClient creates a new WebSocket client.
// apiURL is the base URL (e.g. "https://nlook.me"), token is the API key.
func NewClient(apiURL, token, routerID string) *Client {
	scheme := "wss"
	host := apiURL
	// Strip protocol prefix for WebSocket URL
	if len(host) > 8 && host[:8] == "https://" {
		host = host[8:]
	} else if len(host) > 7 && host[:7] == "http://" {
		host = host[7:]
		scheme = "ws"
	}

	wsURL := scheme + "://" + host + "/api/routers/ws?token=" + token + "&router_id=" + routerID

	return &Client{
		url:          wsURL,
		routerID:     routerID,
		sendCh:       make(chan []byte, 64),
		done:         make(chan struct{}),
		maxReconnect: 30 * time.Second,
	}
}

// IsConnected returns whether the WebSocket is currently connected.
func (c *Client) IsConnected() bool {
	c.connectedMu.RLock()
	defer c.connectedMu.RUnlock()
	return c.connected
}

func (c *Client) setConnected(v bool) {
	c.connectedMu.Lock()
	defer c.connectedMu.Unlock()
	c.connected = v
}

// Start connects and runs the read/write/reconnect loops in background goroutines.
func (c *Client) Start(ctx context.Context) {
	go c.reconnectLoop(ctx)
}

// Stop closes the WebSocket connection.
func (c *Client) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	close(c.done)
	if c.conn != nil {
		c.conn.Close()
	}
}

// Send sends a message to the cloud via WebSocket.
func (c *Client) Send(msg []byte) {
	select {
	case c.sendCh <- msg:
	default:
		log.Printf("ws_client: send buffer full, dropping message")
	}
}

// SendMessage sends a typed message to the cloud.
func (c *Client) SendMessage(msgType string, payload interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	msg, err := json.Marshal(WSMessage{Type: msgType, Payload: payloadBytes})
	if err != nil {
		return err
	}
	c.Send(msg)
	return nil
}

// SendRunStatus reports run status change to cloud.
func (c *Client) SendRunStatus(runID, workflowID int64, status string, output map[string]interface{}, errMsg string) {
	_ = c.SendMessage("run:status", map[string]interface{}{
		"run_id":        runID,
		"workflow_id":   workflowID,
		"status":        status,
		"output":        output,
		"error_message": errMsg,
	})
}

// SendStepStart reports step execution started to cloud.
func (c *Client) SendStepStart(runID int64, nodeID, nodeType string) {
	_ = c.SendMessage("step:start", map[string]interface{}{
		"run_id":    runID,
		"node_id":   nodeID,
		"node_type": nodeType,
	})
}

// SendStepComplete reports step execution completed to cloud.
func (c *Client) SendStepComplete(runID, logID int64, status string, output map[string]interface{}, errMsg string, logLines []string) {
	_ = c.SendMessage("step:complete", map[string]interface{}{
		"run_id":        runID,
		"log_id":        logID,
		"status":        status,
		"output":        output,
		"error_message": errMsg,
		"log_lines":     logLines,
	})
}

func (c *Client) reconnectLoop(ctx context.Context) {
	delay := time.Second

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		default:
		}

		err := c.connect()
		if err != nil {
			log.Printf("ws_client: connect error: %v (retry in %s)", err, delay)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			case <-c.done:
				return
			}
			// Exponential backoff
			delay *= 2
			if delay > c.maxReconnect {
				delay = c.maxReconnect
			}
			continue
		}

		// Reset delay on successful connection
		delay = time.Second
		c.setConnected(true)
		log.Printf("ws_client: connected to %s (routerID=%s)", sanitizeURL(c.url), c.routerID)

		// Run read/write pumps (blocks until disconnected)
		c.runPumps(ctx)

		c.setConnected(false)
		log.Printf("ws_client: disconnected, reconnecting...")
	}
}

func (c *Client) connect() error {
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, _, err := dialer.Dial(c.url, nil)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	return nil
}

func (c *Client) runPumps(ctx context.Context) {
	readDone := make(chan struct{})

	go func() {
		defer close(readDone)
		c.readPump()
	}()

	c.writePump(ctx, readDone)
}

func (c *Client) readPump() {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()

	if conn == nil {
		return
	}

	conn.SetReadLimit(512 * 1024)
	_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	})

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("ws_client: read error: %v", err)
			}
			return
		}

		c.handleMessage(data)
	}
}

func (c *Client) writePump(ctx context.Context, readDone <-chan struct{}) {
	heartbeat := time.NewTicker(30 * time.Second)
	ping := time.NewTicker(50 * time.Second)
	defer func() {
		heartbeat.Stop()
		ping.Stop()
		c.mu.Lock()
		if c.conn != nil {
			c.conn.Close()
		}
		c.mu.Unlock()
	}()

	for {
		select {
		case <-readDone:
			return
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case msg := <-c.sendCh:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()
			if conn == nil {
				continue
			}
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				log.Printf("ws_client: write error: %v", err)
				return
			}
		case <-heartbeat.C:
			_ = c.SendMessage("heartbeat", map[string]string{"router_id": c.routerID})
		case <-ping.C:
			c.mu.Lock()
			conn := c.conn
			c.mu.Unlock()
			if conn == nil {
				continue
			}
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *Client) handleMessage(data []byte) {
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("ws_client: unmarshal message: %v", err)
		return
	}

	switch msg.Type {
	case "run:dispatch":
		var payload RunDispatchPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			log.Printf("ws_client: unmarshal run:dispatch: %v", err)
			return
		}
		if c.OnRunDispatch != nil {
			c.OnRunDispatch(payload)
		}
	case "run:cancel":
		var payload RunCancelPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			log.Printf("ws_client: unmarshal run:cancel: %v", err)
			return
		}
		if c.OnRunCancel != nil {
			c.OnRunCancel(payload.RunID)
		}
	default:
		// Forward to generic handler (for SSH messages etc.)
		if c.OnMessage != nil {
			c.OnMessage(msg.Type, msg.Payload)
		}
	}
}
