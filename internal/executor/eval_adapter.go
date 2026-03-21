package executor

import (
	"context"
	"fmt"
	"time"

	"github.com/nlook-service/nlook-router/internal/engine"
	"github.com/nlook-service/nlook-router/internal/eval"
)

// evalStepAdapter adapts engine.StepHook to eval.StepEvalHook.
// It translates engine.StepEvent into eval.StepCompleteData.
type evalStepAdapter struct {
	hook *eval.StepEvalHook
}

func newEvalStepAdapter(hook *eval.StepEvalHook) *evalStepAdapter {
	return &evalStepAdapter{hook: hook}
}

// OnStepComplete implements engine.StepHook.
func (a *evalStepAdapter) OnStepComplete(ctx context.Context, event *engine.StepEvent) {
	status := event.Result.Status
	data := &eval.StepCompleteData{
		NodeID:   event.NodeID,
		NodeType: event.NodeType,
		Order:    event.StepOrder,
		Input:    event.Input,
		Output:   event.Result.Output,
		Status:   status,
		Duration: event.Duration,
	}
	a.hook.HandleStepComplete(ctx, data)
}

// extractEvalSetID extracts _eval_set_id from run input.
// Returns empty string if not present.
func extractEvalSetID(input map[string]interface{}) string {
	if input == nil {
		return ""
	}
	v, ok := input["_eval_set_id"]
	if !ok {
		return ""
	}
	switch id := v.(type) {
	case string:
		return id
	case float64:
		if id > 0 {
			return fmt.Sprintf("%.0f", id)
		}
	}
	return ""
}

// durationFromMs converts int64 milliseconds to time.Duration.
func durationFromMs(ms int64) time.Duration {
	return time.Duration(ms) * time.Millisecond
}
