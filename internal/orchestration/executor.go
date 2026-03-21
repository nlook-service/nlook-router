package orchestration

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Executor runs subtasks respecting DAG dependencies.
type Executor struct {
	registry *ModelRegistry
	tracker  *Tracker
	caller   LLMCaller
}

// NewExecutor creates a DAG executor.
func NewExecutor(registry *ModelRegistry, tracker *Tracker, caller LLMCaller) *Executor {
	return &Executor{registry: registry, tracker: tracker, caller: caller}
}

// Run executes all subtasks in the plan respecting depends_on ordering.
// Independent subtasks run in parallel.
func (e *Executor) Run(ctx context.Context, plan *ExecutionPlan) error {
	taskMap := make(map[string]*SubTask, len(plan.SubTasks))
	for i := range plan.SubTasks {
		plan.SubTasks[i].Status = TaskPending
		taskMap[plan.SubTasks[i].ID] = &plan.SubTasks[i]
	}

	completed := make(map[string]bool)

	for len(completed) < len(plan.SubTasks) {
		ready := findReady(plan.SubTasks, completed)
		if len(ready) == 0 {
			return fmt.Errorf("orchestration: deadlock, %d/%d tasks completed", len(completed), len(plan.SubTasks))
		}

		var wg sync.WaitGroup
		errCh := make(chan error, len(ready))

		for _, task := range ready {
			task := task
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := e.executeSubTask(ctx, task, plan); err != nil {
					errCh <- err
				}
			}()
		}

		wg.Wait()
		close(errCh)

		// Collect errors but continue — partial results are OK
		for err := range errCh {
			log.Printf("orchestration/executor: subtask error: %v", err)
		}

		for _, task := range ready {
			completed[task.ID] = true
		}
	}

	return nil
}

// findReady returns subtasks whose dependencies are all completed.
func findReady(tasks []SubTask, completed map[string]bool) []*SubTask {
	var ready []*SubTask
	for i := range tasks {
		t := &tasks[i]
		if t.Status == TaskDone || t.Status == TaskFailed {
			continue
		}
		if completed[t.ID] {
			continue
		}
		allDepsReady := true
		for _, dep := range t.DependsOn {
			if !completed[dep] {
				allDepsReady = false
				break
			}
		}
		if allDepsReady {
			ready = append(ready, t)
		}
	}
	return ready
}

// executeSubTask runs a single subtask with the assigned model.
func (e *Executor) executeSubTask(ctx context.Context, task *SubTask, plan *ExecutionPlan) error {
	model := e.registry.Resolve(task.Role)
	task.Model = model
	task.Status = TaskRunning

	e.tracker.EmitTaskStart(task.ID, model, task.Role)

	prompt := buildPromptWithContext(task, plan)

	start := time.Now()
	response, tokens, err := e.caller.Call(ctx, model, "", prompt)
	elapsed := time.Since(start).Milliseconds()

	task.ElapsedMs = elapsed
	task.TokensUsed = tokens

	if err != nil {
		task.Status = TaskFailed
		task.Error = err.Error()
		log.Printf("orchestration/executor: subtask %s failed: %v", task.ID, err)
		return fmt.Errorf("subtask %s (%s): %w", task.ID, task.Role, err)
	}

	task.Result = response
	task.Status = TaskDone
	e.tracker.EmitTaskDone(task)

	return nil
}

// buildPromptWithContext injects dependency results into the subtask prompt.
func buildPromptWithContext(task *SubTask, plan *ExecutionPlan) string {
	if len(task.DependsOn) == 0 {
		return task.Prompt
	}

	var sb strings.Builder
	sb.WriteString("## Context from previous steps\n\n")

	for _, depID := range task.DependsOn {
		for _, t := range plan.SubTasks {
			if t.ID == depID && t.Status == TaskDone {
				sb.WriteString(fmt.Sprintf("### [%s] %s result:\n%s\n\n", t.Role, t.ID, t.Result))
			}
		}
	}

	sb.WriteString("## Your task\n\n")
	sb.WriteString(task.Prompt)

	return sb.String()
}
