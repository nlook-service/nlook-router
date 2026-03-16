package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nlook-service/nlook-router/internal/apiclient"
)

// ToolExecutor runs a tool by name (e.g. Python bridge). If nil, runTool returns a placeholder.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args map[string]interface{}) ([]byte, error)
}

// SkillRunner executes workflow skills by type.
type SkillRunner struct {
	httpClient   *http.Client
	toolExecutor ToolExecutor
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

// RunSkill dispatches execution to the appropriate handler based on skill type.
func (r *SkillRunner) RunSkill(ctx context.Context, skill *apiclient.WorkflowSkill, agent *apiclient.WorkflowAgent, input map[string]interface{}) (map[string]interface{}, []string, error) {
	switch skill.SkillType {
	case "prompt":
		return r.runPrompt(ctx, skill, agent, input)
	case "api":
		return r.runAPI(ctx, skill, input)
	case "tool":
		return r.runTool(ctx, skill, input)
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

	// Route to appropriate API based on model name
	if strings.HasPrefix(model, "claude") || strings.HasPrefix(model, "anthropic") {
		return r.callAnthropic(ctx, model, systemPrompt, prompt, temperature, maxTokens, logs)
	}
	if isLocalModel(model) {
		return r.callOllama(ctx, model, systemPrompt, prompt, temperature, maxTokens, logs)
	}
	return r.callOpenAI(ctx, model, systemPrompt, prompt, temperature, maxTokens, logs)
}

func (r *SkillRunner) callAnthropic(ctx context.Context, model, system, prompt string, temperature float64, maxTokens int, logs []string) (map[string]interface{}, []string, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, logs, fmt.Errorf("ANTHROPIC_API_KEY not set")
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
// Template helpers
// ──────────────────────────────────────────────────────────────────────────────

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
