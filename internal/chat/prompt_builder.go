package chat

import (
	"context"
	"fmt"
	"strings"

	"github.com/nlook-service/nlook-router/internal/cache"
	"github.com/nlook-service/nlook-router/internal/embedding"
	"github.com/nlook-service/nlook-router/internal/memory"
	"github.com/nlook-service/nlook-router/internal/ollama"
)

/*
NotebookLM-style structured prompt:

[System]        - Base instruction + language
[User Profile]  - Role, interests, preferences
[Long Memory]   - Conversation summaries, learned facts
[RAG Context]   - Semantically searched document content
[Data Summary]  - Task/document overview from cache
[Recent Chat]   - Last 6 messages as full context
[User Question] - Current query
*/

// PromptBuilder assembles structured prompts from all context sources.
type PromptBuilder struct {
	cacheStore  *cache.Store
	vectorStore *embedding.VectorStore
	memoryStore *memory.Store
}

// NewPromptBuilder creates a new prompt builder.
func NewPromptBuilder(cs *cache.Store, vs *embedding.VectorStore, ms *memory.Store) *PromptBuilder {
	return &PromptBuilder{
		cacheStore:  cs,
		vectorStore: vs,
		memoryStore: ms,
	}
}

// BuildSystemPrompt assembles the full system prompt for a chat request.
func (pb *PromptBuilder) BuildSystemPrompt(lang, query string, conversationID int64) string {
	if lang == "" {
		lang = detectLang(query)
	}

	var sb strings.Builder

	// [System] — Base instruction
	sb.WriteString(baseSystemPrompt)

	// [User Profile + Long Memory]
	if pb.memoryStore != nil {
		if memCtx := pb.memoryStore.BuildPromptContext(conversationID); memCtx != "" {
			sb.WriteString(memCtx)
		}
	}

	// [RAG Context] — Semantic search
	if pb.vectorStore != nil {
		results := pb.vectorStore.Search(context.Background(), query, 5)
		if len(results) > 0 {
			sb.WriteString("\n\n[Relevant Documents — semantic search]\n")
			for _, r := range results {
				content := r.Entry.Content
				if len(content) > 2000 {
					content = content[:2000] + "..."
				}
				sb.WriteString(fmt.Sprintf("\n## %s (relevance: %.0f%%)\n%s\n", r.Entry.Title, r.Score*100, content))
			}
		}
	}

	// [Data Summary] — Cache overview (tasks, recent docs)
	if pb.cacheStore != nil {
		// Query-specific context first
		if ctx := pb.cacheStore.BuildContextForQuery(query); ctx != "" {
			sb.WriteString(ctx)
		} else if summary := pb.cacheStore.Summary(); summary != "" {
			sb.WriteString(summary)
		}
	}

	// [Language Instruction] — Must be at the end for maximum effect
	switch lang {
	case "ko":
		sb.WriteString("\n\nCRITICAL: You MUST respond ONLY in Korean (한국어). 절대로 중국어, 영어 등 다른 언어를 사용하지 마세요.")
	case "en":
		sb.WriteString("\n\nCRITICAL: You MUST respond ONLY in English.")
	default:
		sb.WriteString("\n\nCRITICAL: Respond ONLY in the same language the user writes in. Never mix languages.")
	}

	return sb.String()
}

// BuildHistory converts request history to Ollama format with sliding window.
func (pb *PromptBuilder) BuildHistory(history []HistoryMessage) []ollama.MessageEntry {
	if len(history) <= recentMessageCount {
		entries := make([]ollama.MessageEntry, 0, len(history))
		for _, m := range history {
			entries = append(entries, ollama.MessageEntry{Role: m.Role, Content: m.Content})
		}
		return entries
	}

	// Sliding window: summarize older + keep recent
	olderMessages := history[:len(history)-recentMessageCount]
	recentMessages := history[len(history)-recentMessageCount:]

	entries := make([]ollama.MessageEntry, 0, recentMessageCount+1)

	// Compressed older messages
	summary := compressHistory(olderMessages)
	entries = append(entries, ollama.MessageEntry{
		Role:    "system",
		Content: summary,
	})

	for _, m := range recentMessages {
		entries = append(entries, ollama.MessageEntry{Role: m.Role, Content: m.Content})
	}
	return entries
}

// LearnFromConversation extracts facts from the conversation.
func (pb *PromptBuilder) LearnFromConversation(messages []HistoryMessage) {
	if pb.memoryStore == nil {
		return
	}
	// Simple heuristic: detect self-introduction patterns
	for _, m := range messages {
		if m.Role != "user" {
			continue
		}
		content := strings.ToLower(m.Content)
		// Detect role mentions
		if strings.Contains(content, "개발자") || strings.Contains(content, "developer") {
			pb.memoryStore.UpdateProfile(memory.UserProfile{Role: "developer"})
		}
		if strings.Contains(content, "디자이너") || strings.Contains(content, "designer") {
			pb.memoryStore.UpdateProfile(memory.UserProfile{Role: "designer"})
		}
	}
}
