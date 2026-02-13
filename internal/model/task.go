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

// String returns the string representation of the type, making it easier to use in string contexts
func (t TaskType) String() string {
	return string(t)
}

// String returns the string representation of the status, making it easier to use in string contexts
func (s TaskStatus) String() string {
	return string(s)
}

// Task represents a scheduled task
type Task struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Command     string     `json:"command,omitempty" description:"command for shell"`
	Prompt      string     `json:"prompt,omitempty" description:"prompt to use for AI"`
	Schedule    string     `json:"schedule"`
	Enabled     bool       `json:"enabled"`
	Type        string     `json:"type"`
	LastRun     time.Time  `json:"lastRun,omitempty"`
	NextRun     time.Time  `json:"nextRun,omitempty"`
	Status      TaskStatus `json:"status"`
	CreatedAt   time.Time  `json:"createdAt,omitempty"`
	UpdatedAt   time.Time  `json:"updatedAt,omitempty"`
}

// Result contains the results of a task execution
type Result struct {
	TaskID    string    `json:"task_id"`
	Command   string    `json:"command,omitempty" description:"command for shell"`
	Prompt    string    `json:"prompt,omitempty" description:"prompt to use for AI"`
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

// ResultProvider defines an interface for getting task execution results
type ResultProvider interface {
	GetTaskResult(taskID string) (*Result, bool)
}

// TaskStore defines an interface for persisting and retrieving task definitions
type TaskStore interface {
	SaveTask(task *Task) error
	UpdateTask(task *Task) error
	DeleteTask(taskID string) error
	LoadTasks() ([]*Task, error)
	GetDueTasks(now time.Time) ([]*Task, error)
	AdvanceNextRun(taskID string, currentNextRun time.Time, newNextRun time.Time) (bool, error)
}

// ResultStore defines an interface for persisting and retrieving task execution results
type ResultStore interface {
	SaveResult(result *Result) error
	GetLatestResult(taskID string) (*Result, error)
	GetResults(taskID string, limit int) ([]*Result, error)
	Close() error
}
