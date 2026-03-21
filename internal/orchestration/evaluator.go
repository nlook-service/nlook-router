package orchestration

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// Evaluator checks subtask results and triggers escalation when needed.
type Evaluator struct {
	threshold      float64
	maxEscalations int
	escalationCount int
}

// NewEvaluator creates an evaluator with config thresholds.
func NewEvaluator(threshold float64, maxEscalations int) *Evaluator {
	return &Evaluator{
		threshold:      threshold,
		maxEscalations: maxEscalations,
	}
}

// Evaluate checks a subtask result and parses confidence.
// Returns true if escalation is needed.
func (e *Evaluator) Evaluate(task *SubTask) bool {
	conf := parseConfidence(task.Result)
	task.Confidence = conf.Confidence
	task.NeedsEscalation = conf.NeedsEscalation
	task.EscalationReason = conf.EscalationReason
	task.SuggestedRole = Role(conf.SuggestedRole)

	if conf.NeedsEscalation || conf.Confidence < e.threshold {
		return true
	}
	return false
}

// CanEscalate returns true if escalation budget remains.
func (e *Evaluator) CanEscalate() bool {
	return e.escalationCount < e.maxEscalations
}

// Escalate creates a new subtask with an upgraded role.
func (e *Evaluator) Escalate(task *SubTask, plan *ExecutionPlan) (*SubTask, error) {
	if !e.CanEscalate() {
		return nil, fmt.Errorf("max escalations (%d) reached", e.maxEscalations)
	}

	e.escalationCount++

	newRole := task.SuggestedRole
	if newRole == "" {
		newRole = escalateRole(task.Role)
	}

	newTask := SubTask{
		ID:        fmt.Sprintf("%s_escalated_%d", task.ID, e.escalationCount),
		Role:      newRole,
		Prompt:    fmt.Sprintf("Previous attempt by %s was insufficient (confidence: %.2f, reason: %s).\n\nOriginal task: %s\n\nPrevious result:\n%s", task.Role, task.Confidence, task.EscalationReason, task.Prompt, task.Result),
		DependsOn: task.DependsOn,
		Status:    TaskPending,
	}

	plan.SubTasks = append(plan.SubTasks, newTask)
	task.Status = TaskEscalated

	log.Printf("orchestration/evaluator: escalated %s (%s -> %s), reason: %s",
		task.ID, task.Role, newRole, task.EscalationReason)

	return &plan.SubTasks[len(plan.SubTasks)-1], nil
}

// escalateRole returns a stronger role for escalation.
func escalateRole(current Role) Role {
	switch current {
	case RoleScout:
		return RoleThinker
	case RoleThinker:
		return RoleBuilder
	case RoleSearcher:
		return RoleThinker
	default:
		return RoleBuilder
	}
}

type confidenceResult struct {
	Confidence       float64 `json:"confidence"`
	NeedsEscalation  bool    `json:"needs_escalation"`
	EscalationReason string  `json:"escalation_reason"`
	SuggestedRole    string  `json:"suggested_role"`
}

// parseConfidence extracts confidence info from model response.
// If response contains no confidence JSON, returns 1.0.
func parseConfidence(response string) confidenceResult {
	result := confidenceResult{Confidence: 1.0}

	// Try to find JSON with confidence field
	idx := strings.Index(response, `"confidence"`)
	if idx < 0 {
		return result
	}

	// Find surrounding braces
	start := strings.LastIndex(response[:idx], "{")
	end := strings.Index(response[idx:], "}")
	if start < 0 || end < 0 {
		return result
	}

	jsonStr := response[start : idx+end+1]
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return confidenceResult{Confidence: 1.0}
	}

	return result
}
