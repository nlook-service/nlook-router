package compression

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/nlook-service/nlook-router/internal/tokenizer"
)

// ruleCompressor applies structural rules to reduce text size.
type ruleCompressor struct {
	maxItems int
}

func (r *ruleCompressor) Compress(_ context.Context, text string, maxTokens int) Result {
	original := tokenizer.EstimateTokens(text)
	if original <= maxTokens {
		return Result{Text: text, Original: original, Compressed: original, Method: "none"}
	}

	compressed := text

	// Try JSON compression first
	if looksLikeJSON(text) {
		compressed = r.compressJSON(text)
	}

	// Apply text compression
	compressed = compressText(compressed)

	ct := tokenizer.EstimateTokens(compressed)
	if ct <= maxTokens {
		return Result{Text: compressed, Original: original, Compressed: ct, Method: "rule"}
	}

	// Last resort: truncate
	truncated := tokenizer.TruncateToTokens(compressed, maxTokens)
	ft := tokenizer.EstimateTokens(truncated)
	return Result{Text: truncated, Original: original, Compressed: ft, Method: "rule"}
}

// compressJSON reduces JSON size by removing unnecessary fields and limiting arrays.
func (r *ruleCompressor) compressJSON(text string) string {
	// Try as array first
	var arr []interface{}
	if err := json.Unmarshal([]byte(text), &arr); err == nil {
		arr = r.limitArray(arr)
		cleaned := make([]interface{}, 0, len(arr))
		for _, item := range arr {
			if m, ok := item.(map[string]interface{}); ok {
				cleaned = append(cleaned, cleanObject(m))
			} else {
				cleaned = append(cleaned, item)
			}
		}
		out, _ := json.Marshal(cleaned)
		return string(out)
	}

	// Try as object
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(text), &obj); err == nil {
		cleaned := cleanObject(obj)
		out, _ := json.Marshal(cleaned)
		return string(out)
	}

	// Not valid JSON — return as-is
	return text
}

// limitArray keeps first maxItems and adds a count note.
func (r *ruleCompressor) limitArray(arr []interface{}) []interface{} {
	if len(arr) <= r.maxItems {
		return arr
	}
	result := arr[:r.maxItems]
	result = append(result, fmt.Sprintf("... (%d more items)", len(arr)-r.maxItems))
	return result
}

// cleanObject removes empty/null fields and verbose metadata keys from list views.
func cleanObject(obj map[string]interface{}) map[string]interface{} {
	// Keys to remove from list/summary views (not important for AI reasoning)
	verboseKeys := map[string]bool{
		"created_at": true, "updated_at": true, "deleted_at": true,
		"CreatedAt": true, "UpdatedAt": true, "DeletedAt": true,
		"metadata": true, "raw": true, "html": true,
	}

	cleaned := make(map[string]interface{}, len(obj))
	for k, v := range obj {
		// Skip null/empty values
		if v == nil {
			continue
		}
		if s, ok := v.(string); ok && s == "" {
			continue
		}
		if a, ok := v.([]interface{}); ok && len(a) == 0 {
			continue
		}

		// Skip verbose metadata keys
		if verboseKeys[k] {
			continue
		}

		// Truncate long string values (notes, content, description)
		if s, ok := v.(string); ok && len(s) > 300 {
			v = s[:300] + "..."
		}

		// Recursively clean nested objects
		if nested, ok := v.(map[string]interface{}); ok {
			v = cleanObject(nested)
		}

		cleaned[k] = v
	}
	return cleaned
}

var (
	markdownHeadingRe = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	markdownBoldRe    = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	markdownCodeRe    = regexp.MustCompile("(?s)```[^`]*```")
	htmlTagRe         = regexp.MustCompile(`<[^>]+>`)
	multiNewlineRe    = regexp.MustCompile(`\n{3,}`)
	multiSpaceRe      = regexp.MustCompile(`  +`)
)

// compressText removes formatting and collapses whitespace.
func compressText(text string) string {
	// Remove code blocks (keep just a note)
	text = markdownCodeRe.ReplaceAllString(text, "[code block removed]")

	// Strip markdown formatting
	text = markdownHeadingRe.ReplaceAllString(text, "")
	text = markdownBoldRe.ReplaceAllString(text, "$1")

	// Strip HTML tags
	text = htmlTagRe.ReplaceAllString(text, "")

	// Collapse whitespace
	text = multiNewlineRe.ReplaceAllString(text, "\n\n")
	text = multiSpaceRe.ReplaceAllString(text, " ")

	return strings.TrimSpace(text)
}

// looksLikeJSON does a quick check if text starts with { or [.
func looksLikeJSON(text string) bool {
	text = strings.TrimSpace(text)
	return len(text) > 0 && (text[0] == '{' || text[0] == '[')
}
