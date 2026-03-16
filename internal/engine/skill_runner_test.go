package engine

import (
	"context"
	"testing"

	"github.com/nlook-service/nlook-router/internal/apiclient"
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

// ──────────────────────────────────────────────────────────────────────────────
// RunSkill — tool type (how tools are used in workflow steps)
// ──────────────────────────────────────────────────────────────────────────────

// TestSkillRunner_RunSkill_tool verifies how a tool skill is executed.
// The skill has SkillType "tool" and Config["tool_name"] (or skill.Name) identifies the tool.
// Currently runTool returns a placeholder; when a tool bridge is wired, it will call the bridge.
func TestSkillRunner_RunSkill_tool(t *testing.T) {
	runner := NewSkillRunner()
	ctx := context.Background()

	skill := &apiclient.WorkflowSkill{
		ID:        1,
		Name:      "my-calculator",
		SkillType: "tool",
		Config:    map[string]interface{}{"tool_name": "add"},
	}
	input := map[string]interface{}{"a": 1.0, "b": 2.0}

	output, logs, err := runner.RunSkill(ctx, skill, nil, input)
	if err != nil {
		t.Fatalf("RunSkill(tool): %v", err)
	}
	if len(logs) == 0 || logs[0] != "tool execution: add" {
		t.Errorf("logs: got %v, want first line 'tool execution: add'", logs)
	}
	if output["tool"] != "add" {
		t.Errorf("output[tool]: got %v, want 'add'", output["tool"])
	}
	if output["input"] == nil {
		t.Error("output should include input passed to the tool")
	}
	// Placeholder message until real tool bridge is connected
	if output["message"] == "" {
		t.Error("output should include message (placeholder or bridge result)")
	}
}

// TestSkillRunner_RunSkill_toolFallbackToName verifies that when tool_name is not in Config,
// the skill name is used as the tool name.
func TestSkillRunner_RunSkill_toolFallbackToName(t *testing.T) {
	runner := NewSkillRunner()
	ctx := context.Background()

	skill := &apiclient.WorkflowSkill{
		ID:        2,
		Name:      "subtract",
		SkillType: "tool",
		Config:    map[string]interface{}{}, // no tool_name
	}
	input := map[string]interface{}{"x": 10, "y": 3}

	output, _, err := runner.RunSkill(ctx, skill, nil, input)
	if err != nil {
		t.Fatalf("RunSkill(tool): %v", err)
	}
	if output["tool"] != "subtract" {
		t.Errorf("output[tool]: got %v, want 'subtract' (fallback to skill name)", output["tool"])
	}
}

// mockToolExecutor returns fixed JSON for Execute (for runTool bridge tests).
type mockToolExecutor struct {
	result []byte
	err    error
}

func (m *mockToolExecutor) Execute(_ context.Context, _ string, _ map[string]interface{}) ([]byte, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

// TestSkillRunner_RunSkill_toolWithExecutor verifies that when SetToolExecutor is set,
// runTool calls it and returns the parsed JSON as output.
func TestSkillRunner_RunSkill_toolWithExecutor(t *testing.T) {
	runner := NewSkillRunner()
	ctx := context.Background()
	runner.SetToolExecutor(&mockToolExecutor{
		result: []byte(`{"status":"success","result":3,"error":null}`),
	})
	skill := &apiclient.WorkflowSkill{
		ID:        1,
		Name:      "add",
		SkillType: "tool",
		Config:    map[string]interface{}{"tool_name": "add"},
	}
	input := map[string]interface{}{"a": 1.0, "b": 2.0}

	output, logs, err := runner.RunSkill(ctx, skill, nil, input)
	if err != nil {
		t.Fatalf("RunSkill(tool): %v", err)
	}
	if len(logs) == 0 || logs[0] != "tool execution: add" {
		t.Errorf("logs: got %v", logs)
	}
	if output["status"] != "success" || output["result"] != float64(3) {
		t.Errorf("output: got %v (expect status=success result=3)", output)
	}
}

// TestSkillRunner_RunSkill_toolExecutorError verifies that when the executor returns an error,
// runTool returns a map with the error and does not fail the skill.
func TestSkillRunner_RunSkill_toolExecutorError(t *testing.T) {
	runner := NewSkillRunner()
	ctx := context.Background()
	runner.SetToolExecutor(&mockToolExecutor{err: context.DeadlineExceeded})
	skill := &apiclient.WorkflowSkill{
		ID:        1,
		Name:      "add",
		SkillType: "tool",
		Config:    map[string]interface{}{"tool_name": "add"},
	}
	input := map[string]interface{}{"a": 1.0}

	output, logs, err := runner.RunSkill(ctx, skill, nil, input)
	if err != nil {
		t.Fatalf("RunSkill(tool): %v", err)
	}
	if output["error"] == nil {
		t.Errorf("expected error in output, got %v", output)
	}
	if len(logs) < 2 {
		t.Errorf("expected tool error in logs, got %v", logs)
	}
}
