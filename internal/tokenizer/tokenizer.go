package tokenizer

import (
	"strings"
	"unicode"
)

// DefaultContextWindow is the max tokens for qwen3:8b.
const DefaultContextWindow = 32768

// ReservedForResponse leaves room for the model's response.
const ReservedForResponse = 4096

// MaxPromptTokens is the max tokens available for prompt (system + history + context).
const MaxPromptTokens = DefaultContextWindow - ReservedForResponse

// EstimateTokens estimates token count for text.
// Calibrated for BPE tokenizers (Qwen, GPT-4, etc.):
//   - English/code: ~4 chars per token
//   - Korean: ~1.5 chars per token (Hangul uses more tokens)
//   - Chinese/Japanese: ~1.5 chars per token
//   - Mixed: weighted average
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}

	var cjkChars, asciiChars, otherChars int
	for _, r := range text {
		if isCJK(r) {
			cjkChars++
		} else if r <= 127 {
			asciiChars++
		} else {
			otherChars++
		}
	}

	// Estimate tokens per character type
	tokens := float64(asciiChars)/4.0 + float64(cjkChars)/1.5 + float64(otherChars)/2.0

	// Add overhead for special tokens, newlines, etc.
	newlines := strings.Count(text, "\n")
	tokens += float64(newlines) * 0.5

	result := int(tokens) + 1 // +1 to avoid undercount
	if result < 1 {
		return 1
	}
	return result
}

// TruncateToTokens truncates text to approximately maxTokens.
func TruncateToTokens(text string, maxTokens int) string {
	if EstimateTokens(text) <= maxTokens {
		return text
	}

	// Binary search for the right cutoff
	runes := []rune(text)
	lo, hi := 0, len(runes)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if EstimateTokens(string(runes[:mid])) <= maxTokens {
			lo = mid
		} else {
			hi = mid - 1
		}
	}

	if lo < len(runes) {
		return string(runes[:lo]) + "\n... (truncated)"
	}
	return text
}

// Budget tracks token usage across prompt components.
type Budget struct {
	MaxTokens int
	used      int
	breakdown map[string]int
}

// NewBudget creates a token budget.
func NewBudget(maxTokens int) *Budget {
	return &Budget{
		MaxTokens: maxTokens,
		breakdown: make(map[string]int),
	}
}

// Add adds text to the budget and returns the (possibly truncated) text.
// label is for tracking (e.g. "system", "history", "rag").
func (b *Budget) Add(label, text string, maxForThis int) string {
	remaining := b.MaxTokens - b.used
	if remaining <= 0 {
		return ""
	}

	limit := remaining
	if maxForThis > 0 && maxForThis < limit {
		limit = maxForThis
	}

	tokens := EstimateTokens(text)
	if tokens > limit {
		text = TruncateToTokens(text, limit)
		tokens = EstimateTokens(text)
	}

	b.used += tokens
	b.breakdown[label] = tokens
	return text
}

// Remaining returns tokens still available.
func (b *Budget) Remaining() int {
	r := b.MaxTokens - b.used
	if r < 0 {
		return 0
	}
	return r
}

// Used returns total tokens used.
func (b *Budget) Used() int {
	return b.used
}

// Breakdown returns per-component token usage.
func (b *Budget) Breakdown() map[string]int {
	return b.breakdown
}

// Summary returns a human-readable summary.
func (b *Budget) Summary() string {
	var sb strings.Builder
	sb.WriteString("[tokens")
	for label, count := range b.breakdown {
		sb.WriteString(" " + label + "=" + itoa(count))
	}
	sb.WriteString(" total=" + itoa(b.used) + "/" + itoa(b.MaxTokens) + "]")
	return sb.String()
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hangul, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
