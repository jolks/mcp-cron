// SPDX-License-Identifier: AGPL-3.0-only
package command

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
)

// CommandExecutor handles executing commands
type CommandExecutor struct {
	resultStore model.ResultStore
	logger      *logging.Logger
}

// NewCommandExecutor creates a new command executor
func NewCommandExecutor(store model.ResultStore, logger *logging.Logger) *CommandExecutor {
	return &CommandExecutor{
		resultStore: store,
		logger:      logger,
	}
}

// Execute implements the Task execution for the scheduler
func (ce *CommandExecutor) Execute(ctx context.Context, task *model.Task, timeout time.Duration) error {
	// Runtime validation only checks fields needed for execution (ID and Command)
	// Schedule is validated at the API level but not required here because:
	// - The scheduler has already used the schedule to determine when to run the task
	// - Execution only needs the task ID and the command to execute
	if task.ID == "" || task.Command == "" {
		return fmt.Errorf("invalid task: missing ID or Command")
	}

	// Execute the command
	result := ce.ExecuteCommand(ctx, task.ID, task.Command, timeout)
	if result.Error != "" {
		return fmt.Errorf("%s", result.Error)
	}

	return nil
}

// ExecuteCommand executes a shell command with a timeout
func (ce *CommandExecutor) ExecuteCommand(ctx context.Context, taskID, command string, timeout time.Duration) *model.Result {
	// Create a cancellable context with timeout
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Prepare the command
	cmd := exec.CommandContext(execCtx, "sh", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Create result object
	result := &model.Result{
		Command:   command,
		StartTime: time.Now(),
		TaskID:    taskID,
	}

	// Execute the command
	err := cmd.Run()

	// Update result fields
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime).String()
	result.Output = strings.TrimSpace(stdout.String() + "\n" + stderr.String())

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
		}
		result.Error = err.Error()
	} else {
		result.ExitCode = 0
	}

	model.PersistAndLogResult(ce.resultStore, result, ce.logger)

	return result
}
