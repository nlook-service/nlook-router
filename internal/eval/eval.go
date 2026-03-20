package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/nlook-service/nlook-router/internal/llm"
)

// DB is the subset of db.DB that EvalRunner needs.
// Defined here to avoid an import cycle (db imports eval for entity types).
type DB interface {
	GetEvalSet(ctx context.Context, id string) (*EvalSet, error)
	ListEvalCases(ctx context.Context, evalSetID string) ([]*EvalCase, error)
	InsertEvalRun(ctx context.Context, run *EvalRun) error
	UpdateEvalRun(ctx context.Context, run *EvalRun) error
	InsertEvalResult(ctx context.Context, result *EvalResult) error
}

// RunOptions configures a single eval run.
type RunOptions struct {
	EvaluatorModel string
	TargetModel    string
	NumIterations  int
}

// WorkflowRunOptions configures a workflow-level eval run with per-step evaluation.
type WorkflowRunOptions struct {
	EvaluatorModel string
}

// EvalRunner orchestrates evaluation: generates answers, scores them, persists results.
type EvalRunner struct {
	db        DB
	engine    *llm.Engine
	evaluator *AccuracyEvaluator
}

// NewEvalRunner creates a runner with the given storage, LLM engine, and evaluator model.
func NewEvalRunner(storage DB, engine *llm.Engine, evaluatorModel string) *EvalRunner {
	return &EvalRunner{
		db:        storage,
		engine:    engine,
		evaluator: NewAccuracyEvaluator(engine, evaluatorModel),
	}
}

// Run executes an eval set: for each case, generates an answer and scores it.
func (r *EvalRunner) Run(ctx context.Context, evalSetID string, opts RunOptions) (*EvalRun, error) {
	set, err := r.db.GetEvalSet(ctx, evalSetID)
	if err != nil {
		return nil, fmt.Errorf("get eval set: %w", err)
	}
	if set == nil {
		return nil, fmt.Errorf("eval set %q not found", evalSetID)
	}

	cases, err := r.db.ListEvalCases(ctx, evalSetID)
	if err != nil {
		return nil, fmt.Errorf("list eval cases: %w", err)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("eval set %q has no cases", evalSetID)
	}

	if opts.NumIterations < 1 {
		opts.NumIterations = 1
	}
	targetModel := opts.TargetModel
	if targetModel == "" {
		targetModel = set.Model
	}
	evaluatorModel := opts.EvaluatorModel
	if evaluatorModel == "" {
		evaluatorModel = r.evaluator.model
	}

	run := &EvalRun{
		ID:             uuid.New().String(),
		EvalSetID:      evalSetID,
		EvaluatorModel: evaluatorModel,
		TargetModel:    targetModel,
		Status:         "running",
		NumIterations:  opts.NumIterations,
		TotalCases:     len(cases) * opts.NumIterations,
		StartedAt:      time.Now(),
	}
	if err := r.db.InsertEvalRun(ctx, run); err != nil {
		return nil, fmt.Errorf("insert eval run: %w", err)
	}

	var scores []float64

	for _, c := range cases {
		for iter := 1; iter <= opts.NumIterations; iter++ {
			log.Printf("eval: case %s iter %d/%d", c.ID[:8], iter, opts.NumIterations)

			// Generate answer from target model
			output, metrics, genErr := Measure(func() (string, int, int, error) {
				resp, err := r.engine.ChatCompletion(ctx, targetModel, "", []map[string]string{
					{"role": "user", "content": c.Input},
				}, nil, false)
				if err != nil {
					return "", 0, 0, err
				}
				defer resp.Body.Close()

				body, err := io.ReadAll(resp.Body)
				if err != nil {
					return "", 0, 0, fmt.Errorf("read response: %w", err)
				}
				if resp.StatusCode != 200 {
					return "", 0, 0, fmt.Errorf("API error: status=%d body=%s", resp.StatusCode, truncate(string(body), 300))
				}

				var cr chatResponse
				if err := json.Unmarshal(body, &cr); err != nil {
					return "", 0, 0, fmt.Errorf("parse response: %w", err)
				}
				if len(cr.Choices) == 0 {
					return "", 0, 0, fmt.Errorf("no choices in response")
				}

				return cr.Choices[0].Message.Content, cr.Usage.PromptTokens, cr.Usage.CompletionTokens, nil
			})
			if genErr != nil {
				log.Printf("eval: generate answer failed for case %s: %v", c.ID[:8], genErr)
				continue
			}

			// Score with evaluator
			scoreResult, scoreErr := r.evaluator.Score(ctx, c.Input, c.ExpectedOutput, output)
			if scoreErr != nil {
				log.Printf("eval: scoring failed for case %s: %v", c.ID[:8], scoreErr)
				continue
			}

			result := &EvalResult{
				ID:             uuid.New().String(),
				EvalRunID:      run.ID,
				EvalCaseID:     c.ID,
				Iteration:      iter,
				ActualOutput:   output,
				AccuracyScore:  scoreResult.Score,
				AccuracyReason: scoreResult.Reason,
				LatencyMs:      metrics.LatencyMs,
				TokensIn:       metrics.TokensIn,
				TokensOut:      metrics.TokensOut,
				CreatedAt:      time.Now(),
			}
			if err := r.db.InsertEvalResult(ctx, result); err != nil {
				log.Printf("eval: save result failed: %v", err)
				continue
			}

			scores = append(scores, float64(scoreResult.Score))
			run.CompletedCases++
		}
	}

	// Calculate statistics
	if len(scores) > 0 {
		var sum float64
		for _, s := range scores {
			sum += s
		}
		run.AvgScore = sum / float64(len(scores))

		var variance float64
		for _, s := range scores {
			diff := s - run.AvgScore
			variance += diff * diff
		}
		run.StdDev = math.Sqrt(variance / float64(len(scores)))
	}

	run.Status = "completed"
	run.CompletedAt = time.Now()
	if err := r.db.UpdateEvalRun(ctx, run); err != nil {
		return nil, fmt.Errorf("update eval run: %w", err)
	}

	return run, nil
}

// PrepareWorkflowEval creates an EvalRun and returns a StepEvalHook to attach to the engine's StepExecutor.
// The hook evaluates each workflow step's output against cases that have a matching NodeID.
// After the workflow completes, call FinalizeWorkflowEval to persist results and statistics.
func (r *EvalRunner) PrepareWorkflowEval(ctx context.Context, evalSetID string, opts WorkflowRunOptions) (*EvalRun, *StepEvalHook, error) {
	set, err := r.db.GetEvalSet(ctx, evalSetID)
	if err != nil {
		return nil, nil, fmt.Errorf("get eval set: %w", err)
	}
	if set == nil {
		return nil, nil, fmt.Errorf("eval set %q not found", evalSetID)
	}

	cases, err := r.db.ListEvalCases(ctx, evalSetID)
	if err != nil {
		return nil, nil, fmt.Errorf("list eval cases: %w", err)
	}

	// Build NodeID → EvalCase map (only cases with a NodeID set)
	expectations := make(map[string]*EvalCase)
	for _, c := range cases {
		if c.NodeID != "" {
			expectations[c.NodeID] = c
		}
	}
	if len(expectations) == 0 {
		return nil, nil, fmt.Errorf("eval set %q has no step-level cases (cases need node_id)", evalSetID)
	}

	evaluatorModel := opts.EvaluatorModel
	if evaluatorModel == "" {
		evaluatorModel = r.evaluator.model
	}

	run := &EvalRun{
		ID:             uuid.New().String(),
		EvalSetID:      evalSetID,
		EvaluatorModel: evaluatorModel,
		TargetModel:    set.Model,
		Status:         "running",
		NumIterations:  1,
		TotalCases:     len(expectations),
		StartedAt:      time.Now(),
	}
	if err := r.db.InsertEvalRun(ctx, run); err != nil {
		return nil, nil, fmt.Errorf("insert eval run: %w", err)
	}

	hook := NewStepEvalHook(expectations, r.evaluator, run.ID)
	return run, hook, nil
}

// FinalizeWorkflowEval persists step-level results from the hook and updates run statistics.
func (r *EvalRunner) FinalizeWorkflowEval(ctx context.Context, run *EvalRun, hook *StepEvalHook) (*EvalRun, error) {
	results := hook.Results()
	var scores []float64

	for _, result := range results {
		if err := r.db.InsertEvalResult(ctx, result); err != nil {
			log.Printf("eval: save step result failed: %v", err)
			continue
		}
		scores = append(scores, float64(result.AccuracyScore))
		run.CompletedCases++
	}

	if len(scores) > 0 {
		var sum float64
		for _, s := range scores {
			sum += s
		}
		run.AvgScore = sum / float64(len(scores))

		var variance float64
		for _, s := range scores {
			diff := s - run.AvgScore
			variance += diff * diff
		}
		run.StdDev = math.Sqrt(variance / float64(len(scores)))
	}

	run.Status = "completed"
	run.CompletedAt = time.Now()
	if err := r.db.UpdateEvalRun(ctx, run); err != nil {
		return nil, fmt.Errorf("update eval run: %w", err)
	}

	return run, nil
}
