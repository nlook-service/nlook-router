package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nlook-service/nlook-router/internal/apiclient"
	"github.com/nlook-service/nlook-router/internal/reasoning"
)

// ToolExecutor runs a tool by name (e.g. Python bridge). If nil, runTool returns a placeholder.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args map[string]interface{}) ([]byte, error)
}

// MCPExecutor calls nlook MCP tools (e.g. create_document, add_document_to_workspace).
type MCPExecutor interface {
	CallTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error)
}

// SkillRunner executes workflow skills by type.
type SkillRunner struct {
	httpClient   *http.Client
	toolExecutor ToolExecutor
	mcpClient    MCPExecutor
	reasoningMgr *reasoning.Manager
}

// NewSkillRunner creates a new SkillRunner.
func NewSkillRunner() *SkillRunner {
	return &SkillRunner{
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

// SetToolExecutor sets the bridge for tool execution. If not set, runTool returns a placeholder.
func (r *SkillRunner) SetToolExecutor(e ToolExecutor) {
	r.toolExecutor = e
}

// SetMCPClient sets the MCP client for mcp-type skill execution.
func (r *SkillRunner) SetMCPClient(c MCPExecutor) {
	r.mcpClient = c
}

// SetReasoningManager sets the reasoning manager for reasoning-enabled skills.
func (r *SkillRunner) SetReasoningManager(mgr *reasoning.Manager) {
	r.reasoningMgr = mgr
}

// RunSkill dispatches execution to the appropriate handler based on skill type.
func (r *SkillRunner) RunSkill(ctx context.Context, skill *apiclient.WorkflowSkill, agent *apiclient.WorkflowAgent, input map[string]interface{}) (map[string]interface{}, []string, error) {
	switch skill.SkillType {
	case "prompt":
		return r.runPrompt(ctx, skill, agent, input)
	case "api":
		return r.runAPI(ctx, skill, input)
	case "tool":
		return r.runTool(ctx, skill, input)
	case "mcp":
		return r.runMCP(ctx, skill, input)
	default:
		return map[string]interface{}{"message": "unsupported skill type"}, nil, nil
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Prompt skill — LLM API call
// ──────────────────────────────────────────────────────────────────────────────

type llmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model       string       `json:"model"`
	MaxTokens   int          `json:"max_tokens"`
	System      string       `json:"system,omitempty"`
	Messages    []llmMessage `json:"messages"`
	Temperature float64      `json:"temperature,omitempty"`
}

type anthropicContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicResponse struct {
	ID      string                  `json:"id"`
	Content []anthropicContentBlock `json:"content"`
	Model   string                  `json:"model"`
	Usage   map[string]interface{}  `json:"usage"`
}

type openaiRequest struct {
	Model       string       `json:"model"`
	Messages    []llmMessage `json:"messages"`
	MaxTokens   int          `json:"max_tokens,omitempty"`
	Temperature float64      `json:"temperature,omitempty"`
}

type openaiChoice struct {
	Message llmMessage `json:"message"`
}

type openaiResponse struct {
	ID      string                 `json:"id"`
	Choices []openaiChoice         `json:"choices"`
	Model   string                 `json:"model"`
	Usage   map[string]interface{} `json:"usage"`
}

func (r *SkillRunner) runPrompt(ctx context.Context, skill *apiclient.WorkflowSkill, agent *apiclient.WorkflowAgent, input map[string]interface{}) (map[string]interface{}, []string, error) {
	// Resolve prompt template
	prompt := resolveTemplate(skill.Content, input)
	logs := []string{fmt.Sprintf("prompt resolved (%d chars)", len(prompt))}

	// Determine model and params from agent
	model := "claude-sonnet-4-20250514"
	systemPrompt := ""
	temperature := 0.7
	maxTokens := 4096

	if agent != nil {
		if agent.Model != "" {
			model = agent.Model
		}
		if agent.SystemPrompt != "" {
			systemPrompt = agent.SystemPrompt
		}
		temperature = agent.Temperature
		if agent.MaxTokens > 0 {
			maxTokens = agent.MaxTokens
		}
	}

	logs = append(logs, fmt.Sprintf("model=%s temperature=%.1f max_tokens=%d", model, temperature, maxTokens))

	// Check reasoning mode from skill/agent config
	reasoningEnabled := getConfigBool(skill.Config, "reasoning_enabled")
	if agent != nil {
		if v := getConfigBool(agent.Config, "reasoning_enabled"); v {
			reasoningEnabled = true
		}
	}

	if reasoningEnabled && r.reasoningMgr != nil {
		cfg := reasoning.DefaultConfig()
		cfg.Model = model
		cfg.Temperature = temperature
		cfg.MaxTokens = maxTokens
		if ms := getConfigInt(skill.Config, "reasoning_max_steps"); ms > 0 {
			cfg.MaxSteps = ms
		}

		logs = append(logs, "reasoning: enabled")
		answer, reasoningData, err := r.reasoningMgr.ReasonWithData(ctx, model, systemPrompt, prompt, cfg)
		if err != nil {
			logs = append(logs, fmt.Sprintf("reasoning failed, fallback to 1-shot: %v", err))
		} else {
			logs = append(logs, fmt.Sprintf("reasoning: %d steps, provider=%s", reasoningData.StepCount, reasoningData.Provider))
			result := map[string]interface{}{
				"text":      answer,
				"model":     model,
				"reasoning": reasoningData,
			}
			return result, logs, nil
		}
	}

	// Route to appropriate backend based on model name (1-shot)
	if strings.HasPrefix(model, "claude") || strings.HasPrefix(model, "anthropic") {
		return r.callClaudeCLI(ctx, model, systemPrompt, prompt, maxTokens, logs)
	}
	if isLocalModel(model) {
		return r.callOllama(ctx, model, systemPrompt, prompt, temperature, maxTokens, logs)
	}
	return r.callOpenAI(ctx, model, systemPrompt, prompt, temperature, maxTokens, logs)
}

func getConfigBool(cfg map[string]interface{}, key string) bool {
	if cfg == nil {
		return false
	}
	v, ok := cfg[key]
	if !ok {
		return false
	}
	b, _ := v.(bool)
	return b
}

func getConfigInt(cfg map[string]interface{}, key string) int {
	if cfg == nil {
		return 0
	}
	v, ok := cfg[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}

// callClaudeCLI uses the Claude Code CLI to call Claude models (no API key needed).
func (r *SkillRunner) callClaudeCLI(ctx context.Context, model, system, prompt string, maxTokens int, logs []string) (map[string]interface{}, []string, error) {
	claudePath := findClaudeCLI()
	if claudePath == "" {
		logs = append(logs, "claude CLI not found, falling back to Anthropic API")
		return r.callAnthropicAPI(ctx, model, system, prompt, 0.7, maxTokens, logs)
	}

	// Build full prompt with system prompt
	var fullPrompt strings.Builder
	if system != "" {
		fullPrompt.WriteString(system)
		fullPrompt.WriteString("\n\n")
	}
	fullPrompt.WriteString(prompt)

	logs = append(logs, fmt.Sprintf("claude CLI: %s model=%s prompt=%d chars", claudePath, model, fullPrompt.Len()))

	args := []string{"-p", fullPrompt.String(), "--model", model, "--output-format", "json"}

	cmd := exec.CommandContext(ctx, claudePath, args...)
	output, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			logs = append(logs, fmt.Sprintf("claude CLI stderr: %s", truncate(string(ee.Stderr), 500)))
		}
		return nil, logs, fmt.Errorf("claude CLI: %w", err)
	}

	// Parse JSON output: {"type":"result","result":"text content","usage":{...}}
	var cliResp struct {
		Type   string `json:"type"`
		Result string `json:"result"`
		Usage  struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(output, &cliResp); err != nil {
		// If not JSON, use raw output as text
		text := strings.TrimSpace(string(output))
		logs = append(logs, fmt.Sprintf("claude CLI raw output: %d chars", len(text)))
		return map[string]interface{}{
			"text":  text,
			"model": model + " (CLI)",
		}, logs, nil
	}

	logs = append(logs, fmt.Sprintf("claude CLI result: %d chars, in=%d out=%d tokens",
		len(cliResp.Result), cliResp.Usage.InputTokens, cliResp.Usage.OutputTokens))

	return map[string]interface{}{
		"text":  cliResp.Result,
		"model": model + " (CLI)",
		"usage": map[string]interface{}{
			"input_tokens":  cliResp.Usage.InputTokens,
			"output_tokens": cliResp.Usage.OutputTokens,
		},
	}, logs, nil
}

// findClaudeCLI returns the path to claude CLI binary.
func findClaudeCLI() string {
	if p, err := exec.LookPath("claude"); err == nil {
		return p
	}
	paths := []string{
		os.Getenv("HOME") + "/.local/bin/claude",
		"/usr/local/bin/claude",
		"/opt/homebrew/bin/claude",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// callAnthropicAPI calls Anthropic API directly (fallback when CLI not available).
func (r *SkillRunner) callAnthropicAPI(ctx context.Context, model, system, prompt string, temperature float64, maxTokens int, logs []string) (map[string]interface{}, []string, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, logs, fmt.Errorf("ANTHROPIC_API_KEY not set and claude CLI not found")
	}

	reqBody := anthropicRequest{
		Model:       model,
		MaxTokens:   maxTokens,
		System:      system,
		Messages:    []llmMessage{{Role: "user", Content: prompt}},
		Temperature: temperature,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, logs, fmt.Errorf("marshal anthropic request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewBuffer(body))
	if err != nil {
		return nil, logs, fmt.Errorf("create anthropic request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, logs, fmt.Errorf("anthropic API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, logs, fmt.Errorf("read anthropic response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logs = append(logs, fmt.Sprintf("anthropic error: status=%d body=%s", resp.StatusCode, truncate(string(respBody), 500)))
		return nil, logs, fmt.Errorf("anthropic API error: status %d", resp.StatusCode)
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, logs, fmt.Errorf("unmarshal anthropic response: %w", err)
	}

	text := ""
	for _, block := range anthropicResp.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}

	logs = append(logs, fmt.Sprintf("anthropic response: %d chars, model=%s", len(text), anthropicResp.Model))

	return map[string]interface{}{
		"text":  text,
		"model": anthropicResp.Model,
		"usage": anthropicResp.Usage,
	}, logs, nil
}

func (r *SkillRunner) callOpenAI(ctx context.Context, model, system, prompt string, temperature float64, maxTokens int, logs []string) (map[string]interface{}, []string, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, logs, fmt.Errorf("OPENAI_API_KEY not set")
	}

	messages := make([]llmMessage, 0, 2)
	if system != "" {
		messages = append(messages, llmMessage{Role: "system", Content: system})
	}
	messages = append(messages, llmMessage{Role: "user", Content: prompt})

	reqBody := openaiRequest{
		Model:       model,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, logs, fmt.Errorf("marshal openai request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewBuffer(body))
	if err != nil {
		return nil, logs, fmt.Errorf("create openai request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, logs, fmt.Errorf("openai API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, logs, fmt.Errorf("read openai response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logs = append(logs, fmt.Sprintf("openai error: status=%d body=%s", resp.StatusCode, truncate(string(respBody), 500)))
		return nil, logs, fmt.Errorf("openai API error: status %d", resp.StatusCode)
	}

	var openaiResp openaiResponse
	if err := json.Unmarshal(respBody, &openaiResp); err != nil {
		return nil, logs, fmt.Errorf("unmarshal openai response: %w", err)
	}

	text := ""
	if len(openaiResp.Choices) > 0 {
		text = openaiResp.Choices[0].Message.Content
	}

	logs = append(logs, fmt.Sprintf("openai response: %d chars, model=%s", len(text), openaiResp.Model))

	return map[string]interface{}{
		"text":  text,
		"model": openaiResp.Model,
		"usage": openaiResp.Usage,
	}, logs, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// API skill — HTTP request to external endpoint
// ──────────────────────────────────────────────────────────────────────────────

func (r *SkillRunner) runAPI(ctx context.Context, skill *apiclient.WorkflowSkill, input map[string]interface{}) (map[string]interface{}, []string, error) {
	config := skill.Config
	if config == nil {
		return nil, nil, fmt.Errorf("api skill has no config")
	}

	url, _ := config["url"].(string)
	method, _ := config["method"].(string)
	if url == "" {
		return nil, nil, fmt.Errorf("api skill config missing 'url'")
	}
	if method == "" {
		method = "GET"
	}

	url = resolveTemplate(url, input)
	logs := []string{fmt.Sprintf("api call: %s %s", method, url)}

	// Build request body from template or input
	var bodyReader io.Reader
	if bodyTmpl, ok := config["body_template"].(string); ok && bodyTmpl != "" {
		resolved := resolveTemplate(bodyTmpl, input)
		bodyReader = strings.NewReader(resolved)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return nil, logs, fmt.Errorf("create api request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Apply custom headers from config
	if headers, ok := config["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if sv, ok := v.(string); ok {
				req.Header.Set(k, sv)
			}
		}
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, logs, fmt.Errorf("api request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, logs, fmt.Errorf("read api response: %w", err)
	}

	logs = append(logs, fmt.Sprintf("api response: status=%d size=%d", resp.StatusCode, len(respBody)))

	// Try to parse as JSON, fallback to raw text
	var jsonResp interface{}
	if err := json.Unmarshal(respBody, &jsonResp); err != nil {
		return map[string]interface{}{
			"status_code": resp.StatusCode,
			"body":        string(respBody),
		}, logs, nil
	}

	return map[string]interface{}{
		"status_code": resp.StatusCode,
		"body":        jsonResp,
	}, logs, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Tool skill — built-in tool execution (placeholder for future tools)
// ──────────────────────────────────────────────────────────────────────────────

func (r *SkillRunner) runTool(ctx context.Context, skill *apiclient.WorkflowSkill, input map[string]interface{}) (map[string]interface{}, []string, error) {
	config := skill.Config
	toolName, _ := config["tool_name"].(string)
	if toolName == "" {
		toolName = skill.Name
	}

	logs := []string{fmt.Sprintf("tool execution: %s", toolName)}

	if r.toolExecutor != nil {
		raw, err := r.toolExecutor.Execute(ctx, toolName, input)
		if err != nil {
			logs = append(logs, fmt.Sprintf("tool error: %v", err))
			return map[string]interface{}{
				"tool": toolName, "error": err.Error(), "input": input,
			}, logs, nil
		}
		var out map[string]interface{}
		if err := json.Unmarshal(raw, &out); err != nil {
			logs = append(logs, fmt.Sprintf("parse result: %v", err))
			return map[string]interface{}{"tool": toolName, "raw": string(raw), "input": input}, logs, nil
		}
		return out, logs, nil
	}

	return map[string]interface{}{
		"tool":    toolName,
		"message": fmt.Sprintf("tool '%s' executed (no built-in handler registered)", toolName),
		"input":   input,
	}, logs, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// MCP skill — nlook API tool execution
// ──────────────────────────────────────────────────────────────────────────────

func (r *SkillRunner) runMCP(ctx context.Context, skill *apiclient.WorkflowSkill, input map[string]interface{}) (map[string]interface{}, []string, error) {
	if r.mcpClient == nil {
		return nil, nil, fmt.Errorf("mcp client not configured")
	}

	config := skill.Config
	toolName, _ := config["tool_name"].(string)
	if toolName == "" {
		toolName = skill.Name
	}

	// Merge config args with input (input overrides)
	args := make(map[string]interface{})
	if configArgs, ok := config["args"].(map[string]interface{}); ok {
		for k, v := range configArgs {
			args[k] = v
		}
	}
	for k, v := range input {
		args[k] = v
	}

	// Resolve templates in string args
	for k, v := range args {
		if sv, ok := v.(string); ok {
			args[k] = resolveTemplate(sv, input)
		}
	}

	logs := []string{fmt.Sprintf("mcp call: %s", toolName)}

	result, err := r.mcpClient.CallTool(ctx, toolName, args)
	if err != nil {
		return nil, logs, fmt.Errorf("mcp tool %s: %w", toolName, err)
	}

	output := toOutputMap(result)
	logs = append(logs, fmt.Sprintf("mcp result: %d keys", len(output)))

	return output, logs, nil
}

// toOutputMap converts an interface{} to map[string]interface{}.
func toOutputMap(v interface{}) map[string]interface{} {
	switch m := v.(type) {
	case map[string]interface{}:
		return m
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return map[string]interface{}{"result": fmt.Sprintf("%v", v)}
		}
		var out map[string]interface{}
		if err := json.Unmarshal(raw, &out); err != nil {
			return map[string]interface{}{"raw": string(raw)}
		}
		return out
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// Ollama / local model support (OpenAI-compatible API)
// ──────────────────────────────────────────────────────────────────────────────

// localModelPrefixes lists model name prefixes that indicate a local Ollama model.
var localModelPrefixes = []string{
	"qwen", "llama", "mistral", "codellama", "gemma", "phi",
	"deepseek", "starcoder", "vicuna", "orca", "wizardcoder",
	"ollama/", "local/",
}

// isLocalModel returns true if the model name matches a known local/Ollama model.
func isLocalModel(model string) bool {
	lower := strings.ToLower(model)
	for _, prefix := range localModelPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func (r *SkillRunner) callOllama(ctx context.Context, model, system, prompt string, temperature float64, maxTokens int, logs []string) (map[string]interface{}, []string, error) {
	// Determine Ollama endpoint (default: http://localhost:11434)
	baseURL := os.Getenv("OLLAMA_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}

	// Strip "ollama/" or "local/" prefix if present
	cleanModel := model
	for _, prefix := range []string{"ollama/", "local/"} {
		if strings.HasPrefix(strings.ToLower(cleanModel), prefix) {
			cleanModel = cleanModel[len(prefix):]
			break
		}
	}

	logs = append(logs, fmt.Sprintf("ollama: model=%s endpoint=%s", cleanModel, baseURL))

	// Ollama supports OpenAI-compatible API at /v1/chat/completions
	messages := make([]llmMessage, 0, 2)
	if system != "" {
		messages = append(messages, llmMessage{Role: "system", Content: system})
	}
	messages = append(messages, llmMessage{Role: "user", Content: prompt})

	reqBody := openaiRequest{
		Model:       cleanModel,
		Messages:    messages,
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, logs, fmt.Errorf("marshal ollama request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewBuffer(body))
	if err != nil {
		return nil, logs, fmt.Errorf("create ollama request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, logs, fmt.Errorf("ollama API call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, logs, fmt.Errorf("read ollama response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logs = append(logs, fmt.Sprintf("ollama error: status=%d body=%s", resp.StatusCode, truncate(string(respBody), 500)))
		return nil, logs, fmt.Errorf("ollama API error: status %d", resp.StatusCode)
	}

	var openaiResp openaiResponse
	if err := json.Unmarshal(respBody, &openaiResp); err != nil {
		return nil, logs, fmt.Errorf("unmarshal ollama response: %w", err)
	}

	text := ""
	if len(openaiResp.Choices) > 0 {
		text = openaiResp.Choices[0].Message.Content
	}

	logs = append(logs, fmt.Sprintf("ollama response: %d chars, model=%s", len(text), cleanModel))

	return map[string]interface{}{
		"text":  text,
		"model": cleanModel,
		"usage": openaiResp.Usage,
		"local": true,
	}, logs, nil
}

// resolveTemplate replaces {{key}} patterns in a template string with values from input.
func resolveTemplate(tmpl string, input map[string]interface{}) string {
	if tmpl == "" || input == nil {
		return tmpl
	}
	result := tmpl
	for key, val := range input {
		placeholder := "{{" + key + "}}"
		switch v := val.(type) {
		case string:
			result = strings.ReplaceAll(result, placeholder, v)
		default:
			jsonVal, err := json.Marshal(v)
			if err == nil {
				result = strings.ReplaceAll(result, placeholder, string(jsonVal))
			}
		}
	}
	return result
}

// truncate cuts a string to maxLen and appends "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// CallLLM provides a public LLM call function for use by the reasoning package.
// Routes to the appropriate backend (Ollama/Claude/OpenAI) based on model name.
func (r *SkillRunner) CallLLM(ctx context.Context, model, system, prompt string, temp float64, maxTokens int) (string, int, error) {
	var result map[string]interface{}
	var err error

	if strings.HasPrefix(model, "claude") || strings.HasPrefix(model, "anthropic") {
		result, _, err = r.callClaudeCLI(ctx, model, system, prompt, maxTokens, nil)
	} else if isLocalModel(model) {
		result, _, err = r.callOllama(ctx, model, system, prompt, temp, maxTokens, nil)
	} else {
		result, _, err = r.callOpenAI(ctx, model, system, prompt, temp, maxTokens, nil)
	}
	if err != nil {
		return "", 0, err
	}

	text, _ := result["text"].(string)
	return text, 0, nil
}

// CallLLMStream is a streaming variant of CallLLM for reasoning.
// For Claude models, uses CLI --output-format stream-json for real-time deltas.
// For other models, falls back to non-streaming CallLLM.
func (r *SkillRunner) CallLLMStream(ctx context.Context, model, system, prompt string, temp float64, maxTokens int, onDelta func(string)) (string, int, error) {
	if strings.HasPrefix(model, "claude") || strings.HasPrefix(model, "anthropic") {
		return r.callClaudeCLIStream(ctx, model, system, prompt, maxTokens, onDelta)
	}
	// Fallback: non-streaming for other models
	text, tokens, err := r.CallLLM(ctx, model, system, prompt, temp, maxTokens)
	if err != nil {
		return "", 0, err
	}
	if onDelta != nil {
		onDelta(text)
	}
	return text, tokens, nil
}

// callClaudeCLIStream calls Claude CLI with stream-json output format.
func (r *SkillRunner) callClaudeCLIStream(ctx context.Context, model, system, prompt string, maxTokens int, onDelta func(string)) (string, int, error) {
	claudePath := findClaudeCLI()
	if claudePath == "" {
		// No CLI: fall back to non-streaming API call
		result, _, err := r.callAnthropicAPI(ctx, model, system, prompt, 0.7, maxTokens, nil)
		if err != nil {
			return "", 0, err
		}
		text, _ := result["text"].(string)
		if onDelta != nil {
			onDelta(text)
		}
		return text, 0, nil
	}

	var fullPrompt strings.Builder
	if system != "" {
		fullPrompt.WriteString(system)
		fullPrompt.WriteString("\n\n")
	}
	fullPrompt.WriteString(prompt)

	args := []string{"-p", fullPrompt.String(), "--model", model, "--output-format", "stream-json"}
	cmd := exec.CommandContext(ctx, claudePath, args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", 0, fmt.Errorf("claude CLI stream pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return "", 0, fmt.Errorf("claude CLI stream start: %w", err)
	}

	var fullText strings.Builder
	var totalTokens int
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event struct {
			Type    string `json:"type"`
			Content string `json:"content"`
			Result  string `json:"result"`
			Usage   struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		switch event.Type {
		case "content":
			if event.Content != "" {
				fullText.WriteString(event.Content)
				if onDelta != nil {
					onDelta(event.Content)
				}
			}
		case "result":
			if event.Result != "" && fullText.Len() == 0 {
				fullText.WriteString(event.Result)
				if onDelta != nil {
					onDelta(event.Result)
				}
			}
			totalTokens = event.Usage.InputTokens + event.Usage.OutputTokens
		}
	}

	if err := cmd.Wait(); err != nil {
		log.Printf("claude CLI stream wait: %v", err)
	}

	return fullText.String(), totalTokens, nil
}
