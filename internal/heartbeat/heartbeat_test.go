package heartbeat

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nlook-service/nlook-router/internal/apiclient"
)

type mockClient struct {
	registerErr error
	heartbeatOk bool
	heartbeatN  int32
	mu          sync.Mutex
}

func (m *mockClient) ListWorkflows(ctx context.Context) ([]apiclient.Workflow, error) {
	return nil, nil
}
func (m *mockClient) GetWorkflow(ctx context.Context, id int64) (*apiclient.Workflow, error) {
	return nil, nil
}
func (m *mockClient) RegisterRouter(ctx context.Context, payload *apiclient.RegisterPayload) error {
	return m.registerErr
}
func (m *mockClient) Heartbeat(ctx context.Context, payload *apiclient.RegisterPayload) error {
	atomic.AddInt32(&m.heartbeatN, 1)
	if !m.heartbeatOk {
		return errors.New("heartbeat failed")
	}
	return nil
}
func (m *mockClient) GetWorkflowDetail(ctx context.Context, id int64) (*apiclient.WorkflowDetail, error) {
	return nil, nil
}
func (m *mockClient) GetPendingRuns(ctx context.Context, workflowID int64) ([]apiclient.RunInfo, error) {
	return nil, nil
}
func (m *mockClient) UpdateRunStatus(ctx context.Context, workflowID, runID int64, status string, output map[string]interface{}, errMsg string) error {
	return nil
}
func (m *mockClient) StartStep(ctx context.Context, workflowID, runID int64, nodeID, nodeType string) (*apiclient.StepLogRef, error) {
	return nil, nil
}
func (m *mockClient) CompleteStep(ctx context.Context, workflowID, runID, logID int64, status string, output map[string]interface{}, errMsg string, logLines []string) error {
	return nil
}
func (m *mockClient) GetSchedules(ctx context.Context, workflowID int64) ([]apiclient.Schedule, error) {
	return nil, nil
}
func (m *mockClient) CreateRun(ctx context.Context, workflowID int64, input map[string]interface{}, triggerType string, scheduleID int64) (*apiclient.RunInfo, error) {
	return nil, nil
}
func (m *mockClient) CreateRunWithParams(ctx context.Context, params apiclient.CreateRunParams) (*apiclient.RunInfo, error) {
	return nil, nil
}
func (m *mockClient) GetAgentDetail(ctx context.Context, agentID int64) (*apiclient.WorkflowAgent, error) {
	return nil, nil
}
func (m *mockClient) ReportUsage(ctx context.Context, buckets interface{}) error {
	return nil
}

func TestRegistrar_Start_ReturnsErrorWhenRegisterFails(t *testing.T) {
	mock := &mockClient{registerErr: errors.New("register failed")}
	payload := &apiclient.RegisterPayload{RouterID: "r1"}
	reg := NewRegistrar(mock, time.Hour, payload)
	ctx := context.Background()
	err := reg.Start(ctx)
	if err == nil {
		t.Fatal("expected error from RegisterRouter")
	}
	if err.Error() != "register failed" {
		t.Errorf("want 'register failed' got %v", err)
	}
}

func TestRegistrar_Start_ThenStop(t *testing.T) {
	mock := &mockClient{heartbeatOk: true}
	payload := &apiclient.RegisterPayload{RouterID: "r1"}
	reg := NewRegistrar(mock, 50*time.Millisecond, payload)
	ctx := context.Background()
	if err := reg.Start(ctx); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	_ = reg.Stop()
	n := atomic.LoadInt32(&mock.heartbeatN)
	if n < 1 {
		t.Errorf("expected at least 1 heartbeat call got %d", n)
	}
}

func TestNewRegistrar_ZeroInterval_UsesDefault(t *testing.T) {
	reg := NewRegistrar(nil, 0, nil)
	if reg.Interval != 60*time.Second {
		t.Errorf("zero interval should default to 60s got %v", reg.Interval)
	}
}
