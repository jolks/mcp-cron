package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Result contains the results of a command execution
type Result struct {
	Command   string    `json:"command"`
	ExitCode  int       `json:"exit_code"`
	Output    string    `json:"output"`
	Error     string    `json:"error,omitempty"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
	TaskID    string    `json:"task_id"`
	Duration  string    `json:"duration"`
}

// CommandExecutor handles executing commands
type CommandExecutor struct {
	mu      sync.Mutex
	results map[string]*Result // Map of taskID -> Result
}

// NewCommandExecutor creates a new command executor
func NewCommandExecutor() *CommandExecutor {
	return &CommandExecutor{
		results: make(map[string]*Result),
	}
}

// ExecuteCommand executes a shell command with a timeout
func (ce *CommandExecutor) ExecuteCommand(ctx context.Context, taskID, command string, timeout time.Duration) *Result {
	// Create a cancellable context with timeout
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Prepare the command
	cmd := exec.CommandContext(execCtx, "sh", "-c", command)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Create result object
	// Set Output to stdout.String() later so can see from get_task tool?
	result := &Result{
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

	// Convert the result to JSON
	jsonData, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		errorJSON, _ := json.Marshal(map[string]string{
			"error":   "marshaling_error",
			"message": err.Error(),
			"task_id": taskID,
		})
		log.Println(string(errorJSON))
	} else {
		log.Println(string(jsonData))
	}

	return result
}
