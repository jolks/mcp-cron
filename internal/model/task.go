// SPDX-License-Identifier: AGPL-3.0-only
package model

import (
	"context"
	"time"
)

// TaskType defines the types of tasks that can be executed
type TaskType string

// TaskStatus represents the current status of a task
type TaskStatus string

// Task types
const (
	TypeShellCommand TaskType = "shell_command"
	TypeAI           TaskType = "AI"
	TypeHTTP         TaskType = "http"
)

// Task status constants
const (
	// StatusPending indicates a task that has not been run yet
	StatusPending TaskStatus = "pending"
	// StatusRunning indicates a task that is currently running
	StatusRunning TaskStatus = "running"
	// StatusCompleted indicates a task that has successfully completed
	StatusCompleted TaskStatus = "completed"
	// StatusFailed indicates a task that has failed
	StatusFailed TaskStatus = "failed"
	// StatusDisabled indicates a task that is disabled
	StatusDisabled TaskStatus = "disabled"
)

// Task represents a scheduled task
type Task struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Command     string     `json:"command,omitempty" description:"command for shell"`
	Prompt      string     `json:"prompt,omitempty" description:"prompt to use for AI"`
	URL         string     `json:"url,omitempty" description:"URL for HTTP tasks"`
	Method      string     `json:"method,omitempty" description:"HTTP method for HTTP tasks (default POST)"`
	Headers     map[string]string `json:"headers,omitempty" description:"HTTP request headers for HTTP tasks"`
	Body        string     `json:"body,omitempty" description:"HTTP request body for HTTP tasks"`
	Schedule    string     `json:"schedule"`
	RunAt       *time.Time `json:"runAt,omitempty"`
	Enabled     bool       `json:"enabled"`
	Type        TaskType   `json:"type"`
	LastRun     time.Time  `json:"lastRun,omitempty"`
	NextRun     time.Time  `json:"nextRun,omitempty"`
	Status      TaskStatus `json:"status"`
	CreatedAt   time.Time  `json:"createdAt,omitempty"`
	UpdatedAt   time.Time  `json:"updatedAt,omitempty"`
}

// Result contains the results of a task execution.
//
// For HTTP tasks, ExitCode holds the HTTP status code (or 0 if no response
// was received) and Output holds a body preview. Error is set when the
// request failed at the transport level or returned a non-2xx status.
type Result struct {
	TaskID    string    `json:"task_id"`
	Command   string    `json:"command,omitempty" description:"command for shell"`
	Prompt    string    `json:"prompt,omitempty" description:"prompt to use for AI"`
	URL       string    `json:"url,omitempty" description:"URL for HTTP tasks"`
	Output    string    `json:"output"`
	Error     string    `json:"error,omitempty"`
	ExitCode  int       `json:"exit_code"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	Duration  string    `json:"duration"`
}

// Executor defines the interface for executing tasks
type Executor interface {
	Execute(ctx context.Context, task *Task, timeout time.Duration) error
}

// TaskStore defines an interface for persisting and retrieving task definitions
type TaskStore interface {
	SaveTask(task *Task) error
	UpdateTask(task *Task) error
	DeleteTask(taskID string) error
	LoadTasks() ([]*Task, error)
	GetDueTasks(now time.Time) ([]*Task, error)
	AdvanceNextRun(taskID string, currentNextRun time.Time, newNextRun time.Time) (bool, error)
	ConsumeOneShot(taskID string, currentNextRun time.Time) (bool, error)
}

// MaxQueryRows is the maximum number of rows returned by QueryDB.
const MaxQueryRows = 1000

// ResultStore defines an interface for persisting and retrieving task execution results
type ResultStore interface {
	SaveResult(result *Result) error
	GetLatestResult(taskID string) (*Result, error)
	GetResults(taskID string, limit int) ([]*Result, error)
	QueryDB(ctx context.Context, query string) ([]map[string]interface{}, error)
	GetSchema() (string, error)
	Close() error
}
