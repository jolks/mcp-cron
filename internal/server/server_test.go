// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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

func testLogger() *logging.Logger {
	return logging.New(logging.Options{Output: io.Discard, Level: logging.Fatal})
}

// createTestSchedulerConfig creates a default scheduler config for testing
func createTestSchedulerConfig() *config.SchedulerConfig {
	return &config.SchedulerConfig{
		DefaultTimeout: 10 * time.Minute,
	}
}

func TestNewMCPServer(t *testing.T) {
	// Create a config
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address:       "localhost",
			Port:          8080,
			TransportMode: "sse",
		},
		Scheduler: *createTestSchedulerConfig(),
	}

	// Create dependencies
	sched := scheduler.NewScheduler(&cfg.Scheduler, testLogger())
	exec := command.NewCommandExecutor(nil, testLogger())
	agentExec := agent.NewAgentExecutor(cfg, nil, testLogger())

	server, err := NewMCPServer(cfg, sched, exec, agentExec, nil, testLogger())
	if err != nil {
		t.Fatalf("Failed to create server with default config: %v", err)
	}

	if server == nil {
		t.Fatal("NewMCPServer returned nil server")
	}

	// Check values directly
	if server.address != "localhost" {
		t.Errorf("Expected address localhost, got %s", server.address)
	}

	if server.port != 8080 {
		t.Errorf("Expected port 8080, got %d", server.port)
	}
}

func TestNewMCPServerWithCustomConfig(t *testing.T) {
	// Create a config
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address:       "127.0.0.1",
			Port:          9090,
			TransportMode: "sse",
		},
		Scheduler: *createTestSchedulerConfig(),
	}

	// Create dependencies
	sched := scheduler.NewScheduler(&cfg.Scheduler, testLogger())
	exec := command.NewCommandExecutor(nil, testLogger())
	agentExec := agent.NewAgentExecutor(cfg, nil, testLogger())

	server, err := NewMCPServer(cfg, sched, exec, agentExec, nil, testLogger())
	if err != nil {
		t.Fatalf("Failed to create server with custom config: %v", err)
	}

	if server.address != "127.0.0.1" {
		t.Errorf("Expected address 127.0.0.1, got %s", server.address)
	}

	if server.port != 9090 {
		t.Errorf("Expected port 9090, got %d", server.port)
	}
}

func TestMCPServerStartStop(t *testing.T) {
	// Skip in CI environment as this starts a real server
	if _, exists := os.LookupEnv("CI"); exists {
		t.Skip("Skipping test in CI environment")
	}

	// Create a config
	cfg := &config.Config{
		Server: config.ServerConfig{
			TransportMode: "stdio", // Use stdio to avoid network binding
		},
		Scheduler: *createTestSchedulerConfig(),
	}

	// Create dependencies
	sched := scheduler.NewScheduler(&cfg.Scheduler, testLogger())
	exec := command.NewCommandExecutor(nil, testLogger())
	agentExec := agent.NewAgentExecutor(cfg, nil, testLogger())

	server, err := NewMCPServer(cfg, sched, exec, agentExec, nil, testLogger())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Create a context that we can cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start the server
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}

	// Wait a short time to let server initialize
	time.Sleep(100 * time.Millisecond)

	// Stop the server
	if err := server.Stop(); err != nil {
		t.Fatalf("Failed to stop server: %v", err)
	}
}

// TestTaskHandlers is a simple smoke test for the handlers
// Note: For a complete test, you would need to mock the protocol package
// which is beyond the scope of this basic test suite
func TestTaskHandlers(t *testing.T) {
	// This is a simple existence test for the handlers
	// Full testing of handlers would require significant mocking of the MCP protocol

	// Create a config
	cfg := &config.Config{
		Server: config.ServerConfig{
			TransportMode: "stdio", // Use stdio to avoid network binding
		},
		Scheduler: *createTestSchedulerConfig(),
	}

	// Create dependencies
	sched := scheduler.NewScheduler(&cfg.Scheduler, testLogger())
	exec := command.NewCommandExecutor(nil, testLogger())
	agentExec := agent.NewAgentExecutor(cfg, nil, testLogger())

	server, err := NewMCPServer(cfg, sched, exec, agentExec, nil, testLogger())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Verify that the server has registered tools by checking reflection
	if server.server == nil {
		t.Fatal("Server MCP server instance is nil")
	}
}

// TestTaskCreationTimeFields verifies that LastRun and NextRun are properly initialized
func TestTaskCreationTimeFields(t *testing.T) {
	// Create a config
	cfg := &config.Config{
		Server: config.ServerConfig{
			TransportMode: "stdio",
		},
		Scheduler: *createTestSchedulerConfig(),
	}

	// Create a scheduler
	sched := scheduler.NewScheduler(&cfg.Scheduler, testLogger())

	// Start the scheduler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	exec := command.NewCommandExecutor(nil, testLogger())
	agentExec := agent.NewAgentExecutor(cfg, nil, testLogger())

	server, err := NewMCPServer(cfg, sched, exec, agentExec, nil, testLogger())
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Start the scheduler after setting the task executor
	sched.Start(ctx)

	beforeTime := time.Now().Add(-time.Second)

	// Create a task using a mock request
	mockRequest := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(`{
			"name": "Test Task",
			"schedule": "* * * * * *",
			"command": "echo test",
			"description": "Test task description",
			"enabled": true
		}`),
		},
	}

	result, err := server.handleAddTask(context.Background(), mockRequest)
	if err != nil {
		t.Fatalf("handleAddTask failed: %v", err)
	}

	// Verify that a result was returned
	if result == nil {
		t.Fatal("handleAddTask returned nil result")
	}

	// Give the scheduler a moment to update NextRun if needed
	time.Sleep(100 * time.Millisecond)

	// Verify that a task was created
	tasks := sched.ListTasks()
	if len(tasks) != 1 {
		t.Fatalf("Expected 1 task, but got %d", len(tasks))
	}

	task := tasks[0]

	// Verify that LastRun and NextRun are properly initialized
	if task.LastRun.IsZero() {
		t.Error("Expected LastRun to be initialized, but it's zero")
	}

	if task.NextRun.IsZero() {
		t.Error("Expected NextRun to be initialized, but it's zero")
	}

	// Verify that LastRun and NextRun are set to a reasonable time (after beforeTime)
	if task.LastRun.Before(beforeTime) {
		t.Errorf("Expected LastRun to be after %v, but got %v", beforeTime, task.LastRun)
	}

	if task.NextRun.Before(beforeTime) {
		t.Errorf("Expected NextRun to be after %v, but got %v", beforeTime, task.NextRun)
	}
}

// TestLogFilePath verifies that the log file path is set correctly
func TestLogFilePath(t *testing.T) {
	// Get the current executable path
	execPath, err := os.Executable()
	if err != nil {
		t.Fatalf("Failed to get executable path: %v", err)
	}

	// Get the directory containing the executable
	expectedDir := filepath.Dir(execPath)

	// Create a config
	cfg := &config.Config{
		Server: config.ServerConfig{
			TransportMode: "stdio",
		},
		Scheduler: *createTestSchedulerConfig(),
	}

	// Create a test logger to capture log messages
	var logBuffer bytes.Buffer
	// Save the original output
	originalOutput := log.Writer()
	// Set our buffer as output
	log.SetOutput(&logBuffer)
	// Restore the original output when test completes
	defer log.SetOutput(originalOutput)

	// Set up a mock for os.OpenFile
	originalOpenFile := osOpenFile
	defer func() { osOpenFile = originalOpenFile }()

	var capturedLogPath string
	osOpenFile = func(name string, flag int, perm os.FileMode) (*os.File, error) {
		capturedLogPath = name
		// Return a temporary file instead of actually opening the log file
		return os.CreateTemp("", "test-log-*")
	}

	// Define server name for the test
	serverName := "mcp-cron"

	_, err = CreateLogger(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	// Expected log file name uses the server name
	expectedLogPath := filepath.Join(expectedDir, serverName+".log")

	// Verify that the log path is set to be in the same directory as the executable
	if capturedLogPath != expectedLogPath {
		t.Errorf("Expected log path %s, got %s", expectedLogPath, capturedLogPath)
	}
}

// TestTaskTypeHandling verifies that task types are correctly handled during creation and update
func TestTaskTypeHandling(t *testing.T) {
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

	// Test 1: Create a task with default type (shell_command)
	defaultTypeRequest := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(`{
			"name": "Default Type Task",
			"schedule": "* * * * *",
			"command": "echo default",
			"description": "Task with default type",
			"enabled": false
		}`),
		},
	}

	result, err := server.handleAddTask(context.Background(), defaultTypeRequest)
	if err != nil {
		t.Fatalf("handleAddTask failed for default type: %v", err)
	}
	if result == nil {
		t.Fatal("handleAddTask returned nil result for default type")
	}

	// Test 2: Create a task with explicit AI type (case insensitive)
	aiTypeRequest := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(`{
			"name": "AI Type Task",
			"schedule": "* * * * *",
			"prompt": "prompt for AI",
			"description": "Task with AI type",
			"type": "ai",
			"enabled": false
		}`),
		},
	}

	result, err = server.handleAddAITask(context.Background(), aiTypeRequest)
	if err != nil {
		t.Fatalf("handleAddAITask failed for AI type: %v", err)
	}
	if result == nil {
		t.Fatal("handleAddAITask returned nil result for AI type")
	}

	// Test 3: Create a task with explicit AI type (different case)
	aiTypeUpperRequest := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(`{
			"name": "AI Type Upper Task",
			"schedule": "* * * * *",
			"prompt": "prompt for AI",
			"description": "Task with AI type uppercase",
			"type": "AI",
			"enabled": false
		}`),
		},
	}

	result, err = server.handleAddAITask(context.Background(), aiTypeUpperRequest)
	if err != nil {
		t.Fatalf("handleAddAITask failed for AI uppercase type: %v", err)
	}
	if result == nil {
		t.Fatal("handleAddAITask returned nil result for AI uppercase type")
	}

	// Verify tasks were created with correct types
	tasks := sched.ListTasks()
	if len(tasks) != 3 {
		t.Fatalf("Expected 3 tasks, but got %d", len(tasks))
	}

	// Check types
	var defaultTypeTask, aiLowerTypeTask, aiUpperTypeTask *model.Task
	for _, task := range tasks {
		switch task.Name {
		case "Default Type Task":
			defaultTypeTask = task
		case "AI Type Task":
			aiLowerTypeTask = task
		case "AI Type Upper Task":
			aiUpperTypeTask = task
		}
	}

	// Verify default type
	if defaultTypeTask == nil {
		t.Fatal("Default type task not found")
	}
	if defaultTypeTask.Type != model.TypeShellCommand {
		t.Errorf("Expected default type to be %s, got %s", model.TypeShellCommand, defaultTypeTask.Type)
	}

	// Verify lowercase AI type
	if aiLowerTypeTask == nil {
		t.Fatal("AI lowercase type task not found")
	}
	if aiLowerTypeTask.Type != model.TypeAI {
		t.Errorf("Expected lowercase AI type to be %s, got %s", model.TypeAI, aiLowerTypeTask.Type)
	}

	// Verify uppercase AI type
	if aiUpperTypeTask == nil {
		t.Fatal("AI uppercase type task not found")
	}
	if aiUpperTypeTask.Type != model.TypeAI {
		t.Errorf("Expected uppercase AI type to be %s, got %s", model.TypeAI, aiUpperTypeTask.Type)
	}

	// Test 4: Update task type from shell_command to AI
	updateTypeRequest := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(fmt.Sprintf(`{
			"id": "%s",
			"type": "ai"
		}`, defaultTypeTask.ID)),
		},
	}

	result, err = server.handleUpdateTask(context.Background(), updateTypeRequest)
	if err != nil {
		t.Fatalf("handleUpdateTask failed for type update: %v", err)
	}
	if result == nil {
		t.Fatal("handleUpdateTask returned nil result for type update")
	}

	// Verify the task type was updated
	updatedTask, err := sched.GetTask(defaultTypeTask.ID)
	if err != nil {
		t.Fatalf("Failed to get updated task: %v", err)
	}
	if updatedTask.Type != model.TypeAI {
		t.Errorf("Expected updated type to be %s, got %s", model.TypeAI, updatedTask.Type)
	}

	// Test 5: Update task type from AI to shell_command
	updateTypeBackRequest := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(fmt.Sprintf(`{
			"id": "%s",
			"type": "shell_command"
		}`, aiLowerTypeTask.ID)),
		},
	}

	result, err = server.handleUpdateTask(context.Background(), updateTypeBackRequest)
	if err != nil {
		t.Fatalf("handleUpdateTask failed for reverse type update: %v", err)
	}
	if result == nil {
		t.Fatal("handleUpdateTask returned nil result for reverse type update")
	}

	// Verify the task type was updated back
	updatedBackTask, err := sched.GetTask(aiLowerTypeTask.ID)
	if err != nil {
		t.Fatalf("Failed to get task updated back: %v", err)
	}
	if updatedBackTask.Type != model.TypeShellCommand {
		t.Errorf("Expected updated back type to be %s, got %s", model.TypeShellCommand, updatedBackTask.Type)
	}
}
