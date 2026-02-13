// SPDX-License-Identifier: AGPL-3.0-only
package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/errors"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/robfig/cron/v3"
)

// Scheduler manages cron tasks using a poll-based database scheduler.
// Instead of an in-memory cron engine, it stores next_run in the database
// and polls for due tasks on each tick. Optimistic locking on next_run
// prevents duplicate execution across multiple instances.
type Scheduler struct {
	parser       cron.Parser
	tasks        map[string]*model.Task
	mu           sync.RWMutex
	taskExecutor model.Executor
	taskStore    model.TaskStore
	config       *config.SchedulerConfig
	stopPoll     chan struct{}
	// nowFunc allows tests to override time.Now
	nowFunc func() time.Time
}

// NewScheduler creates a new scheduler instance
func NewScheduler(cfg *config.SchedulerConfig) *Scheduler {
	return &Scheduler{
		parser: cron.NewParser(
			cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor),
		tasks:    make(map[string]*model.Task),
		config:   cfg,
		stopPoll: make(chan struct{}),
		nowFunc:  time.Now,
	}
}

// now returns the current time, using nowFunc for testability.
func (s *Scheduler) now() time.Time {
	return s.nowFunc()
}

// Start begins the scheduler's poll loop
func (s *Scheduler) Start(ctx context.Context) {
	go s.pollLoop(ctx)
}

// Stop halts the scheduler
func (s *Scheduler) Stop() error {
	select {
	case <-s.stopPoll:
		// Already stopped
	default:
		close(s.stopPoll)
	}
	return nil
}

// AddTask adds a new task to the scheduler
func (s *Scheduler) AddTask(task *model.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[task.ID]; exists {
		return errors.AlreadyExists("task", task.ID)
	}

	// Compute next_run for enabled tasks
	if task.Enabled {
		nextRun, err := s.computeNextRun(task.Schedule)
		if err != nil {
			task.Status = model.StatusFailed
			return err
		}
		task.NextRun = nextRun
	}

	// Persist to store first
	if s.taskStore != nil {
		if err := s.taskStore.SaveTask(task); err != nil {
			return fmt.Errorf("persist task: %w", err)
		}
	}

	// Store the task in memory
	s.tasks[task.ID] = task

	return nil
}

// RemoveTask removes a task from the scheduler
func (s *Scheduler) RemoveTask(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.tasks[taskID]
	if !exists {
		return errors.NotFound("task", taskID)
	}

	// Remove from store
	if s.taskStore != nil {
		if err := s.taskStore.DeleteTask(taskID); err != nil {
			return fmt.Errorf("delete task from store: %w", err)
		}
	}

	// Remove the task from our map
	delete(s.tasks, taskID)

	return nil
}

// EnableTask enables a disabled task
func (s *Scheduler) EnableTask(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return errors.NotFound("task", taskID)
	}

	if task.Enabled {
		return nil // Already enabled
	}

	// Compute next_run
	nextRun, err := s.computeNextRun(task.Schedule)
	if err != nil {
		task.Status = model.StatusFailed
		return err
	}

	task.Enabled = true
	task.NextRun = nextRun
	task.Status = model.StatusPending
	task.UpdatedAt = s.now()

	// Persist to store
	if s.taskStore != nil {
		if err := s.taskStore.UpdateTask(task); err != nil {
			task.Enabled = false // rollback
			return fmt.Errorf("persist task enable: %w", err)
		}
	}

	return nil
}

// DisableTask disables a running task
func (s *Scheduler) DisableTask(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return errors.NotFound("task", taskID)
	}

	if !task.Enabled {
		return nil // Already disabled
	}

	task.Enabled = false
	task.Status = model.StatusDisabled
	task.NextRun = time.Time{} // Clear next_run
	task.UpdatedAt = s.now()

	// Persist to store
	if s.taskStore != nil {
		if err := s.taskStore.UpdateTask(task); err != nil {
			return fmt.Errorf("persist task disable: %w", err)
		}
	}

	return nil
}

// GetTask retrieves a task by ID
func (s *Scheduler) GetTask(taskID string) (*model.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, exists := s.tasks[taskID]
	if !exists {
		return nil, errors.NotFound("task", taskID)
	}

	return task, nil
}

// ListTasks returns all tasks
func (s *Scheduler) ListTasks() []*model.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]*model.Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		tasks = append(tasks, task)
	}

	return tasks
}

// UpdateTask updates an existing task
func (s *Scheduler) UpdateTask(task *model.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.tasks[task.ID]
	if !exists {
		return errors.NotFound("task", task.ID)
	}

	// Recompute next_run if enabled
	if task.Enabled {
		nextRun, err := s.computeNextRun(task.Schedule)
		if err != nil {
			return err
		}
		task.NextRun = nextRun
	} else {
		task.NextRun = time.Time{} // Clear next_run for disabled tasks
	}

	// Update the task
	task.UpdatedAt = s.now()
	s.tasks[task.ID] = task

	// Persist to store
	if s.taskStore != nil {
		if err := s.taskStore.UpdateTask(task); err != nil {
			return fmt.Errorf("persist task update: %w", err)
		}
	}

	return nil
}

// SetTaskExecutor sets the executor to be used for task execution
func (s *Scheduler) SetTaskExecutor(executor model.Executor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskExecutor = executor
}

// SetTaskStore sets the store to be used for task persistence
func (s *Scheduler) SetTaskStore(store model.TaskStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskStore = store
}

// LoadTasks restores persisted tasks from the store into the scheduler.
func (s *Scheduler) LoadTasks() error {
	if s.taskStore == nil {
		return nil
	}

	tasks, err := s.taskStore.LoadTasks()
	if err != nil {
		return fmt.Errorf("load tasks from store: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, task := range tasks {
		if _, exists := s.tasks[task.ID]; exists {
			continue // skip duplicates
		}
		s.tasks[task.ID] = task
		// Compute next_run for enabled tasks that don't have one
		if task.Enabled && task.NextRun.IsZero() {
			nextRun, err := s.computeNextRun(task.Schedule)
			if err != nil {
				task.Status = model.StatusFailed
				fmt.Printf("Failed to compute next_run for persisted task %s: %v\n", task.ID, err)
				continue
			}
			task.NextRun = nextRun
			if s.taskStore != nil {
				if err := s.taskStore.UpdateTask(task); err != nil {
					fmt.Printf("Failed to persist next_run for task %s: %v\n", task.ID, err)
				}
			}
		}
	}

	return nil
}

// NewTask creates a new task with default values
func NewTask() *model.Task {
	now := time.Now()
	return &model.Task{
		Enabled:   false,
		Status:    model.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// computeNextRun parses the cron schedule and returns the next run time.
func (s *Scheduler) computeNextRun(schedule string) (time.Time, error) {
	sched, err := s.parser.Parse(schedule)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse schedule: %w", err)
	}
	return sched.Next(s.now()), nil
}

// pollLoop is the main scheduler loop that checks for due tasks.
func (s *Scheduler) pollLoop(ctx context.Context) {
	interval := s.config.PollInterval
	if interval <= 0 {
		interval = 1 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopPoll:
			return
		case <-ticker.C:
			s.pollTick()
		}
	}
}

// pollTick executes a single poll cycle: refresh from DB, find due tasks, claim & execute.
func (s *Scheduler) pollTick() {
	if s.taskStore == nil {
		return
	}

	// Refresh in-memory tasks from DB
	tasks, err := s.taskStore.LoadTasks()
	if err != nil {
		fmt.Printf("Poll: failed to load tasks: %v\n", err)
		return
	}

	s.mu.Lock()
	// Merge fresh DB state into the in-memory map, preserving runtime-only fields
	newTasks := make(map[string]*model.Task, len(tasks))
	for _, t := range tasks {
		if existing, ok := s.tasks[t.ID]; ok {
			// Preserve runtime-only state (not stored in DB)
			t.Status = existing.Status
			t.LastRun = existing.LastRun
		}
		newTasks[t.ID] = t
	}
	s.tasks = newTasks
	s.mu.Unlock()

	// Query for due tasks
	now := s.now()
	dueTasks, err := s.taskStore.GetDueTasks(now)
	if err != nil {
		fmt.Printf("Poll: failed to get due tasks: %v\n", err)
		return
	}

	for _, task := range dueTasks {
		// Compute next_run from schedule
		newNextRun, err := s.computeNextRun(task.Schedule)
		if err != nil {
			fmt.Printf("Poll: failed to compute next_run for task %s: %v\n", task.ID, err)
			continue
		}

		// Optimistic lock: try to claim this execution
		claimed, err := s.taskStore.AdvanceNextRun(task.ID, task.NextRun, newNextRun)
		if err != nil {
			fmt.Printf("Poll: failed to advance next_run for task %s: %v\n", task.ID, err)
			continue
		}

		if !claimed {
			continue // Another instance got it
		}

		// Update in-memory state
		s.mu.Lock()
		if memTask, exists := s.tasks[task.ID]; exists {
			memTask.NextRun = newNextRun
		}
		s.mu.Unlock()

		// Execute in a goroutine
		go s.executeTask(task)
	}
}

// executeTask runs a single task execution.
func (s *Scheduler) executeTask(task *model.Task) {
	s.mu.RLock()
	executor := s.taskExecutor
	s.mu.RUnlock()

	if executor == nil {
		return
	}

	// Update status to running
	s.mu.Lock()
	if memTask, exists := s.tasks[task.ID]; exists {
		memTask.LastRun = s.now()
		memTask.Status = model.StatusRunning
	}
	s.mu.Unlock()

	// Execute the task
	ctx := context.Background()
	timeout := s.config.DefaultTimeout

	execErr := executor.Execute(ctx, task, timeout)

	// Update status after execution
	s.mu.Lock()
	if memTask, exists := s.tasks[task.ID]; exists {
		if execErr != nil {
			memTask.Status = model.StatusFailed
		} else {
			memTask.Status = model.StatusCompleted
		}
		memTask.UpdatedAt = s.now()
	}
	s.mu.Unlock()
}
