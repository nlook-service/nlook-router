package memory

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"time"

	"github.com/nlook-service/nlook-router/internal/ollama"
	"github.com/nlook-service/nlook-router/internal/tokenizer"
)

// MemoryOptimizer defines a strategy for compressing/optimizing memories.
type MemoryOptimizer interface {
	Optimize(ctx context.Context, memories []UserMemory) ([]UserMemory, error)
}

// SummarizeStrategy compresses multiple memories into a single narrative using LLM.
type SummarizeStrategy struct {
	client *ollama.Client
	model  string
}

// NewSummarizeStrategy creates a new LLM-based summarize strategy.
func NewSummarizeStrategy(client *ollama.Client, model string) *SummarizeStrategy {
	return &SummarizeStrategy{client: client, model: model}
}

const summarizeSystemPrompt = `You are a memory compression assistant. Your task:
1. Combine all provided memories into a single concise third-person narrative.
2. Remove duplicate facts and redundant information.
3. Preserve ALL unique information — do not lose any distinct facts.
4. Write in the same language as the input memories.
5. Output ONLY the compressed memory text, nothing else. No explanations.`

// Optimize merges multiple UserMemory entries into one compressed memory.
func (s *SummarizeStrategy) Optimize(ctx context.Context, memories []UserMemory) ([]UserMemory, error) {
	if s.client == nil || len(memories) < 2 {
		return memories, nil
	}

	// Build prompt with all memories
	prompt := ""
	for i, m := range memories {
		prompt += fmt.Sprintf("Memory %d: %s\n", i+1, m.Memory)
	}

	beforeTokens := countMemoryTokens(memories)

	// Call Ollama for compression
	model := s.model
	if model == "" {
		model = "qwen3:8b"
	}

	fullText, _, _, err := s.client.ChatStream(ctx, model, summarizeSystemPrompt, prompt, ollama.ChatOptions{
		Temperature: 0.3,
		MaxTokens:   2048,
	}, nil)
	if err != nil {
		log.Printf("memory: optimize failed (ollama): %v", err)
		return memories, nil // fallback: return originals
	}

	if fullText == "" {
		log.Printf("memory: optimize returned empty response")
		return memories, nil
	}

	// Merge topics from all memories (dedup)
	topicSet := make(map[string]struct{})
	var userID int64
	for _, m := range memories {
		for _, t := range m.Topics {
			topicSet[t] = struct{}{}
		}
		if m.UserID != 0 {
			userID = m.UserID
		}
	}
	topics := make([]string, 0, len(topicSet))
	for t := range topicSet {
		topics = append(topics, t)
	}

	afterTokens := tokenizer.EstimateTokens(fullText)

	// If compression didn't help, keep originals
	if afterTokens >= beforeTokens {
		log.Printf("memory: optimize skipped — compressed (%d tokens) >= original (%d tokens)", afterTokens, beforeTokens)
		return memories, nil
	}

	now := time.Now()
	compressed := UserMemory{
		ID:         generateMemoryID(),
		Memory:     fullText,
		Topics:     topics,
		UserID:     userID,
		TokenCount: afterTokens,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	log.Printf("memory: optimized %d memories (%d tokens → %d tokens, %.0f%% reduction)",
		len(memories), beforeTokens, afterTokens, float64(beforeTokens-afterTokens)/float64(beforeTokens)*100)

	return []UserMemory{compressed}, nil
}

func countMemoryTokens(memories []UserMemory) int {
	total := 0
	for _, m := range memories {
		if m.TokenCount > 0 {
			total += m.TokenCount
		} else {
			total += tokenizer.EstimateTokens(m.Memory)
		}
	}
	return total
}

func generateMemoryID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("mem_%x", b)
}
