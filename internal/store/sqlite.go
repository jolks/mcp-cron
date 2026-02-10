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
		db.Close()
		return nil, fmt.Errorf("enable WAL mode: %w", err)
	}

	if err := runMigrations(db); err != nil {
		db.Close()
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
	defer rows.Close()

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

// Close closes the underlying database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
