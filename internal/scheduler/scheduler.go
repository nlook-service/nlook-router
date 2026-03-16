package scheduler

import (
	"context"
	"log"
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

	if sched.WorkflowID > 0 {
		// Has workflow — create run via API, router will execute the DAG
		log.Printf("scheduler: triggering run for schedule %d '%s' (workflow %d)", sched.ID, sched.Name, sched.WorkflowID)

		run, err := s.client.CreateRun(ctx, sched.WorkflowID, input, "schedule", sched.ID)
		if err != nil {
			log.Printf("scheduler: create run for schedule %d failed: %v", sched.ID, err)
			return
		}

		log.Printf("scheduler: run %d created for schedule %d", run.ID, sched.ID)

		if s.OnRunCreated != nil {
			s.OnRunCreated(sched.WorkflowID, run.ID, sched.ID)
		}
	} else {
		// No workflow — execute with input only (agent or passthrough)
		log.Printf("scheduler: triggering schedule %d '%s' (no workflow, input-only)", sched.ID, sched.Name)

		// Log the execution as a standalone task
		log.Printf("scheduler: schedule %d executed with input: %v", sched.ID, input)
	}
}

// ActiveCount returns the number of active scheduled jobs.
func (s *Scheduler) ActiveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entryMap)
}
