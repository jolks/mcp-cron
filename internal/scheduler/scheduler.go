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

// Scheduler manages cron tasks
type Scheduler struct {
	cron         *cron.Cron
	tasks        map[string]*model.Task
	entryIDs     map[string]cron.EntryID
	mu           sync.RWMutex
	taskExecutor model.Executor
	taskStore    model.TaskStore
	config       *config.SchedulerConfig
}

// NewScheduler creates a new scheduler instance
func NewScheduler(cfg *config.SchedulerConfig) *Scheduler {
	cronOpts := cron.New(
		cron.WithParser(cron.NewParser(
			cron.SecondOptional|cron.Minute|cron.Hour|cron.Dom|cron.Month|cron.Dow|cron.Descriptor)),
		cron.WithChain(
			cron.Recover(cron.DefaultLogger),
		),
	)

	scheduler := &Scheduler{
		cron:     cronOpts,
		tasks:    make(map[string]*model.Task),
		entryIDs: make(map[string]cron.EntryID),
		config:   cfg,
	}

	return scheduler
}

// Start begins the scheduler
func (s *Scheduler) Start(ctx context.Context) {
	s.cron.Start()

	// Listen for context cancellation to stop the scheduler
	go func() {
		<-ctx.Done()
		if err := s.Stop(); err != nil {
			// We cannot return the error here since we're in a goroutine,
			// so we'll just log it
			fmt.Printf("Error stopping scheduler: %v\n", err)
		}
	}()
}

// Stop halts the scheduler
func (s *Scheduler) Stop() error {
	s.cron.Stop()
	return nil
}

// AddTask adds a new task to the scheduler
func (s *Scheduler) AddTask(task *model.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[task.ID]; exists {
		return errors.AlreadyExists("task", task.ID)
	}

	// Persist to store first
	if s.taskStore != nil {
		if err := s.taskStore.SaveTask(task); err != nil {
			return fmt.Errorf("persist task: %w", err)
		}
	}

	// Store the task in memory
	s.tasks[task.ID] = task

	if task.Enabled {
		err := s.scheduleTask(task)
		if err != nil {
			// If scheduling fails, set the task status to failed
			task.Status = model.StatusFailed
			return err
		}
	}

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

	// Remove the task from cron if it's scheduled
	if entryID, exists := s.entryIDs[taskID]; exists {
		s.cron.Remove(entryID)
		delete(s.entryIDs, taskID)
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

	task.Enabled = true
	task.UpdatedAt = time.Now()

	// Persist to store
	if s.taskStore != nil {
		if err := s.taskStore.UpdateTask(task); err != nil {
			task.Enabled = false // rollback
			return fmt.Errorf("persist task enable: %w", err)
		}
	}

	err := s.scheduleTask(task)
	if err != nil {
		// If scheduling fails, set the task status to failed
		task.Status = model.StatusFailed
		return err
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

	// Remove from cron
	if entryID, exists := s.entryIDs[taskID]; exists {
		s.cron.Remove(entryID)
		delete(s.entryIDs, taskID)
	}

	task.Enabled = false
	task.Status = model.StatusDisabled
	task.UpdatedAt = time.Now()

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

	existingTask, exists := s.tasks[task.ID]
	if !exists {
		return errors.NotFound("task", task.ID)
	}

	// If the task was scheduled, remove it
	if existingTask.Enabled {
		if entryID, exists := s.entryIDs[task.ID]; exists {
			s.cron.Remove(entryID)
			delete(s.entryIDs, task.ID)
		}
	}

	// Update the task
	task.UpdatedAt = time.Now()
	s.tasks[task.ID] = task

	// Persist to store
	if s.taskStore != nil {
		if err := s.taskStore.UpdateTask(task); err != nil {
			return fmt.Errorf("persist task update: %w", err)
		}
	}

	// If enabled, schedule it
	if task.Enabled {
		return s.scheduleTask(task)
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
// Must be called after SetTaskExecutor so that enabled tasks can be scheduled.
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
		if task.Enabled {
			if err := s.scheduleTask(task); err != nil {
				task.Status = model.StatusFailed
				fmt.Printf("Failed to schedule persisted task %s: %v\n", task.ID, err)
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

// scheduleTask adds a task to the cron scheduler (internal method)
func (s *Scheduler) scheduleTask(task *model.Task) error {
	// Ensure we have a task executor
	if s.taskExecutor == nil {
		return fmt.Errorf("cannot schedule task: no task executor set")
	}

	// Create the job function that will execute when scheduled
	jobFunc := func() {
		// Check if the task still exists (may have been removed between dispatch and execution)
		s.mu.RLock()
		if _, exists := s.tasks[task.ID]; !exists {
			s.mu.RUnlock()
			return
		}
		s.mu.RUnlock()

		task.LastRun = time.Now()
		task.Status = model.StatusRunning

		// Execute the task
		ctx := context.Background()
		timeout := s.config.DefaultTimeout // Use the configured default timeout

		if err := s.taskExecutor.Execute(ctx, task, timeout); err != nil {
			task.Status = model.StatusFailed
		} else {
			task.Status = model.StatusCompleted
		}

		task.UpdatedAt = time.Now()
		s.updateNextRunTime(task)
	}

	// Add the job to cron
	entryID, err := s.cron.AddFunc(task.Schedule, jobFunc)
	if err != nil {
		return fmt.Errorf("failed to schedule task: %w", err)
	}

	// Store the cron entry ID
	s.entryIDs[task.ID] = entryID
	s.updateNextRunTime(task)

	return nil
}

// updateNextRunTime updates the task's next run time based on its cron entry
func (s *Scheduler) updateNextRunTime(task *model.Task) {
	if entryID, exists := s.entryIDs[task.ID]; exists {
		entries := s.cron.Entries()
		for _, entry := range entries {
			if entry.ID == entryID {
				task.NextRun = entry.Next
				break
			}
		}
	}
}
