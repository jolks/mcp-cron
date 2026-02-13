// SPDX-License-Identifier: AGPL-3.0-only
package scheduler

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
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
		PollInterval:   100 * time.Millisecond,
	}
}

func TestNewScheduler(t *testing.T) {
	cfg := createTestConfig()
	s := NewScheduler(cfg)
	if s == nil {
		t.Fatal("NewScheduler() returned nil")
	}
	if s.tasks == nil {
		t.Error("Scheduler.tasks is nil")
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

	// Verify LastRun value (NextRun stays as-is since task is disabled)
	if retrieved.LastRun.IsZero() {
		t.Error("Expected LastRun to be initialized, but it's zero")
	}

	// Verify LastRun matches the value we set
	if !retrieved.LastRun.Equal(now) {
		t.Errorf("Expected LastRun %v, got %v", now, retrieved.LastRun)
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
	if retrieved.NextRun.IsZero() {
		t.Error("Expected NextRun to be set after enabling")
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
	if !retrieved.NextRun.IsZero() {
		t.Error("Expected NextRun to be cleared after disabling")
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

	// Verify both tasks are enabled with NextRun set
	if !standardRetrieved.Enabled {
		t.Error("Standard task should be enabled")
	}
	if !nonStandardRetrieved.Enabled {
		t.Error("Non-standard task should be enabled")
	}
	if standardRetrieved.NextRun.IsZero() {
		t.Error("Standard task should have NextRun set")
	}
	if nonStandardRetrieved.NextRun.IsZero() {
		t.Error("Non-standard task should have NextRun set")
	}
}

// TestComputeNextRun verifies next run time computation from cron expressions
func TestComputeNextRun(t *testing.T) {
	cfg := createTestConfig()
	s := NewScheduler(cfg)

	// Fix time for deterministic testing
	fixedNow := time.Date(2024, 1, 15, 10, 30, 0, 0, time.Local)
	s.nowFunc = func() time.Time { return fixedNow }

	// Every minute: next should be :31
	nextRun, err := s.computeNextRun("* * * * *")
	if err != nil {
		t.Fatalf("computeNextRun: %v", err)
	}
	expected := time.Date(2024, 1, 15, 10, 31, 0, 0, time.Local)
	if !nextRun.Equal(expected) {
		t.Errorf("Expected next run %v, got %v", expected, nextRun)
	}

	// Every second: next should be 10:30:01
	nextRun, err = s.computeNextRun("* * * * * *")
	if err != nil {
		t.Fatalf("computeNextRun: %v", err)
	}
	expected = time.Date(2024, 1, 15, 10, 30, 1, 0, time.Local)
	if !nextRun.Equal(expected) {
		t.Errorf("Expected next run %v, got %v", expected, nextRun)
	}

	// Invalid expression
	_, err = s.computeNextRun("invalid")
	if err == nil {
		t.Error("Expected error for invalid schedule, got nil")
	}
}

// TestTaskExecutorPattern tests the execution of tasks via the poll loop
func TestTaskExecutorPattern(t *testing.T) {
	taskStore := newTestStoreForScheduler(t)
	cfg := createTestConfig()

	var taskExecuted atomic.Bool
	var executedTaskID atomic.Value

	mockExecutor := &MockTaskExecutor{
		ExecuteFunc: func(ctx context.Context, task *model.Task, timeout time.Duration) error {
			taskExecuted.Store(true)
			executedTaskID.Store(task.ID)
			return nil
		},
	}

	s := NewScheduler(cfg)
	s.SetTaskStore(taskStore)
	s.SetTaskExecutor(mockExecutor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer func() { _ = s.Stop() }()

	// Create a task with an every-second schedule
	now := time.Now()
	task := &model.Task{
		ID:          "test-executor-task",
		Name:        "Test Executor Task",
		Schedule:    "* * * * * *", // Run every second
		Command:     "echo test",
		Description: "A task for testing the executor pattern",
		Enabled:     true,
		Status:      model.StatusPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	err := s.AddTask(task)
	if err != nil {
		t.Fatalf("Failed to add task: %v", err)
	}

	// Wait for the task to execute (poll interval is 100ms, task fires every second)
	deadline := time.After(5 * time.Second)
	for !taskExecuted.Load() {
		select {
		case <-deadline:
			t.Fatal("Task was not executed within timeout")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Verify the right task was executed
	if id, ok := executedTaskID.Load().(string); !ok || id != task.ID {
		t.Errorf("Expected task ID %s, got %v", task.ID, executedTaskID.Load())
	}

	// Test error handling from TaskExecutor
	cancel()
	_ = s.Stop()

	errorExecutor := &MockTaskExecutor{
		ExecuteFunc: func(ctx context.Context, task *model.Task, timeout time.Duration) error {
			return fmt.Errorf("test error from executor")
		},
	}

	s2 := NewScheduler(cfg)
	s2.SetTaskStore(taskStore)
	s2.SetTaskExecutor(errorExecutor)

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	s2.Start(ctx2)
	defer func() { _ = s2.Stop() }()

	// Load existing tasks
	if err := s2.LoadTasks(); err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}

	// Wait for task to execute and fail
	time.Sleep(3 * time.Second)

	// Get the task to verify its status was set to failed
	retrievedTask, err := s2.GetTask(task.ID)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	if retrievedTask.Status != model.StatusFailed {
		t.Errorf("Expected status %s, got %s", model.StatusFailed, retrievedTask.Status)
	}
}

// TestMissingTaskExecutor verifies behavior when no executor is set
func TestMissingTaskExecutor(t *testing.T) {
	cfg := createTestConfig()
	s := NewScheduler(cfg)

	// Adding an enabled task should succeed (next_run is computed by parser, no executor needed)
	// but the task won't execute without an executor
	task := &model.Task{
		ID:          "missing-executor-task",
		Name:        "Missing Executor Task",
		Schedule:    "* * * * * *",
		Command:     "echo test",
		Description: "A task for testing missing executor",
		Enabled:     true,
		Status:      model.StatusPending,
	}

	err := s.AddTask(task)
	if err != nil {
		t.Fatalf("AddTask should succeed even without executor: %v", err)
	}

	// Verify next_run was computed
	retrieved, _ := s.GetTask("missing-executor-task")
	if retrieved.NextRun.IsZero() {
		t.Error("Expected NextRun to be set for enabled task")
	}

	// Disabled task should also work
	task2 := &model.Task{
		ID:       "missing-executor-task-2",
		Name:     "Disabled Task",
		Schedule: "* * * * * *",
		Command:  "echo test",
		Enabled:  false,
		Status:   model.StatusPending,
	}
	err = s.AddTask(task2)
	if err != nil {
		t.Errorf("Failed to add disabled task: %v", err)
	}
}

// TestRemoveTaskStopsExecution verifies that after removing a task, execution stops
func TestRemoveTaskStopsExecution(t *testing.T) {
	taskStore := newTestStoreForScheduler(t)
	cfg := createTestConfig()

	var mu sync.Mutex
	executionCount := 0

	mockExecutor := &MockTaskExecutor{
		ExecuteFunc: func(ctx context.Context, task *model.Task, timeout time.Duration) error {
			mu.Lock()
			executionCount++
			mu.Unlock()
			return nil
		},
	}

	s := NewScheduler(cfg)
	s.SetTaskStore(taskStore)
	s.SetTaskExecutor(mockExecutor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer func() { _ = s.Stop() }()

	now := time.Now()
	task := &model.Task{
		ID:        "remove-while-running",
		Name:      "Remove While Running",
		Schedule:  "* * * * * *", // every second
		Command:   "echo test",
		Enabled:   true,
		Status:    model.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.AddTask(task); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	// Wait for at least one execution
	deadline := time.After(5 * time.Second)
	for {
		mu.Lock()
		count := executionCount
		mu.Unlock()
		if count > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("expected at least one execution before removal")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Remove the task
	if err := s.RemoveTask("remove-while-running"); err != nil {
		t.Fatalf("RemoveTask: %v", err)
	}

	// Record count right after removal
	mu.Lock()
	countAfterRemoval := executionCount
	mu.Unlock()

	// Wait and verify no more executions happen
	time.Sleep(2 * time.Second)

	mu.Lock()
	countAfterWait := executionCount
	mu.Unlock()

	if countAfterWait != countAfterRemoval {
		t.Errorf("task executed %d more times after removal",
			countAfterWait-countAfterRemoval)
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
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPersistenceRoundTrip(t *testing.T) {
	taskStore := newTestStoreForScheduler(t)
	cfg := createTestConfig()

	// Create first scheduler, add tasks
	s1 := NewScheduler(cfg)
	s1.SetTaskStore(taskStore)
	s1.SetTaskExecutor(&MockTaskExecutor{})

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

	// Verify shell task has NextRun set (it's enabled)
	got, _ := s1.GetTask("persist-shell")
	if got.NextRun.IsZero() {
		t.Error("enabled shell task should have NextRun set")
	}

	// Stop first scheduler
	_ = s1.Stop()

	// Create second scheduler with same store, load tasks
	s2 := NewScheduler(cfg)
	s2.SetTaskStore(taskStore)
	s2.SetTaskExecutor(&MockTaskExecutor{})

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
	if got.NextRun.IsZero() {
		t.Error("enabled shell task should have NextRun after reload")
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

	// Enable — should persist with next_run
	if err := s.EnableTask("toggle-task"); err != nil {
		t.Fatalf("EnableTask: %v", err)
	}

	tasks, _ := taskStore.LoadTasks()
	if !tasks[0].Enabled {
		t.Error("expected enabled=true in DB after EnableTask")
	}
	if tasks[0].NextRun.IsZero() {
		t.Error("expected next_run to be set in DB after EnableTask")
	}

	// Disable — should persist with cleared next_run
	if err := s.DisableTask("toggle-task"); err != nil {
		t.Fatalf("DisableTask: %v", err)
	}

	tasks, _ = taskStore.LoadTasks()
	if tasks[0].Enabled {
		t.Error("expected enabled=false in DB after DisableTask")
	}
	if !tasks[0].NextRun.IsZero() {
		t.Error("expected next_run to be cleared in DB after DisableTask")
	}
}

// TestPollLoopExecutesDueTasks verifies that the poll loop picks up and executes due tasks
func TestPollLoopExecutesDueTasks(t *testing.T) {
	taskStore := newTestStoreForScheduler(t)
	cfg := createTestConfig()

	var executed atomic.Bool

	mockExecutor := &MockTaskExecutor{
		ExecuteFunc: func(ctx context.Context, task *model.Task, timeout time.Duration) error {
			executed.Store(true)
			return nil
		},
	}

	s := NewScheduler(cfg)
	s.SetTaskStore(taskStore)
	s.SetTaskExecutor(mockExecutor)

	// Add a task with next_run in the past (immediately due)
	now := time.Now()
	task := &model.Task{
		ID:        "due-task",
		Name:      "Due Task",
		Type:      model.TypeShellCommand.String(),
		Command:   "echo due",
		Schedule:  "* * * * * *",
		Enabled:   true,
		NextRun:   now.Add(-1 * time.Second), // Already due
		Status:    model.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := taskStore.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	// Load into scheduler and start polling
	if err := s.LoadTasks(); err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Start(ctx)
	defer func() { _ = s.Stop() }()

	// Wait for execution
	deadline := time.After(5 * time.Second)
	for !executed.Load() {
		select {
		case <-deadline:
			t.Fatal("Task was not executed within timeout")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// TestPollLoopDedup verifies that two schedulers sharing the same DB
// execute a due task exactly once
func TestPollLoopDedup(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "dedup.db")

	taskStore1, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore 1: %v", err)
	}
	t.Cleanup(func() { _ = taskStore1.Close() })

	taskStore2, err := store.NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore 2: %v", err)
	}
	t.Cleanup(func() { _ = taskStore2.Close() })

	cfg := createTestConfig()

	var totalExecutions atomic.Int32

	makeExecutor := func() *MockTaskExecutor {
		return &MockTaskExecutor{
			ExecuteFunc: func(ctx context.Context, task *model.Task, timeout time.Duration) error {
				totalExecutions.Add(1)
				return nil
			},
		}
	}

	// Create two schedulers sharing the same DB
	s1 := NewScheduler(cfg)
	s1.SetTaskStore(taskStore1)
	s1.SetTaskExecutor(makeExecutor())

	s2 := NewScheduler(cfg)
	s2.SetTaskStore(taskStore2)
	s2.SetTaskExecutor(makeExecutor())

	// Insert a task directly into the DB with next_run in the past
	// (bypassing AddTask which would recompute next_run)
	now := time.Now()
	task := &model.Task{
		ID:        "dedup-task",
		Name:      "Dedup Task",
		Type:      model.TypeShellCommand.String(),
		Command:   "echo dedup",
		Schedule:  "0 0 1 1 *", // Yearly — won't naturally become due again soon
		Enabled:   true,
		NextRun:   now.Add(-1 * time.Second),
		Status:    model.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := taskStore1.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	// Load tasks into both schedulers
	if err := s1.LoadTasks(); err != nil {
		t.Fatalf("LoadTasks s1: %v", err)
	}
	if err := s2.LoadTasks(); err != nil {
		t.Fatalf("LoadTasks s2: %v", err)
	}

	// Start both schedulers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s1.Start(ctx)
	s2.Start(ctx)
	defer func() {
		_ = s1.Stop()
		_ = s2.Stop()
	}()

	// Wait enough time for multiple poll cycles
	time.Sleep(2 * time.Second)

	// Exactly one execution should have happened
	count := totalExecutions.Load()
	if count != 1 {
		t.Errorf("Expected exactly 1 execution, got %d", count)
	}
}

// TestNextRunPersistedOnAdd verifies that next_run is saved to DB when adding an enabled task
func TestNextRunPersistedOnAdd(t *testing.T) {
	taskStore := newTestStoreForScheduler(t)
	cfg := createTestConfig()

	s := NewScheduler(cfg)
	s.SetTaskStore(taskStore)

	now := time.Now()
	task := &model.Task{
		ID:        "persist-nextrun",
		Name:      "Persist NextRun",
		Type:      model.TypeShellCommand.String(),
		Command:   "echo test",
		Schedule:  "*/5 * * * *",
		Enabled:   true,
		Status:    model.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.AddTask(task); err != nil {
		t.Fatalf("AddTask: %v", err)
	}

	// Load from DB and verify next_run was persisted
	tasks, err := taskStore.LoadTasks()
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].NextRun.IsZero() {
		t.Error("expected next_run to be persisted in DB for enabled task")
	}
}
