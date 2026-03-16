package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
	runType := run.RunType
	if runType == "" {
		runType = "workflow"
	}

	switch runType {
	case "workflow":
		s.executeWorkflowRun(ctx, run)
	case "agent":
		s.executeAgentRun(ctx, run)
	case "api":
		s.executeAPIRun(ctx, run)
	default:
		log.Printf("executor: unknown run type '%s' for run %d, skipping", runType, run.ID)
	}
}

func (s *ExecutionService) executeWorkflowRun(ctx context.Context, run apiclient.RunInfo) {
	if run.WorkflowID == 0 {
		log.Printf("executor: skipping workflow run %d (no workflow_id)", run.ID)
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

func (s *ExecutionService) executeAgentRun(ctx context.Context, run apiclient.RunInfo) {
	if run.AgentID == 0 {
		log.Printf("executor: skipping agent run %d (no agent_id)", run.ID)
		return
	}

	// Mark run as running
	if err := s.client.UpdateRunStatus(ctx, run.WorkflowID, run.ID, "running", nil, ""); err != nil {
		log.Printf("executor: update agent run %d to running: %v", run.ID, err)
	}

	// Fetch agent detail from server
	agent, err := s.client.GetAgentDetail(ctx, run.AgentID)
	if err != nil {
		errMsg := fmt.Sprintf("get agent detail: %v", err)
		log.Printf("executor: agent run %d failed: %s", run.ID, errMsg)
		s.client.UpdateRunStatus(ctx, run.WorkflowID, run.ID, "failed", nil, errMsg)
		return
	}

	log.Printf("executor: agent run %d — agent=%s model=%s", run.ID, agent.Name, agent.Model)

	// Build prompt from run input
	prompt := ""
	if run.Input != nil {
		if text, ok := run.Input["text"].(string); ok {
			prompt = text
		} else if msg, ok := run.Input["message"].(string); ok {
			prompt = msg
		} else {
			// Fallback: serialize input as JSON
			b, _ := json.Marshal(run.Input)
			prompt = string(b)
		}
	}

	if prompt == "" {
		s.client.UpdateRunStatus(ctx, run.WorkflowID, run.ID, "failed", nil, "no input text provided")
		return
	}

	// Create a synthetic skill and run through SkillRunner
	skill := &apiclient.WorkflowSkill{
		Name:      agent.Name,
		SkillType: "prompt",
		Content:   prompt,
	}

	output, logs, err := s.engine.SkillRunner().RunSkill(ctx, skill, agent, run.Input)
	if err != nil {
		errMsg := fmt.Sprintf("agent execution: %v", err)
		log.Printf("executor: agent run %d failed: %s", run.ID, errMsg)
		s.client.UpdateRunStatus(ctx, run.WorkflowID, run.ID, "failed", nil, errMsg)
		return
	}

	// Add logs to output
	if output == nil {
		output = make(map[string]interface{})
	}
	if len(logs) > 0 {
		output["_logs"] = logs
	}

	log.Printf("executor: agent run %d completed (agent=%s)", run.ID, agent.Name)
	s.client.UpdateRunStatus(ctx, run.WorkflowID, run.ID, "completed", output, "")
}

func (s *ExecutionService) executeAPIRun(ctx context.Context, run apiclient.RunInfo) {
	if run.EndpointURL == "" {
		log.Printf("executor: skipping api run %d (no endpoint_url)", run.ID)
		return
	}

	// Mark as running
	if err := s.client.UpdateRunStatus(ctx, run.WorkflowID, run.ID, "running", nil, ""); err != nil {
		log.Printf("executor: update api run %d to running: %v", run.ID, err)
	}

	method := run.HTTPMethod
	if method == "" {
		method = "POST"
	}

	// Build request body from input
	var body io.Reader
	if run.Input != nil && (method == "POST" || method == "PUT" || method == "PATCH") {
		jsonBytes, err := json.Marshal(run.Input)
		if err != nil {
			errMsg := fmt.Sprintf("marshal payload: %v", err)
			log.Printf("executor: api run %d failed: %s", run.ID, errMsg)
			s.client.UpdateRunStatus(ctx, run.WorkflowID, run.ID, "failed", nil, errMsg)
			return
		}
		body = bytes.NewReader(jsonBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, run.EndpointURL, body)
	if err != nil {
		errMsg := fmt.Sprintf("create request: %v", err)
		log.Printf("executor: api run %d failed: %s", run.ID, errMsg)
		s.client.UpdateRunStatus(ctx, run.WorkflowID, run.ID, "failed", nil, errMsg)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		errMsg := fmt.Sprintf("http request: %v", err)
		log.Printf("executor: api run %d failed: %s", run.ID, errMsg)
		s.client.UpdateRunStatus(ctx, run.WorkflowID, run.ID, "failed", nil, errMsg)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	output := map[string]interface{}{
		"status_code": resp.StatusCode,
	}
	var jsonResp interface{}
	if err := json.Unmarshal(respBody, &jsonResp); err == nil {
		output["body"] = jsonResp
	} else {
		output["body"] = string(respBody)
	}

	if resp.StatusCode >= 400 {
		errMsg := fmt.Sprintf("api returned %d", resp.StatusCode)
		log.Printf("executor: api run %d failed: %s", run.ID, errMsg)
		s.client.UpdateRunStatus(ctx, run.WorkflowID, run.ID, "failed", output, errMsg)
		return
	}

	log.Printf("executor: api run %d completed (%s %s → %d)", run.ID, method, run.EndpointURL, resp.StatusCode)
	s.client.UpdateRunStatus(ctx, run.WorkflowID, run.ID, "completed", output, "")
}
