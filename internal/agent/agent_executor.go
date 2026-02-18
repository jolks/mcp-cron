// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
)

// AgentExecutor handles executing commands with an agent
type AgentExecutor struct {
	config      *config.Config
	resultStore model.ResultStore
	logger      *logging.Logger
}

// NewAgentExecutor creates a new agent executor
func NewAgentExecutor(cfg *config.Config, store model.ResultStore, logger *logging.Logger) *AgentExecutor {
	return &AgentExecutor{
		config:      cfg,
		resultStore: store,
		logger:      logger,
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

	model.PersistAndLogResult(ae.resultStore, result, ae.logger)

	return result
}
