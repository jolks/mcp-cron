// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"context"
	"encoding/json"
	"os"
	"strings"
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

// createIntegrationTestServer creates an MCPServer with a started scheduler and real executors.
func createIntegrationTestServer(t *testing.T) (*MCPServer, context.CancelFunc) {
	t.Helper()

	cfg := config.DefaultConfig()

	sched := scheduler.NewScheduler(&cfg.Scheduler)
	cmdExec := command.NewCommandExecutor(nil)
	agentExec := agent.NewAgentExecutor(cfg, nil)

	logger := logging.New(logging.Options{
		Level: logging.Info,
	})

	srv := &MCPServer{
		scheduler:     sched,
		cmdExecutor:   cmdExec,
		agentExecutor: agentExec,
		logger:        logger,
		config:        cfg,
	}

	sched.SetTaskExecutor(srv)

	ctx, cancel := context.WithCancel(context.Background())
	sched.Start(ctx)

	t.Cleanup(func() {
		cancel()
	})

	return srv, cancel
}

// makeRequest marshals args into a *mcp.CallToolRequest.
func makeRequest(t *testing.T, args interface{}) *mcp.CallToolRequest {
	t.Helper()
	data, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("failed to marshal request args: %v", err)
	}
	return &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: json.RawMessage(data),
		},
	}
}

// parseResponse extracts TextContent from a CallToolResult and unmarshals it into dest.
func parseResponse(t *testing.T, result *mcp.CallToolResult, dest interface{}) {
	t.Helper()
	if result == nil {
		t.Fatal("result is nil")
	}
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", result.Content[0])
	}
	if err := json.Unmarshal([]byte(tc.Text), dest); err != nil {
		t.Fatalf("failed to unmarshal response: %v\nraw: %s", err, tc.Text)
	}
}

// TestIntegration_ShellCommandFullLifecycle exercises the full shell command workflow:
// add → list → get → execute → get_task_result
func TestIntegration_ShellCommandFullLifecycle(t *testing.T) {
	srv, _ := createIntegrationTestServer(t)
	ctx := context.Background()

	// 1. Add a shell command task (disabled, never-fire schedule)
	addReq := makeRequest(t, TaskParams{
		Name:     "echo-test",
		Schedule: "0 0 1 1 *",
		Command:  "echo hello",
		Enabled:  false,
	})
	addResult, err := srv.handleAddTask(ctx, addReq)
	if err != nil {
		t.Fatalf("handleAddTask failed: %v", err)
	}

	var task model.Task
	parseResponse(t, addResult, &task)

	if task.ID == "" {
		t.Fatal("expected non-empty task ID")
	}
	if task.Type != model.TypeShellCommand.String() {
		t.Errorf("expected type %s, got %s", model.TypeShellCommand, task.Type)
	}

	// 2. List tasks — should contain exactly 1
	listResult, err := srv.handleListTasks(ctx, nil)
	if err != nil {
		t.Fatalf("handleListTasks failed: %v", err)
	}
	var tasks []*model.Task
	parseResponse(t, listResult, &tasks)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	// 3. Get task by ID
	getReq := makeRequest(t, TaskIDParams{ID: task.ID})
	getResult, err := srv.handleGetTask(ctx, getReq)
	if err != nil {
		t.Fatalf("handleGetTask failed: %v", err)
	}
	var gotTask model.Task
	parseResponse(t, getResult, &gotTask)
	if gotTask.ID != task.ID {
		t.Errorf("expected ID %s, got %s", task.ID, gotTask.ID)
	}
	if gotTask.Command != "echo hello" {
		t.Errorf("expected command 'echo hello', got %q", gotTask.Command)
	}

	// 4. Execute manually
	execTask, _ := srv.scheduler.GetTask(task.ID)
	if err := srv.Execute(ctx, execTask, 10*time.Second); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// 5. Get result
	resultReq := makeRequest(t, TaskResultParams{ID: task.ID})
	resultResult, err := srv.handleGetTaskResult(ctx, resultReq)
	if err != nil {
		t.Fatalf("handleGetTaskResult failed: %v", err)
	}
	var result model.Result
	parseResponse(t, resultResult, &result)

	if result.ExitCode != 0 {
		t.Errorf("expected exit_code 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Output, "hello") {
		t.Errorf("expected output to contain 'hello', got %q", result.Output)
	}
}

// TestIntegration_AITaskFullLifecycle exercises the full AI task workflow:
// add_ai_task → list → get → update → execute → get_task_result → remove → verify removed
func TestIntegration_AITaskFullLifecycle(t *testing.T) {
	srv, _ := createIntegrationTestServer(t)
	ctx := context.Background()

	// 1. Add AI task
	addReq := makeRequest(t, AITaskParams{
		TaskParams: TaskParams{
			Name:     "ai-math-test",
			Schedule: "0 0 1 1 *",
			Enabled:  false,
		},
		Prompt: "What is 1+1? Answer with just the number",
	})
	addResult, err := srv.handleAddAITask(ctx, addReq)
	if err != nil {
		t.Fatalf("handleAddAITask failed: %v", err)
	}
	var task model.Task
	parseResponse(t, addResult, &task)

	if task.Type != model.TypeAI.String() {
		t.Errorf("expected type %s, got %s", model.TypeAI, task.Type)
	}

	// 2. List
	listResult, err := srv.handleListTasks(ctx, nil)
	if err != nil {
		t.Fatalf("handleListTasks failed: %v", err)
	}
	var tasks []*model.Task
	parseResponse(t, listResult, &tasks)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	// 3. Get
	getReq := makeRequest(t, TaskIDParams{ID: task.ID})
	getResult, err := srv.handleGetTask(ctx, getReq)
	if err != nil {
		t.Fatalf("handleGetTask failed: %v", err)
	}
	var gotTask model.Task
	parseResponse(t, getResult, &gotTask)
	if gotTask.Prompt != "What is 1+1? Answer with just the number" {
		t.Errorf("unexpected prompt: %q", gotTask.Prompt)
	}

	// 4. Update description
	updateReq := makeRequest(t, AITaskParams{
		TaskParams: TaskParams{
			ID:          task.ID,
			Description: "Updated description for AI test",
		},
	})
	updateResult, err := srv.handleUpdateTask(ctx, updateReq)
	if err != nil {
		t.Fatalf("handleUpdateTask failed: %v", err)
	}
	var updatedTask model.Task
	parseResponse(t, updateResult, &updatedTask)
	if updatedTask.Description != "Updated description for AI test" {
		t.Errorf("expected updated description, got %q", updatedTask.Description)
	}

	// 5. Execute AI task (only if env var is set)
	openaiEnabled := os.Getenv("MCP_CRON_ENABLE_OPENAI_TESTS") == "true"
	anthropicEnabled := os.Getenv("MCP_CRON_ENABLE_ANTHROPIC_TESTS") == "true"

	if openaiEnabled || anthropicEnabled {
		if anthropicEnabled {
			srv.config.AI.Provider = "anthropic"
			srv.config.AI.Model = "claude-sonnet-4-5-20250929"
			srv.config.AI.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
		} else {
			srv.config.AI.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
		}

		execTask, _ := srv.scheduler.GetTask(task.ID)
		if err := srv.Execute(ctx, execTask, 30*time.Second); err != nil {
			t.Fatalf("Execute AI task failed: %v", err)
		}

		resultReq := makeRequest(t, TaskResultParams{ID: task.ID})
		resultResult, err := srv.handleGetTaskResult(ctx, resultReq)
		if err != nil {
			t.Fatalf("handleGetTaskResult failed: %v", err)
		}
		var result model.Result
		parseResponse(t, resultResult, &result)

		if result.ExitCode != 0 {
			t.Errorf("expected exit_code 0, got %d (error: %s)", result.ExitCode, result.Error)
		}
		if !strings.Contains(result.Output, "2") {
			t.Errorf("expected output to contain '2', got %q", result.Output)
		}
	} else {
		t.Log("Skipping AI execution — set MCP_CRON_ENABLE_OPENAI_TESTS=true or MCP_CRON_ENABLE_ANTHROPIC_TESTS=true to run")
	}

	// 6. Remove
	removeReq := makeRequest(t, TaskIDParams{ID: task.ID})
	_, err = srv.handleRemoveTask(ctx, removeReq)
	if err != nil {
		t.Fatalf("handleRemoveTask failed: %v", err)
	}

	// 7. Verify removed
	_, err = srv.handleGetTask(ctx, getReq)
	if err == nil {
		t.Fatal("expected error after removing task, got nil")
	}
}

// TestIntegration_EnableDisableFlow tests enable/disable toggling and idempotency.
func TestIntegration_EnableDisableFlow(t *testing.T) {
	srv, _ := createIntegrationTestServer(t)
	ctx := context.Background()

	// Add a disabled task
	addReq := makeRequest(t, TaskParams{
		Name:     "toggle-test",
		Schedule: "0 0 1 1 *",
		Command:  "echo toggle",
		Enabled:  false,
	})
	addResult, err := srv.handleAddTask(ctx, addReq)
	if err != nil {
		t.Fatalf("handleAddTask failed: %v", err)
	}
	var task model.Task
	parseResponse(t, addResult, &task)

	idReq := makeRequest(t, TaskIDParams{ID: task.ID})

	// Enable
	enableResult, err := srv.handleEnableTask(ctx, idReq)
	if err != nil {
		t.Fatalf("handleEnableTask failed: %v", err)
	}
	var enabled model.Task
	parseResponse(t, enableResult, &enabled)
	if !enabled.Enabled {
		t.Error("expected task to be enabled")
	}

	// Disable
	disableResult, err := srv.handleDisableTask(ctx, idReq)
	if err != nil {
		t.Fatalf("handleDisableTask failed: %v", err)
	}
	var disabled model.Task
	parseResponse(t, disableResult, &disabled)
	if disabled.Enabled {
		t.Error("expected task to be disabled")
	}
	if disabled.Status != model.StatusDisabled {
		t.Errorf("expected status %s, got %s", model.StatusDisabled, disabled.Status)
	}

	// Re-enable (idempotent path: enable an already-disabled task)
	enableResult2, err := srv.handleEnableTask(ctx, idReq)
	if err != nil {
		t.Fatalf("handleEnableTask (re-enable) failed: %v", err)
	}
	var reEnabled model.Task
	parseResponse(t, enableResult2, &reEnabled)
	if !reEnabled.Enabled {
		t.Error("expected task to be re-enabled")
	}

	// Re-disable
	disableResult2, err := srv.handleDisableTask(ctx, idReq)
	if err != nil {
		t.Fatalf("handleDisableTask (re-disable) failed: %v", err)
	}
	var reDisabled model.Task
	parseResponse(t, disableResult2, &reDisabled)
	if reDisabled.Enabled {
		t.Error("expected task to be re-disabled")
	}
}

// TestIntegration_ShellCommandFailure verifies that a failing command produces the correct error result.
func TestIntegration_ShellCommandFailure(t *testing.T) {
	srv, _ := createIntegrationTestServer(t)
	ctx := context.Background()

	// Add a task that will fail
	addReq := makeRequest(t, TaskParams{
		Name:     "fail-test",
		Schedule: "0 0 1 1 *",
		Command:  "exit 1",
		Enabled:  false,
	})
	addResult, err := srv.handleAddTask(ctx, addReq)
	if err != nil {
		t.Fatalf("handleAddTask failed: %v", err)
	}
	var task model.Task
	parseResponse(t, addResult, &task)

	// Execute
	execTask, _ := srv.scheduler.GetTask(task.ID)
	if err := srv.Execute(ctx, execTask, 10*time.Second); err != nil {
		// Execute may return an error for failed commands — that's expected
		t.Logf("Execute returned error (expected): %v", err)
	}

	// Get result
	resultReq := makeRequest(t, TaskResultParams{ID: task.ID})
	resultResult, err := srv.handleGetTaskResult(ctx, resultReq)
	if err != nil {
		t.Fatalf("handleGetTaskResult failed: %v", err)
	}
	var result model.Result
	parseResponse(t, resultResult, &result)

	if result.ExitCode == 0 {
		t.Error("expected non-zero exit_code for failed command")
	}
}

// TestIntegration_ErrorCases tests error handling for invalid requests.
func TestIntegration_ErrorCases(t *testing.T) {
	srv, _ := createIntegrationTestServer(t)
	ctx := context.Background()

	tests := []struct {
		name    string
		handler func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args    interface{}
	}{
		{
			name:    "add_task missing name",
			handler: srv.handleAddTask,
			args:    TaskParams{Schedule: "* * * * *", Command: "echo hi"},
		},
		{
			name:    "add_task missing schedule",
			handler: srv.handleAddTask,
			args:    TaskParams{Name: "test", Command: "echo hi"},
		},
		{
			name:    "add_task missing command",
			handler: srv.handleAddTask,
			args:    TaskParams{Name: "test", Schedule: "* * * * *"},
		},
		{
			name:    "add_ai_task missing prompt",
			handler: srv.handleAddAITask,
			args:    AITaskParams{TaskParams: TaskParams{Name: "test", Schedule: "* * * * *"}},
		},
		{
			name:    "get_task not found",
			handler: srv.handleGetTask,
			args:    TaskIDParams{ID: "nonexistent-id"},
		},
		{
			name:    "get_task missing id",
			handler: srv.handleGetTask,
			args:    TaskIDParams{},
		},
		{
			name:    "remove_task not found",
			handler: srv.handleRemoveTask,
			args:    TaskIDParams{ID: "nonexistent-id"},
		},
		{
			name:    "enable_task not found",
			handler: srv.handleEnableTask,
			args:    TaskIDParams{ID: "nonexistent-id"},
		},
		{
			name:    "disable_task not found",
			handler: srv.handleDisableTask,
			args:    TaskIDParams{ID: "nonexistent-id"},
		},
		{
			name:    "update_task missing id",
			handler: srv.handleUpdateTask,
			args:    AITaskParams{TaskParams: TaskParams{Name: "updated"}},
		},
		{
			name:    "get_task_result not found",
			handler: srv.handleGetTaskResult,
			args:    TaskResultParams{ID: "nonexistent-id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := makeRequest(t, tt.args)
			result, err := tt.handler(ctx, req)
			if err == nil {
				t.Error("expected error, got nil")
			}
			if result != nil {
				t.Error("expected nil result on error")
			}
		})
	}
}

// TestIntegration_MultipleTasksIsolation verifies that executing one task does not affect others' results.
func TestIntegration_MultipleTasksIsolation(t *testing.T) {
	srv, _ := createIntegrationTestServer(t)
	ctx := context.Background()

	// Add 3 tasks
	taskIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		addReq := makeRequest(t, TaskParams{
			Name:     "isolation-test-" + string(rune('A'+i)),
			Schedule: "0 0 1 1 *",
			Command:  "echo task-" + string(rune('A'+i)),
			Enabled:  false,
		})
		addResult, err := srv.handleAddTask(ctx, addReq)
		if err != nil {
			t.Fatalf("handleAddTask[%d] failed: %v", i, err)
		}
		var task model.Task
		parseResponse(t, addResult, &task)
		taskIDs[i] = task.ID
	}

	// Verify 3 tasks exist
	listResult, err := srv.handleListTasks(ctx, nil)
	if err != nil {
		t.Fatalf("handleListTasks failed: %v", err)
	}
	var tasks []*model.Task
	parseResponse(t, listResult, &tasks)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	// Execute only the second task
	execTask, _ := srv.scheduler.GetTask(taskIDs[1])
	if err := srv.Execute(ctx, execTask, 10*time.Second); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// The executed task should have a result
	resultReq := makeRequest(t, TaskResultParams{ID: taskIDs[1]})
	resultResult, err := srv.handleGetTaskResult(ctx, resultReq)
	if err != nil {
		t.Fatalf("handleGetTaskResult for executed task failed: %v", err)
	}
	var result model.Result
	parseResponse(t, resultResult, &result)
	if !strings.Contains(result.Output, "task-B") {
		t.Errorf("expected output to contain 'task-B', got %q", result.Output)
	}

	// The other tasks should NOT have results
	for _, idx := range []int{0, 2} {
		noResultReq := makeRequest(t, TaskResultParams{ID: taskIDs[idx]})
		_, err := srv.handleGetTaskResult(ctx, noResultReq)
		if err == nil {
			t.Errorf("expected no result for task %d, but got one", idx)
		}
	}
}
