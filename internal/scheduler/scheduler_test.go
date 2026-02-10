// SPDX-License-Identifier: AGPL-3.0-only
package scheduler

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/jolks/mcp-cron/internal/store"
)

// MockTaskExecutor implements the model.Executor interface for testing
type MockTaskExecutor struct {
	ExecuteFunc func(ctx context.Context, task *model.Task, timeout time.Duration) error
}

// Execute fulfills the model.Executor interface
func (m *MockTaskExecutor) Execute(ctx context.Context, task *model.Task, timeout time.Duration) error {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, task, timeout)
	}
	return nil
}

// createTestConfig creates a default config for testing
func createTestConfig() *config.SchedulerConfig {
	return &config.SchedulerConfig{
		DefaultTimeout: 10 * time.Minute,
	}
}

func TestNewScheduler(t *testing.T) {
	cfg := createTestConfig()
	s := NewScheduler(cfg)
	if s == nil {
		t.Fatal("NewScheduler() returned nil")
	}
	if s.cron == nil {
		t.Error("Scheduler.cron is nil")
	}
	if s.tasks == nil {
		t.Error("Scheduler.tasks is nil")
	}
	if s.entryIDs == nil {
		t.Error("Scheduler.entryIDs is nil")
	}
}

func TestAddGetTask(t *testing.T) {
	cfg := createTestConfig()
	s := NewScheduler(cfg)
	now := time.Now()
	task := &model.Task{
		ID:          "test-task",
		Name:        "Test Task",
		Schedule:    "* * * * * *", // Run every second
		Command:     "echo hello",
		Description: "A test task",
		Enabled:     false,
		Status:      model.StatusPending,
		LastRun:     now,
		NextRun:     now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	// Add the task
	err := s.AddTask(task)
	if err != nil {
		t.Fatalf("Failed to add task: %v", err)
	}

	// Get the task
	retrieved, err := s.GetTask("test-task")
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	if retrieved.ID != task.ID {
		t.Errorf("Expected task ID %s, got %s", task.ID, retrieved.ID)
	}
	if retrieved.Name != task.Name {
		t.Errorf("Expected task name %s, got %s", task.Name, retrieved.Name)
	}

	// Verify LastRun and NextRun values
	if retrieved.LastRun.IsZero() {
		t.Error("Expected LastRun to be initialized, but it's zero")
	}
	if retrieved.NextRun.IsZero() {
		t.Error("Expected NextRun to be initialized, but it's zero")
	}

	// Verify LastRun and NextRun match the values we set
	if !retrieved.LastRun.Equal(now) {
		t.Errorf("Expected LastRun %v, got %v", now, retrieved.LastRun)
	}
	if !retrieved.NextRun.Equal(now) {
		t.Errorf("Expected NextRun %v, got %v", now, retrieved.NextRun)
	}
}

func TestAddDuplicateTask(t *testing.T) {
	cfg := createTestConfig()
	s := NewScheduler(cfg)
	task := &model.Task{
		ID:          "test-task",
		Name:        "Test Task",
		Schedule:    "* * * * * *",
		Command:     "echo hello",
		Description: "A test task",
		Enabled:     false,
		Status:      model.StatusPending,
	}

	// Add the task
	err := s.AddTask(task)
	if err != nil {
		t.Fatalf("Failed to add task: %v", err)
	}

	// Try to add it again
	err = s.AddTask(task)
	if err == nil {
		t.Error("Expected error when adding duplicate task, got nil")
	}
}

func TestListTasks(t *testing.T) {
	cfg := createTestConfig()
	s := NewScheduler(cfg)
	task1 := &model.Task{
		ID:      "task1",
		Name:    "Task 1",
		Enabled: false,
		Status:  model.StatusPending,
	}
	task2 := &model.Task{
		ID:      "task2",
		Name:    "Task 2",
		Enabled: false,
		Status:  model.StatusPending,
	}

	// Add tasks
	_ = s.AddTask(task1)
	_ = s.AddTask(task2)

	// List tasks
	tasks := s.ListTasks()
	if len(tasks) != 2 {
		t.Fatalf("Expected 2 tasks, got %d", len(tasks))
	}
}

func TestRemoveTask(t *testing.T) {
	cfg := createTestConfig()
	s := NewScheduler(cfg)
	task := &model.Task{
		ID:      "test-task",
		Name:    "Test Task",
		Enabled: false,
		Status:  model.StatusPending,
	}

	// Add the task
	_ = s.AddTask(task)

	// Remove the task
	err := s.RemoveTask("test-task")
	if err != nil {
		t.Fatalf("Failed to remove task: %v", err)
	}

	// Try to get the task
	_, err = s.GetTask("test-task")
	if err == nil {
		t.Error("Expected error when getting removed task, got nil")
	}
}

func TestUpdateTask(t *testing.T) {
	cfg := createTestConfig()
	s := NewScheduler(cfg)
	task := &model.Task{
		ID:          "test-task",
		Name:        "Test Task",
		Schedule:    "* * * * * *",
		Command:     "echo hello",
		Description: "A test task",
		Enabled:     false,
		Status:      model.StatusPending,
	}

	// Add the task
	_ = s.AddTask(task)

	// Update the task
	updatedTask := &model.Task{
		ID:          "test-task",
		Name:        "Updated Task",
		Schedule:    "* * * * * *",
		Command:     "echo updated",
		Description: "An updated test task",
		Enabled:     false,
		Status:      model.StatusPending,
	}

	err := s.UpdateTask(updatedTask)
	if err != nil {
		t.Fatalf("Failed to update task: %v", err)
	}

	// Get the updated task
	retrieved, _ := s.GetTask("test-task")
	if retrieved.Name != "Updated Task" {
		t.Errorf("Expected updated name 'Updated Task', got %s", retrieved.Name)
	}
	if retrieved.Command != "echo updated" {
		t.Errorf("Expected updated command 'echo updated', got %s", retrieved.Command)
	}
}

func TestEnableDisableTask(t *testing.T) {
	cfg := createTestConfig()
	s := NewScheduler(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up a mock executor
	mockExecutor := &MockTaskExecutor{}
	s.SetTaskExecutor(mockExecutor)

	s.Start(ctx)
	defer func() {
		if err := s.Stop(); err != nil {
			t.Logf("Failed to stop scheduler: %v", err)
		}
	}()

	task := &model.Task{
		ID:          "test-task",
		Name:        "Test Task",
		Schedule:    "* * * * * *", // Run every second
		Command:     "echo hello",
		Description: "A test task",
		Enabled:     false,
		Status:      model.StatusPending,
	}

	// Add the task
	_ = s.AddTask(task)

	// Enable the task
	err := s.EnableTask("test-task")
	if err != nil {
		t.Fatalf("Failed to enable task: %v", err)
	}

	// Get the task to verify it's enabled
	retrieved, _ := s.GetTask("test-task")
	if !retrieved.Enabled {
		t.Error("Task should be enabled")
	}

	// Disable the task
	err = s.DisableTask("test-task")
	if err != nil {
		t.Fatalf("Failed to disable task: %v", err)
	}

	// Get the task to verify it's disabled
	retrieved, _ = s.GetTask("test-task")
	if retrieved.Enabled {
		t.Error("Task should be disabled")
	}
}

// TestNewTask verifies that NewTask initializes time fields properly
func TestNewTask(t *testing.T) {
	beforeTime := time.Now().Add(-1 * time.Second)
	task := NewTask()

	// Check that CreatedAt and UpdatedAt are initialized
	if task.CreatedAt.IsZero() {
		t.Error("Expected CreatedAt to be initialized, but it's zero")
	}

	if task.UpdatedAt.IsZero() {
		t.Error("Expected UpdatedAt to be initialized, but it's zero")
	}

	// Verify times are recent
	if task.CreatedAt.Before(beforeTime) {
		t.Errorf("Expected CreatedAt to be after %v, but was %v", beforeTime, task.CreatedAt)
	}

	if task.UpdatedAt.Before(beforeTime) {
		t.Errorf("Expected UpdatedAt to be after %v, but was %v", beforeTime, task.UpdatedAt)
	}

	// LastRun and NextRun should be zero in the default case
	// since they will be set when the task is scheduled

	// Check default values
	if task.Enabled != false {
		t.Errorf("Expected Enabled to be false, but was %v", task.Enabled)
	}

	if task.Status != model.StatusPending {
		t.Errorf("Expected Status to be %q, but was %q", model.StatusPending, task.Status)
	}
}

// TestCronExpressionSupport confirms that both standard (minute-based) and non-standard (second-based) cron expressions are supported
func TestCronExpressionSupport(t *testing.T) {
	cfg := createTestConfig()
	s := NewScheduler(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Set up a mock executor
	mockExecutor := &MockTaskExecutor{}
	s.SetTaskExecutor(mockExecutor)

	s.Start(ctx)
	defer func() {
		if err := s.Stop(); err != nil {
			t.Logf("Failed to stop scheduler: %v", err)
		}
	}()

	// Test standard cron expression (every minute)
	standardTask := &model.Task{
		ID:          "standard-cron-task",
		Name:        "Standard Cron Task",
		Schedule:    "*/1 * * * *", // Every minute
		Command:     "echo running standard cron",
		Description: "A task using standard cron expression",
		Enabled:     true,
		Status:      model.StatusPending,
	}

	// Test non-standard cron expression (every second)
	nonStandardTask := &model.Task{
		ID:          "non-standard-cron-task",
		Name:        "Non-Standard Cron Task",
		Schedule:    "*/1 * * * * *", // Every second
		Command:     "echo running non-standard cron",
		Description: "A task using non-standard cron expression with seconds",
		Enabled:     true,
		Status:      model.StatusPending,
	}

	// Add the tasks
	err := s.AddTask(standardTask)
	if err != nil {
		t.Fatalf("Failed to add standard task: %v", err)
	}

	err = s.AddTask(nonStandardTask)
	if err != nil {
		t.Fatalf("Failed to add non-standard task: %v", err)
	}

	// Verify the tasks were added correctly
	standardRetrieved, err := s.GetTask("standard-cron-task")
	if err != nil {
		t.Fatalf("Failed to get standard task: %v", err)
	}
	if standardRetrieved.Schedule != "*/1 * * * *" {
		t.Errorf("Expected standard schedule '*/1 * * * *', got %s", standardRetrieved.Schedule)
	}

	nonStandardRetrieved, err := s.GetTask("non-standard-cron-task")
	if err != nil {
		t.Fatalf("Failed to get non-standard task: %v", err)
	}
	if nonStandardRetrieved.Schedule != "*/1 * * * * *" {
		t.Errorf("Expected non-standard schedule '*/1 * * * * *', got %s", nonStandardRetrieved.Schedule)
	}

	// Verify both tasks are enabled
	if !standardRetrieved.Enabled {
		t.Error("Standard task should be enabled")
	}
	if !nonStandardRetrieved.Enabled {
		t.Error("Non-standard task should be enabled")
	}

	// Verify both tasks have entry IDs (are properly scheduled)
	s.mu.RLock()
	_, standardExists := s.entryIDs[standardTask.ID]
	_, nonStandardExists := s.entryIDs[nonStandardTask.ID]
	s.mu.RUnlock()

	if !standardExists {
		t.Error("Standard task should have an entry ID")
	}
	if !nonStandardExists {
		t.Error("Non-standard task should have an entry ID")
	}
}

// TestTaskExecutorPattern tests the direct execution of tasks using the TaskExecutor interface
func TestTaskExecutorPattern(t *testing.T) {
	cfg := createTestConfig()
	s := NewScheduler(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a variable to track if the task was executed
	taskExecuted := false
	executedTaskID := ""

	// Create a mock executor that records execution
	mockExecutor := &MockTaskExecutor{
		ExecuteFunc: func(ctx context.Context, task *model.Task, timeout time.Duration) error {
			taskExecuted = true

			// Get task ID from the task
			executedTaskID = task.ID
			return nil
		},
	}

	// Set the executor
	s.SetTaskExecutor(mockExecutor)

	// Start the scheduler
	s.Start(ctx)
	defer func() {
		if err := s.Stop(); err != nil {
			t.Logf("Failed to stop scheduler: %v", err)
		}
	}()

	// Create a task that will run immediately
	task := &model.Task{
		ID:          "test-executor-task",
		Name:        "Test Executor Task",
		Schedule:    "* * * * * *", // Run every second
		Command:     "echo test",
		Description: "A task for testing the executor pattern",
		Enabled:     true,
		Status:      model.StatusPending,
	}

	// Add the task
	err := s.AddTask(task)
	if err != nil {
		t.Fatalf("Failed to add task: %v", err)
	}

	// Wait a moment for the task to execute
	time.Sleep(1500 * time.Millisecond)

	// Verify the task was executed
	if !taskExecuted {
		t.Error("Task was not executed by the TaskExecutor")
	}

	// Verify the right task was executed
	if executedTaskID != task.ID {
		t.Errorf("Expected task ID %s, got %s", task.ID, executedTaskID)
	}

	// Test error handling from TaskExecutor
	// Set up a mock executor that returns an error
	errorExecutor := &MockTaskExecutor{
		ExecuteFunc: func(ctx context.Context, task *model.Task, timeout time.Duration) error {
			return fmt.Errorf("test error from executor")
		},
	}

	// Reset the scheduler
	err = s.Stop()
	if err != nil {
		t.Fatalf("Failed to stop scheduler: %v", err)
	}
	s = NewScheduler(cfg)
	s.SetTaskExecutor(errorExecutor)
	s.Start(ctx)

	// Create a new task
	errorTask := &model.Task{
		ID:          "test-error-task",
		Name:        "Test Error Task",
		Schedule:    "* * * * * *", // Run every second
		Command:     "echo error test",
		Description: "A task for testing error handling",
		Enabled:     true,
		Status:      model.StatusPending,
	}

	// Add the task
	err = s.AddTask(errorTask)
	if err != nil {
		t.Fatalf("Failed to add error task: %v", err)
	}

	// Wait a moment for the task to execute
	time.Sleep(1500 * time.Millisecond)

	// Get the task to verify its status was set to failed
	retrievedTask, err := s.GetTask(errorTask.ID)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	// Verify the task status was updated to failed
	if retrievedTask.Status != model.StatusFailed {
		t.Errorf("Expected status %s, got %s", model.StatusFailed, retrievedTask.Status)
	}
}

// TestMissingTaskExecutor verifies that the scheduler fails to schedule tasks if no executor is set
func TestMissingTaskExecutor(t *testing.T) {
	cfg := createTestConfig()
	s := NewScheduler(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.Start(ctx)
	defer func(s *Scheduler) {
		err := s.Stop()
		if err != nil {
			t.Fatalf("Failed to stop scheduler: %v", err)
		}
	}(s)

	// Create a task
	task := &model.Task{
		ID:          "missing-executor-task",
		Name:        "Missing Executor Task",
		Schedule:    "* * * * * *",
		Command:     "echo test",
		Description: "A task for testing missing executor",
		Enabled:     true,
		Status:      model.StatusPending,
	}

	// Try to add the task - this should fail because we enabled the task but didn't set an executor
	err := s.AddTask(task)
	if err == nil {
		t.Error("Expected AddTask to fail with no executor, but it succeeded")
	}

	// Now try with disabled task - this should work
	task.ID = "missing-executor-task-2" // Use a different ID
	task.Enabled = false
	err = s.AddTask(task)
	if err != nil {
		t.Errorf("Failed to add disabled task: %v", err)
	}

	// Now try to enable it - this should fail
	err = s.EnableTask(task.ID)
	if err == nil {
		t.Error("Expected EnableTask to fail with no executor, but it succeeded")
	}

	// In TestMissingTaskExecutor, update this check:
	if task.Status != model.StatusFailed {
		t.Errorf("Expected task status to be %s after error, got %s",
			model.StatusFailed, task.Status)
	}
}

// --- Task persistence round-trip tests ---

func newTestStoreForScheduler(t *testing.T) *store.SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestPersistenceRoundTrip(t *testing.T) {
	taskStore := newTestStoreForScheduler(t)
	cfg := createTestConfig()

	// Create first scheduler, add tasks
	s1 := NewScheduler(cfg)
	s1.SetTaskStore(taskStore)
	s1.SetTaskExecutor(&MockTaskExecutor{})

	ctx, cancel := context.WithCancel(context.Background())
	s1.Start(ctx)

	now := time.Now()
	shellTask := &model.Task{
		ID:          "persist-shell",
		Name:        "Shell Task",
		Description: "persisted shell task",
		Type:        model.TypeShellCommand.String(),
		Command:     "echo persisted",
		Schedule:    "*/5 * * * *",
		Enabled:     true,
		Status:      model.StatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	aiTask := &model.Task{
		ID:          "persist-ai",
		Name:        "AI Task",
		Type:        model.TypeAI.String(),
		Prompt:      "Summarize the news",
		Schedule:    "0 9 * * *",
		Enabled:     false,
		Status:      model.StatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s1.AddTask(shellTask); err != nil {
		t.Fatalf("AddTask shell: %v", err)
	}
	if err := s1.AddTask(aiTask); err != nil {
		t.Fatalf("AddTask ai: %v", err)
	}

	// Stop first scheduler
	cancel()
	s1.Stop()

	// Create second scheduler with same store, load tasks
	s2 := NewScheduler(cfg)
	s2.SetTaskStore(taskStore)
	s2.SetTaskExecutor(&MockTaskExecutor{})

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	s2.Start(ctx2)

	if err := s2.LoadTasks(); err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}

	// Verify tasks were restored
	tasks := s2.ListTasks()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks after reload, got %d", len(tasks))
	}

	got, err := s2.GetTask("persist-shell")
	if err != nil {
		t.Fatalf("GetTask persist-shell: %v", err)
	}
	if got.Name != "Shell Task" {
		t.Errorf("Name = %q, want %q", got.Name, "Shell Task")
	}
	if got.Command != "echo persisted" {
		t.Errorf("Command = %q, want %q", got.Command, "echo persisted")
	}
	if !got.Enabled {
		t.Error("shell task should be enabled after reload")
	}

	got, err = s2.GetTask("persist-ai")
	if err != nil {
		t.Fatalf("GetTask persist-ai: %v", err)
	}
	if got.Prompt != "Summarize the news" {
		t.Errorf("Prompt = %q, want %q", got.Prompt, "Summarize the news")
	}
	if got.Enabled {
		t.Error("AI task should be disabled after reload")
	}

	// Verify enabled task is scheduled
	s2.mu.RLock()
	_, shellScheduled := s2.entryIDs["persist-shell"]
	_, aiScheduled := s2.entryIDs["persist-ai"]
	s2.mu.RUnlock()

	if !shellScheduled {
		t.Error("enabled shell task should be scheduled after LoadTasks")
	}
	if aiScheduled {
		t.Error("disabled AI task should not be scheduled after LoadTasks")
	}
}

func TestPersistenceRemoveTask(t *testing.T) {
	taskStore := newTestStoreForScheduler(t)
	cfg := createTestConfig()

	s := NewScheduler(cfg)
	s.SetTaskStore(taskStore)

	now := time.Now()
	task := &model.Task{
		ID:        "to-remove",
		Name:      "Remove Me",
		Type:      model.TypeShellCommand.String(),
		Command:   "echo remove",
		Schedule:  "* * * * *",
		Enabled:   false,
		Status:    model.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.AddTask(task); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	if err := s.RemoveTask("to-remove"); err != nil {
		t.Fatalf("RemoveTask: %v", err)
	}

	// Verify removed from DB too
	tasks, err := taskStore.LoadTasks()
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks in DB after remove, got %d", len(tasks))
	}
}

func TestPersistenceUpdateTask(t *testing.T) {
	taskStore := newTestStoreForScheduler(t)
	cfg := createTestConfig()

	s := NewScheduler(cfg)
	s.SetTaskStore(taskStore)

	now := time.Now()
	task := &model.Task{
		ID:        "to-update",
		Name:      "Original",
		Type:      model.TypeShellCommand.String(),
		Command:   "echo old",
		Schedule:  "* * * * *",
		Enabled:   false,
		Status:    model.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.AddTask(task); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	updated := &model.Task{
		ID:       "to-update",
		Name:     "Updated",
		Type:     model.TypeShellCommand.String(),
		Command:  "echo new",
		Schedule: "*/10 * * * *",
		Enabled:  false,
		Status:   model.StatusPending,
	}

	if err := s.UpdateTask(updated); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	// Verify persisted to DB
	tasks, err := taskStore.LoadTasks()
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].Name != "Updated" {
		t.Errorf("Name = %q, want %q", tasks[0].Name, "Updated")
	}
	if tasks[0].Command != "echo new" {
		t.Errorf("Command = %q, want %q", tasks[0].Command, "echo new")
	}
}

func TestPersistenceEnableDisable(t *testing.T) {
	taskStore := newTestStoreForScheduler(t)
	cfg := createTestConfig()

	s := NewScheduler(cfg)
	s.SetTaskStore(taskStore)
	s.SetTaskExecutor(&MockTaskExecutor{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)

	now := time.Now()
	task := &model.Task{
		ID:        "toggle-task",
		Name:      "Toggle",
		Type:      model.TypeShellCommand.String(),
		Command:   "echo toggle",
		Schedule:  "*/5 * * * *",
		Enabled:   false,
		Status:    model.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.AddTask(task); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	// Enable — should persist
	if err := s.EnableTask("toggle-task"); err != nil {
		t.Fatalf("EnableTask: %v", err)
	}

	tasks, _ := taskStore.LoadTasks()
	if !tasks[0].Enabled {
		t.Error("expected enabled=true in DB after EnableTask")
	}

	// Disable — should persist
	if err := s.DisableTask("toggle-task"); err != nil {
		t.Fatalf("DisableTask: %v", err)
	}

	tasks, _ = taskStore.LoadTasks()
	if tasks[0].Enabled {
		t.Error("expected enabled=false in DB after DisableTask")
	}
}
