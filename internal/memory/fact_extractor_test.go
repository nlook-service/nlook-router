package memory

import (
	"context"
	"testing"
)

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "markdown json block",
			input: "Here are the facts:\n```json\n[{\"memory\": \"test\"}]\n```\nDone.",
			want:  `[{"memory": "test"}]`,
		},
		{
			name:  "markdown code block",
			input: "```\n[{\"memory\": \"test\"}]\n```",
			want:  `[{"memory": "test"}]`,
		},
		{
			name:  "raw json array",
			input: `Some text [{"memory": "test"}] more text`,
			want:  `[{"memory": "test"}]`,
		},
		{
			name:  "empty array",
			input: "No facts found.\n[]",
			want:  "[]",
		},
		{
			name:  "no json",
			input: "No JSON here at all",
			want:  "",
		},
		{
			name:  "nested brackets",
			input: `[{"memory": "uses [Go]", "topics": ["lang"]}]`,
			want:  `[{"memory": "uses [Go]", "topics": ["lang"]}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractJSON(tt.input)
			if got != tt.want {
				t.Errorf("extractJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	if truncate("short", 10) != "short" {
		t.Error("expected short string unchanged")
	}
	result := truncate("this is a long string", 10)
	if result != "this is a ..." {
		t.Errorf("expected truncated, got %q", result)
	}
}

func TestExtractorNilClient(t *testing.T) {
	fe := NewFactExtractor(nil, "test")

	msgs := []HistoryMessage{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	}

	result, err := fe.Extract(context.Background(), msgs)
	if err != nil {
		t.Errorf("expected nil error for nil client, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for nil client, got %v", result)
	}
}

func TestExtractorTooFewMessages(t *testing.T) {
	fe := NewFactExtractor(nil, "test")

	// Less than 2 messages should return nil
	result, err := fe.Extract(nil, []HistoryMessage{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for too few messages, got %v", result)
	}
}

func TestHistoryMessageStruct(t *testing.T) {
	m := HistoryMessage{Role: "user", Content: "hello"}
	if m.Role != "user" {
		t.Errorf("expected user, got %s", m.Role)
	}
	if m.Content != "hello" {
		t.Errorf("expected hello, got %s", m.Content)
	}
}
