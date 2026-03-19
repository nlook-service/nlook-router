package llm

import (
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
	"sync"
	"time"
)

// EngineType represents the LLM backend.
type EngineType string

const (
	EngineOllama EngineType = "ollama"
	EngineVLLM   EngineType = "vllm"
)

// Engine manages the LLM backend (Ollama or vLLM) and provides a unified API.
type Engine struct {
	mu         sync.RWMutex
	engineType EngineType
	baseURL    string
	model      string
	process    *exec.Cmd
	httpClient *http.Client
}

// NewEngine creates a new LLM engine manager.
// Auto-detects: VLLM_BASE_URL → vLLM, otherwise Ollama.
func NewEngine() *Engine {
	e := &Engine{
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}

	// Check for vLLM first
	if url := os.Getenv("VLLM_BASE_URL"); url != "" {
		e.engineType = EngineVLLM
		e.baseURL = url
		e.model = os.Getenv("NLOOK_AI_MODEL")
		log.Printf("llm: using vLLM at %s", url)
		return e
	}

	// Check for managed vLLM (explicit or auto-detect)
	engineEnv := os.Getenv("NLOOK_LLM_ENGINE")
	if engineEnv == "vllm" || engineEnv == "" {
		// Auto-detect: check if vllm binary exists
		if engineEnv == "vllm" || isVLLMInstalled() {
			if engineEnv == "vllm" || engineEnv == "" {
				e.engineType = EngineVLLM
				e.baseURL = "http://localhost:18000"
				e.model = os.Getenv("NLOOK_AI_MODEL")
				if e.model == "" {
					e.model = "gemma3:4b"
				}
				log.Printf("llm: vLLM detected, using as default engine")
				return e
			}
		}
	}

	// Fallback: Ollama
	e.engineType = EngineOllama
	e.baseURL = os.Getenv("OLLAMA_BASE_URL")
	if e.baseURL == "" {
		e.baseURL = "http://localhost:11434"
	}
	e.model = os.Getenv("NLOOK_AI_MODEL")
	log.Printf("llm: using Ollama at %s", e.baseURL)
	return e
}

// Type returns the engine type.
func (e *Engine) Type() EngineType {
	return e.engineType
}

// BaseURL returns the engine's API URL.
func (e *Engine) BaseURL() string {
	return e.baseURL
}

// Model returns the current model name.
func (e *Engine) Model() string {
	return e.model
}

// StartManaged starts vLLM as a managed subprocess on internal port.
func (e *Engine) StartManaged(ctx context.Context) error {
	if e.engineType != EngineVLLM {
		return nil
	}
	if e.IsRunning(ctx) {
		log.Printf("llm: vLLM already running at %s", e.baseURL)
		return nil
	}

	model := e.model
	if model == "" {
		model = "gemma3:4b"
	}

	log.Printf("llm: starting vLLM with model %s on %s", model, e.baseURL)

	// Extract port from baseURL
	port := "18000"
	if parts := strings.Split(e.baseURL, ":"); len(parts) == 3 {
		port = parts[2]
	}

	cmd := exec.CommandContext(ctx, "vllm", "serve", model, "--port", port, "--trust-remote-code")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start vLLM: %w", err)
	}

	e.mu.Lock()
	e.process = cmd
	e.mu.Unlock()

	// Wait for server to be ready
	for i := 0; i < 120; i++ { // Up to 2 minutes
		time.Sleep(1 * time.Second)
		if e.IsRunning(ctx) {
			log.Printf("llm: vLLM ready on %s", e.baseURL)
			return nil
		}
	}

	return fmt.Errorf("vLLM did not start in time")
}

// Stop stops the managed process.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.process != nil {
		e.process.Process.Kill()
		e.process = nil
	}
}

// IsRunning checks if the engine is reachable.
func (e *Engine) IsRunning(ctx context.Context) bool {
	url := e.baseURL
	if e.engineType == EngineVLLM {
		url += "/health"
	} else {
		url += "/api/tags"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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

// ChatCompletion sends a chat request via OpenAI-compatible API (works with vLLM and Ollama).
func (e *Engine) ChatCompletion(ctx context.Context, model, system string, messages []map[string]string, tools []map[string]interface{}, stream bool) (*http.Response, error) {
	if model == "" {
		model = e.model
	}

	// Build messages
	allMessages := make([]map[string]string, 0)
	if system != "" {
		allMessages = append(allMessages, map[string]string{"role": "system", "content": system})
	}
	allMessages = append(allMessages, messages...)

	reqBody := map[string]interface{}{
		"model":    model,
		"messages": allMessages,
		"stream":   stream,
	}
	if len(tools) > 0 {
		reqBody["tools"] = tools
	}

	body, _ := json.Marshal(reqBody)

	// Use OpenAI-compatible endpoint
	url := e.baseURL
	if e.engineType == EngineVLLM {
		url += "/v1/chat/completions"
	} else {
		url += "/v1/chat/completions" // Ollama also supports this
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// vLLM may need API key
	if apiKey := os.Getenv("VLLM_API_KEY"); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	return e.httpClient.Do(req)
}

// ChatStream sends a streaming chat and calls onDelta per token.
func (e *Engine) ChatStream(ctx context.Context, model, system string, messages []map[string]string, onDelta func(string)) (string, error) {
	resp, err := e.ChatCompletion(ctx, model, system, messages, nil, true)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("API error: status=%d body=%s", resp.StatusCode, truncate(string(body), 300))
	}

	var fullText strings.Builder
	decoder := json.NewDecoder(resp.Body)

	for {
		// Read SSE lines
		var line string
		buf := make([]byte, 1)
		for {
			_, err := resp.Body.Read(buf)
			if err != nil {
				goto done
			}
			if buf[0] == '\n' {
				break
			}
			line += string(buf)
		}

		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			text := chunk.Choices[0].Delta.Content
			fullText.WriteString(text)
			if onDelta != nil {
				onDelta(text)
			}
		}
	}

done:
	_ = decoder
	return fullText.String(), nil
}

// isVLLMInstalled checks if vllm binary/command is available.
func isVLLMInstalled() bool {
	_, err := exec.LookPath("vllm")
	return err == nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
