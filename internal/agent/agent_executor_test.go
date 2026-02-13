// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/model"
)

// TestAgentExecutor is a test structure with a mockable RunTask function
type TestAgentExecutor struct {
	*AgentExecutor
	mockRunTask func(ctx context.Context, t *model.Task, cfg *config.Config) (string, error)
}

// NewTestAgentExecutor creates a test executor with a mockable RunTask function
func NewTestAgentExecutor(mockFunc func(ctx context.Context, t *model.Task, cfg *config.Config) (string, error)) *TestAgentExecutor {
	// Create a default config for testing
	cfg := config.DefaultConfig()

	return &TestAgentExecutor{
		AgentExecutor: NewAgentExecutor(cfg, nil),
		mockRunTask:   mockFunc,
	}
}

// ExecuteAgentTask overrides the base implementation to use the mock function
func (tae *TestAgentExecutor) ExecuteAgentTask(
	ctx context.Context,
	taskID string,
	prompt string,
	timeout time.Duration,
) *model.Result {
	result := &model.Result{
		StartTime: time.Now(),
		TaskID:    taskID,
		Prompt:    prompt,
	}

	// Store the result
	tae.mu.Lock()
	tae.results[taskID] = result
	tae.mu.Unlock()

	// Create a context with timeout
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Create a task structure for RunTask
	task := &model.Task{
		ID:     taskID,
		Prompt: prompt,
	}

	// Execute the task using the mock function
	output, err := tae.mockRunTask(execCtx, task, tae.config)

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

	return result
}

func TestExecuteAgentTask(t *testing.T) {
	// Create mock output
	mockOutput := "This is a test result from the AI agent"

	// Create test executor with mock function
	executor := NewTestAgentExecutor(func(ctx context.Context, task *model.Task, cfg *config.Config) (string, error) {
		// Verify that the task has the expected fields
		if task.ID != "test-task-id" {
			t.Errorf("Expected task ID 'test-task-id', got '%s'", task.ID)
		}
		// Return mock output
		return mockOutput, nil
	})

	// Set up test parameters
	ctx := context.Background()
	taskID := "test-task-id"
	prompt := "Test prompt for the AI agent"
	timeout := 5 * time.Second

	// Execute the agent task
	result := executor.ExecuteAgentTask(ctx, taskID, prompt, timeout)

	// Verify result
	if result.TaskID != taskID {
		t.Errorf("Expected TaskID %s, got %s", taskID, result.TaskID)
	}
	if result.ExitCode != 0 {
		t.Errorf("Expected ExitCode 0, got %d", result.ExitCode)
	}
	if result.Error != "" {
		t.Errorf("Expected empty Error, got %s", result.Error)
	}
	if result.Output != mockOutput {
		t.Errorf("Expected Output '%s', got '%s'", mockOutput, result.Output)
	}

	// Verify GetTaskResult
	storedResult, exists := executor.GetTaskResult(taskID)
	if !exists {
		t.Error("Task result not stored in executor")
	}
	if storedResult != result {
		t.Error("Stored result is not the same as returned result")
	}
}

func TestExecuteAgentTaskWithError(t *testing.T) {
	// Create a mock error
	mockError := "test error from AI agent"

	// Create test executor with mock function that returns an error
	executor := NewTestAgentExecutor(func(ctx context.Context, task *model.Task, cfg *config.Config) (string, error) {
		return "", errors.New(mockError)
	})

	// Set up test parameters
	ctx := context.Background()
	taskID := "test-task-error"
	prompt := "Test prompt for the AI agent"
	timeout := 5 * time.Second

	// Execute the agent task
	result := executor.ExecuteAgentTask(ctx, taskID, prompt, timeout)

	// Verify result shows the error
	if result.ExitCode != 1 {
		t.Errorf("Expected ExitCode 1, got %d", result.ExitCode)
	}
	if result.Error != mockError {
		t.Errorf("Expected Error '%s', got '%s'", mockError, result.Error)
	}
}

// Execute overrides the base implementation for testing
func (tae *TestAgentExecutor) Execute(ctx context.Context, task *model.Task, timeout time.Duration) error {
	if task.ID == "" || task.Prompt == "" {
		return fmt.Errorf("invalid task: missing ID or Prompt")
	}

	// Use our overridden ExecuteAgentTask
	result := tae.ExecuteAgentTask(ctx, task.ID, task.Prompt, timeout)
	if result.Error != "" {
		return fmt.Errorf("%s", result.Error)
	}

	return nil
}

func TestExecute(t *testing.T) {
	// Create mock output
	mockOutput := "Test output from Execute"

	// Create test executor with mock function
	testExecutor := NewTestAgentExecutor(func(ctx context.Context, task *model.Task, cfg *config.Config) (string, error) {
		return mockOutput, nil
	})

	// Set up test parameters
	ctx := context.Background()
	task := &model.Task{
		ID:     "test-execute-task",
		Type:   model.TypeAI.String(),
		Prompt: "Test prompt for execution",
	}
	timeout := 5 * time.Second

	// Execute the task using our test executor's Execute method
	err := testExecutor.Execute(ctx, task, timeout)

	// Verify execution was successful
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	// Verify result was stored
	result, exists := testExecutor.GetTaskResult(task.ID)
	if !exists {
		t.Error("Result not stored after Execute")
	}
	if result.Output != mockOutput {
		t.Errorf("Expected output '%s', got '%s'", mockOutput, result.Output)
	}
}

// TestRunTaskIntegration updates the integration test to use config
func TestRunTaskIntegration(t *testing.T) {
	// Skip by default to avoid making actual API calls during routine testing
	if os.Getenv("MCP_CRON_ENABLE_OPENAI_TESTS") != "true" {
		t.Skip("Skipping OpenAI integration test. Set MCP_CRON_ENABLE_OPENAI_TESTS=true to run.")
	}

	// Create a default config for testing
	cfg := config.DefaultConfig()

	// Set the API key from environment for the test
	cfg.AI.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
	if cfg.AI.OpenAIAPIKey == "" {
		t.Skip("OPENAI_API_KEY environment variable not set")
	}

	// Create a simple task
	task := &model.Task{
		ID:     "integration-test",
		Prompt: "What is 2+2? Answer with just the number",
	}

	// Run the task
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := RunTask(ctx, task, cfg)
	if err != nil {
		t.Fatalf("RunTask failed: %v", err)
	}

	// Verify we got some kind of response
	if output == "" {
		t.Error("Expected non-empty output from OpenAI API")
	}

	t.Logf("OpenAI API response: %s", output)
}

func TestRunTaskIntegrationAnthropic(t *testing.T) {
	// Skip by default to avoid making actual API calls during routine testing
	if os.Getenv("MCP_CRON_ENABLE_ANTHROPIC_TESTS") != "true" {
		t.Skip("Skipping Anthropic integration test. Set MCP_CRON_ENABLE_ANTHROPIC_TESTS=true to run.")
	}

	// Create a default config for testing
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "anthropic"
	cfg.AI.Model = "claude-sonnet-4-5-20250929"

	// Set the API key from environment for the test
	cfg.AI.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	if cfg.AI.AnthropicAPIKey == "" {
		t.Skip("ANTHROPIC_API_KEY environment variable not set")
	}

	// Create a simple task
	task := &model.Task{
		ID:     "integration-test-anthropic",
		Prompt: "What is 2+2? Answer with just the number",
	}

	// Run the task
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := RunTask(ctx, task, cfg)
	if err != nil {
		t.Fatalf("RunTask failed: %v", err)
	}

	// Verify we got some kind of response
	if output == "" {
		t.Error("Expected non-empty output from Anthropic API")
	}

	t.Logf("Anthropic API response: %s", output)
}

func TestRunTaskIntegrationListToolsOpenAI(t *testing.T) {
	if os.Getenv("MCP_CRON_ENABLE_OPENAI_TESTS") != "true" {
		t.Skip("Skipping OpenAI list-tools integration test. Set MCP_CRON_ENABLE_OPENAI_TESTS=true to run.")
	}

	cfg := config.DefaultConfig()
	cfg.AI.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
	if cfg.AI.OpenAIAPIKey == "" {
		t.Skip("OPENAI_API_KEY environment variable not set")
	}

	// Verify MCP tools are actually available
	tools, _, err := buildToolsFromConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to build tools: %v", err)
	}
	if len(tools) == 0 {
		t.Skip("No MCP tools available — cannot test tool visibility")
	}
	t.Logf("Loaded %d MCP tools", len(tools))

	task := &model.Task{
		ID:     "integration-test-openai-list-tools",
		Prompt: "List all the tools you have available. Just list their names, one per line. Do not call any tool.",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := RunTask(ctx, task, cfg)
	if err != nil {
		t.Fatalf("RunTask failed: %v", err)
	}

	if output == "" {
		t.Fatal("Expected non-empty output")
	}

	// Verify the AI mentions at least some of the actually loaded tools
	matched := 0
	for _, td := range tools {
		if strings.Contains(output, td.Name) {
			matched++
		}
	}
	if matched == 0 {
		t.Errorf("Expected output to mention at least one loaded tool, but none matched")
	}
	t.Logf("AI mentioned %d/%d loaded tools", matched, len(tools))

	t.Logf("OpenAI listed tools:\n%s", output)
}

func TestRunTaskIntegrationListToolsAnthropic(t *testing.T) {
	if os.Getenv("MCP_CRON_ENABLE_ANTHROPIC_TESTS") != "true" {
		t.Skip("Skipping Anthropic list-tools integration test. Set MCP_CRON_ENABLE_ANTHROPIC_TESTS=true to run.")
	}

	cfg := config.DefaultConfig()
	cfg.AI.Provider = "anthropic"
	cfg.AI.Model = "claude-sonnet-4-5-20250929"
	cfg.AI.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	if cfg.AI.AnthropicAPIKey == "" {
		t.Skip("ANTHROPIC_API_KEY environment variable not set")
	}

	// Verify MCP tools are actually available
	tools, _, err := buildToolsFromConfig(cfg)
	if err != nil {
		t.Fatalf("Failed to build tools: %v", err)
	}
	if len(tools) == 0 {
		t.Skip("No MCP tools available — cannot test tool visibility")
	}
	t.Logf("Loaded %d MCP tools", len(tools))

	task := &model.Task{
		ID:     "integration-test-anthropic-list-tools",
		Prompt: "List all the tools you have available. Just list their names, one per line. Do not call any tool.",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	output, err := RunTask(ctx, task, cfg)
	if err != nil {
		t.Fatalf("RunTask failed: %v", err)
	}

	if output == "" {
		t.Fatal("Expected non-empty output")
	}

	// Verify the AI mentions at least some of the actually loaded tools
	matched := 0
	for _, td := range tools {
		if strings.Contains(output, td.Name) {
			matched++
		}
	}
	if matched == 0 {
		t.Errorf("Expected output to mention at least one loaded tool, but none matched")
	}
	t.Logf("AI mentioned %d/%d loaded tools", matched, len(tools))

	t.Logf("Anthropic listed tools:\n%s", output)
}
