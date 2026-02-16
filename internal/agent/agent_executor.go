// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/model"
)

// AgentExecutor handles executing commands with an agent
type AgentExecutor struct {
	mu          sync.Mutex
	results     map[string]*model.Result // Map of taskID -> Result
	config      *config.Config
	resultStore model.ResultStore
}

// NewAgentExecutor creates a new agent executor
func NewAgentExecutor(cfg *config.Config, store model.ResultStore) *AgentExecutor {
	return &AgentExecutor{
		results:     make(map[string]*model.Result),
		config:      cfg,
		resultStore: store,
	}
}

// Execute implements the Task execution for the scheduler
func (ae *AgentExecutor) Execute(ctx context.Context, task *model.Task, timeout time.Duration) error {
	// Runtime validation only checks fields needed for execution (ID and Prompt)
	// Schedule is validated at the API level but not required here because:
	// - The scheduler has already used the schedule to determine when to run the task
	// - Execution only needs the task ID and the content to execute
	if task.ID == "" || task.Prompt == "" {
		return fmt.Errorf("invalid task: missing ID or Prompt")
	}

	// Execute the command
	result := ae.ExecuteAgentTask(ctx, task.ID, task.Prompt, timeout)
	if result.Error != "" {
		return fmt.Errorf("%s", result.Error)
	}

	return nil
}

// ExecuteAgentTask executes a command using an AI agent
func (ae *AgentExecutor) ExecuteAgentTask(
	ctx context.Context,
	taskID string,
	prompt string,
	timeout time.Duration,
) *model.Result {
	result := &model.Result{
		Prompt:    prompt,
		StartTime: time.Now(),
		TaskID:    taskID,
	}

	// Store the result
	ae.mu.Lock()
	ae.results[taskID] = result
	ae.mu.Unlock()

	// Create a context with timeout
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create a task structure for RunTask
	task := &model.Task{
		ID:     taskID,
		Prompt: prompt,
	}

	// Execute the task using RunTask
	output, err := RunTask(execCtx, task, ae.config, ae.resultStore)

	// Update result fields
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime).String()

	if err != nil {
		result.Error = err.Error()
		result.ExitCode = 1
		result.Output = fmt.Sprintf("Error executing AI task: %v", err)
	} else {
		result.Output = output
		result.ExitCode = 0
	}

	// Persist result to store (best-effort)
	if ae.resultStore != nil {
		if storeErr := ae.resultStore.SaveResult(result); storeErr != nil {
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

// GetTaskResult implements the ResultProvider interface
func (ae *AgentExecutor) GetTaskResult(taskID string) (*model.Result, bool) {
	ae.mu.Lock()
	defer ae.mu.Unlock()

	result, exists := ae.results[taskID]
	return result, exists
}
