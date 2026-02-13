// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jolks/mcp-cron/internal/model"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSaveAndGetLatestResult(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().Truncate(time.Microsecond)
	r := &model.Result{
		TaskID:    "task-1",
		Command:   "echo hello",
		Output:    "hello",
		ExitCode:  0,
		StartTime: now,
		EndTime:   now.Add(time.Second),
		Duration:  "1s",
	}

	if err := s.SaveResult(r); err != nil {
		t.Fatalf("SaveResult: %v", err)
	}

	got, err := s.GetLatestResult("task-1")
	if err != nil {
		t.Fatalf("GetLatestResult: %v", err)
	}
	if got == nil {
		t.Fatal("expected result, got nil")
	}
	if got.TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", got.TaskID, "task-1")
	}
	if got.Command != "echo hello" {
		t.Errorf("Command = %q, want %q", got.Command, "echo hello")
	}
	if got.Output != "hello" {
		t.Errorf("Output = %q, want %q", got.Output, "hello")
	}
	if got.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", got.ExitCode)
	}
	if got.Duration != "1s" {
		t.Errorf("Duration = %q, want %q", got.Duration, "1s")
	}
}

func TestGetLatestResultNotFound(t *testing.T) {
	s := newTestStore(t)

	got, err := s.GetLatestResult("nonexistent")
	if err != nil {
		t.Fatalf("GetLatestResult: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil result, got %+v", got)
	}
}

func TestGetResultsOrdering(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().Truncate(time.Microsecond)

	// Save 3 results with ascending start times.
	for i := 0; i < 3; i++ {
		r := &model.Result{
			TaskID:    "task-order",
			Command:   "echo",
			Output:    time.Duration(i).String(),
			StartTime: now.Add(time.Duration(i) * time.Minute),
			EndTime:   now.Add(time.Duration(i)*time.Minute + time.Second),
			Duration:  "1s",
		}
		if err := s.SaveResult(r); err != nil {
			t.Fatalf("SaveResult %d: %v", i, err)
		}
	}

	results, err := s.GetResults("task-order", 10)
	if err != nil {
		t.Fatalf("GetResults: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Most recent first.
	if results[0].Output != "2ns" {
		t.Errorf("first result output = %q, want %q", results[0].Output, "2ns")
	}
	if results[2].Output != "0s" {
		t.Errorf("last result output = %q, want %q", results[2].Output, "0s")
	}
}

func TestGetResultsLimit(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().Truncate(time.Microsecond)

	for i := 0; i < 5; i++ {
		r := &model.Result{
			TaskID:    "task-limit",
			Command:   "echo",
			StartTime: now.Add(time.Duration(i) * time.Minute),
			EndTime:   now.Add(time.Duration(i)*time.Minute + time.Second),
			Duration:  "1s",
		}
		if err := s.SaveResult(r); err != nil {
			t.Fatalf("SaveResult: %v", err)
		}
	}

	results, err := s.GetResults("task-limit", 2)
	if err != nil {
		t.Fatalf("GetResults: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestGetResultsLimitClamp(t *testing.T) {
	s := newTestStore(t)

	// Limit < 1 should be clamped to 1.
	results, err := s.GetResults("nonexistent", 0)
	if err != nil {
		t.Fatalf("GetResults with limit 0: %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results for nonexistent task, got %d", len(results))
	}

	// Limit > 100 should be clamped to 100 (no error).
	results, err = s.GetResults("nonexistent", 200)
	if err != nil {
		t.Fatalf("GetResults with limit 200: %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results for nonexistent task, got %d", len(results))
	}
}

func TestSaveResultAITask(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().Truncate(time.Microsecond)
	r := &model.Result{
		TaskID:    "ai-task-1",
		Prompt:    "What is 2+2?",
		Output:    "4",
		ExitCode:  0,
		StartTime: now,
		EndTime:   now.Add(2 * time.Second),
		Duration:  "2s",
	}

	if err := s.SaveResult(r); err != nil {
		t.Fatalf("SaveResult: %v", err)
	}

	got, err := s.GetLatestResult("ai-task-1")
	if err != nil {
		t.Fatalf("GetLatestResult: %v", err)
	}
	if got.Prompt != "What is 2+2?" {
		t.Errorf("Prompt = %q, want %q", got.Prompt, "What is 2+2?")
	}
	if got.Output != "4" {
		t.Errorf("Output = %q, want %q", got.Output, "4")
	}
}

func TestMigrationIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migrate.db")

	// Open, run migrations, close.
	s1, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	_ = s1.Close()

	// Open again — migrations should be a no-op.
	s2, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	_ = s2.Close()
}

// --- Task persistence tests ---

func TestSaveAndLoadTask(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().Truncate(time.Microsecond)
	task := &model.Task{
		ID:          "task-1",
		Name:        "Test Task",
		Description: "A test task",
		Type:        "shell_command",
		Command:     "echo hello",
		Schedule:    "*/5 * * * *",
		Enabled:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	tasks, err := s.LoadTasks()
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	got := tasks[0]
	if got.ID != "task-1" {
		t.Errorf("ID = %q, want %q", got.ID, "task-1")
	}
	if got.Name != "Test Task" {
		t.Errorf("Name = %q, want %q", got.Name, "Test Task")
	}
	if got.Description != "A test task" {
		t.Errorf("Description = %q, want %q", got.Description, "A test task")
	}
	if got.Type != "shell_command" {
		t.Errorf("Type = %q, want %q", got.Type, "shell_command")
	}
	if got.Command != "echo hello" {
		t.Errorf("Command = %q, want %q", got.Command, "echo hello")
	}
	if got.Schedule != "*/5 * * * *" {
		t.Errorf("Schedule = %q, want %q", got.Schedule, "*/5 * * * *")
	}
	if !got.Enabled {
		t.Error("Enabled = false, want true")
	}
	if got.Status != model.StatusPending {
		t.Errorf("Status = %q, want %q", got.Status, model.StatusPending)
	}
}

func TestSaveAndLoadAITask(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().Truncate(time.Microsecond)
	task := &model.Task{
		ID:        "ai-task-1",
		Name:      "AI Task",
		Type:      "AI",
		Prompt:    "Summarize the news",
		Schedule:  "0 9 * * *",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	tasks, err := s.LoadTasks()
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	got := tasks[0]
	if got.Prompt != "Summarize the news" {
		t.Errorf("Prompt = %q, want %q", got.Prompt, "Summarize the news")
	}
	if got.Type != "AI" {
		t.Errorf("Type = %q, want %q", got.Type, "AI")
	}
}

func TestUpdateTaskStore(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().Truncate(time.Microsecond)
	task := &model.Task{
		ID:        "task-upd",
		Name:      "Original",
		Type:      "shell_command",
		Command:   "echo old",
		Schedule:  "* * * * *",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	// Update fields
	task.Name = "Updated"
	task.Command = "echo new"
	task.Enabled = false
	task.UpdatedAt = now.Add(time.Minute)

	if err := s.UpdateTask(task); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	tasks, err := s.LoadTasks()
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	got := tasks[0]
	if got.Name != "Updated" {
		t.Errorf("Name = %q, want %q", got.Name, "Updated")
	}
	if got.Command != "echo new" {
		t.Errorf("Command = %q, want %q", got.Command, "echo new")
	}
	if got.Enabled {
		t.Error("Enabled = true, want false")
	}
	if got.Status != model.StatusDisabled {
		t.Errorf("Status = %q, want %q", got.Status, model.StatusDisabled)
	}
}

func TestUpdateTaskNotFound(t *testing.T) {
	s := newTestStore(t)

	task := &model.Task{
		ID:        "nonexistent",
		Name:      "Ghost",
		Type:      "shell_command",
		Schedule:  "* * * * *",
		UpdatedAt: time.Now(),
	}

	err := s.UpdateTask(task)
	if err == nil {
		t.Error("expected error updating nonexistent task, got nil")
	}
}

func TestDeleteTaskStore(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().Truncate(time.Microsecond)
	task := &model.Task{
		ID:        "task-del",
		Name:      "To Delete",
		Type:      "shell_command",
		Command:   "echo bye",
		Schedule:  "* * * * *",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	if err := s.DeleteTask("task-del"); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	tasks, err := s.LoadTasks()
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks after delete, got %d", len(tasks))
	}
}

func TestLoadTasksEmpty(t *testing.T) {
	s := newTestStore(t)

	tasks, err := s.LoadTasks()
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if tasks != nil {
		t.Fatalf("expected nil tasks for empty table, got %d", len(tasks))
	}
}

func TestSaveDuplicateTask(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().Truncate(time.Microsecond)
	task := &model.Task{
		ID:        "dup-task",
		Name:      "Dup",
		Type:      "shell_command",
		Command:   "echo dup",
		Schedule:  "* * * * *",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.SaveTask(task); err != nil {
		t.Fatalf("first SaveTask: %v", err)
	}

	err := s.SaveTask(task)
	if err == nil {
		t.Error("expected error saving duplicate task, got nil")
	}
}

// --- next_run and multi-instance tests ---

func TestSaveTaskWithNextRun(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().Truncate(time.Microsecond)
	nextRun := now.Add(5 * time.Minute)
	task := &model.Task{
		ID:        "task-nextrun",
		Name:      "NextRun Task",
		Type:      "shell_command",
		Command:   "echo hello",
		Schedule:  "*/5 * * * *",
		Enabled:   true,
		NextRun:   nextRun,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	tasks, err := s.LoadTasks()
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].NextRun.IsZero() {
		t.Error("expected NextRun to be set after round-trip")
	}
	// Compare truncated to microsecond (RFC3339Nano precision)
	if !tasks[0].NextRun.Truncate(time.Microsecond).Equal(nextRun) {
		t.Errorf("NextRun = %v, want %v", tasks[0].NextRun, nextRun)
	}
}

func TestLoadTasksWithNextRun(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().Truncate(time.Microsecond)
	// Task with next_run
	task1 := &model.Task{
		ID:        "with-nextrun",
		Name:      "With NextRun",
		Type:      "shell_command",
		Command:   "echo 1",
		Schedule:  "* * * * *",
		Enabled:   true,
		NextRun:   now.Add(time.Minute),
		CreatedAt: now,
		UpdatedAt: now,
	}
	// Task without next_run (disabled)
	task2 := &model.Task{
		ID:        "without-nextrun",
		Name:      "Without NextRun",
		Type:      "shell_command",
		Command:   "echo 2",
		Schedule:  "* * * * *",
		Enabled:   false,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.SaveTask(task1); err != nil {
		t.Fatalf("SaveTask 1: %v", err)
	}
	if err := s.SaveTask(task2); err != nil {
		t.Fatalf("SaveTask 2: %v", err)
	}

	tasks, err := s.LoadTasks()
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}

	// Find task by ID
	var loaded1, loaded2 *model.Task
	for _, t := range tasks {
		switch t.ID {
		case "with-nextrun":
			loaded1 = t
		case "without-nextrun":
			loaded2 = t
		}
	}

	if loaded1 == nil || loaded1.NextRun.IsZero() {
		t.Error("task with next_run should have NextRun set")
	}
	if loaded2 == nil || !loaded2.NextRun.IsZero() {
		t.Error("task without next_run should have zero NextRun")
	}
}

func TestGetDueTasks(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().Truncate(time.Microsecond)

	// Past next_run (due)
	due := &model.Task{
		ID:        "due-task",
		Name:      "Due Task",
		Type:      "shell_command",
		Command:   "echo due",
		Schedule:  "* * * * *",
		Enabled:   true,
		NextRun:   now.Add(-1 * time.Minute),
		CreatedAt: now,
		UpdatedAt: now,
	}
	// Future next_run (not due)
	future := &model.Task{
		ID:        "future-task",
		Name:      "Future Task",
		Type:      "shell_command",
		Command:   "echo future",
		Schedule:  "* * * * *",
		Enabled:   true,
		NextRun:   now.Add(10 * time.Minute),
		CreatedAt: now,
		UpdatedAt: now,
	}
	// Disabled with past next_run (should not be returned)
	disabled := &model.Task{
		ID:        "disabled-task",
		Name:      "Disabled Task",
		Type:      "shell_command",
		Command:   "echo disabled",
		Schedule:  "* * * * *",
		Enabled:   false,
		NextRun:   now.Add(-1 * time.Minute),
		CreatedAt: now,
		UpdatedAt: now,
	}
	// No next_run (should not be returned)
	noNextRun := &model.Task{
		ID:        "no-nextrun-task",
		Name:      "No NextRun Task",
		Type:      "shell_command",
		Command:   "echo none",
		Schedule:  "* * * * *",
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}

	for _, task := range []*model.Task{due, future, disabled, noNextRun} {
		if err := s.SaveTask(task); err != nil {
			t.Fatalf("SaveTask %s: %v", task.ID, err)
		}
	}

	dueTasks, err := s.GetDueTasks(now)
	if err != nil {
		t.Fatalf("GetDueTasks: %v", err)
	}

	if len(dueTasks) != 1 {
		t.Fatalf("expected 1 due task, got %d", len(dueTasks))
	}
	if dueTasks[0].ID != "due-task" {
		t.Errorf("expected due-task, got %s", dueTasks[0].ID)
	}
}

func TestAdvanceNextRun(t *testing.T) {
	s := newTestStore(t)

	now := time.Now().Truncate(time.Microsecond)
	currentNextRun := now.Add(-1 * time.Minute)
	newNextRun := now.Add(1 * time.Minute)

	task := &model.Task{
		ID:        "advance-task",
		Name:      "Advance Task",
		Type:      "shell_command",
		Command:   "echo advance",
		Schedule:  "* * * * *",
		Enabled:   true,
		NextRun:   currentNextRun,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := s.SaveTask(task); err != nil {
		t.Fatalf("SaveTask: %v", err)
	}

	// First call should succeed (claim)
	claimed, err := s.AdvanceNextRun("advance-task", currentNextRun, newNextRun)
	if err != nil {
		t.Fatalf("AdvanceNextRun (first): %v", err)
	}
	if !claimed {
		t.Error("expected first AdvanceNextRun to succeed")
	}

	// Second call with same currentNextRun should fail (already advanced)
	claimed, err = s.AdvanceNextRun("advance-task", currentNextRun, newNextRun.Add(time.Minute))
	if err != nil {
		t.Fatalf("AdvanceNextRun (second): %v", err)
	}
	if claimed {
		t.Error("expected second AdvanceNextRun with stale currentNextRun to fail")
	}

	// Verify the next_run was actually updated
	tasks, err := s.LoadTasks()
	if err != nil {
		t.Fatalf("LoadTasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if !tasks[0].NextRun.Truncate(time.Microsecond).Equal(newNextRun) {
		t.Errorf("NextRun = %v, want %v", tasks[0].NextRun, newNextRun)
	}
}

func TestClosePreventsFurtherOps(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "close.db")
	s, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Operations after close should fail.
	err = s.SaveResult(&model.Result{
		TaskID:    "x",
		StartTime: time.Now(),
		EndTime:   time.Now(),
	})
	if err == nil {
		t.Error("expected error after Close, got nil")
	}
}
