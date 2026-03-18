package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/nlook-service/nlook-router/internal/ollama"
)

const defaultAISystemPrompt = `You are nlook AI assistant. You help users manage documents, tasks, and provide analysis.
Respond concisely in the user's language. Be helpful and direct.`

func (s *Server) aiSearchHandler(w http.ResponseWriter, r *http.Request) {
	message := r.URL.Query().Get("message")
	if message == "" {
		http.Error(w, `{"error":"message parameter required"}`, http.StatusBadRequest)
		return
	}

	// SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	model := r.URL.Query().Get("model")
	if model == "" {
		model = os.Getenv("NLOOK_AI_MODEL")
		if model == "" {
			model = "qwen2.5:7b"
		}
	}

	system := r.URL.Query().Get("system")
	if system == "" {
		system = defaultAISystemPrompt
	}

	client := ollama.NewClient()
	if !client.IsRunning(r.Context()) {
		writeSSE(w, flusher, map[string]interface{}{
			"error": "Ollama is not running. Run: ollama serve && nlook-router ai setup",
			"done":  true,
		})
		return
	}

	var fullText strings.Builder

	_, err := client.ChatStream(r.Context(), model, system, message,
		ollama.ChatOptions{Temperature: 0.7, MaxTokens: 4096},
		func(text string) {
			fullText.WriteString(text)
			writeSSE(w, flusher, map[string]interface{}{
				"text": text,
				"done": false,
			})
		},
	)

	if err != nil {
		writeSSE(w, flusher, map[string]interface{}{
			"error": err.Error(),
			"done":  true,
		})
		return
	}

	writeSSE(w, flusher, map[string]interface{}{
		"text":      "",
		"done":      true,
		"model":     model,
		"full_text": fullText.String(),
	})
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, data map[string]interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(w, "data: %s\n\n", jsonData)
	flusher.Flush()
}
