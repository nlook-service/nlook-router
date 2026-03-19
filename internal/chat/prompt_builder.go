package chat

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/nlook-service/nlook-router/internal/cache"
	"github.com/nlook-service/nlook-router/internal/embedding"
	"github.com/nlook-service/nlook-router/internal/memory"
	"github.com/nlook-service/nlook-router/internal/ollama"
	"github.com/nlook-service/nlook-router/internal/tokenizer"
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

// BuildSystemPrompt assembles the full system prompt with token budget management.
func (pb *PromptBuilder) BuildSystemPrompt(lang, query string, conversationID int64) string {
	if lang == "" {
		lang = detectLang(query)
	}

	budget := tokenizer.NewBudget(tokenizer.MaxPromptTokens)

	// [System] — Base instruction (always included, ~200 tokens)
	system := budget.Add("system", baseSystemPrompt, 0)

	var sb strings.Builder
	sb.WriteString(system)

	// [User Profile + Long Memory] — ~200 tokens max
	if pb.memoryStore != nil {
		if memCtx := pb.memoryStore.BuildPromptContext(conversationID); memCtx != "" {
			sb.WriteString(budget.Add("memory", memCtx, 500))
		}
	}

	// [RAG Context] — Up to 8000 tokens for document content
	if pb.vectorStore != nil {
		results := pb.vectorStore.Search(context.Background(), query, 5)
		if len(results) > 0 {
			var ragSb strings.Builder
			ragSb.WriteString("\n\n[Relevant Documents]\n")
			for _, r := range results {
				ragSb.WriteString(fmt.Sprintf("\n## %s (%.0f%%)\n%s\n", r.Entry.Title, r.Score*100, r.Entry.Content))
			}
			sb.WriteString(budget.Add("rag", ragSb.String(), 8000))
		}
	}

	// [Data Summary] — Up to 4000 tokens for tasks/docs overview
	if pb.cacheStore != nil {
		if ctx := pb.cacheStore.BuildContextForQuery(query); ctx != "" {
			sb.WriteString(budget.Add("cache", ctx, 4000))
		} else if summary := pb.cacheStore.Summary(); summary != "" {
			sb.WriteString(budget.Add("cache", summary, 2000))
		}
	}

	// [Language Instruction] — detect actual language from query, override client lang
	actualLang := lang
	if actualLang == "" || actualLang == "en" {
		for _, r := range query {
			if r >= 0xAC00 && r <= 0xD7AF || r >= 0x3131 && r <= 0x318E {
				actualLang = "ko"
				break
			}
			if r >= 0x4E00 && r <= 0x9FFF {
				actualLang = "zh"
				break
			}
			if r >= 0x3040 && r <= 0x30FF {
				actualLang = "ja"
				break
			}
		}
	}
	langInstr := ""
	switch actualLang {
	case "ko":
		langInstr = "\n\nCRITICAL: You MUST respond ONLY in Korean (한국어). 절대로 중국어, 영어 등 다른 언어를 사용하지 마세요."
	case "en":
		langInstr = "\n\nCRITICAL: You MUST respond ONLY in English."
	default:
		langInstr = "\n\nCRITICAL: Respond ONLY in the same language the user writes in. Never mix languages."
	}
	sb.WriteString(budget.Add("lang", langInstr, 0))

	log.Printf("prompt: %s", budget.Summary())
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
