// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jolks/mcp-cron/internal/agent"
	"github.com/jolks/mcp-cron/internal/command"
	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/jolks/mcp-cron/internal/scheduler"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// createTestServer creates a minimal MCPServer for testing
func createAITestServer(t *testing.T) *MCPServer {
	// Create a config for testing
	cfg := config.DefaultConfig()

	// Create dependencies
	sched := scheduler.NewScheduler(&cfg.Scheduler, testLogger())
	cmdExec := command.NewCommandExecutor(nil, testLogger())
	agentExec := agent.NewAgentExecutor(cfg, nil, testLogger())

	// Create a logger
	logger := logging.New(logging.Options{
		Level: logging.Info,
	})

	// Create server
	server := &MCPServer{
		scheduler:     sched,
		cmdExecutor:   cmdExec,
		agentExecutor: agentExec,
		logger:        logger,
		config:        cfg,
	}

	// Set the server as task executor (required for scheduling)
	sched.SetTaskExecutor(server)

	return server
}

// TestHandleAddAITask tests the AI task creation handler
func TestHandleAddAITask_AI(t *testing.T) {
	server := createAITestServer(t)

	// Test case 1: Valid AI task with minimum fields
	validTask := AITaskParams{
		TaskParams: TaskParams{
			Name:     "AI Test Task",
			Schedule: "*/5 * * * *",
			Enabled:  true,
		},
		Prompt: "Generate a report on system health",
	}

	// Create the request with the valid task
	validRequestJSON, _ := json.Marshal(validTask)
	validRequest := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(validRequestJSON),
		},
	}

	// Call the handler
	result, err := server.handleAddAITask(context.Background(), validRequest)
	if err != nil {
		t.Fatalf("Valid request should not fail: %v", err)
	}
	if result == nil {
		t.Fatal("Result should not be nil for valid request")
	}

	// Test case 2: Missing required fields (prompt)
	invalidTask := AITaskParams{
		TaskParams: TaskParams{
			Name:     "Invalid AI Task",
			Schedule: "*/5 * * * *",
		},
		// Missing prompt
	}

	invalidRequestJSON, _ := json.Marshal(invalidTask)
	invalidRequest := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(invalidRequestJSON),
		},
	}

	// Call the handler
	result, err = server.handleAddAITask(context.Background(), invalidRequest)
	if err == nil {
		t.Fatal("Invalid request should fail")
	}
	if result != nil {
		t.Fatal("Result should be nil for invalid request")
	}

	// Test case 3: Missing required fields (name)
	missingNameTask := AITaskParams{
		TaskParams: TaskParams{
			Schedule: "*/5 * * * *",
		},
		Prompt: "Generate a report on system health",
	}

	missingNameRequestJSON, _ := json.Marshal(missingNameTask)
	missingNameRequest := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(missingNameRequestJSON),
		},
	}

	// Call the handler
	result, err = server.handleAddAITask(context.Background(), missingNameRequest)
	if err == nil {
		t.Fatal("Request with missing name should fail")
	}
	if result != nil {
		t.Fatal("Result should be nil for request with missing name")
	}

	// Test case 4: Missing schedule creates an on-demand task (should succeed)
	onDemandTask := AITaskParams{
		TaskParams: TaskParams{
			Name: "On-Demand AI Task",
		},
		Prompt: "Generate a report on system health",
	}

	onDemandRequestJSON, _ := json.Marshal(onDemandTask)
	onDemandRequest := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(onDemandRequestJSON),
		},
	}

	// Call the handler — should succeed (on-demand task)
	result, err = server.handleAddAITask(context.Background(), onDemandRequest)
	if err != nil {
		t.Fatalf("On-demand AI task should succeed: %v", err)
	}
	if result == nil {
		t.Fatal("Result should not be nil for on-demand AI task")
	}
}

// TestUpdateAITask tests updating AI tasks
func TestUpdateAITask_AI(t *testing.T) {
	server := createAITestServer(t)

	// First, create an AI task to update
	taskID := fmt.Sprintf("task_%d", time.Now().UnixNano())
	initialTask := &model.Task{
		ID:          taskID,
		Name:        "Initial AI Task",
		Schedule:    "*/5 * * * *",
		Type:        model.TypeAI,
		Prompt:      "Initial prompt",
		Description: "Initial description",
		Enabled:     false,
		Status:      model.StatusPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Add task directly to the scheduler
	err := server.scheduler.AddTask(initialTask)
	if err != nil {
		t.Fatalf("Failed to add initial task: %v", err)
	}

	// Test case 1: Update AI task prompt
	updateParams := AITaskParams{
		TaskParams: TaskParams{
			ID: taskID,
		},
		Prompt: "Updated prompt",
	}

	updateRequestJSON, _ := json.Marshal(updateParams)
	updateRequest := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(updateRequestJSON),
		},
	}

	// Call the handler
	result, err := server.handleUpdateTask(context.Background(), updateRequest)
	if err != nil {
		t.Fatalf("Valid update should not fail: %v", err)
	}
	if result == nil {
		t.Fatal("Result should not be nil for valid update")
	}

	// Verify the task was updated
	updatedTask, err := server.scheduler.GetTask(taskID)
	if err != nil {
		t.Fatalf("Failed to get updated task: %v", err)
	}
	if updatedTask.Prompt != "Updated prompt" {
		t.Errorf("Expected prompt to be updated to 'Updated prompt', got '%s'", updatedTask.Prompt)
	}

	// Test case 2: Update multiple fields
	multiUpdateParams := AITaskParams{
		TaskParams: TaskParams{
			ID:          taskID,
			Name:        "Updated AI Task",
			Schedule:    "*/10 * * * *",
			Description: "Updated description",
			Enabled:     true,
		},
	}

	multiUpdateRequestJSON, _ := json.Marshal(multiUpdateParams)
	multiUpdateRequest := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(multiUpdateRequestJSON),
		},
	}

	// Call the handler
	result, err = server.handleUpdateTask(context.Background(), multiUpdateRequest)
	if err != nil {
		t.Fatalf("Valid multi-update should not fail: %v", err)
	}
	if result == nil {
		t.Fatal("Result should not be nil for valid multi-update")
	}

	// Verify the task was updated
	updatedTask, err = server.scheduler.GetTask(taskID)
	if err != nil {
		t.Fatalf("Failed to get multi-updated task: %v", err)
	}
	if updatedTask.Name != "Updated AI Task" {
		t.Errorf("Expected name to be updated to 'Updated AI Task', got '%s'", updatedTask.Name)
	}
	if updatedTask.Schedule != "*/10 * * * *" {
		t.Errorf("Expected schedule to be updated to '*/10 * * * *', got '%s'", updatedTask.Schedule)
	}
	if updatedTask.Description != "Updated description" {
		t.Errorf("Expected description to be updated to 'Updated description', got '%s'", updatedTask.Description)
	}
	if !updatedTask.Enabled {
		t.Error("Expected task to be enabled after update")
	}
}

// TestConvertTaskTypes tests converting between task types
func TestConvertTaskTypes_AI(t *testing.T) {
	server := createAITestServer(t)

	// Create a shell command task
	shellTaskID := fmt.Sprintf("shell_task_%d", time.Now().UnixNano())
	shellTask := &model.Task{
		ID:          shellTaskID,
		Name:        "Shell Command Task",
		Schedule:    "*/5 * * * *",
		Type:        model.TypeShellCommand,
		Command:     "echo hello",
		Description: "A shell command task",
		Enabled:     false,
		Status:      model.StatusPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Add task directly to the scheduler
	err := server.scheduler.AddTask(shellTask)
	if err != nil {
		t.Fatalf("Failed to add shell task: %v", err)
	}

	// Convert shell command task to AI task
	updateParams := AITaskParams{
		TaskParams: TaskParams{
			ID:   shellTaskID,
			Type: string(model.TypeAI),
		},
		Prompt: "New AI prompt",
	}

	updateRequestJSON, _ := json.Marshal(updateParams)
	updateRequest := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(updateRequestJSON),
		},
	}

	// Call the handler
	result, err := server.handleUpdateTask(context.Background(), updateRequest)
	if err != nil {
		t.Fatalf("Valid conversion should not fail: %v", err)
	}
	if result == nil {
		t.Fatal("Result should not be nil for valid conversion")
	}

	// Verify the task was converted
	convertedTask, err := server.scheduler.GetTask(shellTaskID)
	if err != nil {
		t.Fatalf("Failed to get converted task: %v", err)
	}
	if convertedTask.Type != model.TypeAI {
		t.Errorf("Expected type to be converted to '%s', got '%s'", model.TypeAI, convertedTask.Type)
	}
	if convertedTask.Prompt != "New AI prompt" {
		t.Errorf("Expected prompt to be set to 'New AI prompt', got '%s'", convertedTask.Prompt)
	}

	// Create an AI task
	aiTaskID := fmt.Sprintf("ai_task_%d", time.Now().UnixNano())
	aiTask := &model.Task{
		ID:          aiTaskID,
		Name:        "AI Task",
		Schedule:    "*/5 * * * *",
		Type:        model.TypeAI,
		Prompt:      "AI prompt",
		Description: "An AI task",
		Enabled:     false,
		Status:      model.StatusPending,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Add task directly to the scheduler
	err = server.scheduler.AddTask(aiTask)
	if err != nil {
		t.Fatalf("Failed to add AI task: %v", err)
	}

	// Convert AI task to shell command task
	convertParams := AITaskParams{
		TaskParams: TaskParams{
			ID:      aiTaskID,
			Type:    string(model.TypeShellCommand),
			Command: "echo converted",
		},
	}

	convertRequestJSON, _ := json.Marshal(convertParams)
	convertRequest := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(convertRequestJSON),
		},
	}

	// Call the handler
	result, err = server.handleUpdateTask(context.Background(), convertRequest)
	if err != nil {
		t.Fatalf("Valid conversion should not fail: %v", err)
	}
	if result == nil {
		t.Fatal("Result should not be nil for valid conversion")
	}

	// Verify the task was converted
	reconvertedTask, err := server.scheduler.GetTask(aiTaskID)
	if err != nil {
		t.Fatalf("Failed to get reconverted task: %v", err)
	}
	if reconvertedTask.Type != model.TypeShellCommand {
		t.Errorf("Expected type to be converted to '%s', got '%s'", model.TypeShellCommand, reconvertedTask.Type)
	}
	if reconvertedTask.Command != "echo converted" {
		t.Errorf("Expected command to be set to 'echo converted', got '%s'", reconvertedTask.Command)
	}
}
