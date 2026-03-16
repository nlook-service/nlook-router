package sshproxy

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"time"

	"github.com/creack/pty"
)

// isLocalHost returns true if the host is localhost or 127.0.0.1.
func isLocalHost(host string) bool {
	return host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// OpenLocalSession spawns a local shell with PTY instead of SSH.
// This avoids macOS Full Disk Access permission issues with sshd.
func (p *Proxy) OpenLocalSession(sessionID string, cols, rows int, onOutput func(data []byte), onClose func()) error {
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}

	// Security: check max sessions
	if p.config.MaxSessions > 0 && p.ActiveSessions() >= p.config.MaxSessions {
		return fmt.Errorf("max sessions limit reached (%d)", p.config.MaxSessions)
	}

	// Determine shell
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}

	// Get current user for home dir
	u, err := user.Current()
	if err != nil {
		return fmt.Errorf("get current user: %w", err)
	}

	cmd := exec.Command(shell, "-l")
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		fmt.Sprintf("HOME=%s", u.HomeDir),
	)
	cmd.Dir = u.HomeDir

	// Start with PTY
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
	if err != nil {
		return fmt.Errorf("start pty: %w", err)
	}

	s := &Session{
		ID:           sessionID,
		stdinPipe:    ptmx, // PTY master is both read and write
		onOutput:     onOutput,
		onClose:      onClose,
		lastActivity: time.Now(),
		outputWindow: time.Now(),
		localPTY:     ptmx,
		localCmd:     cmd,
	}

	// Apply rate limiting
	onOutput = p.throttledOutput(s, onOutput)

	p.mu.Lock()
	p.sessions[sessionID] = s
	p.mu.Unlock()

	// Start idle timeout timer
	p.startIdleTimer(s)

	// Read PTY output in background
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
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

	// Wait for command to exit
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("local shell %s ended: %v", sessionID, err)
		}
		s.Close()
	}()

	log.Printf("local shell session opened: id=%s shell=%s", sessionID, shell)
	return nil
}
