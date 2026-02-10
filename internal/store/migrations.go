// SPDX-License-Identifier: AGPL-3.0-only
package store

import (
	"database/sql"
	"fmt"
)

// migration represents a single schema migration.
type migration struct {
	version int
	up      func(tx *sql.Tx) error
}

// migrations is the ordered list of schema migrations.
var migrations = []migration{
	{
		version: 1,
		up: func(tx *sql.Tx) error {
			_, err := tx.Exec(`
				CREATE TABLE results (
					id         INTEGER PRIMARY KEY AUTOINCREMENT,
					task_id    TEXT NOT NULL,
					command    TEXT DEFAULT '',
					prompt     TEXT DEFAULT '',
					output     TEXT DEFAULT '',
					error      TEXT DEFAULT '',
					exit_code  INTEGER DEFAULT 0,
					start_time TEXT NOT NULL,
					end_time   TEXT NOT NULL,
					duration   TEXT DEFAULT ''
				);
				CREATE INDEX idx_results_task_start ON results (task_id, start_time DESC);
			`)
			return err
		},
	},
}

// runMigrations ensures the schema_version table exists and runs any pending migrations.
func runMigrations(db *sql.DB) error {
	// Create the schema_version table if it doesn't exist.
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_version (
			version INTEGER NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create schema_version table: %w", err)
	}

	// Read the current version.
	var current int
	row := db.QueryRow("SELECT version FROM schema_version LIMIT 1")
	if err := row.Scan(&current); err != nil {
		if err == sql.ErrNoRows {
			// No version row yet — insert version 0.
			if _, err := db.Exec("INSERT INTO schema_version (version) VALUES (0)"); err != nil {
				return fmt.Errorf("insert initial schema version: %w", err)
			}
			current = 0
		} else {
			return fmt.Errorf("read schema version: %w", err)
		}
	}

	// Run pending migrations.
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", m.version, err)
		}
		if err := m.up(tx); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec("UPDATE schema_version SET version = ?", m.version); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("update schema version to %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %d: %w", m.version, err)
		}
	}

	return nil
}
