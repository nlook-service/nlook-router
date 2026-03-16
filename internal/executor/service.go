package executor

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/nlook-service/nlook-router/internal/apiclient"
	"github.com/nlook-service/nlook-router/internal/engine"
)

// ExecutionService polls for pending runs and executes them locally.
// When WebSocket is connected, it receives runs via dispatch instead of polling.
type ExecutionService struct {
	client       apiclient.Interface
	engine       *engine.WorkflowEngine
	pollInterval time.Duration
	stopCh       chan struct{}
	mu           sync.RWMutex
	running      map[int64]context.CancelFunc
	wsConnected  func() bool // returns true when WebSocket is connected
}

// NewExecutionService creates a new ExecutionService.
func NewExecutionService(client apiclient.Interface, eng *engine.WorkflowEngine, pollInterval time.Duration) *ExecutionService {
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}
	return &ExecutionService{
		client:       client,
		engine:       eng,
		pollInterval: pollInterval,
		stopCh:       make(chan struct{}),
		running:      make(map[int64]context.CancelFunc),
	}
}

// Start begins the poll loop in a background goroutine.
func (s *ExecutionService) Start(ctx context.Context) {
	go s.pollLoop(ctx)
}

// SetWSConnected sets a function that reports whether WebSocket is connected.
// When connected, polling is skipped (runs arrive via WebSocket dispatch).
func (s *ExecutionService) SetWSConnected(fn func() bool) {
	s.wsConnected = fn
}

// Stop signals the poll loop to exit.
func (s *ExecutionService) Stop() {
	close(s.stopCh)
}

// CancelRun cancels a running workflow execution by its run ID.
// If the run is not currently executing on this router, this is a no-op.
func (s *ExecutionService) CancelRun(runID int64) {
	s.mu.RLock()
	cancel, ok := s.running[runID]
	s.mu.RUnlock()
	if !ok {
		return
	}
	log.Printf("executor: cancelling run %d via WebSocket request", runID)
	cancel()
}

// DispatchRun is called by the WebSocket client when a run:dispatch message arrives.
// It immediately starts execution without polling.
func (s *ExecutionService) DispatchRun(ctx context.Context, run apiclient.RunInfo) {
	s.mu.RLock()
	_, already := s.running[run.ID]
	s.mu.RUnlock()

	if already {
		return
	}

	s.spawnRun(ctx, run)
}

func (s *ExecutionService) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.poll(ctx)
		}
	}
}

func (s *ExecutionService) poll(ctx context.Context) {
	// Skip polling when WebSocket is connected (runs arrive via dispatch)
	if s.wsConnected != nil && s.wsConnected() {
		return
	}

	workflows, err := s.client.ListWorkflows(ctx)
	if err != nil {
		log.Printf("executor: list workflows error: %v", err)
		return
	}

	for _, wf := range workflows {
		runs, err := s.client.GetPendingRuns(ctx, wf.ID)
		if err != nil {
			log.Printf("executor: get pending runs for workflow %d: %v", wf.ID, err)
			continue
		}

		for _, run := range runs {
			s.mu.RLock()
			_, already := s.running[run.ID]
			s.mu.RUnlock()

			if already {
				continue
			}

			s.spawnRun(ctx, run)
		}
	}
}

func (s *ExecutionService) spawnRun(ctx context.Context, run apiclient.RunInfo) {
	runCtx, cancel := context.WithCancel(ctx)

	s.mu.Lock()
	s.running[run.ID] = cancel
	s.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			s.mu.Lock()
			delete(s.running, run.ID)
			s.mu.Unlock()
		}()

		s.executeRun(runCtx, run)
	}()
}

func (s *ExecutionService) executeRun(ctx context.Context, run apiclient.RunInfo) {
	// Skip execution for standalone runs (no workflow attached)
	if run.WorkflowID == 0 {
		log.Printf("executor: skipping run %d (standalone, no workflow)", run.ID)
		return
	}

	// Mark run as running
	if err := s.client.UpdateRunStatus(ctx, run.WorkflowID, run.ID, "running", nil, ""); err != nil {
		log.Printf("executor: update run %d to running: %v", run.ID, err)
	}

	// Fetch full workflow detail
	detail, err := s.client.GetWorkflowDetail(ctx, run.WorkflowID)
	if err != nil {
		errMsg := err.Error()
		log.Printf("executor: get workflow detail for run %d: %v", run.ID, err)
		if updateErr := s.client.UpdateRunStatus(ctx, run.WorkflowID, run.ID, "failed", nil, errMsg); updateErr != nil {
			log.Printf("executor: update run %d to failed: %v", run.ID, updateErr)
		}
		return
	}

	// Execute the workflow
	output, err := s.engine.Execute(ctx, detail, run)
	if err != nil {
		errMsg := err.Error()
		log.Printf("executor: run %d failed: %v", run.ID, err)
		if updateErr := s.client.UpdateRunStatus(ctx, run.WorkflowID, run.ID, "failed", nil, errMsg); updateErr != nil {
			log.Printf("executor: update run %d to failed: %v", run.ID, updateErr)
		}
		return
	}

	// Mark run as completed
	if err := s.client.UpdateRunStatus(ctx, run.WorkflowID, run.ID, "completed", output, ""); err != nil {
		log.Printf("executor: update run %d to completed: %v", run.ID, err)
	}
}
