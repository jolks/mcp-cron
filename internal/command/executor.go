// SPDX-License-Identifier: AGPL-3.0-only
package command

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/jolks/mcp-cron/internal/model"
)

// CommandExecutor handles executing commands
type CommandExecutor struct {
	mu          sync.Mutex
	results     map[string]*model.Result // Map of taskID -> Result
	resultStore model.ResultStore
}

// NewCommandExecutor creates a new command executor
func NewCommandExecutor(store model.ResultStore) *CommandExecutor {
	return &CommandExecutor{
		results:     make(map[string]*model.Result),
		resultStore: store,
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

	// Store the result
	ce.mu.Lock()
	ce.results[taskID] = result
	ce.mu.Unlock()

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

	// Persist result to store (best-effort)
	if ce.resultStore != nil {
		if storeErr := ce.resultStore.SaveResult(result); storeErr != nil {
			log.Printf("WARN: failed to persist result for task %s: %v", taskID, storeErr)
		}
	}

	// Convert the result to JSON for debug logging
	jsonData, jsonErr := json.MarshalIndent(result, "", "  ")
	if jsonErr != nil {
		errorJSON, _ := json.Marshal(map[string]string{
			"error":   "marshaling_error",
			"message": jsonErr.Error(),
			"task_id": taskID,
		})
		log.Println(string(errorJSON))
	} else {
		log.Println("[DEBUG]", string(jsonData))
	}

	return result
}

// GetTaskResult returns the result of a previously executed task
func (ce *CommandExecutor) GetTaskResult(taskID string) (*model.Result, bool) {
	ce.mu.Lock()
	defer ce.mu.Unlock()

	result, exists := ce.results[taskID]
	return result, exists
}
