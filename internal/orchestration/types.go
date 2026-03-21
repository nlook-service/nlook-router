package orchestration

import (
	"context"
	"time"
)

// Role defines a model's specialization.
type Role string

const (
	RoleOrchestrator Role = "orchestrator"
	RoleScout        Role = "scout"
	RoleThinker      Role = "thinker"
	RoleBuilder      Role = "builder"
	RoleSearcher     Role = "searcher"
)

// TaskStatus tracks subtask lifecycle.
type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskDone      TaskStatus = "done"
	TaskFailed    TaskStatus = "failed"
	TaskEscalated TaskStatus = "escalated"
)

// SubTask represents a unit of work assigned to a specific model.
type SubTask struct {
	ID        string     `json:"id"`
	Role      Role       `json:"role"`
	Model     string     `json:"model"`
	Prompt    string     `json:"prompt"`
	DependsOn []string   `json:"depends_on"`
	Status    TaskStatus `json:"status"`
	Result    string     `json:"result,omitempty"`
	Error     string     `json:"error,omitempty"`

	// Metrics
	TokensUsed int   `json:"tokens_used,omitempty"`
	ElapsedMs  int64 `json:"elapsed_ms,omitempty"`

	// Self-evaluation
	Confidence       float64 `json:"confidence,omitempty"`
	NeedsEscalation  bool    `json:"needs_escalation,omitempty"`
	EscalationReason string  `json:"escalation_reason,omitempty"`
	SuggestedRole    Role    `json:"suggested_role,omitempty"`
}

// ExecutionPlan is the orchestrator's decomposition output.
type ExecutionPlan struct {
	OriginalQuery string    `json:"original_query"`
	SubTasks      []SubTask `json:"subtasks"`
	CreatedAt     time.Time `json:"created_at"`
}

// ExecutionResult is the final aggregated output.
type ExecutionResult struct {
	Content     string         `json:"content"`
	Model       string         `json:"model"`
	ElapsedMs   int64          `json:"elapsed_ms"`
	UsageReport []SubTaskUsage `json:"usage_report"`
	Escalations []Escalation   `json:"escalations,omitempty"`
}

// SubTaskUsage summarizes one subtask's resource consumption.
type SubTaskUsage struct {
	TaskID    string `json:"task_id"`
	Role      Role   `json:"role"`
	Model     string `json:"model"`
	Tokens    int    `json:"tokens"`
	ElapsedMs int64  `json:"elapsed_ms"`
}

// Escalation records a model-to-model handoff.
type Escalation struct {
	FromModel string `json:"from_model"`
	FromRole  Role   `json:"from_role"`
	ToModel   string `json:"to_model"`
	ToRole    Role   `json:"to_role"`
	Reason    string `json:"reason"`
}

// LLMCaller abstracts model invocation (CLI, API, Ollama).
type LLMCaller interface {
	Call(ctx context.Context, model, system, prompt string) (response string, tokens int, err error)
}

// Config holds orchestration settings from config.yaml.
type Config struct {
	Enabled             bool            `yaml:"enabled" json:"enabled"`
	OrchestratorModel   string          `yaml:"orchestrator_model" json:"orchestrator_model"`
	Roles               map[Role]string `yaml:"roles" json:"roles"`
	EscalationThreshold float64         `yaml:"escalation_threshold" json:"escalation_threshold"`
	MaxSubTasks         int             `yaml:"max_subtasks" json:"max_subtasks"`
	MaxEscalations      int             `yaml:"max_escalations" json:"max_escalations"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Enabled:           false,
		OrchestratorModel: "claude-haiku-4-5-20251001",
		Roles: map[Role]string{
			RoleScout:    "gemma3:4b",
			RoleSearcher: "claude-haiku-4-5-20251001",
			RoleThinker:  "claude-sonnet-4-6",
			RoleBuilder:  "claude-opus-4-6",
		},
		EscalationThreshold: 0.7,
		MaxSubTasks:         10,
		MaxEscalations:      3,
	}
}
