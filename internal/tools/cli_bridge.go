package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nlook-service/nlook-router/internal/apiclient"
)

// CLIBridge calls the Python tools-bridge CLI (python -m tool_bridge --list | --run NAME --args JSON).
type CLIBridge struct {
	// Command is the full command (e.g. "python3 -m tool_bridge" or path to script).
	// If empty, defaults to "python3" with args "-m", "tool_bridge".
	Command string
	// Dir is the working directory (e.g. tools-bridge). Optional.
	Dir string
	// Timeout for each call.
	Timeout time.Duration
}

// DefaultCLIBridge returns a bridge that runs from router repo root: python3 -m tool_bridge.
// BridgeDir should be the path to tools-bridge (e.g. executable-relative or config).
func DefaultCLIBridge(bridgeDir string) *CLIBridge {
	if bridgeDir == "" {
		bridgeDir = "tools-bridge"
	}
	return &CLIBridge{
		Command: "python3",
		Dir:     bridgeDir,
		Timeout: 30 * time.Second,
	}
}

// ListTools runs the bridge with --list and parses JSON array of tool meta.
func (c *CLIBridge) ListTools(ctx context.Context) ([]apiclient.ToolMeta, error) {
	args := c.bridgeArgs("--list")
	cmd := c.buildCmd(ctx, args)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("tools-bridge --list: %w (stderr: %s)", err, ee.Stderr)
		}
		return nil, fmt.Errorf("tools-bridge --list: %w", err)
	}
	var list []apiclient.ToolMeta
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("parse tools list: %w", err)
	}
	return list, nil
}

// TestAllResult is one entry from --test-all (name, status, error).
type TestAllResult struct {
	Name   string  `json:"name"`
	Status string  `json:"status"`
	Error  *string `json:"error"`
}

// TestAll runs the bridge with -q --test-all and returns the parsed slice of results.
// Can take tens of seconds when many toolkits are loaded.
func (c *CLIBridge) TestAll(ctx context.Context) ([]TestAllResult, error) {
	args := c.bridgeArgs("-q", "--test-all")
	cmd := c.buildCmd(ctx, args)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("tools-bridge --test-all: %w (stderr: %s)", err, ee.Stderr)
		}
		return nil, fmt.Errorf("tools-bridge --test-all: %w", err)
	}
	var list []TestAllResult
	if err := json.Unmarshal(bytes.TrimSpace(out), &list); err != nil {
		return nil, fmt.Errorf("parse test-all result: %w", err)
	}
	return list, nil
}

// Execute runs the bridge with --run name --args json and returns raw JSON bytes.
func (c *CLIBridge) Execute(ctx context.Context, name string, args map[string]interface{}) ([]byte, error) {
	argsJSON := "{}"
	if len(args) > 0 {
		b, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("marshal args: %w", err)
		}
		argsJSON = string(b)
	}
	cliArgs := c.bridgeArgs("--run", name, "--args", argsJSON)
	cmd := c.buildCmd(ctx, cliArgs)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("tools-bridge --run %s: %w (stderr: %s)", name, err, ee.Stderr)
		}
		return nil, fmt.Errorf("tools-bridge --run %s: %w", name, err)
	}
	return bytes.TrimSpace(out), nil
}

// bridgeArgs returns CLI args: either ["-m", "tool_bridge", extra...] or extra only if Command is full path to script.
func (c *CLIBridge) bridgeArgs(extra ...string) []string {
	if c.Command == "" || strings.HasSuffix(c.Command, "python3") || c.Command == "python" {
		return append([]string{"-m", "tool_bridge"}, extra...)
	}
	return extra
}

func (c *CLIBridge) buildCmd(ctx context.Context, args []string) *exec.Cmd {
	name := c.Command
	if name == "" {
		name = "python3"
	}
	cmd := exec.CommandContext(ctx, name, args...)
	if c.Dir != "" {
		cmd.Dir = c.resolveDir()
	}
	if c.Timeout > 0 {
		// CommandContext already uses ctx; timeout can be enforced by caller
		_ = c.Timeout
	}
	return cmd
}

func (c *CLIBridge) resolveDir() string {
	if filepath.IsAbs(c.Dir) {
		return c.Dir
	}
	// Prefer executable-relative path (e.g. next to nlook-router binary)
	exe, err := os.Executable()
	if err != nil {
		return c.Dir
	}
	base := filepath.Dir(exe)
	return filepath.Join(base, c.Dir)
}
