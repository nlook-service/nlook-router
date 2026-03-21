package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- Mock LLMCaller ---

type mockCaller struct {
	mu        sync.Mutex
	calls     []mockCall
	responses map[string]string // model -> response
}

type mockCall struct {
	Model  string
	System string
	Prompt string
}

func newMockCaller() *mockCaller {
	return &mockCaller{
		responses: make(map[string]string),
	}
}

func (m *mockCaller) SetResponse(model, response string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.responses[model] = response
}

func (m *mockCaller) Call(ctx context.Context, model, system, prompt string) (string, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, mockCall{Model: model, System: system, Prompt: prompt})

	if resp, ok := m.responses[model]; ok {
		return resp, 100, nil
	}
	return fmt.Sprintf("mock response from %s", model), 50, nil
}

func (m *mockCaller) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockCaller) GetCalls() []mockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]mockCall, len(m.calls))
	copy(cp, m.calls)
	return cp
}

// --- Mock WS sender ---

type mockWS struct {
	mu       sync.Mutex
	messages []map[string]interface{}
}

func newMockWS() *mockWS {
	return &mockWS{}
}

func (w *mockWS) send(data []byte) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var msg map[string]interface{}
	json.Unmarshal(data, &msg)
	w.messages = append(w.messages, msg)
}

func (w *mockWS) count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.messages)
}

func (w *mockWS) types() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	var types []string
	for _, m := range w.messages {
		if t, ok := m["type"].(string); ok {
			types = append(types, t)
		}
	}
	return types
}

// === Tests ===

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.EscalationThreshold != 0.7 {
		t.Errorf("expected threshold 0.7, got %f", cfg.EscalationThreshold)
	}
	if cfg.MaxSubTasks != 10 {
		t.Errorf("expected max_subtasks 10, got %d", cfg.MaxSubTasks)
	}
	if cfg.Roles[RoleScout] != "gemma3:4b" {
		t.Errorf("expected scout=gemma3:4b, got %s", cfg.Roles[RoleScout])
	}
}

func TestModelRegistry_Resolve(t *testing.T) {
	roles := map[Role]string{
		RoleScout:   "gemma3:4b",
		RoleThinker: "claude-sonnet-4-6",
		RoleBuilder: "claude-opus-4-6",
	}
	reg := NewModelRegistry(roles)

	// Claude models are always "available"
	if m := reg.Resolve(RoleThinker); m != "claude-sonnet-4-6" {
		t.Errorf("expected claude-sonnet-4-6, got %s", m)
	}
	if m := reg.Resolve(RoleBuilder); m != "claude-opus-4-6" {
		t.Errorf("expected claude-opus-4-6, got %s", m)
	}

	// Unknown role falls back to gemma3:4b
	if m := reg.Resolve("unknown_role"); m != "gemma3:4b" {
		t.Errorf("expected gemma3:4b fallback, got %s", m)
	}
}

func TestIsClaudeModel(t *testing.T) {
	tests := []struct {
		model    string
		expected bool
	}{
		{"claude-sonnet-4-6", true},
		{"claude-opus-4-6", true},
		{"claude-haiku-4-5-20251001", true},
		{"gemma3:4b", false},
		{"qwen3:4b", false},
	}
	for _, tt := range tests {
		if got := isClaudeModel(tt.model); got != tt.expected {
			t.Errorf("isClaudeModel(%s) = %v, want %v", tt.model, got, tt.expected)
		}
	}
}

func TestTracker_Events(t *testing.T) {
	ws := newMockWS()
	tracker := NewTracker(ws.send, 1, 2)

	plan := &ExecutionPlan{
		OriginalQuery: "test",
		SubTasks:      []SubTask{{ID: "t1", Role: RoleScout}},
	}
	tracker.EmitStart(plan)
	tracker.EmitTaskStart("t1", "gemma3:4b", RoleScout)
	tracker.EmitTaskDelta("t1", "progress...")
	tracker.EmitTaskDone(&SubTask{ID: "t1", Model: "gemma3:4b", Role: RoleScout, TokensUsed: 100, ElapsedMs: 200})
	tracker.EmitComplete(&ExecutionResult{ElapsedMs: 500})

	types := ws.types()
	expected := []string{
		"orchestration:start",
		"orchestration:task_start",
		"orchestration:task_delta",
		"orchestration:task_done",
		"orchestration:complete",
	}
	if len(types) != len(expected) {
		t.Fatalf("expected %d events, got %d: %v", len(expected), len(types), types)
	}
	for i, e := range expected {
		if types[i] != e {
			t.Errorf("event[%d] = %s, want %s", i, types[i], e)
		}
	}
}

func TestTracker_NilSendWS(t *testing.T) {
	tracker := NewTracker(nil, 1, 2)
	// Should not panic
	tracker.EmitStart(&ExecutionPlan{})
	tracker.EmitTaskStart("t1", "m", RoleScout)
}

func TestExecutor_ParallelIndependent(t *testing.T) {
	caller := newMockCaller()
	ws := newMockWS()
	tracker := NewTracker(ws.send, 1, 1)
	reg := NewModelRegistry(map[Role]string{
		RoleScout:   "claude-haiku-4-5-20251001",
		RoleThinker: "claude-sonnet-4-6",
	})

	exec := NewExecutor(reg, tracker, caller)

	plan := &ExecutionPlan{
		OriginalQuery: "test",
		SubTasks: []SubTask{
			{ID: "a", Role: RoleScout, Prompt: "task A", DependsOn: []string{}},
			{ID: "b", Role: RoleThinker, Prompt: "task B", DependsOn: []string{}},
		},
	}

	err := exec.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if caller.CallCount() != 2 {
		t.Errorf("expected 2 calls, got %d", caller.CallCount())
	}

	for _, task := range plan.SubTasks {
		if task.Status != TaskDone {
			t.Errorf("task %s status = %s, want done", task.ID, task.Status)
		}
	}
}

func TestExecutor_SequentialDependency(t *testing.T) {
	caller := newMockCaller()
	caller.SetResponse("claude-haiku-4-5-20251001", "scout result")
	caller.SetResponse("claude-sonnet-4-6", "thinker result")

	ws := newMockWS()
	tracker := NewTracker(ws.send, 1, 1)
	reg := NewModelRegistry(map[Role]string{
		RoleScout:   "claude-haiku-4-5-20251001",
		RoleThinker: "claude-sonnet-4-6",
	})

	exec := NewExecutor(reg, tracker, caller)

	plan := &ExecutionPlan{
		SubTasks: []SubTask{
			{ID: "search", Role: RoleScout, Prompt: "find files", DependsOn: []string{}},
			{ID: "analyze", Role: RoleThinker, Prompt: "analyze results", DependsOn: []string{"search"}},
		},
	}

	err := exec.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify analyze received search result in prompt
	calls := caller.GetCalls()
	var analyzePrompt string
	for _, c := range calls {
		if c.Model == "claude-sonnet-4-6" {
			analyzePrompt = c.Prompt
		}
	}
	if !strings.Contains(analyzePrompt, "scout result") {
		t.Error("analyze prompt should contain search result as context")
	}
}

func TestExecutor_Deadlock(t *testing.T) {
	caller := newMockCaller()
	ws := newMockWS()
	tracker := NewTracker(ws.send, 1, 1)
	reg := NewModelRegistry(map[Role]string{RoleScout: "claude-haiku-4-5-20251001"})

	exec := NewExecutor(reg, tracker, caller)

	// Circular dependency: a -> b -> a
	plan := &ExecutionPlan{
		SubTasks: []SubTask{
			{ID: "a", Role: RoleScout, Prompt: "A", DependsOn: []string{"b"}},
			{ID: "b", Role: RoleScout, Prompt: "B", DependsOn: []string{"a"}},
		},
	}

	err := exec.Run(context.Background(), plan)
	if err == nil {
		t.Fatal("expected deadlock error, got nil")
	}
	if !strings.Contains(err.Error(), "deadlock") {
		t.Errorf("expected deadlock error, got: %v", err)
	}
}

func TestEvaluator_HighConfidence(t *testing.T) {
	eval := NewEvaluator(0.7, 3)
	task := &SubTask{Result: "just a normal response without confidence"}

	needsEsc := eval.Evaluate(task)
	if needsEsc {
		t.Error("should not need escalation for response without confidence (defaults to 1.0)")
	}
	if task.Confidence != 1.0 {
		t.Errorf("expected confidence 1.0, got %f", task.Confidence)
	}
}

func TestEvaluator_LowConfidence(t *testing.T) {
	eval := NewEvaluator(0.7, 3)
	task := &SubTask{
		Result: `Some analysis. {"confidence": 0.4, "needs_escalation": true, "escalation_reason": "too complex", "suggested_role": "builder"}`,
	}

	needsEsc := eval.Evaluate(task)
	if !needsEsc {
		t.Error("should need escalation for low confidence")
	}
	if task.Confidence != 0.4 {
		t.Errorf("expected confidence 0.4, got %f", task.Confidence)
	}
	if task.SuggestedRole != RoleBuilder {
		t.Errorf("expected suggested role builder, got %s", task.SuggestedRole)
	}
}

func TestEvaluator_MaxEscalations(t *testing.T) {
	eval := NewEvaluator(0.7, 2)
	plan := &ExecutionPlan{SubTasks: []SubTask{{ID: "t1", Role: RoleScout, Prompt: "test"}}}

	// Exhaust escalation budget
	for i := 0; i < 2; i++ {
		_, err := eval.Escalate(&plan.SubTasks[0], plan)
		if err != nil {
			t.Fatalf("escalation %d should succeed: %v", i, err)
		}
	}

	if eval.CanEscalate() {
		t.Error("should not be able to escalate after max")
	}

	_, err := eval.Escalate(&plan.SubTasks[0], plan)
	if err == nil {
		t.Error("should fail after max escalations")
	}
}

func TestEscalateRole(t *testing.T) {
	tests := []struct {
		from, to Role
	}{
		{RoleScout, RoleThinker},
		{RoleThinker, RoleBuilder},
		{RoleSearcher, RoleThinker},
		{RoleBuilder, RoleBuilder},
	}
	for _, tt := range tests {
		got := escalateRole(tt.from)
		if got != tt.to {
			t.Errorf("escalateRole(%s) = %s, want %s", tt.from, got, tt.to)
		}
	}
}

func TestParsePlan(t *testing.T) {
	response := `{"subtasks": [{"id": "search", "role": "scout", "prompt": "find files", "depends_on": []}, {"id": "analyze", "role": "thinker", "prompt": "analyze", "depends_on": ["search"]}]}`

	plan, err := parsePlan(response, "test query")
	if err != nil {
		t.Fatalf("parsePlan failed: %v", err)
	}
	if len(plan.SubTasks) != 2 {
		t.Fatalf("expected 2 subtasks, got %d", len(plan.SubTasks))
	}
	if plan.SubTasks[0].ID != "search" {
		t.Errorf("first task id = %s, want search", plan.SubTasks[0].ID)
	}
	if plan.SubTasks[1].DependsOn[0] != "search" {
		t.Errorf("second task depends_on = %v, want [search]", plan.SubTasks[1].DependsOn)
	}
}

func TestParsePlan_WithMarkdownFence(t *testing.T) {
	response := "```json\n{\"subtasks\": [{\"id\": \"t1\", \"role\": \"scout\", \"prompt\": \"do stuff\", \"depends_on\": []}]}\n```"
	plan, err := parsePlan(response, "q")
	if err != nil {
		t.Fatalf("parsePlan with fence failed: %v", err)
	}
	if len(plan.SubTasks) != 1 {
		t.Fatalf("expected 1 subtask, got %d", len(plan.SubTasks))
	}
}

func TestParsePlan_Empty(t *testing.T) {
	_, err := parsePlan(`{"subtasks": []}`, "q")
	if err == nil {
		t.Error("expected error for empty plan")
	}
}

func TestManager_Execute(t *testing.T) {
	caller := newMockCaller()

	// Orchestrator (Haiku) returns a plan
	planJSON := `{"subtasks": [{"id": "search", "role": "scout", "prompt": "find info", "depends_on": []}, {"id": "analyze", "role": "thinker", "prompt": "analyze", "depends_on": ["search"]}]}`
	caller.SetResponse("claude-haiku-4-5-20251001", planJSON)
	caller.SetResponse("claude-sonnet-4-6", "analysis complete")

	ws := newMockWS()
	cfg := DefaultConfig()
	cfg.Enabled = true

	mgr := NewManager(cfg, caller, ws.send)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := mgr.Execute(ctx, "test query", 1, 1)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Model != "orchestrated" {
		t.Errorf("model = %s, want orchestrated", result.Model)
	}
	if len(result.UsageReport) == 0 {
		t.Error("expected usage report entries")
	}

	// Verify WS events: start + task_start*2 + task_done*2 + complete + aggregate calls
	if ws.count() < 3 {
		t.Errorf("expected at least 3 WS events, got %d", ws.count())
	}

	types := ws.types()
	if types[0] != "orchestration:start" {
		t.Errorf("first event = %s, want orchestration:start", types[0])
	}
	if types[len(types)-1] != "orchestration:complete" {
		t.Errorf("last event = %s, want orchestration:complete", types[len(types)-1])
	}
}

func TestBuildPromptWithContext(t *testing.T) {
	plan := &ExecutionPlan{
		SubTasks: []SubTask{
			{ID: "s1", Role: RoleScout, Status: TaskDone, Result: "found 3 files"},
			{ID: "s2", Role: RoleThinker, Prompt: "analyze files", DependsOn: []string{"s1"}},
		},
	}

	prompt := buildPromptWithContext(&plan.SubTasks[1], plan)
	if !strings.Contains(prompt, "found 3 files") {
		t.Error("prompt should contain dependency result")
	}
	if !strings.Contains(prompt, "analyze files") {
		t.Error("prompt should contain original task prompt")
	}
}

func TestParseConfidence(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantConf float64
		wantEsc  bool
	}{
		{"no confidence", "just text", 1.0, false},
		{"with confidence", `result {"confidence": 0.5, "needs_escalation": true, "escalation_reason": "hard"}`, 0.5, true},
		{"high confidence", `result {"confidence": 0.95, "needs_escalation": false}`, 0.95, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := parseConfidence(tt.input)
			if r.Confidence != tt.wantConf {
				t.Errorf("confidence = %f, want %f", r.Confidence, tt.wantConf)
			}
			if r.NeedsEscalation != tt.wantEsc {
				t.Errorf("needs_escalation = %v, want %v", r.NeedsEscalation, tt.wantEsc)
			}
		})
	}
}
