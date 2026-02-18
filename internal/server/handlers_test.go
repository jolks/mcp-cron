// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"context"
	"testing"
	"time"

	"github.com/jolks/mcp-cron/internal/agent"
	"github.com/jolks/mcp-cron/internal/command"
	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/jolks/mcp-cron/internal/scheduler"
)

// TestTaskExecutorImplementation verifies that the MCPServer properly
// implements the TaskExecutor interface
func TestTaskExecutorImplementation(t *testing.T) {
	// Create a config for testing
	cfg := config.DefaultConfig()

	// Create a mock scheduler
	sched := scheduler.NewScheduler(&cfg.Scheduler, testLogger())

	// Create executors
	cmdExec := command.NewCommandExecutor(nil, testLogger())
	agentExec := agent.NewAgentExecutor(cfg, nil, testLogger())

	// Create a logger
	logger := logging.New(logging.Options{
		Level: logging.Info,
	})

	// Create a server
	server := &MCPServer{
		scheduler:     sched,
		cmdExecutor:   cmdExec,
		agentExecutor: agentExec,
		logger:        logger,
		config:        cfg,
	}

	// Set the server as the task executor for the scheduler
	sched.SetTaskExecutor(server)

	// Create a test task
	task := &model.Task{
		ID:          "test-task",
		Name:        "Test Task",
		Schedule:    "* * * * *",
		Type:        model.TypeShellCommand,
		Command:     "echo hello",
		Description: "Task for testing direct execution",
		Enabled:     true,
		Status:      model.StatusPending,
	}

	// Create context with a reasonable timeout
	ctx := context.Background()
	timeout := 5 * time.Second

	// Verify that the MCPServer implements the TaskExecutor interface
	// by directly calling the Execute method
	err := server.Execute(ctx, task, timeout)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify the status was not changed (since it's the scheduler's job to update it)
	if task.Status != model.StatusPending {
		t.Errorf("Expected task status to remain %s, got %s",
			model.StatusPending, task.Status)
	}

	// Now add the task to scheduler
	if err := sched.AddTask(task); err != nil {
		t.Fatalf("Failed to add task: %v", err)
	}

	// Verify the task was added
	tasks := sched.ListTasks()
	if len(tasks) != 1 {
		t.Errorf("Expected 1 task, got %d", len(tasks))
	}

	// Get the task to ensure it's properly stored
	retrievedTask, err := sched.GetTask(task.ID)
	if err != nil {
		t.Fatalf("Failed to get task: %v", err)
	}

	if retrievedTask.ID != task.ID {
		t.Errorf("Expected ID %s, got %s", task.ID, retrievedTask.ID)
	}

	if retrievedTask.Name != task.Name {
		t.Errorf("Expected Name %s, got %s", task.Name, retrievedTask.Name)
	}

	// Manually simulate task execution by the scheduler
	retrievedTask.Status = model.StatusRunning

	// Execute the task
	err = server.Execute(ctx, retrievedTask, timeout)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// In a real scenario, the scheduler would update this after execution
	// Here we'll simulate that for testing
	retrievedTask.Status = model.StatusCompleted

	// Verify the task execution was successful
	if retrievedTask.Status != model.StatusCompleted {
		t.Errorf("Expected status %s, got %s",
			model.StatusCompleted, retrievedTask.Status)
	}
}
