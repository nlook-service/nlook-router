package agentproxy

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// SessionConfig holds agent session settings.
type SessionConfig struct {
	Workspaces      []string
	MaxSessions     int
	SessionTimeout  time.Duration
	AllowedCommands []string
}

// AgentSession represents a running claude CLI agent session.
type AgentSession struct {
	ID        string
	Workspace string
	TaskID    int64
	Process   *ClaudeProcess
	StartedAt time.Time
	mu        sync.Mutex
	closed    bool
	idleTimer *time.Timer
	onOutput  func(event ClaudeStreamEvent)
	onDone    func(result ClaudeResult)
}

// SessionManager manages agent sessions.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*AgentSession
	config   SessionConfig
	ctx      context.Context
}

// NewSessionManager creates a new SessionManager.
func NewSessionManager(ctx context.Context, cfg SessionConfig) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*AgentSession),
		config:   cfg,
		ctx:      ctx,
	}
}

// StartSession validates the workspace, starts claude CLI, and returns immediately.
func (m *SessionManager) StartSession(id string, workspace string, prompt string, args []string, taskID int64, onEvent func(ClaudeStreamEvent), onDone func(ClaudeResult)) error {
	// Validate workspace
	resolvedDir, err := ValidateWorkspace(workspace, m.config.Workspaces)
	if err != nil {
		return fmt.Errorf("validate workspace: %w", err)
	}

	// Validate command (only "claude" allowed by default)
	if !IsCommandAllowed("claude", m.config.AllowedCommands) {
		return fmt.Errorf("command 'claude' not in allowed list")
	}

	// Sanitize extra args
	safeArgs := SanitizeArgs(args)

	// Check max sessions
	m.mu.RLock()
	count := len(m.sessions)
	m.mu.RUnlock()
	if m.config.MaxSessions > 0 && count >= m.config.MaxSessions {
		return fmt.Errorf("max agent sessions reached (%d)", m.config.MaxSessions)
	}

	sess := &AgentSession{
		ID:        id,
		Workspace: resolvedDir,
		TaskID:    taskID,
		StartedAt: time.Now(),
		onOutput:  onEvent,
		onDone:    onDone,
	}

	// Wrap onDone to also clean up session
	wrappedDone := func(result ClaudeResult) {
		m.removeSession(id)
		if onDone != nil {
			onDone(result)
		}
	}

	process, err := StartClaude(m.ctx, resolvedDir, prompt, safeArgs, onEvent, wrappedDone)
	if err != nil {
		return fmt.Errorf("start claude: %w", err)
	}
	sess.Process = process

	// Session timeout
	if m.config.SessionTimeout > 0 {
		sess.idleTimer = time.AfterFunc(m.config.SessionTimeout, func() {
			log.Printf("agentproxy: session %s timed out after %v", id, m.config.SessionTimeout)
			m.StopSession(id)
		})
	}

	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()

	log.Printf("agentproxy: session started: id=%s workspace=%s task=%d", id, resolvedDir, taskID)
	return nil
}

// StopSession terminates an agent session.
func (m *SessionManager) StopSession(id string) {
	m.mu.RLock()
	sess, ok := m.sessions[id]
	m.mu.RUnlock()
	if !ok {
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		return
	}
	sess.closed = true

	if sess.idleTimer != nil {
		sess.idleTimer.Stop()
	}
	if sess.Process != nil {
		sess.Process.Stop()
	}

	m.removeSession(id)
	log.Printf("agentproxy: session stopped: id=%s", id)
}

// ListSessions returns info about active sessions.
func (m *SessionManager) ListSessions() []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	infos := make([]SessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		infos = append(infos, SessionInfo{
			ID:        s.ID,
			Workspace: s.Workspace,
			TaskID:    s.TaskID,
			StartedAt: s.StartedAt,
		})
	}
	return infos
}

// SessionInfo is the public view of a session.
type SessionInfo struct {
	ID        string    `json:"id"`
	Workspace string    `json:"workspace"`
	TaskID    int64     `json:"task_id,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

// CloseAll terminates all active sessions.
func (m *SessionManager) CloseAll() {
	m.mu.Lock()
	sessions := make([]*AgentSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.sessions = make(map[string]*AgentSession)
	m.mu.Unlock()

	for _, s := range sessions {
		if s.idleTimer != nil {
			s.idleTimer.Stop()
		}
		if s.Process != nil {
			s.Process.Stop()
		}
	}
}

func (m *SessionManager) removeSession(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}
