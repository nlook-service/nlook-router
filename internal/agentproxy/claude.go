package agentproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
)

// ClaudeStreamEvent represents a parsed event from claude --output-format stream-json.
type ClaudeStreamEvent struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype,omitempty"`

	// For type="assistant"
	Message *ClaudeMessage `json:"message,omitempty"`

	// For type="result"
	Result    string  `json:"result,omitempty"`
	IsError   bool    `json:"is_error,omitempty"`
	StopReason string `json:"stop_reason,omitempty"`

	// Common fields
	SessionID  string         `json:"session_id,omitempty"`
	UUID       string         `json:"uuid,omitempty"`
	DurationMs int64          `json:"duration_ms,omitempty"`
	TotalCost  float64        `json:"total_cost_usd,omitempty"`
	Usage      *ClaudeUsage   `json:"usage,omitempty"`
	ModelUsage json.RawMessage `json:"modelUsage,omitempty"`
}

// ClaudeMessage is the assistant message from stream events.
type ClaudeMessage struct {
	Model   string          `json:"model,omitempty"`
	ID      string          `json:"id,omitempty"`
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
	Usage   *ClaudeUsage    `json:"usage,omitempty"`
}

// ClaudeUsage tracks token consumption.
type ClaudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// ClaudeResult is the final result after a claude session completes.
type ClaudeResult struct {
	SessionID  string       `json:"session_id"`
	Result     string       `json:"result"`
	IsError    bool         `json:"is_error"`
	StopReason string       `json:"stop_reason"`
	DurationMs int64        `json:"duration_ms"`
	TotalCost  float64      `json:"total_cost_usd"`
	Usage      *ClaudeUsage `json:"usage,omitempty"`
	ExitCode   int          `json:"exit_code"`
}

// ClaudeProcess manages a running claude CLI process.
type ClaudeProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
}

// StartClaude launches the claude CLI in print+stream-json mode within the given directory.
// onEvent is called for each parsed stream event.
// onDone is called when the process exits with the final result.
func StartClaude(ctx context.Context, dir string, prompt string, args []string, onEvent func(ClaudeStreamEvent), onDone func(ClaudeResult)) (*ClaudeProcess, error) {
	ctx, cancel := context.WithCancel(ctx)

	cmdArgs := []string{
		"--print",
		"--output-format", "stream-json",
		"--verbose",
		"--dangerously-skip-permissions",
		"--no-session-persistence",
	}
	cmdArgs = append(cmdArgs, args...)
	cmdArgs = append(cmdArgs, prompt)

	cmd := exec.CommandContext(ctx, "claude", cmdArgs...)
	cmd.Dir = dir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start claude: %w", err)
	}

	cp := &ClaudeProcess{
		cmd:    cmd,
		cancel: cancel,
	}

	// Parse stream-json output in background
	go func() {
		var finalResult ClaudeResult
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 256*1024), 256*1024)

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 {
				continue
			}

			var event ClaudeStreamEvent
			if err := json.Unmarshal(line, &event); err != nil {
				log.Printf("agentproxy: parse stream event: %v", err)
				continue
			}

			// Skip system/hook events
			if event.Type == "system" {
				continue
			}

			if event.Type == "result" {
				finalResult = ClaudeResult{
					SessionID:  event.SessionID,
					Result:     event.Result,
					IsError:    event.IsError,
					StopReason: event.StopReason,
					DurationMs: event.DurationMs,
					TotalCost:  event.TotalCost,
					Usage:      event.Usage,
				}
			}

			if onEvent != nil {
				onEvent(event)
			}
		}

		// Drain stderr
		stderrBytes, _ := io.ReadAll(stderr)
		if len(stderrBytes) > 0 {
			log.Printf("agentproxy: claude stderr: %s", string(stderrBytes))
		}

		exitCode := 0
		if err := cmd.Wait(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
		}
		finalResult.ExitCode = exitCode

		if onDone != nil {
			onDone(finalResult)
		}
	}()

	return cp, nil
}

// Stop terminates the claude process.
func (cp *ClaudeProcess) Stop() {
	cp.mu.Lock()
	defer cp.mu.Unlock()
	if cp.closed {
		return
	}
	cp.closed = true
	cp.cancel()
}
