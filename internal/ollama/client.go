package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Client wraps the Ollama REST API.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Ollama client.
func NewClient() *Client {
	baseURL := os.Getenv("OLLAMA_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}
}

// ModelInfo represents an installed model.
type ModelInfo struct {
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	ModifiedAt time.Time `json:"modified_at"`
}

// ChatOptions configures a chat request.
type ChatOptions struct {
	Temperature float64
	MaxTokens   int
	History     []MessageEntry
}

// IsRunning checks if Ollama server is reachable.
func (c *Client) IsRunning(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return false
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// List returns installed models.
func (c *Client) List(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Models []ModelInfo `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return result.Models, nil
}

// ModelDetail holds detailed model information from Ollama /api/show.
type ModelDetail struct {
	Name            string `json:"name"`
	ParameterSize   string `json:"parameter_size"`
	QuantizationLevel string `json:"quantization_level"`
	Format          string `json:"format"`
	Family          string `json:"family"`
	Size            int64  `json:"size"`
}

// Show returns detailed information about a specific model.
func (c *Client) Show(ctx context.Context, model string) (*ModelDetail, error) {
	body, _ := json.Marshal(map[string]string{"name": model})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/show", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("show model: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("show model: status %d", resp.StatusCode)
	}

	var result struct {
		Details struct {
			ParameterSize     string `json:"parameter_size"`
			QuantizationLevel string `json:"quantization_level"`
			Format            string `json:"format"`
			Family            string `json:"family"`
		} `json:"details"`
		Size int64 `json:"size"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &ModelDetail{
		Name:              model,
		ParameterSize:     result.Details.ParameterSize,
		QuantizationLevel: result.Details.QuantizationLevel,
		Format:            result.Details.Format,
		Family:            result.Details.Family,
		Size:              result.Size,
	}, nil
}

// Pull downloads a model with progress callback.
func (c *Client) Pull(ctx context.Context, model string, progress func(status string, completed, total int64)) error {
	body, _ := json.Marshal(map[string]interface{}{"name": model, "stream": true})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/pull", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("pull model: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("pull failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		var event struct {
			Status    string `json:"status"`
			Total     int64  `json:"total"`
			Completed int64  `json:"completed"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if progress != nil {
			progress(event.Status, event.Completed, event.Total)
		}
	}
	return scanner.Err()
}

// Remove deletes a model.
func (c *Client) Remove(ctx context.Context, model string) error {
	body, _ := json.Marshal(map[string]string{"name": model})
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/api/delete", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete model: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("delete failed: status=%d", resp.StatusCode)
	}
	return nil
}

// ToolCall represents a tool call from the model.
type ToolCall struct {
	Function struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	} `json:"function"`
}

// ChatResponse holds the result of a non-streaming chat.
type ChatResponse struct {
	Content        string     `json:"content"`
	ToolCalls      []ToolCall `json:"tool_calls,omitempty"`
	Done           bool       `json:"done"`
	PromptEvalCount int       `json:"prompt_eval_count,omitempty"`
	EvalCount       int       `json:"eval_count,omitempty"`
}

// ChatWithTools sends a chat with tool definitions (non-streaming) and returns tool calls if any.
func (c *Client) ChatWithTools(ctx context.Context, model, system, prompt string, tools []map[string]interface{}, history []MessageEntry) (*ChatResponse, error) {
	messages := []map[string]interface{}{}
	if system != "" {
		messages = append(messages, map[string]interface{}{"role": "system", "content": system})
	}
	for _, h := range history {
		messages = append(messages, map[string]interface{}{"role": h.Role, "content": h.Content})
	}
	messages = append(messages, map[string]interface{}{"role": "user", "content": prompt})

	reqBody := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   false,
		"tools":    tools,
		"think":    false,
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("chat request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("chat failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
		Done            bool `json:"done"`
		PromptEvalCount int  `json:"prompt_eval_count"`
		EvalCount       int  `json:"eval_count"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	return &ChatResponse{
		Content:         result.Message.Content,
		ToolCalls:       result.Message.ToolCalls,
		Done:            result.Done,
		PromptEvalCount: result.PromptEvalCount,
		EvalCount:       result.EvalCount,
	}, nil
}

// ChatWithToolResults sends tool results back to the model for final response (streaming).
// Returns full text, input tokens, output tokens, error.
func (c *Client) ChatWithToolResults(ctx context.Context, model, system, prompt string, toolCalls []ToolCall, toolResults []map[string]interface{}, onDelta func(string)) (string, int, int, error) {
	messages := []map[string]interface{}{}
	if system != "" {
		messages = append(messages, map[string]interface{}{"role": "system", "content": system})
	}
	messages = append(messages, map[string]interface{}{"role": "user", "content": prompt})

	// Add assistant message with tool calls
	assistantMsg := map[string]interface{}{"role": "assistant", "content": ""}
	if len(toolCalls) > 0 {
		calls := make([]map[string]interface{}, len(toolCalls))
		for i, tc := range toolCalls {
			calls[i] = map[string]interface{}{
				"function": map[string]interface{}{
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				},
			}
		}
		assistantMsg["tool_calls"] = calls
	}
	messages = append(messages, assistantMsg)

	// Add tool results
	for _, tr := range toolResults {
		messages = append(messages, map[string]interface{}{
			"role":    "tool",
			"content": tr["content"],
		})
	}

	reqBody := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   true,
		"think":    false,
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewBuffer(body))
	if err != nil {
		return "", 0, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, 0, fmt.Errorf("chat request: %w", err)
	}
	defer resp.Body.Close()

	var fullText string
	var inputTokens, outputTokens int
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		var event struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done            bool `json:"done"`
			PromptEvalCount int  `json:"prompt_eval_count"`
			EvalCount       int  `json:"eval_count"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if event.Message.Content != "" {
			fullText += event.Message.Content
			if onDelta != nil {
				onDelta(event.Message.Content)
			}
		}
		if event.Done {
			inputTokens = event.PromptEvalCount
			outputTokens = event.EvalCount
			break
		}
	}
	return fullText, inputTokens, outputTokens, scanner.Err()
}

// MessageEntry is a conversation history entry.
type MessageEntry struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatStream sends a chat message and streams the response token by token.
// Returns full text, input tokens, output tokens, error.
func (c *Client) ChatStream(ctx context.Context, model, system, prompt string, opts ChatOptions, onDelta func(text string)) (string, int, int, error) {
	messages := []map[string]string{}
	if system != "" {
		messages = append(messages, map[string]string{"role": "system", "content": system})
	}
	for _, h := range opts.History {
		messages = append(messages, map[string]string{"role": h.Role, "content": h.Content})
	}
	messages = append(messages, map[string]string{"role": "user", "content": prompt})

	// Apply model-specific optimal defaults from unsloth config
	defaults := GetModelDefaults(model)
	temp := defaults.Temperature
	if opts.Temperature > 0 {
		temp = opts.Temperature
	}
	numPredict := defaults.NumPredict
	if opts.MaxTokens > 0 {
		numPredict = opts.MaxTokens
	}

	options := map[string]interface{}{
		"temperature":      temp,
		"top_p":            defaults.TopP,
		"num_predict":      numPredict,
	}
	if defaults.TopK > 0 {
		options["top_k"] = defaults.TopK
	}
	if defaults.RepetitionPenalty > 1.0 {
		options["repeat_penalty"] = defaults.RepetitionPenalty
	}
	if defaults.PresencePenalty > 0 {
		options["presence_penalty"] = defaults.PresencePenalty
	}

	reqBody := map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   true,
		"options":  options,
		"think":    false,
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewBuffer(body))
	if err != nil {
		return "", 0, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, 0, fmt.Errorf("chat request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return "", 0, 0, fmt.Errorf("chat failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var fullText string
	var inputTokens, outputTokens int
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		var event struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done            bool `json:"done"`
			PromptEvalCount int  `json:"prompt_eval_count"`
			EvalCount       int  `json:"eval_count"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		if event.Message.Content != "" {
			fullText += event.Message.Content
			if onDelta != nil {
				onDelta(event.Message.Content)
			}
		}
		if event.Done {
			inputTokens = event.PromptEvalCount
			outputTokens = event.EvalCount
			break
		}
	}
	return fullText, inputTokens, outputTokens, scanner.Err()
}
