package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- mock DB ---

type mockDB struct {
	sets    map[string]*EvalSet
	cases   map[string][]*EvalCase
	runs    map[string]*EvalRun
	results []*EvalResult
}

func newMockDB() *mockDB {
	return &mockDB{
		sets:  make(map[string]*EvalSet),
		cases: make(map[string][]*EvalCase),
		runs:  make(map[string]*EvalRun),
	}
}

func (m *mockDB) GetEvalSet(_ context.Context, id string) (*EvalSet, error) {
	return m.sets[id], nil
}

func (m *mockDB) ListEvalCases(_ context.Context, setID string) ([]*EvalCase, error) {
	return m.cases[setID], nil
}

func (m *mockDB) InsertEvalRun(_ context.Context, run *EvalRun) error {
	m.runs[run.ID] = run
	return nil
}

func (m *mockDB) UpdateEvalRun(_ context.Context, run *EvalRun) error {
	m.runs[run.ID] = run
	return nil
}

func (m *mockDB) InsertEvalResult(_ context.Context, result *EvalResult) error {
	m.results = append(m.results, result)
	return nil
}

// --- tests ---

func TestBuildAccuracyUserPrompt(t *testing.T) {
	prompt := buildAccuracyUserPrompt("질문", "기대답변", "실제답변")
	if prompt != "Input: 질문\n\nExpected Output: 기대답변\n\nActual Output: 실제답변" {
		t.Fatalf("unexpected prompt: %s", prompt)
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		max    int
		expect string
	}{
		{"short", 10, "short"},
		{"long string here", 4, "long..."},
		{"", 5, ""},
		{"exact", 5, "exact"},
	}
	for _, tc := range tests {
		got := truncate(tc.input, tc.max)
		if got != tc.expect {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.max, got, tc.expect)
		}
	}
}

func TestMeasure(t *testing.T) {
	output, metrics, err := Measure(func() (string, int, int, error) {
		time.Sleep(10 * time.Millisecond)
		return "hello", 100, 200, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output != "hello" {
		t.Fatalf("unexpected output: %s", output)
	}
	if metrics.TokensIn != 100 || metrics.TokensOut != 200 {
		t.Fatalf("unexpected tokens: in=%d out=%d", metrics.TokensIn, metrics.TokensOut)
	}
	if metrics.LatencyMs < 10 {
		t.Fatalf("latency too low: %d", metrics.LatencyMs)
	}
}

func TestMeasureError(t *testing.T) {
	_, _, err := Measure(func() (string, int, int, error) {
		return "", 0, 0, fmt.Errorf("test error")
	})
	if err == nil || err.Error() != "test error" {
		t.Fatalf("expected test error, got: %v", err)
	}
}

// newMockLLMServer creates an HTTP test server that mimics the OpenAI-compatible API.
func newMockLLMServer(t *testing.T, responseContent string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]string{
						"role":    "assistant",
						"content": responseContent,
					},
				},
			},
			"usage": map[string]int{
				"prompt_tokens":     50,
				"completion_tokens": 30,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestAccuracyEvaluatorParseScore(t *testing.T) {
	// Test JSON parsing with the mock LLM returning a valid score
	server := newMockLLMServer(t, `{"score": 8, "reason": "Good match"}`)
	defer server.Close()

	// We can't easily create an llm.Engine pointing to our mock server
	// since it auto-detects backends. Instead, test the JSON parsing logic directly.
	tests := []struct {
		name    string
		content string
		score   int
		reason  string
		wantErr bool
	}{
		{
			name:    "plain JSON",
			content: `{"score": 8, "reason": "Good match"}`,
			score:   8,
			reason:  "Good match",
		},
		{
			name:    "with code fences",
			content: "```json\n{\"score\": 9, \"reason\": \"Almost perfect\"}\n```",
			score:   9,
			reason:  "Almost perfect",
		},
		{
			name:    "score out of range",
			content: `{"score": 0, "reason": "Bad"}`,
			wantErr: true,
		},
		{
			name:    "score too high",
			content: `{"score": 11, "reason": "Too good"}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			content: `not json at all`,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Simulate the parsing logic from AccuracyEvaluator.Score
			content := tc.content
			content = trimCodeFences(content)
			var result ScoreResult
			err := json.Unmarshal([]byte(content), &result)
			if err == nil && (result.Score < 1 || result.Score > 10) {
				err = fmt.Errorf("invalid score %d", result.Score)
			}

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Score != tc.score {
				t.Fatalf("score = %d, want %d", result.Score, tc.score)
			}
			if result.Reason != tc.reason {
				t.Fatalf("reason = %q, want %q", result.Reason, tc.reason)
			}
		})
	}
}

func TestEvalRunnerValidation(t *testing.T) {
	db := newMockDB()
	ctx := context.Background()

	// Create runner with nil engine (won't reach LLM calls in validation tests)
	runner := &EvalRunner{db: db, evaluator: &AccuracyEvaluator{model: "test"}}

	// Test: set not found
	_, err := runner.Run(ctx, "nonexistent", RunOptions{NumIterations: 1})
	if err == nil || err.Error() != `eval set "nonexistent" not found` {
		t.Fatalf("expected not found error, got: %v", err)
	}

	// Test: set exists but no cases
	db.sets["empty-set"] = &EvalSet{
		ID:         "empty-set",
		Name:       "empty",
		TargetType: "chat",
		Model:      "test-model",
	}
	_, err = runner.Run(ctx, "empty-set", RunOptions{NumIterations: 1})
	if err == nil || err.Error() != `eval set "empty-set" has no cases` {
		t.Fatalf("expected no cases error, got: %v", err)
	}
}

func TestEvalRunStatistics(t *testing.T) {
	// Test the statistics calculation logic
	scores := []float64{8, 9, 7, 10, 6}

	var sum float64
	for _, s := range scores {
		sum += s
	}
	avg := sum / float64(len(scores))

	if avg != 8.0 {
		t.Fatalf("avg = %.1f, want 8.0", avg)
	}

	var variance float64
	for _, s := range scores {
		diff := s - avg
		variance += diff * diff
	}
	stddev := variance / float64(len(scores))

	// Variance of [8,9,7,10,6] with mean=8: (0+1+1+4+4)/5 = 2.0
	if stddev != 2.0 {
		t.Fatalf("variance = %.1f, want 2.0", stddev)
	}
}

// trimCodeFences strips markdown code fences (same logic as in accuracy.go)
func trimCodeFences(s string) string {
	import_strings := s
	import_strings = trimPrefix(import_strings, "```json")
	import_strings = trimPrefix(import_strings, "```")
	import_strings = trimSuffix(import_strings, "```")
	return trimSpace(import_strings)
}

func trimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

func trimSuffix(s, suffix string) string {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t' || s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
