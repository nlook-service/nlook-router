package executor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nlook-service/nlook-router/internal/apiclient"
)

// mockClient implements apiclient.Interface for testing.
type mockClient struct {
	mu             sync.Mutex
	workflows      []apiclient.Workflow
	pendingRuns    map[int64][]apiclient.RunInfo
	statusUpdates  []statusUpdate
	workflowDetail *apiclient.WorkflowDetail
}

type statusUpdate struct {
	RunID  int64
	Status string
}

func (m *mockClient) ListWorkflows(ctx context.Context) ([]apiclient.Workflow, error) {
	return m.workflows, nil
}

func (m *mockClient) GetWorkflow(ctx context.Context, id int64) (*apiclient.Workflow, error) {
	return &apiclient.Workflow{ID: id, Title: "test"}, nil
}

func (m *mockClient) RegisterRouter(ctx context.Context, payload *apiclient.RegisterPayload) error {
	return nil
}

func (m *mockClient) Heartbeat(ctx context.Context, payload *apiclient.RegisterPayload) error {
	return nil
}

func (m *mockClient) GetWorkflowDetail(ctx context.Context, id int64) (*apiclient.WorkflowDetail, error) {
	if m.workflowDetail != nil {
		return m.workflowDetail, nil
	}
	return &apiclient.WorkflowDetail{
		ID:    id,
		Title: "test",
		Nodes: []apiclient.WorkflowNode{
			{NodeID: "start-1", NodeType: "start"},
			{NodeID: "end-1", NodeType: "end"},
		},
		Edges: []apiclient.WorkflowEdge{
			{EdgeID: "e1", SourceNodeID: "start-1", TargetNodeID: "end-1"},
		},
	}, nil
}

func (m *mockClient) GetPendingRuns(ctx context.Context, workflowID int64) ([]apiclient.RunInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pendingRuns[workflowID], nil
}

func (m *mockClient) UpdateRunStatus(ctx context.Context, workflowID, runID int64, status string, output map[string]interface{}, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statusUpdates = append(m.statusUpdates, statusUpdate{RunID: runID, Status: status})
	return nil
}

func (m *mockClient) StartStep(ctx context.Context, workflowID, runID int64, nodeID, nodeType string) (*apiclient.StepLogRef, error) {
	return &apiclient.StepLogRef{ID: 1}, nil
}

func (m *mockClient) CompleteStep(ctx context.Context, workflowID, runID, logID int64, status string, output map[string]interface{}, errMsg string, logLines []string) error {
	return nil
}

func TestExecutionService_DispatchRun(t *testing.T) {
	mc := &mockClient{
		pendingRuns: make(map[int64][]apiclient.RunInfo),
	}

	// Minimal engine that can handle start→end
	svc := NewExecutionService(mc, nil, 5*time.Second)

	// We can't actually execute without a real engine, but we can verify dispatch tracking
	// For now just test that Stop doesn't panic after dispatch
	svc.Stop()
}

func TestExecutionService_SkipPollingWhenWSConnected(t *testing.T) {
	mc := &mockClient{
		workflows:   []apiclient.Workflow{{ID: 1, Title: "wf1"}},
		pendingRuns: map[int64][]apiclient.RunInfo{1: {{ID: 10, WorkflowID: 1, UserID: 1}}},
	}

	svc := NewExecutionService(mc, nil, 100*time.Millisecond)

	// Set WS as connected
	svc.SetWSConnected(func() bool { return true })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Manually call poll — it should skip because WS is connected
	svc.poll(ctx)

	mc.mu.Lock()
	updates := len(mc.statusUpdates)
	mc.mu.Unlock()

	if updates != 0 {
		t.Errorf("expected 0 status updates when WS connected, got %d", updates)
	}
}

func TestExecutionService_PollsWhenWSDisconnected(t *testing.T) {
	mc := &mockClient{
		workflows:   []apiclient.Workflow{{ID: 1, Title: "wf1"}},
		pendingRuns: map[int64][]apiclient.RunInfo{},
	}

	svc := NewExecutionService(mc, nil, 100*time.Millisecond)

	// Set WS as disconnected
	svc.SetWSConnected(func() bool { return false })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Manually call poll — it should run (but find no pending runs)
	svc.poll(ctx)

	// If it reached ListWorkflows without error, polling is working
	// No pending runs, so no status updates expected
}
