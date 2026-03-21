package orchestration

import (
	"encoding/json"
	"log"
)

// Tracker emits WebSocket events for orchestration progress.
type Tracker struct {
	sendWS func([]byte)
	convID int64
	msgID  int64
}

// NewTracker creates a tracker bound to a conversation.
func NewTracker(sendWS func([]byte), convID, msgID int64) *Tracker {
	return &Tracker{sendWS: sendWS, convID: convID, msgID: msgID}
}

type wsMessage struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// EmitStart sends orchestration:start with the execution plan.
func (t *Tracker) EmitStart(plan *ExecutionPlan) {
	t.emit("orchestration:start", map[string]interface{}{
		"conversation_id": t.convID,
		"message_id":      t.msgID,
		"plan":            plan,
	})
}

// EmitTaskStart signals a subtask has begun execution.
func (t *Tracker) EmitTaskStart(taskID, model string, role Role) {
	t.emit("orchestration:task_start", map[string]interface{}{
		"conversation_id": t.convID,
		"message_id":      t.msgID,
		"task_id":         taskID,
		"model":           model,
		"role":            role,
	})
}

// EmitTaskDelta sends streaming progress for a subtask.
func (t *Tracker) EmitTaskDelta(taskID, delta string) {
	t.emit("orchestration:task_delta", map[string]interface{}{
		"conversation_id": t.convID,
		"message_id":      t.msgID,
		"task_id":         taskID,
		"delta":           delta,
	})
}

// EmitTaskDone signals a subtask has completed.
func (t *Tracker) EmitTaskDone(task *SubTask) {
	t.emit("orchestration:task_done", map[string]interface{}{
		"conversation_id": t.convID,
		"message_id":      t.msgID,
		"task_id":         task.ID,
		"model":           task.Model,
		"role":            task.Role,
		"tokens":          task.TokensUsed,
		"elapsed_ms":      task.ElapsedMs,
		"confidence":      task.Confidence,
	})
}

// EmitEscalate signals a model-to-model handoff.
func (t *Tracker) EmitEscalate(esc Escalation) {
	t.emit("orchestration:escalate", map[string]interface{}{
		"conversation_id": t.convID,
		"message_id":      t.msgID,
		"from_model":      esc.FromModel,
		"from_role":       esc.FromRole,
		"to_model":        esc.ToModel,
		"to_role":         esc.ToRole,
		"reason":          esc.Reason,
	})
}

// EmitComplete sends the final result with usage report.
func (t *Tracker) EmitComplete(result *ExecutionResult) {
	t.emit("orchestration:complete", map[string]interface{}{
		"conversation_id": t.convID,
		"message_id":      t.msgID,
		"usage":           result.UsageReport,
		"total_elapsed_ms": result.ElapsedMs,
		"escalations":     result.Escalations,
	})
}

func (t *Tracker) emit(msgType string, payload interface{}) {
	if t.sendWS == nil {
		return
	}
	msg := wsMessage{Type: msgType, Payload: payload}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("orchestration/tracker: marshal %s: %v", msgType, err)
		return
	}
	t.sendWS(data)
}
