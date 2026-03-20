package session

import "time"

const MaxMessages = 50

// Message represents a single chat turn.
type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// AgentResult is the summary of an agent execution.
type AgentResult struct {
	AgentSessionID string    `json:"agent_session_id"`
	Result         string    `json:"result"`
	IsError        bool      `json:"is_error"`
	DurationMs     int64     `json:"duration_ms"`
	TotalCost      float64   `json:"total_cost_usd,omitempty"`
	CompletedAt    time.Time `json:"completed_at"`
}

// Context holds session-level state.
type Context struct {
	Messages     []Message              `json:"messages"`
	Variables    map[string]interface{} `json:"variables,omitempty"`
	AgentResults []AgentResult          `json:"agent_results,omitempty"`
	Summary      string                 `json:"summary,omitempty"`
}

// NewContext creates an empty context.
func NewContext() *Context {
	return &Context{
		Variables: make(map[string]interface{}),
	}
}

// AddMessage appends a message, maintaining sliding window.
func (c *Context) AddMessage(role, content string) {
	c.Messages = append(c.Messages, Message{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})
	if len(c.Messages) > MaxMessages {
		c.Messages = c.Messages[len(c.Messages)-MaxMessages:]
	}
}

// AddAgentResult records an agent completion.
func (c *Context) AddAgentResult(r AgentResult) {
	c.AgentResults = append(c.AgentResults, r)
}

// SetVariable stores a key-value pair.
func (c *Context) SetVariable(key string, value interface{}) {
	c.Variables[key] = value
}

// GetVariable returns a variable value.
func (c *Context) GetVariable(key string) (interface{}, bool) {
	v, ok := c.Variables[key]
	return v, ok
}
