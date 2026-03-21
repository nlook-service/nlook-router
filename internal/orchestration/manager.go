package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
)

// Manager is the entry point for multi-model orchestration.
type Manager struct {
	config    Config
	registry  *ModelRegistry
	caller    LLMCaller
	sendWS    func([]byte)
}

// NewManager creates an orchestration manager.
func NewManager(cfg Config, caller LLMCaller, sendWS func([]byte)) *Manager {
	return &Manager{
		config:   cfg,
		registry: NewModelRegistry(cfg.Roles),
		caller:   caller,
		sendWS:   sendWS,
	}
}

// IsEnabled returns whether orchestration is enabled.
func (m *Manager) IsEnabled() bool {
	return m.config.Enabled
}

// Execute decomposes a query, executes subtasks, evaluates, aggregates, and returns.
func (m *Manager) Execute(ctx context.Context, query string, convID, msgID int64) (*ExecutionResult, error) {
	start := time.Now()
	tracker := NewTracker(m.sendWS, convID, msgID)
	evaluator := NewEvaluator(m.config.EscalationThreshold, m.config.MaxEscalations)

	// 1. Decompose
	plan, err := m.decompose(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("orchestration decompose: %w", err)
	}

	log.Printf("orchestration: decomposed into %d subtasks", len(plan.SubTasks))
	tracker.EmitStart(plan)

	// 2. Execute
	executor := NewExecutor(m.registry, tracker, m.caller)
	if err := executor.Run(ctx, plan); err != nil {
		return nil, fmt.Errorf("orchestration execute: %w", err)
	}

	// 3. Self-evaluate and escalate if needed
	for i := range plan.SubTasks {
		task := &plan.SubTasks[i]
		if task.Status != TaskDone {
			continue
		}
		if evaluator.Evaluate(task) && evaluator.CanEscalate() {
			newTask, err := evaluator.Escalate(task, plan)
			if err != nil {
				log.Printf("orchestration: escalation failed: %v", err)
				continue
			}
			tracker.EmitEscalate(Escalation{
				FromModel: task.Model,
				FromRole:  task.Role,
				ToModel:   m.registry.Resolve(newTask.Role),
				ToRole:    newTask.Role,
				Reason:    task.EscalationReason,
			})
			// Run escalated task
			if err := executor.Run(ctx, plan); err != nil {
				log.Printf("orchestration: escalated execution failed: %v", err)
			}
		}
	}

	// 4. Aggregate
	content, err := m.aggregate(ctx, query, plan)
	if err != nil {
		// Fallback: use best subtask result directly
		content = bestResult(plan)
	}

	// 5. Build result
	result := &ExecutionResult{
		Content:   content,
		Model:     "orchestrated",
		ElapsedMs: time.Since(start).Milliseconds(),
	}

	for _, t := range plan.SubTasks {
		if t.Status == TaskDone || t.Status == TaskEscalated {
			result.UsageReport = append(result.UsageReport, SubTaskUsage{
				TaskID:    t.ID,
				Role:      t.Role,
				Model:     t.Model,
				Tokens:    t.TokensUsed,
				ElapsedMs: t.ElapsedMs,
			})
		}
	}

	tracker.EmitComplete(result)

	return result, nil
}

// decompose asks the orchestrator model to break a query into subtasks.
func (m *Manager) decompose(ctx context.Context, query string) (*ExecutionPlan, error) {
	maxTasks := m.config.MaxSubTasks
	if maxTasks == 0 {
		maxTasks = 10
	}

	prompt := fmt.Sprintf(`You are a task decomposition engine. Given a user query, break it into subtasks.

Available roles:
- scout: Fast local model for file search, listing, simple Q&A
- thinker: Balanced reasoning for analysis, comparison, strategy
- builder: Best quality code generation, refactoring, architecture
- searcher: Web search and real-time information retrieval

Rules:
1. Use minimum subtasks needed (prefer 1-3, max %d)
2. If a single role can handle the query, use only that role
3. Set depends_on only when a subtask needs another's result
4. Independent subtasks should have empty depends_on (parallel execution)

Reply with ONLY a JSON object:
{"subtasks": [{"id": "string", "role": "scout|thinker|builder|searcher", "prompt": "specific instruction", "depends_on": []}]}

User query: "%s"`, maxTasks, query)

	response, _, err := m.caller.Call(ctx, m.config.OrchestratorModel, "", prompt)
	if err != nil {
		return nil, fmt.Errorf("decompose call: %w", err)
	}

	plan, err := parsePlan(response, query)
	if err != nil {
		return nil, fmt.Errorf("parse plan: %w", err)
	}

	return plan, nil
}

// aggregate asks the orchestrator to combine subtask results into a final response.
func (m *Manager) aggregate(ctx context.Context, query string, plan *ExecutionPlan) (string, error) {
	var sb strings.Builder
	sb.WriteString("Original user query: " + query + "\n\n")
	sb.WriteString("Subtask results:\n\n")

	for _, t := range plan.SubTasks {
		if t.Status == TaskDone {
			sb.WriteString(fmt.Sprintf("### [%s] %s:\n%s\n\n", t.Role, t.ID, t.Result))
		}
	}

	sb.WriteString("Based on the subtask results above, provide a comprehensive final response to the user's original query. Be concise and direct.")

	response, _, err := m.caller.Call(ctx, m.config.OrchestratorModel, "", sb.String())
	if err != nil {
		return "", fmt.Errorf("aggregate call: %w", err)
	}

	return response, nil
}

// parsePlan extracts an ExecutionPlan from the orchestrator's JSON response.
func parsePlan(response, query string) (*ExecutionPlan, error) {
	// Find JSON in response
	jsonStr := response
	if idx := strings.Index(response, "{"); idx >= 0 {
		if end := strings.LastIndex(response, "}"); end > idx {
			jsonStr = response[idx : end+1]
		}
	}

	var parsed struct {
		SubTasks []SubTask `json:"subtasks"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, fmt.Errorf("unmarshal plan: %w (raw: %s)", err, truncateStr(response, 200))
	}

	if len(parsed.SubTasks) == 0 {
		return nil, fmt.Errorf("empty plan from orchestrator")
	}

	return &ExecutionPlan{
		OriginalQuery: query,
		SubTasks:      parsed.SubTasks,
		CreatedAt:     time.Now(),
	}, nil
}

// bestResult returns the result from the subtask with highest confidence.
func bestResult(plan *ExecutionPlan) string {
	var best string
	var bestConf float64 = -1
	for _, t := range plan.SubTasks {
		if t.Status == TaskDone && t.Confidence > bestConf {
			bestConf = t.Confidence
			best = t.Result
		}
	}
	if best == "" {
		return "I wasn't able to complete this request. Please try again."
	}
	return best
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
