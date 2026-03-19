package engine

import (
	"context"
	"fmt"
	"sync"

	"github.com/nlook-service/nlook-router/internal/apiclient"
)

// mockClient implements apiclient.Interface for testing.
// All methods record calls and return configurable responses.
type mockClient struct {
	mu sync.Mutex

	// Call counters
	startStepCalls    []startStepCall
	completeStepCalls []completeStepCall
	updateRunCalls    []updateRunCall

	// Configurable responses
	startStepFn    func(workflowID, runID int64, nodeID, nodeType string) (*apiclient.StepLogRef, error)
	completeStepFn func(workflowID, runID, logID int64, status string, output map[string]interface{}, errMsg string, logLines []string) error
	nextLogID      int64
}

type startStepCall struct {
	WorkflowID int64
	RunID      int64
	NodeID     string
	NodeType   string
}

type completeStepCall struct {
	WorkflowID int64
	RunID      int64
	LogID      int64
	Status     string
	Output     map[string]interface{}
	ErrMsg     string
	LogLines   []string
}

type updateRunCall struct {
	WorkflowID int64
	RunID      int64
	Status     string
	Output     map[string]interface{}
	ErrMsg     string
}

func newMockClient() *mockClient {
	return &mockClient{nextLogID: 1}
}

func (m *mockClient) ListWorkflows(_ context.Context) ([]apiclient.Workflow, error) {
	return nil, nil
}

func (m *mockClient) GetWorkflow(_ context.Context, _ int64) (*apiclient.Workflow, error) {
	return nil, nil
}

func (m *mockClient) RegisterRouter(_ context.Context, _ *apiclient.RegisterPayload) error {
	return nil
}

func (m *mockClient) Heartbeat(_ context.Context, _ *apiclient.RegisterPayload) error {
	return nil
}

func (m *mockClient) GetSchedules(_ context.Context, _ int64) ([]apiclient.Schedule, error) {
	return nil, nil
}

func (m *mockClient) CreateRun(_ context.Context, _ int64, _ map[string]interface{}, _ string, _ int64) (*apiclient.RunInfo, error) {
	return nil, nil
}

func (m *mockClient) CreateRunWithParams(_ context.Context, _ apiclient.CreateRunParams) (*apiclient.RunInfo, error) {
	return nil, nil
}

func (m *mockClient) GetWorkflowDetail(_ context.Context, id int64) (*apiclient.WorkflowDetail, error) {
	return nil, fmt.Errorf("not implemented in mock")
}

func (m *mockClient) GetPendingRuns(_ context.Context, _ int64) ([]apiclient.RunInfo, error) {
	return nil, nil
}

func (m *mockClient) UpdateRunStatus(_ context.Context, workflowID, runID int64, status string, output map[string]interface{}, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateRunCalls = append(m.updateRunCalls, updateRunCall{
		WorkflowID: workflowID,
		RunID:      runID,
		Status:     status,
		Output:     output,
		ErrMsg:     errMsg,
	})
	return nil
}

func (m *mockClient) StartStep(_ context.Context, workflowID, runID int64, nodeID, nodeType string) (*apiclient.StepLogRef, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.startStepCalls = append(m.startStepCalls, startStepCall{
		WorkflowID: workflowID,
		RunID:      runID,
		NodeID:     nodeID,
		NodeType:   nodeType,
	})

	if m.startStepFn != nil {
		return m.startStepFn(workflowID, runID, nodeID, nodeType)
	}

	ref := &apiclient.StepLogRef{ID: m.nextLogID}
	m.nextLogID++
	return ref, nil
}

func (m *mockClient) CompleteStep(_ context.Context, workflowID, runID, logID int64, status string, output map[string]interface{}, errMsg string, logLines []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.completeStepCalls = append(m.completeStepCalls, completeStepCall{
		WorkflowID: workflowID,
		RunID:      runID,
		LogID:      logID,
		Status:     status,
		Output:     output,
		ErrMsg:     errMsg,
		LogLines:   logLines,
	})

	if m.completeStepFn != nil {
		return m.completeStepFn(workflowID, runID, logID, status, output, errMsg, logLines)
	}

	return nil
}

// helpers for assertions

func (m *mockClient) startStepCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.startStepCalls)
}

func (m *mockClient) completeStepCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.completeStepCalls)
}

func (m *mockClient) ReportUsage(_ context.Context, _ interface{}) error { return nil }
func (m *mockClient) GetAgentDetail(_ context.Context, _ int64) (*apiclient.WorkflowAgent, error) {
	return nil, fmt.Errorf("not implemented")
}
