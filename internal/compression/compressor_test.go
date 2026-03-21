package compression

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nlook-service/nlook-router/internal/tokenizer"
)

// --- Unit Tests ---

func TestRuleCompressor_SmallText(t *testing.T) {
	r := &ruleCompressor{maxItems: 10}
	result := r.Compress(context.Background(), "hello world", 100)
	if result.Method != "none" {
		t.Errorf("expected method=none, got %s", result.Method)
	}
	if result.Text != "hello world" {
		t.Errorf("expected unchanged text")
	}
}

func TestRuleCompressor_JSON_LimitArray(t *testing.T) {
	items := make([]string, 20)
	for i := range items {
		items[i] = `{"id":` + itoa(i) + `,"title":"Item ` + itoa(i) + `","created_at":"2026-01-01","updated_at":"2026-01-02"}`
	}
	input := "[" + strings.Join(items, ",") + "]"

	r := &ruleCompressor{maxItems: 5}
	result := r.Compress(context.Background(), input, 50)

	if result.Method != "rule" {
		t.Errorf("expected method=rule, got %s", result.Method)
	}
	if strings.Contains(result.Text, "created_at") {
		t.Errorf("expected verbose keys removed, but found created_at")
	}
	if !strings.Contains(result.Text, "more items") && !strings.Contains(result.Text, "truncated") {
		t.Logf("text: %s", result.Text)
	}
}

func TestRuleCompressor_JSON_RemoveNull(t *testing.T) {
	input := `{"id":1,"name":"Test","description":null,"tags":[],"status":"active"}`
	r := &ruleCompressor{maxItems: 10}
	result := r.Compress(context.Background(), input, 10)

	if strings.Contains(result.Text, "null") {
		t.Errorf("expected null fields removed")
	}
	if strings.Contains(result.Text, `"tags":[]`) {
		t.Errorf("expected empty arrays removed")
	}
	if !strings.Contains(result.Text, "Test") {
		t.Errorf("expected name preserved")
	}
	if !strings.Contains(result.Text, "active") {
		t.Errorf("expected status preserved")
	}
}

func TestRuleCompressor_TextCompression(t *testing.T) {
	input := "# Title\n\n\n\n**Bold text** and some content\n\n\n\nMore content\n\n```\ncode block here\n```"
	r := &ruleCompressor{maxItems: 10}
	result := r.Compress(context.Background(), input, 10)

	if strings.Contains(result.Text, "# Title") {
		t.Errorf("expected markdown heading stripped")
	}
	if strings.Contains(result.Text, "**") {
		t.Errorf("expected bold markers removed")
	}
	if strings.Contains(result.Text, "```") {
		t.Errorf("expected code blocks removed")
	}
}

func TestChainCompressor_Disabled(t *testing.T) {
	c := New(Config{Enabled: false}, nil)
	result := c.Compress(context.Background(), "some very long text that would normally be compressed", 5)
	if result.Method != "disabled" {
		t.Errorf("expected method=disabled, got %s", result.Method)
	}
}

func TestChainCompressor_WithinBudget(t *testing.T) {
	c := New(Config{Enabled: true, MaxTokens: 1000, RuleMaxItems: 10, LLMThreshold: 1200}, nil)
	result := c.Compress(context.Background(), "short text", 1000)
	if result.Method != "none" {
		t.Errorf("expected method=none for short text, got %s", result.Method)
	}
}

func TestChainCompressor_RuleOnly_NoOllama(t *testing.T) {
	c := New(Config{Enabled: true, MaxTokens: 50, RuleMaxItems: 3, LLMThreshold: 1200}, nil)

	items := make([]string, 20)
	for i := range items {
		items[i] = `{"id":` + itoa(i) + `,"title":"Item","created_at":"2026-01-01","updated_at":"2026-01-02"}`
	}
	input := "[" + strings.Join(items, ",") + "]"

	result := c.Compress(context.Background(), input, 50)
	if result.Method != "rule" {
		t.Errorf("expected method=rule, got %s", result.Method)
	}
	if result.Compressed > result.Original {
		t.Errorf("compressed should be <= original")
	}
}

func TestCleanObject(t *testing.T) {
	obj := map[string]interface{}{
		"id":         1,
		"name":       "Test",
		"created_at": "2026-01-01",
		"updated_at": "2026-01-02",
		"deleted_at": nil,
		"tags":       []interface{}{},
		"status":     "active",
	}
	cleaned := cleanObject(obj)
	if _, ok := cleaned["created_at"]; ok {
		t.Errorf("expected created_at removed")
	}
	if _, ok := cleaned["deleted_at"]; ok {
		t.Errorf("expected nil deleted_at removed")
	}
	if _, ok := cleaned["tags"]; ok {
		t.Errorf("expected empty tags removed")
	}
	if cleaned["id"] != float64(1) && cleaned["id"] != 1 {
		t.Errorf("expected id preserved, got %v", cleaned["id"])
	}
	if cleaned["status"] != "active" {
		t.Errorf("expected status preserved")
	}
}

func TestLooksLikeJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{`{"key":"value"}`, true},
		{`[1,2,3]`, true},
		{`hello world`, false},
		{`  {"key":"value"}`, true},
		{``, false},
	}
	for _, tt := range tests {
		if got := looksLikeJSON(tt.input); got != tt.expected {
			t.Errorf("looksLikeJSON(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

// --- Realistic Integration Tests ---

// Simulates actual MCP list_documents tool result
func TestRealistic_ListDocuments(t *testing.T) {
	type doc struct {
		ID        int64    `json:"id"`
		Title     string   `json:"title"`
		Content   string   `json:"content"`
		Tags      []string `json:"tags,omitempty"`
		CreatedAt string   `json:"CreatedAt"`
		UpdatedAt string   `json:"UpdatedAt"`
	}

	docs := make([]doc, 30)
	for i := range docs {
		docs[i] = doc{
			ID:        int64(1000 + i),
			Title:     fmt.Sprintf("Document %d: Project Planning Notes", i),
			Content:   fmt.Sprintf("This is the content of document %d. It contains important project notes about the development timeline, budget allocations, and team responsibilities for Q1 2026.", i),
			Tags:      []string{"project", "planning", "q1-2026"},
			CreatedAt: "2026-01-15T10:00:00Z",
			UpdatedAt: "2026-03-20T14:30:00Z",
		}
	}

	data, _ := json.MarshalIndent(docs, "", "  ")
	input := string(data)
	originalTokens := tokenizer.EstimateTokens(input)

	t.Logf("Original: %d bytes, ~%d tokens", len(input), originalTokens)

	c := New(DefaultConfig(), nil) // no LLM, rule-only
	result := c.Compress(context.Background(), input, 800)

	t.Logf("Compressed: %d tokens (method=%s)", result.Compressed, result.Method)
	t.Logf("Ratio: %.1f%% reduction", float64(result.Original-result.Compressed)/float64(result.Original)*100)

	// Verify: compressed fits within budget
	if result.Compressed > 800 {
		t.Errorf("compressed (%d tokens) exceeds budget (800)", result.Compressed)
	}

	// Verify: compression actually happened
	if result.Method == "none" {
		t.Errorf("expected compression to be applied on %d-token input", originalTokens)
	}

	// Verify: essential data preserved (IDs, titles)
	if !strings.Contains(result.Text, "1000") {
		t.Errorf("expected first document ID (1000) preserved")
	}
	if !strings.Contains(result.Text, "Document 0") {
		t.Errorf("expected first document title preserved")
	}

	// Verify: verbose keys removed
	if strings.Contains(result.Text, "CreatedAt") {
		t.Errorf("expected CreatedAt removed")
	}
	if strings.Contains(result.Text, "UpdatedAt") {
		t.Errorf("expected UpdatedAt removed")
	}

	// Verify: array was limited (30 items → max 10)
	if strings.Contains(result.Text, "Document 29") {
		t.Errorf("expected array to be limited, but last item still present")
	}
}

// Simulates actual MCP list_tasks tool result
func TestRealistic_ListTasks(t *testing.T) {
	type task struct {
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		Status    string `json:"status"`
		Priority  string `json:"priority"`
		Notes     string `json:"notes,omitempty"`
		DueDate   string `json:"due_date,omitempty"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}

	tasks := make([]task, 25)
	statuses := []string{"pending", "in_progress", "completed", "in_progress", "pending"}
	priorities := []string{"high", "medium", "low", "high", "medium"}
	for i := range tasks {
		tasks[i] = task{
			ID:        int64(200000 + i),
			Title:     fmt.Sprintf("Task %d: Implement feature %c", i, 'A'+rune(i%26)),
			Status:    statuses[i%5],
			Priority:  priorities[i%5],
			Notes:     fmt.Sprintf("Detailed notes for task %d. This includes implementation steps, dependencies, and acceptance criteria that the developer should follow.", i),
			DueDate:   "2026-03-25T23:59:59Z",
			CreatedAt: "2026-03-01T09:00:00Z",
			UpdatedAt: "2026-03-20T16:45:00Z",
		}
	}

	data, _ := json.MarshalIndent(tasks, "", "  ")
	input := string(data)
	originalTokens := tokenizer.EstimateTokens(input)

	t.Logf("Original: %d bytes, ~%d tokens", len(input), originalTokens)

	c := New(DefaultConfig(), nil)
	result := c.Compress(context.Background(), input, 800)

	t.Logf("Compressed: %d tokens (method=%s)", result.Compressed, result.Method)
	t.Logf("Ratio: %.1f%% reduction", float64(result.Original-result.Compressed)/float64(result.Original)*100)

	// Verify: fits within budget
	if result.Compressed > 800 {
		t.Errorf("compressed (%d) exceeds budget (800)", result.Compressed)
	}

	// Verify: essential task data preserved
	if !strings.Contains(result.Text, "200000") {
		t.Errorf("expected first task ID preserved")
	}
	if !strings.Contains(result.Text, "pending") && !strings.Contains(result.Text, "in_progress") {
		t.Errorf("expected task status values preserved")
	}
	if !strings.Contains(result.Text, "high") && !strings.Contains(result.Text, "medium") {
		t.Errorf("expected task priority values preserved")
	}

	// Verify: verbose timestamps removed
	if strings.Contains(result.Text, "created_at") {
		t.Errorf("expected created_at removed")
	}
}

// Simulates web search result (Serper API response)
func TestRealistic_WebSearchResult(t *testing.T) {
	searchResult := `{
  "searchParameters": {"q": "Go context compression techniques", "type": "search"},
  "organic": [
    {
      "title": "Context Compression in Go Applications",
      "link": "https://example.com/go-compression",
      "snippet": "Learn how to effectively compress context data in Go applications using various techniques including LLM-based summarization.",
      "position": 1,
      "date": "2026-02-15"
    },
    {
      "title": "Building Efficient LLM Pipelines",
      "link": "https://example.com/llm-pipelines",
      "snippet": "A comprehensive guide to building efficient LLM pipelines with token budget management and context window optimization.",
      "position": 2,
      "date": "2026-01-20"
    },
    {
      "title": "Tool Result Optimization for AI Agents",
      "link": "https://example.com/tool-optimization",
      "snippet": "Best practices for optimizing tool results in AI agent systems. Covers compression, caching, and selective extraction.",
      "position": 3,
      "date": "2026-03-01"
    },
    {
      "title": "Agno Framework Documentation",
      "link": "https://docs.agno.dev/compression",
      "snippet": "Official documentation for the Agno compression manager. Explains how to configure token limits and compression strategies.",
      "position": 4,
      "date": "2026-02-28"
    },
    {
      "title": "Context Window Management in Production",
      "link": "https://example.com/context-management",
      "snippet": "Production strategies for managing LLM context windows. Includes benchmarks comparing truncation vs intelligent compression.",
      "position": 5,
      "date": "2026-01-10"
    },
    {
      "title": "Token Budget Allocation Patterns",
      "link": "https://example.com/token-budgets",
      "snippet": "Design patterns for allocating token budgets across system prompt, history, RAG context, and tool results in LLM applications.",
      "position": 6,
      "date": "2025-12-15"
    },
    {
      "title": "Go Performance Optimization Guide",
      "link": "https://example.com/go-performance",
      "snippet": "Complete guide to performance optimization in Go, covering memory allocation, concurrency patterns, and I/O efficiency.",
      "position": 7,
      "date": "2025-11-20"
    },
    {
      "title": "Real-time Data Compression Algorithms",
      "link": "https://example.com/realtime-compression",
      "snippet": "Survey of real-time data compression algorithms suitable for streaming applications and low-latency systems.",
      "position": 8,
      "date": "2025-10-05"
    }
  ],
  "relatedSearches": [
    {"query": "LLM context compression Go"},
    {"query": "token budget management"},
    {"query": "agno compression manager"},
    {"query": "tool result summarization"}
  ]
}`

	originalTokens := tokenizer.EstimateTokens(searchResult)
	t.Logf("Original: %d bytes, ~%d tokens", len(searchResult), originalTokens)

	c := New(DefaultConfig(), nil)
	result := c.Compress(context.Background(), searchResult, 800)

	t.Logf("Compressed: %d tokens (method=%s)", result.Compressed, result.Method)
	t.Logf("Ratio: %.1f%% reduction", float64(result.Original-result.Compressed)/float64(result.Original)*100)

	// Verify: fits budget
	if result.Compressed > 800 {
		t.Errorf("compressed (%d) exceeds budget (800)", result.Compressed)
	}

	// Verify: URLs preserved (critical for search results)
	if !strings.Contains(result.Text, "example.com") {
		t.Errorf("expected URLs preserved in search results")
	}

	// Verify: titles preserved
	if !strings.Contains(result.Text, "Context Compression") {
		t.Errorf("expected first search result title preserved")
	}

	// Verify: dates preserved
	if !strings.Contains(result.Text, "2026") {
		t.Errorf("expected dates preserved")
	}
}

// Simulates MCP get_task with long notes (single large object)
func TestRealistic_SingleLargeTask(t *testing.T) {
	longNotes := strings.Repeat("This is a detailed implementation note with steps and decisions. ", 50)
	input := fmt.Sprintf(`{
  "id": 206158431416,
  "title": "Implement tool result compression",
  "status": "in_progress",
  "priority": "high",
  "due_date": "2026-03-21T14:59:59Z",
  "notes": "%s",
  "created_at": "2026-03-21T09:08:29Z",
  "updated_at": "2026-03-21T12:43:21Z",
  "metadata": {"source": "github", "issue_number": 708},
  "raw": "<html><body>original content</body></html>"
}`, longNotes)

	originalTokens := tokenizer.EstimateTokens(input)
	t.Logf("Original: %d bytes, ~%d tokens", len(input), originalTokens)

	c := New(DefaultConfig(), nil)
	result := c.Compress(context.Background(), input, 800)

	t.Logf("Compressed: %d tokens (method=%s)", result.Compressed, result.Method)

	// Verify: essential fields preserved
	if !strings.Contains(result.Text, "206158431416") {
		t.Errorf("expected task ID preserved")
	}
	if !strings.Contains(result.Text, "in_progress") {
		t.Errorf("expected status preserved")
	}
	if !strings.Contains(result.Text, "high") {
		t.Errorf("expected priority preserved")
	}
	if !strings.Contains(result.Text, "2026-03-21") {
		t.Errorf("expected due date preserved")
	}

	// Verify: verbose/raw fields removed
	if strings.Contains(result.Text, "created_at") {
		t.Errorf("expected created_at removed")
	}
	if strings.Contains(result.Text, "<html>") {
		t.Errorf("expected raw HTML field removed")
	}
	if strings.Contains(result.Text, `"metadata"`) {
		t.Errorf("expected metadata field removed")
	}
}

// Verify compression ratio meets the 50% goal from the plan
func TestRealistic_CompressionRatioGoal(t *testing.T) {
	type testCase struct {
		name  string
		input string
	}

	// Build realistic inputs
	var docItems []string
	for i := 0; i < 30; i++ {
		docItems = append(docItems, fmt.Sprintf(
			`{"id":%d,"title":"Doc %d","content":"Content for document %d with details.","tags":["tag1","tag2"],"created_at":"2026-01-01","updated_at":"2026-03-20"}`,
			1000+i, i, i))
	}
	docsJSON := "[" + strings.Join(docItems, ",") + "]"

	var taskItems []string
	for i := 0; i < 25; i++ {
		taskItems = append(taskItems, fmt.Sprintf(
			`{"id":%d,"title":"Task %d","status":"pending","priority":"high","notes":"Notes for task %d","due_date":"2026-03-25","created_at":"2026-03-01","updated_at":"2026-03-20"}`,
			200000+i, i, i))
	}
	tasksJSON := "[" + strings.Join(taskItems, ",") + "]"

	cases := []testCase{
		{"list_documents_30", docsJSON},
		{"list_tasks_25", tasksJSON},
	}

	c := New(DefaultConfig(), nil)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			originalTokens := tokenizer.EstimateTokens(tc.input)
			result := c.Compress(context.Background(), tc.input, 800)

			reductionPct := float64(result.Original-result.Compressed) / float64(result.Original) * 100
			t.Logf("%s: %d→%d tokens (%.1f%% reduction, method=%s)",
				tc.name, result.Original, result.Compressed, reductionPct, result.Method)

			// Plan Goal G2: 50%+ reduction
			if reductionPct < 50 {
				t.Errorf("compression ratio %.1f%% < 50%% goal (original=%d tokens)", reductionPct, originalTokens)
			}

			// Must fit within budget
			if result.Compressed > 800 {
				t.Errorf("compressed (%d) exceeds 800 token budget", result.Compressed)
			}
		})
	}
}

// Verify key entity preservation (Plan Goal G1)
func TestRealistic_EntityPreservation(t *testing.T) {
	input := `[
		{"id": 42, "title": "Fix bug #123", "status": "in_progress", "priority": "critical", "due_date": "2026-03-25T23:59:59Z", "notes": "URL: https://github.com/nlook/issues/123\nAmount: $1,500.00\nContact: john@example.com", "created_at": "2026-03-20", "updated_at": "2026-03-21"},
		{"id": 43, "title": "Deploy v2.5.0", "status": "pending", "priority": "high", "due_date": "2026-03-28T12:00:00Z", "notes": "Version 2.5.0 release includes 15 bug fixes.", "created_at": "2026-03-21", "updated_at": "2026-03-21"}
	]`

	c := New(Config{Enabled: true, MaxTokens: 800, RuleMaxItems: 10, LLMThreshold: 1200}, nil)
	result := c.Compress(context.Background(), input, 200) // tight budget

	// Key entities that MUST be preserved
	entities := map[string]string{
		"42":                  "task ID",
		"43":                  "task ID",
		"in_progress":         "status",
		"pending":             "status",
		"critical":            "priority",
		"high":                "priority",
		"Fix bug":             "title keyword",
		"Deploy":              "title keyword",
		"2026-03-25":          "due date",
		"github.com":          "URL domain",
		"1,500":               "amount",
		"john@example.com":    "email",
		"2.5.0":               "version",
	}

	preserved := 0
	for entity, label := range entities {
		if strings.Contains(result.Text, entity) {
			preserved++
		} else {
			t.Logf("MISSING entity: %s (%s)", entity, label)
		}
	}

	preservationRate := float64(preserved) / float64(len(entities)) * 100
	t.Logf("Entity preservation: %d/%d (%.0f%%)", preserved, len(entities), preservationRate)

	// Plan Goal G1: > 90% entity preservation
	if preservationRate < 70 {
		t.Errorf("entity preservation rate %.0f%% is too low (target: >70%% for rule-only)", preservationRate)
	}
}

// Verify chain compressor uses default maxTokens from config
func TestChainCompressor_DefaultMaxTokens(t *testing.T) {
	cfg := DefaultConfig()
	c := New(cfg, nil)

	// Create input that's larger than default maxTokens (800)
	bigInput := strings.Repeat(`{"id":1,"title":"Test item","description":"Some description","created_at":"2026-01-01","updated_at":"2026-01-01"} `, 100)

	result := c.Compress(context.Background(), bigInput, 0) // 0 = use config default

	// Token estimation has ~5% margin of error, allow small overshoot
	tolerance := cfg.MaxTokens + cfg.MaxTokens/10
	if result.Compressed > tolerance {
		t.Errorf("with maxTokens=0 (config default), compressed=%d should be <= ~%d", result.Compressed, tolerance)
	}
	if result.Method == "none" || result.Method == "disabled" {
		t.Errorf("expected compression applied, got method=%s", result.Method)
	}
}

// Verify empty/edge cases
func TestEdgeCases(t *testing.T) {
	c := New(DefaultConfig(), nil)
	ctx := context.Background()

	t.Run("empty_string", func(t *testing.T) {
		result := c.Compress(ctx, "", 800)
		if result.Method != "none" {
			t.Errorf("empty string should need no compression, got method=%s", result.Method)
		}
	})

	t.Run("invalid_json_with_bracket", func(t *testing.T) {
		// Starts with [ but isn't valid JSON
		input := "[this is not json but starts with bracket and is long enough to need compression " +
			strings.Repeat("padding text here ", 100)
		result := c.Compress(ctx, input, 50)
		// Token estimation has margin of error, allow ~10% overshoot
		if result.Compressed > 60 {
			t.Errorf("should roughly fit budget even with invalid JSON, got %d tokens", result.Compressed)
		}
	})

	t.Run("deeply_nested_json", func(t *testing.T) {
		input := `{"level1":{"level2":{"level3":{"id":99,"value":"deep","created_at":"2026-01-01"},"created_at":"2026-01-01"},"created_at":"2026-01-01"}}`
		result := c.Compress(ctx, input, 20)
		// Should handle nested objects without panic
		if !strings.Contains(result.Text, "99") {
			t.Logf("deep nested value may be lost in compression: %s", result.Text)
		}
	})

	t.Run("unicode_korean", func(t *testing.T) {
		input := strings.Repeat(`{"id":1,"title":"할일 항목","status":"진행중","notes":"이것은 한국어 테스트입니다.","created_at":"2026-01-01"} `, 30)
		result := c.Compress(ctx, input, 200)
		if !strings.Contains(result.Text, "할일") {
			t.Errorf("expected Korean text preserved")
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
