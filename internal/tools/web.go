package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// WebTools provides native Go implementations of web search and URL reading.
// Much faster than tools-bridge (no Python spawn overhead).
type WebTools struct {
	serperKey  string
	httpClient *http.Client
}

// NewWebTools creates web tools. Reads SERPER_API_KEY from env.
func NewWebTools() *WebTools {
	return &WebTools{
		serperKey:  os.Getenv("SERPER_API_KEY"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// SearchWeb calls Serper API for Google search results.
func (w *WebTools) SearchWeb(ctx context.Context, query string) (string, error) {
	if w.serperKey == "" {
		return "", fmt.Errorf("SERPER_API_KEY not set")
	}

	body, _ := json.Marshal(map[string]interface{}{
		"q":   query,
		"gl":  "kr",
		"hl":  "ko",
		"num": 5,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", "https://google.serper.dev/search", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-API-KEY", w.serperKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("serper request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("serper %d: %s", resp.StatusCode, string(respBody))
	}

	// Extract organic results into concise format
	return formatSearchResults(respBody), nil
}

// ReadURL fetches a URL and extracts text content.
func (w *WebTools) ReadURL(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; nlook-router/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")

	resp, err := w.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch url: %w", err)
	}
	defer resp.Body.Close()

	// Limit read to 200KB
	limited := io.LimitReader(resp.Body, 200*1024)
	body, err := io.ReadAll(limited)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	text := htmlToText(string(body))

	// Truncate to 2000 chars
	if len(text) > 2000 {
		text = text[:2000] + "\n... (truncated)"
	}

	return text, nil
}

// formatSearchResults extracts organic results into a readable format.
func formatSearchResults(data []byte) string {
	var result struct {
		Organic []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"organic"`
		AnswerBox *struct {
			Answer  string `json:"answer"`
			Snippet string `json:"snippet"`
			Title   string `json:"title"`
		} `json:"answerBox"`
		KnowledgeGraph *struct {
			Title       string `json:"title"`
			Description string `json:"description"`
		} `json:"knowledgeGraph"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return string(data)
	}

	var sb strings.Builder

	// Answer box (direct answer)
	if result.AnswerBox != nil {
		if result.AnswerBox.Answer != "" {
			sb.WriteString("Answer: " + result.AnswerBox.Answer + "\n\n")
		} else if result.AnswerBox.Snippet != "" {
			sb.WriteString("Answer: " + result.AnswerBox.Snippet + "\n\n")
		}
	}

	// Knowledge graph
	if result.KnowledgeGraph != nil && result.KnowledgeGraph.Description != "" {
		sb.WriteString(result.KnowledgeGraph.Title + ": " + result.KnowledgeGraph.Description + "\n\n")
	}

	// Organic results
	for i, r := range result.Organic {
		if i >= 5 {
			break
		}
		sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n\n", i+1, r.Title, r.Snippet, r.Link))
	}

	return sb.String()
}

var (
	reScript  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle   = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	reTag     = regexp.MustCompile(`<[^>]+>`)
	reSpaces  = regexp.MustCompile(`[\t ]+`)
	reLines   = regexp.MustCompile(`\n{3,}`)
)

// htmlToText strips HTML tags and extracts readable text.
func htmlToText(html string) string {
	text := reScript.ReplaceAllString(html, "")
	text = reStyle.ReplaceAllString(text, "")
	text = reComment.ReplaceAllString(text, "")
	text = reTag.ReplaceAllString(text, " ")
	text = strings.ReplaceAll(text, "&nbsp;", " ")
	text = strings.ReplaceAll(text, "&amp;", "&")
	text = strings.ReplaceAll(text, "&lt;", "<")
	text = strings.ReplaceAll(text, "&gt;", ">")
	text = strings.ReplaceAll(text, "&quot;", `"`)
	text = strings.ReplaceAll(text, "&#39;", "'")
	text = reSpaces.ReplaceAllString(text, " ")
	text = reLines.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text)
}
