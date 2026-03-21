package orchestration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// CLICaller implements LLMCaller using Claude CLI and Ollama HTTP API.
type CLICaller struct {
	ollamaURL  string
	httpClient *http.Client
}

// NewCLICaller creates a caller with default settings.
func NewCLICaller() *CLICaller {
	return &CLICaller{
		ollamaURL:  "http://localhost:11434",
		httpClient: &http.Client{Timeout: 3 * time.Minute},
	}
}

// Call invokes the appropriate backend based on model name.
func (c *CLICaller) Call(ctx context.Context, model, system, prompt string) (string, int, error) {
	if isClaudeModel(model) {
		return c.callClaudeCLI(ctx, model, system, prompt)
	}
	return c.callOllama(ctx, model, system, prompt)
}

func (c *CLICaller) callClaudeCLI(ctx context.Context, model, system, prompt string) (string, int, error) {
	claudePath := findClaudeBinary()
	if claudePath == "" {
		return "", 0, fmt.Errorf("claude CLI not found")
	}

	args := []string{"-p", prompt, "--model", model, "--output-format", "json"}
	if system != "" {
		args = append(args, "--system", system)
	}

	cmd := exec.CommandContext(ctx, claudePath, args...)
	output, err := cmd.Output()
	if err != nil {
		return "", 0, fmt.Errorf("claude CLI %s: %w", model, err)
	}

	var cliResp struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(output, &cliResp); err != nil {
		return string(output), 0, nil
	}

	return cliResp.Result, 0, nil
}

func (c *CLICaller) callOllama(ctx context.Context, model, system, prompt string) (string, int, error) {
	body := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
		"stream": false,
	}
	if system != "" {
		body["system"] = system
	}

	data, err := json.Marshal(body)
	if err != nil {
		return "", 0, fmt.Errorf("marshal ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.ollamaURL+"/api/generate", bytes.NewReader(data))
	if err != nil {
		return "", 0, fmt.Errorf("create ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("ollama call %s: %w", model, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("read ollama response: %w", err)
	}

	var ollamaResp struct {
		Response   string `json:"response"`
		EvalCount  int    `json:"eval_count"`
	}
	if err := json.Unmarshal(respBody, &ollamaResp); err != nil {
		return string(respBody), 0, nil
	}

	return ollamaResp.Response, ollamaResp.EvalCount, nil
}

func findClaudeBinary() string {
	paths := []string{"claude", "/usr/local/bin/claude", "/opt/homebrew/bin/claude"}
	for _, p := range paths {
		if path, err := exec.LookPath(p); err == nil {
			return path
		}
	}
	// Check npm global
	if out, err := exec.Command("npm", "root", "-g").Output(); err == nil {
		npmPath := strings.TrimSpace(string(out)) + "/@anthropic-ai/claude-code/cli.js"
		if _, err := exec.LookPath("node"); err == nil {
			return npmPath
		}
	}
	return ""
}
