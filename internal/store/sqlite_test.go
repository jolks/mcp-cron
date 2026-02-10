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
	t.Cleanup(func() { s.Close() })
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
	s1.Close()

	// Open again — migrations should be a no-op.
	s2, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	s2.Close()
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
