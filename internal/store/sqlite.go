// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jolks/mcp-cron/internal/model"

	_ "modernc.org/sqlite"
)

const timeFormat = time.RFC3339Nano

// SQLiteStore implements model.ResultStore backed by a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) the SQLite database at dbPath,
// enables WAL mode, and runs any pending schema migrations.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	// Ensure the parent directory exists.
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	// Set busy timeout to handle concurrent writer contention across instances.
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	if err := runMigrations(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// SaveResult persists a task execution result.
func (s *SQLiteStore) SaveResult(result *model.Result) error {
	_, err := s.db.Exec(`
		INSERT INTO results (task_id, command, prompt, output, error, exit_code, start_time, end_time, duration)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		result.TaskID,
		result.Command,
		result.Prompt,
		result.Output,
		result.Error,
		result.ExitCode,
		result.StartTime.Format(timeFormat),
		result.EndTime.Format(timeFormat),
		result.Duration,
	)
	if err != nil {
		return fmt.Errorf("insert result: %w", err)
	}
	return nil
}

// GetLatestResult returns the most recent result for the given task ID.
// Returns nil, nil if no result exists.
func (s *SQLiteStore) GetLatestResult(taskID string) (*model.Result, error) {
	results, err := s.GetResults(taskID, 1)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, nil
	}
	return results[0], nil
}

// GetResults returns up to limit results for the given task ID, ordered
// by start_time descending (most recent first).
func (s *SQLiteStore) GetResults(taskID string, limit int) ([]*model.Result, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := s.db.Query(`
		SELECT task_id, command, prompt, output, error, exit_code, start_time, end_time, duration
		FROM results
		WHERE task_id = ?
		ORDER BY start_time DESC
		LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("query results: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []*model.Result
	for rows.Next() {
		var r model.Result
		var startStr, endStr string
		if err := rows.Scan(
			&r.TaskID, &r.Command, &r.Prompt, &r.Output,
			&r.Error, &r.ExitCode, &startStr, &endStr, &r.Duration,
		); err != nil {
			return nil, fmt.Errorf("scan result row: %w", err)
		}
		r.StartTime, _ = time.Parse(timeFormat, startStr)
		r.EndTime, _ = time.Parse(timeFormat, endStr)
		results = append(results, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate result rows: %w", err)
	}

	return results, nil
}

// SaveTask persists a new task definition.
func (s *SQLiteStore) SaveTask(task *model.Task) error {
	nextRun := ""
	if !task.NextRun.IsZero() {
		nextRun = task.NextRun.Format(timeFormat)
	}
	_, err := s.db.Exec(`
		INSERT INTO tasks (id, name, description, type, command, prompt, schedule, enabled, created_at, updated_at, next_run)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID,
		task.Name,
		task.Description,
		task.Type,
		task.Command,
		task.Prompt,
		task.Schedule,
		boolToInt(task.Enabled),
		task.CreatedAt.Format(timeFormat),
		task.UpdatedAt.Format(timeFormat),
		nextRun,
	)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
}

// UpdateTask updates an existing task definition.
func (s *SQLiteStore) UpdateTask(task *model.Task) error {
	nextRun := ""
	if !task.NextRun.IsZero() {
		nextRun = task.NextRun.Format(timeFormat)
	}
	result, err := s.db.Exec(`
		UPDATE tasks SET name=?, description=?, type=?, command=?, prompt=?, schedule=?, enabled=?, updated_at=?, next_run=?
		WHERE id=?`,
		task.Name,
		task.Description,
		task.Type,
		task.Command,
		task.Prompt,
		task.Schedule,
		boolToInt(task.Enabled),
		task.UpdatedAt.Format(timeFormat),
		nextRun,
		task.ID,
	)
	if err != nil {
		return fmt.Errorf("update task: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check update result: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("task %s not found", task.ID)
	}
	return nil
}

// DeleteTask removes a task definition by ID.
func (s *SQLiteStore) DeleteTask(taskID string) error {
	_, err := s.db.Exec("DELETE FROM tasks WHERE id=?", taskID)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	return nil
}

// LoadTasks returns all persisted task definitions.
func (s *SQLiteStore) LoadTasks() ([]*model.Task, error) {
	rows, err := s.db.Query(`
		SELECT id, name, description, type, command, prompt, schedule, enabled, created_at, updated_at, next_run
		FROM tasks`)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tasks []*model.Task
	for rows.Next() {
		var t model.Task
		var enabled int
		var createdStr, updatedStr, nextRunStr string
		if err := rows.Scan(
			&t.ID, &t.Name, &t.Description, &t.Type,
			&t.Command, &t.Prompt, &t.Schedule,
			&enabled, &createdStr, &updatedStr, &nextRunStr,
		); err != nil {
			return nil, fmt.Errorf("scan task row: %w", err)
		}
		t.Enabled = enabled != 0
		t.CreatedAt, _ = time.Parse(timeFormat, createdStr)
		t.UpdatedAt, _ = time.Parse(timeFormat, updatedStr)
		if nextRunStr != "" {
			t.NextRun, _ = time.Parse(timeFormat, nextRunStr)
		}
		t.Status = model.StatusPending
		if !t.Enabled {
			t.Status = model.StatusDisabled
		}
		tasks = append(tasks, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate task rows: %w", err)
	}
	return tasks, nil
}

// GetDueTasks returns all enabled tasks whose next_run is at or before the given time.
func (s *SQLiteStore) GetDueTasks(now time.Time) ([]*model.Task, error) {
	rows, err := s.db.Query(`
		SELECT id, name, description, type, command, prompt, schedule, enabled, created_at, updated_at, next_run
		FROM tasks
		WHERE enabled = 1 AND next_run != '' AND next_run <= ?`,
		now.Format(timeFormat))
	if err != nil {
		return nil, fmt.Errorf("query due tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var tasks []*model.Task
	for rows.Next() {
		var t model.Task
		var enabled int
		var createdStr, updatedStr, nextRunStr string
		if err := rows.Scan(
			&t.ID, &t.Name, &t.Description, &t.Type,
			&t.Command, &t.Prompt, &t.Schedule,
			&enabled, &createdStr, &updatedStr, &nextRunStr,
		); err != nil {
			return nil, fmt.Errorf("scan due task row: %w", err)
		}
		t.Enabled = enabled != 0
		t.CreatedAt, _ = time.Parse(timeFormat, createdStr)
		t.UpdatedAt, _ = time.Parse(timeFormat, updatedStr)
		if nextRunStr != "" {
			t.NextRun, _ = time.Parse(timeFormat, nextRunStr)
		}
		t.Status = model.StatusPending
		tasks = append(tasks, &t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due task rows: %w", err)
	}
	return tasks, nil
}

// AdvanceNextRun atomically advances a task's next_run using optimistic locking.
// Returns true if the update succeeded (this instance claimed the execution),
// false if another instance already advanced it.
func (s *SQLiteStore) AdvanceNextRun(taskID string, currentNextRun time.Time, newNextRun time.Time) (bool, error) {
	result, err := s.db.Exec(`
		UPDATE tasks SET next_run = ?, updated_at = ?
		WHERE id = ? AND next_run = ?`,
		newNextRun.Format(timeFormat),
		time.Now().Format(timeFormat),
		taskID,
		currentNextRun.Format(timeFormat),
	)
	if err != nil {
		return false, fmt.Errorf("advance next_run: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check advance result: %w", err)
	}
	return rows == 1, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
