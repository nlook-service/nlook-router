package memory

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/nlook-service/nlook-router/internal/ollama"
	"github.com/nlook-service/nlook-router/internal/tokenizer"
)

// FactExtractor uses LLM to extract user facts from conversation.
type FactExtractor struct {
	client *ollama.Client
	model  string
}

// NewFactExtractor creates a new LLM-based fact extractor.
func NewFactExtractor(client *ollama.Client, model string) *FactExtractor {
	return &FactExtractor{client: client, model: model}
}

const factExtractionPrompt = `Extract facts about the user from this conversation. Output as JSON array:
[{"memory": "fact text", "topics": ["topic1"]}]

Only extract:
- User role, job, expertise
- User preferences and interests
- Project context and goals
- Stated constraints or requirements

Rules:
- Write facts in third person ("The user is...", "사용자는...")
- Use the same language as the conversation
- If no facts found, output: []
- Maximum 5 facts per extraction
- Be concise — one sentence per fact`

// HistoryMessage mirrors chat.HistoryMessage to avoid import cycle.
type HistoryMessage struct {
	Role    string
	Content string
}

type extractedFact struct {
	Memory string   `json:"memory"`
	Topics []string `json:"topics"`
}

// Extract analyzes conversation messages and returns new facts as UserMemory entries.
func (fe *FactExtractor) Extract(ctx context.Context, messages []HistoryMessage) ([]UserMemory, error) {
	if fe.client == nil || len(messages) < 2 {
		return nil, nil
	}

	// Build conversation text (last 10 messages max)
	msgs := messages
	if len(msgs) > 10 {
		msgs = msgs[len(msgs)-10:]
	}

	var sb strings.Builder
	for _, m := range msgs {
		role := "User"
		if m.Role == "assistant" {
			role = "Assistant"
		}
		content := m.Content
		if len(content) > 300 {
			content = content[:300] + "..."
		}
		sb.WriteString(role + ": " + content + "\n")
	}

	model := fe.model
	if model == "" {
		model = "qwen3:8b"
	}

	fullText, _, _, err := fe.client.ChatStream(ctx, model, factExtractionPrompt, sb.String(), ollama.ChatOptions{
		Temperature: 0.2,
		MaxTokens:   1024,
	}, nil)
	if err != nil {
		log.Printf("memory: fact extraction failed: %v", err)
		return nil, nil
	}

	// Extract JSON from response (handle markdown code blocks)
	jsonStr := extractJSON(fullText)
	if jsonStr == "" {
		return nil, nil
	}

	var facts []extractedFact
	if err := json.Unmarshal([]byte(jsonStr), &facts); err != nil {
		log.Printf("memory: fact parse failed: %v (response: %s)", err, truncate(fullText, 200))
		return nil, nil
	}

	if len(facts) == 0 {
		return nil, nil
	}

	now := time.Now()
	memories := make([]UserMemory, 0, len(facts))
	for _, f := range facts {
		if f.Memory == "" {
			continue
		}
		memories = append(memories, UserMemory{
			ID:         generateMemoryID(),
			Memory:     f.Memory,
			Topics:     f.Topics,
			TokenCount: tokenizer.EstimateTokens(f.Memory),
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}

	if len(memories) > 0 {
		log.Printf("memory: extracted %d facts from conversation", len(memories))
	}
	return memories, nil
}

// extractJSON finds JSON array in text, handling markdown code blocks.
func extractJSON(text string) string {
	// Try to find ```json ... ``` block
	if idx := strings.Index(text, "```json"); idx >= 0 {
		start := idx + 7
		if end := strings.Index(text[start:], "```"); end >= 0 {
			return strings.TrimSpace(text[start : start+end])
		}
	}
	// Try to find ``` ... ``` block
	if idx := strings.Index(text, "```"); idx >= 0 {
		start := idx + 3
		// Skip optional language tag on same line
		if nl := strings.Index(text[start:], "\n"); nl >= 0 {
			start += nl + 1
		}
		if end := strings.Index(text[start:], "```"); end >= 0 {
			return strings.TrimSpace(text[start : start+end])
		}
	}
	// Try to find raw JSON array
	if idx := strings.Index(text, "["); idx >= 0 {
		if end := strings.LastIndex(text, "]"); end > idx {
			return strings.TrimSpace(text[idx : end+1])
		}
	}
	return ""
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
