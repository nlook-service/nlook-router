package scheduler

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
	"github.com/robfig/cron/v3"
)

// Scheduler polls the server for workflow schedules and triggers runs at the right time.
// It re-syncs schedules periodically to pick up changes (create/update/delete).
type Scheduler struct {
	client       apiclient.Interface
	syncInterval time.Duration
	cron         *cron.Cron
	mu           sync.Mutex
	entryMap     map[int64]cron.EntryID // scheduleID → cron entry
	schedules    map[int64]*apiclient.Schedule
	stopCh       chan struct{}

	// OnRunCreated is called when a scheduled run is dispatched (for logging/WebSocket notification)
	OnRunCreated func(workflowID, runID, scheduleID int64)
}

// New creates a new Scheduler.
func New(client apiclient.Interface, syncInterval time.Duration) *Scheduler {
	if syncInterval <= 0 {
		syncInterval = 30 * time.Second
	}
	return &Scheduler{
		client:       client,
		syncInterval: syncInterval,
		cron:         cron.New(cron.WithSeconds()),
		entryMap:     make(map[int64]cron.EntryID),
		schedules:    make(map[int64]*apiclient.Schedule),
		stopCh:       make(chan struct{}),
	}
}

// Start begins the scheduler: initial sync + periodic re-sync.
func (s *Scheduler) Start(ctx context.Context) {
	// Use standard cron (5-field, no seconds) for user-facing schedules
	s.cron = cron.New()
	s.cron.Start()

	// Initial sync
	s.sync(ctx)

	// Periodic re-sync
	go s.syncLoop(ctx)

	log.Printf("scheduler: started (sync every %s)", s.syncInterval)
}

// Stop stops the scheduler and all cron jobs.
func (s *Scheduler) Stop() {
	close(s.stopCh)
	s.cron.Stop()
	log.Printf("scheduler: stopped")
}

func (s *Scheduler) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(s.syncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sync(ctx)
		}
	}
}

func (s *Scheduler) sync(ctx context.Context) {
	// Fetch all workflows
	workflows, err := s.client.ListWorkflows(ctx)
	if err != nil {
		log.Printf("scheduler: list workflows error: %v", err)
		return
	}

	// Collect all enabled schedules
	allSchedules := make(map[int64]*apiclient.Schedule)
	for _, wf := range workflows {
		schedules, err := s.client.GetSchedules(ctx, wf.ID)
		if err != nil {
			log.Printf("scheduler: get schedules for workflow %d: %v", wf.ID, err)
			continue
		}
		for i := range schedules {
			sched := &schedules[i]
			if sched.Enabled {
				allSchedules[sched.ID] = sched
			}
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove deleted or disabled schedules
	for id, entryID := range s.entryMap {
		if _, exists := allSchedules[id]; !exists {
			s.cron.Remove(entryID)
			delete(s.entryMap, id)
			delete(s.schedules, id)
			log.Printf("scheduler: removed schedule %d", id)
		}
	}

	// Add or update schedules
	for id, sched := range allSchedules {
		existing, exists := s.schedules[id]

		// Skip if unchanged
		if exists && existing.CronExpression == sched.CronExpression && existing.Enabled == sched.Enabled {
			// Update input if changed
			s.schedules[id] = sched
			continue
		}

		// Remove old entry if updating
		if exists {
			if entryID, ok := s.entryMap[id]; ok {
				s.cron.Remove(entryID)
				delete(s.entryMap, id)
			}
		}

		// Add new cron entry
		schedCopy := *sched
		entryID, err := s.cron.AddFunc(sched.CronExpression, func() {
			s.triggerRun(ctx, &schedCopy)
		})
		if err != nil {
			log.Printf("scheduler: invalid cron '%s' for schedule %d: %v", sched.CronExpression, id, err)
			continue
		}

		s.entryMap[id] = entryID
		s.schedules[id] = sched

		if !exists {
			log.Printf("scheduler: added schedule %d '%s' cron='%s'", id, sched.Name, sched.CronExpression)
		} else {
			log.Printf("scheduler: updated schedule %d '%s' cron='%s'", id, sched.Name, sched.CronExpression)
		}
	}
}

func (s *Scheduler) triggerRun(ctx context.Context, sched *apiclient.Schedule) {
	input := sched.Input
	if input == nil {
		input = make(map[string]interface{})
	}

	execType := sched.ExecutionType
	if execType == "" {
		execType = "workflow"
	}

	log.Printf("scheduler: triggering schedule %d '%s' type=%s (workflow %d, agent %d)",
		sched.ID, sched.Name, execType, sched.WorkflowID, sched.AgentID)

	// Create run on server with proper execution type
	run, err := s.client.CreateRunWithParams(ctx, apiclient.CreateRunParams{
		WorkflowID:  sched.WorkflowID,
		Input:       input,
		TriggerType: "schedule",
		RunType:     execType,
		AgentID:     sched.AgentID,
		ScheduleID:  sched.ID,
	})
	if err != nil {
		log.Printf("scheduler: create run for schedule %d failed: %v", sched.ID, err)
		return
	}

	log.Printf("scheduler: run %d created for schedule %d '%s' (type=%s)", run.ID, sched.ID, sched.Name, execType)

	// For API-type schedules, execute the HTTP call directly
	if execType == "api" && sched.EndpointURL != "" {
		go s.executeAPICall(ctx, sched, run)
	}

	if s.OnRunCreated != nil {
		s.OnRunCreated(sched.WorkflowID, run.ID, sched.ID)
	}
}

// executeAPICall performs the actual HTTP request for API-type schedules.
func (s *Scheduler) executeAPICall(ctx context.Context, sched *apiclient.Schedule, run *apiclient.RunInfo) {
	// Mark as running
	if err := s.client.UpdateRunStatus(ctx, sched.WorkflowID, run.ID, "running", nil, ""); err != nil {
		log.Printf("scheduler: api run %d update to running failed: %v", run.ID, err)
	}

	method := sched.HTTPMethod
	if method == "" {
		method = "POST"
	}

	// Build request body from input/payload
	var body io.Reader
	if sched.Input != nil && (method == "POST" || method == "PUT" || method == "PATCH") {
		jsonBytes, err := json.Marshal(sched.Input)
		if err != nil {
			errMsg := fmt.Sprintf("marshal payload: %v", err)
			log.Printf("scheduler: api run %d failed: %s", run.ID, errMsg)
			s.client.UpdateRunStatus(ctx, sched.WorkflowID, run.ID, "failed", nil, errMsg)
			return
		}
		body = bytes.NewReader(jsonBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, sched.EndpointURL, body)
	if err != nil {
		errMsg := fmt.Sprintf("create request: %v", err)
		log.Printf("scheduler: api run %d failed: %s", run.ID, errMsg)
		s.client.UpdateRunStatus(ctx, sched.WorkflowID, run.ID, "failed", nil, errMsg)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		errMsg := fmt.Sprintf("http request: %v", err)
		log.Printf("scheduler: api run %d failed: %s", run.ID, errMsg)
		s.client.UpdateRunStatus(ctx, sched.WorkflowID, run.ID, "failed", nil, errMsg)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB max

	output := map[string]interface{}{
		"status_code": resp.StatusCode,
		"headers":     flattenHeaders(resp.Header),
	}

	// Try to parse response as JSON
	var jsonResp interface{}
	if err := json.Unmarshal(respBody, &jsonResp); err == nil {
		output["body"] = jsonResp
	} else {
		output["body"] = string(respBody)
	}

	if resp.StatusCode >= 400 {
		errMsg := fmt.Sprintf("api returned %d", resp.StatusCode)
		log.Printf("scheduler: api run %d failed: %s", run.ID, errMsg)
		s.client.UpdateRunStatus(ctx, sched.WorkflowID, run.ID, "failed", output, errMsg)
		return
	}

	log.Printf("scheduler: api run %d completed (%s %s → %d)", run.ID, method, sched.EndpointURL, resp.StatusCode)
	s.client.UpdateRunStatus(ctx, sched.WorkflowID, run.ID, "completed", output, "")
}

func flattenHeaders(h http.Header) map[string]string {
	result := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			result[k] = v[0]
		}
	}
	return result
}

// ActiveCount returns the number of active scheduled jobs.
func (s *Scheduler) ActiveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entryMap)
}
