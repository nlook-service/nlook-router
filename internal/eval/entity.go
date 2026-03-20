package eval

import "time"

type EvalSet struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	TargetType  string    `json:"target_type"` // "chat" | "workflow" | "skill"
	Model       string    `json:"model,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type EvalCase struct {
	ID             string    `json:"id"`
	EvalSetID      string    `json:"eval_set_id"`
	Input          string    `json:"input"`
	ExpectedOutput string    `json:"expected_output"`
	Context        string    `json:"context,omitempty"`
	Metadata       string    `json:"metadata,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type EvalRun struct {
	ID             string    `json:"id"`
	EvalSetID      string    `json:"eval_set_id"`
	EvaluatorModel string    `json:"evaluator_model"`
	TargetModel    string    `json:"target_model"`
	Status         string    `json:"status"` // pending | running | completed | failed
	NumIterations  int       `json:"num_iterations"`
	TotalCases     int       `json:"total_cases"`
	CompletedCases int       `json:"completed_cases"`
	AvgScore       float64   `json:"avg_score"`
	StdDev         float64   `json:"std_dev"`
	StartedAt      time.Time `json:"started_at"`
	CompletedAt    time.Time `json:"completed_at"`
}

type EvalResult struct {
	ID             string    `json:"id"`
	EvalRunID      string    `json:"eval_run_id"`
	EvalCaseID     string    `json:"eval_case_id"`
	Iteration      int       `json:"iteration"`
	ActualOutput   string    `json:"actual_output"`
	AccuracyScore  int       `json:"accuracy_score"`
	AccuracyReason string    `json:"accuracy_reason"`
	LatencyMs      int64     `json:"latency_ms"`
	TokensIn       int       `json:"tokens_in"`
	TokensOut      int       `json:"tokens_out"`
	CreatedAt      time.Time `json:"created_at"`
}
