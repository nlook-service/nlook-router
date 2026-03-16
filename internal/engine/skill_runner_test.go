package engine

import (
	"testing"
)

// ──────────────────────────────────────────────────────────────────────────────
// resolveTemplate
// ──────────────────────────────────────────────────────────────────────────────

func TestResolveTemplate_noPlaceholders(t *testing.T) {
	got := resolveTemplate("hello world", map[string]interface{}{"x": "y"})
	if got != "hello world" {
		t.Errorf("got %q, want 'hello world'", got)
	}
}

func TestResolveTemplate_singleString(t *testing.T) {
	got := resolveTemplate("Hello {{name}}!", map[string]interface{}{"name": "Alice"})
	if got != "Hello Alice!" {
		t.Errorf("got %q, want 'Hello Alice!'", got)
	}
}

func TestResolveTemplate_multiplePlaceholders(t *testing.T) {
	tmpl := "{{greeting}}, {{name}}. You are {{age}} years old."
	input := map[string]interface{}{
		"greeting": "Hi",
		"name":     "Bob",
		"age":      30,
	}
	got := resolveTemplate(tmpl, input)
	want := "Hi, Bob. You are 30 years old."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveTemplate_emptyTemplate(t *testing.T) {
	got := resolveTemplate("", map[string]interface{}{"k": "v"})
	if got != "" {
		t.Errorf("empty template should return empty string, got %q", got)
	}
}

func TestResolveTemplate_nilInput(t *testing.T) {
	got := resolveTemplate("{{key}}", nil)
	// nil input → no substitution, placeholder stays
	if got != "{{key}}" {
		t.Errorf("got %q, want '{{key}}'", got)
	}
}

func TestResolveTemplate_missingKey(t *testing.T) {
	got := resolveTemplate("{{missing}}", map[string]interface{}{"other": "value"})
	if got != "{{missing}}" {
		t.Errorf("missing key should leave placeholder intact, got %q", got)
	}
}

func TestResolveTemplate_jsonValueForNonString(t *testing.T) {
	input := map[string]interface{}{
		"data": map[string]interface{}{"key": "val"},
	}
	got := resolveTemplate("result: {{data}}", input)
	// Should contain JSON representation
	if got == "result: {{data}}" {
		t.Error("non-string value should be JSON-encoded")
	}
	if len(got) == 0 {
		t.Error("result should not be empty")
	}
}

func TestResolveTemplate_boolValue(t *testing.T) {
	got := resolveTemplate("flag={{flag}}", map[string]interface{}{"flag": true})
	if got != "flag=true" {
		t.Errorf("got %q, want 'flag=true'", got)
	}
}

func TestResolveTemplate_repeatedPlaceholder(t *testing.T) {
	got := resolveTemplate("{{x}} {{x}}", map[string]interface{}{"x": "hi"})
	if got != "hi hi" {
		t.Errorf("got %q, want 'hi hi'", got)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// truncate
// ──────────────────────────────────────────────────────────────────────────────

func TestTruncate_shortString(t *testing.T) {
	got := truncate("hello", 10)
	if got != "hello" {
		t.Errorf("got %q, want 'hello'", got)
	}
}

func TestTruncate_exactLength(t *testing.T) {
	got := truncate("hello", 5)
	if got != "hello" {
		t.Errorf("got %q, want 'hello'", got)
	}
}

func TestTruncate_longString(t *testing.T) {
	got := truncate("hello world", 5)
	if got != "hello..." {
		t.Errorf("got %q, want 'hello...'", got)
	}
}

func TestTruncate_emptyString(t *testing.T) {
	got := truncate("", 10)
	if got != "" {
		t.Errorf("got %q, want ''", got)
	}
}

func TestTruncate_zeroMaxLen(t *testing.T) {
	got := truncate("hello", 0)
	if got != "..." {
		t.Errorf("got %q, want '...'", got)
	}
}

// ──────────────────────────────────────────────────────────────────────────────
// isLocalModel
// ──────────────────────────────────────────────────────────────────────────────

func TestIsLocalModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"qwen2.5:7b", true},
		{"llama3.1:8b", true},
		{"mistral:latest", true},
		{"codellama:13b", true},
		{"gemma:2b", true},
		{"phi3:mini", true},
		{"deepseek-coder:6.7b", true},
		{"ollama/qwen", true},
		{"local/my-model", true},
		{"claude-sonnet-4-20250514", false},
		{"gpt-4o", false},
		{"gpt-3.5-turbo", false},
		{"", false},
	}

	for _, tt := range tests {
		got := isLocalModel(tt.model)
		if got != tt.want {
			t.Errorf("isLocalModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}
