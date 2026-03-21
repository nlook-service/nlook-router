package compression

import (
	"context"
	"log"
	"os"

	"github.com/nlook-service/nlook-router/internal/ollama"
	"github.com/nlook-service/nlook-router/internal/tokenizer"
)

// Result holds compressed output with metadata.
type Result struct {
	Text       string // compressed text
	Original   int    // original token count (estimated)
	Compressed int    // compressed token count
	Method     string // "none", "rule", "llm", "rule+llm", "disabled"
}

// Compressor compresses tool results to fit within token budget.
type Compressor interface {
	Compress(ctx context.Context, text string, maxTokens int) Result
}

// Config holds compression settings.
type Config struct {
	Enabled      bool   `yaml:"enabled"`
	MaxTokens    int    `yaml:"max_tokens"`
	LLMModel     string `yaml:"llm_model,omitempty"`
	LLMThreshold int    `yaml:"llm_threshold"`
	RuleMaxItems int    `yaml:"rule_max_items"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:      true,
		MaxTokens:    800,
		LLMThreshold: 1200,
		RuleMaxItems: 10,
	}
}

// New creates a hybrid compressor chain (rule → llm fallback).
// ollamaClient can be nil (LLM compression will be skipped).
func New(cfg Config, ollamaClient *ollama.Client) Compressor {
	if !cfg.Enabled {
		return &disabledCompressor{}
	}

	rule := &ruleCompressor{maxItems: cfg.RuleMaxItems}
	if rule.maxItems <= 0 {
		rule.maxItems = 10
	}

	var llm *llmCompressor
	if ollamaClient != nil {
		model := cfg.LLMModel
		if model == "" {
			model = os.Getenv("NLOOK_AI_MODEL")
		}
		if model != "" {
			llm = &llmCompressor{client: ollamaClient, model: model}
		}
	}

	return &chainCompressor{rule: rule, llm: llm, cfg: cfg}
}

// chainCompressor applies rule-based compression first, then LLM if needed.
type chainCompressor struct {
	rule *ruleCompressor
	llm  *llmCompressor
	cfg  Config
}

func (c *chainCompressor) Compress(ctx context.Context, text string, maxTokens int) Result {
	if maxTokens <= 0 {
		maxTokens = c.cfg.MaxTokens
	}

	tokens := tokenizer.EstimateTokens(text)
	if tokens <= maxTokens {
		return Result{Text: text, Original: tokens, Compressed: tokens, Method: "none"}
	}

	// Step 1: Rule-based compression
	result := c.rule.Compress(ctx, text, maxTokens)
	if result.Compressed <= maxTokens {
		return result
	}

	// Step 2: LLM compression (only if available and threshold exceeded)
	if c.llm != nil && result.Compressed > c.cfg.LLMThreshold {
		llmResult := c.llm.Compress(ctx, result.Text, maxTokens)
		if llmResult.Method != "llm-error" {
			llmResult.Original = result.Original
			llmResult.Method = "rule+llm"
			log.Printf("compression: rule+llm %d→%d tokens", llmResult.Original, llmResult.Compressed)
			return llmResult
		}
	}

	return result
}

// disabledCompressor passes through without compression.
type disabledCompressor struct{}

func (d *disabledCompressor) Compress(_ context.Context, text string, _ int) Result {
	tokens := tokenizer.EstimateTokens(text)
	return Result{Text: text, Original: tokens, Compressed: tokens, Method: "disabled"}
}
