package sshproxy

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"
)

// Config holds SSH connection settings.
type Config struct {
	Host       string `yaml:"host" json:"host"`
	Port       int    `yaml:"port" json:"port"`
	User       string `yaml:"user" json:"user"`
	Password   string `yaml:"password,omitempty" json:"password,omitempty"`
	PrivateKey string `yaml:"private_key,omitempty" json:"private_key,omitempty"`
	AuthType   string `yaml:"auth_type,omitempty" json:"auth_type,omitempty"` // "password" or "key"
}

// ProxyConfig holds security settings for the SSH proxy.
type ProxyConfig struct {
	AllowedHosts   []string      `yaml:"allowed_hosts" json:"allowed_hosts"`     // empty = allow all
	IdleTimeout    time.Duration `yaml:"idle_timeout" json:"idle_timeout"`       // 0 = no timeout
	MaxSessions    int           `yaml:"max_sessions" json:"max_sessions"`       // 0 = unlimited
	MaxOutputRate  int           `yaml:"max_output_rate" json:"max_output_rate"` // bytes per second per session, 0 = unlimited
}

// Session represents an active SSH terminal session.
type Session struct {
	ID           string
	sshClient    *ssh.Client
	session      *ssh.Session
	stdinPipe    io.WriteCloser
	mu           sync.Mutex
	closed       bool
	onOutput     func(data []byte) // callback for terminal output
	onClose      func()            // callback when session ends
	lastActivity time.Time         // last read/write activity
	idleTimer    *time.Timer       // idle timeout timer
	// Local shell mode (no SSH)
	localPTY     *os.File          // PTY master fd (nil for SSH sessions)
	localCmd     *exec.Cmd         // local shell process (nil for SSH sessions)
	// Output rate limiting
	outputBytes  int64             // bytes sent in current window
	outputWindow time.Time         // current rate limit window start
}

// Proxy manages SSH sessions on the local machine.
type Proxy struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	config   ProxyConfig
}

// DefaultProxyConfig returns default security settings.
func DefaultProxyConfig() ProxyConfig {
	return ProxyConfig{
		AllowedHosts:  nil,                    // allow all
		IdleTimeout:   30 * time.Minute,       // 30 min idle timeout
		MaxSessions:   10,                     // max 10 concurrent sessions
		MaxOutputRate: 512 * 1024,             // 512 KB/s per session (enough for terminal, blocks large cat)
	}
}

// NewProxy creates a new SSH Proxy with default config.
func NewProxy() *Proxy {
	return NewProxyWithConfig(DefaultProxyConfig())
}

// NewProxyWithConfig creates a new SSH Proxy with custom config.
func NewProxyWithConfig(cfg ProxyConfig) *Proxy {
	return &Proxy{
		sessions: make(map[string]*Session),
		config:   cfg,
	}
}

// OpenSession establishes a new SSH connection and PTY session.
// For localhost, it spawns a local shell directly (no SSH) to avoid macOS permission issues.
func (p *Proxy) OpenSession(sessionID string, cfg Config, cols, rows int, onOutput func(data []byte), onClose func()) error {
	if cfg.Host == "" {
		cfg.Host = "127.0.0.1"
	}
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}

	// Localhost: spawn local shell directly (faster, no sshd permission issues)
	if isLocalHost(cfg.Host) {
		return p.OpenLocalSession(sessionID, cols, rows, onOutput, onClose)
	}

	// Security: check allowed hosts
	if !p.isHostAllowed(cfg.Host) {
		return fmt.Errorf("host %s is not in the allowed hosts list", cfg.Host)
	}

	// Security: check max sessions
	if p.config.MaxSessions > 0 && p.ActiveSessions() >= p.config.MaxSessions {
		return fmt.Errorf("max sessions limit reached (%d)", p.config.MaxSessions)
	}

	// Build SSH client config
	hostKeyCallback, err := newHostKeyCallback()
	if err != nil {
		return fmt.Errorf("host key callback: %w", err)
	}
	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}

	if cfg.Password != "" {
		sshCfg.Auth = append(sshCfg.Auth, ssh.Password(cfg.Password))
	}
	if cfg.PrivateKey != "" {
		signer, err := ssh.ParsePrivateKey([]byte(cfg.PrivateKey))
		if err != nil {
			return fmt.Errorf("parse private key: %w", err)
		}
		sshCfg.Auth = append(sshCfg.Auth, ssh.PublicKeys(signer))
	}

	if len(sshCfg.Auth) == 0 {
		return fmt.Errorf("no authentication method provided (password or private_key required)")
	}

	// Connect
	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	sshClient, err := ssh.Dial("tcp", addr, sshCfg)
	if err != nil {
		return fmt.Errorf("ssh dial %s: %w", addr, err)
	}

	sess, err := sshClient.NewSession()
	if err != nil {
		sshClient.Close()
		return fmt.Errorf("new ssh session: %w", err)
	}

	// Request PTY
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		sess.Close()
		sshClient.Close()
		return fmt.Errorf("request pty: %w", err)
	}

	stdinPipe, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		sshClient.Close()
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdoutPipe, err := sess.StdoutPipe()
	if err != nil {
		sess.Close()
		sshClient.Close()
		return fmt.Errorf("stdout pipe: %w", err)
	}

	stderrPipe, err := sess.StderrPipe()
	if err != nil {
		sess.Close()
		sshClient.Close()
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := sess.Shell(); err != nil {
		sess.Close()
		sshClient.Close()
		return fmt.Errorf("start shell: %w", err)
	}

	s := &Session{
		ID:           sessionID,
		sshClient:    sshClient,
		session:      sess,
		stdinPipe:    stdinPipe,
		onOutput:     onOutput,
		onClose:      onClose,
		lastActivity: time.Now(),
		outputWindow: time.Now(),
	}

	// Apply rate limiting
	onOutput = p.throttledOutput(s, onOutput)

	p.mu.Lock()
	p.sessions[sessionID] = s
	p.mu.Unlock()

	// Start idle timeout timer
	p.startIdleTimer(s)

	// Read stdout and stderr in background, forward to onOutput
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stdoutPipe.Read(buf)
			if n > 0 && onOutput != nil {
				p.resetIdleTimer(s)
				data := make([]byte, n)
				copy(data, buf[:n])
				onOutput(data)
			}
			if err != nil {
				break
			}
		}
		s.Close()
	}()

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderrPipe.Read(buf)
			if n > 0 && onOutput != nil {
				p.resetIdleTimer(s)
				data := make([]byte, n)
				copy(data, buf[:n])
				onOutput(data)
			}
			if err != nil {
				break
			}
		}
	}()

	// Wait for session to end
	go func() {
		if err := sess.Wait(); err != nil {
			log.Printf("ssh session %s ended: %v", sessionID, err)
		}
		s.Close()
	}()

	log.Printf("ssh session opened: id=%s user=%s@%s", sessionID, cfg.User, addr)
	return nil
}

// WriteData sends input data to an active SSH session (keyboard input from web terminal).
func (p *Proxy) WriteData(sessionID string, data []byte) error {
	p.mu.RLock()
	s, ok := p.sessions[sessionID]
	p.mu.RUnlock()

	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("session %s is closed", sessionID)
	}

	s.lastActivity = time.Now()
	if s.idleTimer != nil {
		s.idleTimer.Reset(p.config.IdleTimeout)
	}

	_, err := s.stdinPipe.Write(data)
	return err
}

// Resize changes the terminal dimensions of an active SSH session.
func (p *Proxy) Resize(sessionID string, cols, rows int) error {
	p.mu.RLock()
	s, ok := p.sessions[sessionID]
	p.mu.RUnlock()

	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return fmt.Errorf("session %s is closed", sessionID)
	}

	// Local shell mode: resize PTY
	if s.localPTY != nil {
		return pty.Setsize(s.localPTY, &pty.Winsize{
			Rows: uint16(rows),
			Cols: uint16(cols),
		})
	}

	return s.session.WindowChange(rows, cols)
}

// CloseSession terminates an SSH session.
func (p *Proxy) CloseSession(sessionID string) {
	p.mu.RLock()
	s, ok := p.sessions[sessionID]
	p.mu.RUnlock()

	if !ok {
		return
	}

	s.Close()
	p.mu.Lock()
	delete(p.sessions, sessionID)
	p.mu.Unlock()
}

// Close terminates the SSH session and cleans up resources.
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}
	s.closed = true

	if s.idleTimer != nil {
		s.idleTimer.Stop()
	}
	// Local shell mode cleanup
	if s.localCmd != nil && s.localCmd.Process != nil {
		s.localCmd.Process.Signal(os.Interrupt)
	}
	if s.localPTY != nil {
		s.localPTY.Close()
	}

	if s.stdinPipe != nil && s.localPTY == nil {
		// Only close stdinPipe for SSH mode (localPTY is the same fd)
		s.stdinPipe.Close()
	}
	if s.session != nil {
		s.session.Close()
	}
	if s.sshClient != nil {
		s.sshClient.Close()
	}

	if s.onClose != nil {
		s.onClose()
	}

	log.Printf("session closed: id=%s", s.ID)
}

// ActiveSessions returns the number of active sessions.
func (p *Proxy) ActiveSessions() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.sessions)
}

// isHostAllowed checks if a host is in the allowed hosts list.
// If no allowed hosts are configured, all hosts are permitted.
func (p *Proxy) isHostAllowed(host string) bool {
	if len(p.config.AllowedHosts) == 0 {
		return true
	}
	for _, h := range p.config.AllowedHosts {
		if h == host {
			return true
		}
	}
	return false
}

// resetIdleTimer resets the idle timeout timer for a session.
func (p *Proxy) resetIdleTimer(s *Session) {
	if p.config.IdleTimeout <= 0 {
		return
	}
	s.lastActivity = time.Now()
	if s.idleTimer != nil {
		s.idleTimer.Reset(p.config.IdleTimeout)
	}
}

// startIdleTimer starts the idle timeout timer for a session.
func (p *Proxy) startIdleTimer(s *Session) {
	if p.config.IdleTimeout <= 0 {
		return
	}
	s.lastActivity = time.Now()
	s.idleTimer = time.AfterFunc(p.config.IdleTimeout, func() {
		log.Printf("ssh session %s idle timeout (%v)", s.ID, p.config.IdleTimeout)
		p.CloseSession(s.ID)
	})
}

// throttledOutput wraps onOutput with rate limiting.
// Returns a function that drops data exceeding MaxOutputRate bytes/sec.
func (p *Proxy) throttledOutput(s *Session, onOutput func(data []byte)) func(data []byte) {
	if p.config.MaxOutputRate <= 0 {
		return onOutput
	}
	return func(data []byte) {
		now := time.Now()
		s.mu.Lock()
		// Reset window every second
		if now.Sub(s.outputWindow) >= time.Second {
			s.outputBytes = 0
			s.outputWindow = now
		}
		remaining := int64(p.config.MaxOutputRate) - s.outputBytes
		s.mu.Unlock()

		if remaining <= 0 {
			// Drop data exceeding rate limit — terminal will still work,
			// just large output (cat big_file) gets truncated
			return
		}

		send := data
		if int64(len(data)) > remaining {
			send = data[:remaining]
		}

		s.mu.Lock()
		s.outputBytes += int64(len(send))
		s.mu.Unlock()

		onOutput(send)
	}
}

// CloseAll terminates all active sessions.
func (p *Proxy) CloseAll() {
	p.mu.Lock()
	sessions := make([]*Session, 0, len(p.sessions))
	for _, s := range p.sessions {
		sessions = append(sessions, s)
	}
	p.sessions = make(map[string]*Session)
	p.mu.Unlock()

	for _, s := range sessions {
		s.Close()
	}
}
