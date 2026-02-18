// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jolks/mcp-cron/internal/agent"
	"github.com/jolks/mcp-cron/internal/command"
	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/jolks/mcp-cron/internal/scheduler"
	"github.com/jolks/mcp-cron/internal/store"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// Server constructor
// ---------------------------------------------------------------------------

type integrationOpts struct {
	withStore    bool
	pollInterval time.Duration
}

// createIntegrationTestServer creates an MCPServer with a started scheduler
// and real executors. When opts.withStore is true, a SQLite store is created
// in t.TempDir() so the poll loop can persist and retrieve results.
func createIntegrationTestServer(t *testing.T, opts ...integrationOpts) *MCPServer {
	t.Helper()

	var opt integrationOpts
	if len(opts) > 0 {
		opt = opts[0]
	}

	cfg := config.DefaultConfig()
	if opt.pollInterval > 0 {
		cfg.Scheduler.PollInterval = opt.pollInterval
	}

	var (
		resultStore model.ResultStore
		taskStore   model.TaskStore
	)
	if opt.withStore {
		dbPath := filepath.Join(t.TempDir(), "integration.db")
		sqlStore, err := store.NewSQLiteStore(dbPath)
		if err != nil {
			t.Fatalf("NewSQLiteStore: %v", err)
		}
		t.Cleanup(func() { _ = sqlStore.Close() })
		resultStore = sqlStore
		taskStore = sqlStore
	}

	logger := testLogger()

	sched := scheduler.NewScheduler(&cfg.Scheduler, logger)
	if taskStore != nil {
		sched.SetTaskStore(taskStore)
	}

	cmdExec := command.NewCommandExecutor(resultStore, logger)
	agentExec := agent.NewAgentExecutor(cfg, resultStore, logger)

	srv := &MCPServer{
		scheduler:     sched,
		cmdExecutor:   cmdExec,
		agentExecutor: agentExec,
		resultStore:   resultStore,
		logger:        logger,
		config:        cfg,
	}

	sched.SetTaskExecutor(srv)

	ctx, cancel := context.WithCancel(context.Background())
	sched.Start(ctx)

	t.Cleanup(func() {
		cancel()
	})

	return srv
}

// ---------------------------------------------------------------------------
// Low-level request/response helpers
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Task-operation helpers (must* pattern — fatal on error)
// ---------------------------------------------------------------------------

func mustAddShellTask(t *testing.T, srv *MCPServer, params TaskParams) model.Task {
	t.Helper()
	result, err := srv.handleAddTask(context.Background(), makeRequest(t, params))
	if err != nil {
		t.Fatalf("handleAddTask failed: %v", err)
	}
	var task model.Task
	parseResponse(t, result, &task)
	return task
}

func mustAddAITask(t *testing.T, srv *MCPServer, params AITaskParams) model.Task {
	t.Helper()
	result, err := srv.handleAddAITask(context.Background(), makeRequest(t, params))
	if err != nil {
		t.Fatalf("handleAddAITask failed: %v", err)
	}
	var task model.Task
	parseResponse(t, result, &task)
	return task
}

func mustGetTask(t *testing.T, srv *MCPServer, id string) model.Task {
	t.Helper()
	result, err := srv.handleGetTask(context.Background(), makeRequest(t, TaskIDParams{ID: id}))
	if err != nil {
		t.Fatalf("handleGetTask(%s) failed: %v", id, err)
	}
	var task model.Task
	parseResponse(t, result, &task)
	return task
}

func mustListTasks(t *testing.T, srv *MCPServer) []*model.Task {
	t.Helper()
	result, err := srv.handleListTasks(context.Background(), nil)
	if err != nil {
		t.Fatalf("handleListTasks failed: %v", err)
	}
	var tasks []*model.Task
	parseResponse(t, result, &tasks)
	return tasks
}

func mustGetResult(t *testing.T, srv *MCPServer, id string) model.Result {
	t.Helper()
	result, err := srv.handleGetTaskResult(context.Background(), makeRequest(t, TaskResultParams{ID: id}))
	if err != nil {
		t.Fatalf("handleGetTaskResult(%s) failed: %v", id, err)
	}
	var r model.Result
	parseResponse(t, result, &r)
	return r
}

func mustExecute(t *testing.T, srv *MCPServer, taskID string, timeout time.Duration) {
	t.Helper()
	execTask, _ := srv.scheduler.GetTask(taskID)
	if err := srv.Execute(context.Background(), execTask, timeout); err != nil {
		t.Fatalf("Execute(%s) failed: %v", taskID, err)
	}
}

func mustRunTask(t *testing.T, srv *MCPServer, id string) map[string]interface{} {
	t.Helper()
	result, err := srv.handleRunTask(context.Background(), makeRequest(t, TaskIDParams{ID: id}))
	if err != nil {
		t.Fatalf("handleRunTask(%s) failed: %v", id, err)
	}
	var resp map[string]interface{}
	parseResponse(t, result, &resp)
	return resp
}

func mustUpdateTask(t *testing.T, srv *MCPServer, params AITaskParams) model.Task {
	t.Helper()
	result, err := srv.handleUpdateTask(context.Background(), makeRequest(t, params))
	if err != nil {
		t.Fatalf("handleUpdateTask failed: %v", err)
	}
	var task model.Task
	parseResponse(t, result, &task)
	return task
}

func mustEnableTask(t *testing.T, srv *MCPServer, id string) model.Task {
	t.Helper()
	result, err := srv.handleEnableTask(context.Background(), makeRequest(t, TaskIDParams{ID: id}))
	if err != nil {
		t.Fatalf("handleEnableTask(%s) failed: %v", id, err)
	}
	var task model.Task
	parseResponse(t, result, &task)
	return task
}

func mustDisableTask(t *testing.T, srv *MCPServer, id string) model.Task {
	t.Helper()
	result, err := srv.handleDisableTask(context.Background(), makeRequest(t, TaskIDParams{ID: id}))
	if err != nil {
		t.Fatalf("handleDisableTask(%s) failed: %v", id, err)
	}
	var task model.Task
	parseResponse(t, result, &task)
	return task
}

// waitForResult polls get_task_result until a completed result (non-empty output
// or non-zero exit code) appears or the deadline expires.
func waitForResult(t *testing.T, srv *MCPServer, taskID string, timeout time.Duration) model.Result {
	t.Helper()
	ctx := context.Background()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("no completed result for task %s within %s", taskID, timeout)
		default:
		}
		req := makeRequest(t, TaskResultParams{ID: taskID})
		res, err := srv.handleGetTaskResult(ctx, req)
		if err == nil {
			var result model.Result
			parseResponse(t, res, &result)
			if result.Output != "" || result.Error != "" {
				return result
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// waitForNResults polls the result store until at least n results exist for
// the given task, or the deadline expires.
func waitForNResults(t *testing.T, srv *MCPServer, taskID string, n int, timeout time.Duration) []*model.Result {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case <-deadline:
			t.Fatalf("did not get %d results for task %s within %s", n, taskID, timeout)
		default:
		}
		req := makeRequest(t, TaskResultParams{ID: taskID, Limit: n})
		res, err := srv.handleGetTaskResult(context.Background(), req)
		if err == nil {
			var results []*model.Result
			parseResponse(t, res, &results)
			if len(results) >= n {
				return results
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// configureAIProvider sets up the AI provider on the server config based on
// available env vars. Returns true if an AI provider is available, false otherwise.
func configureAIProvider(t *testing.T, srv *MCPServer) bool {
	t.Helper()
	openaiEnabled := os.Getenv("MCP_CRON_ENABLE_OPENAI_TESTS") == "true"
	anthropicEnabled := os.Getenv("MCP_CRON_ENABLE_ANTHROPIC_TESTS") == "true"

	if !openaiEnabled && !anthropicEnabled {
		return false
	}

	if anthropicEnabled {
		srv.config.AI.Provider = "anthropic"
		srv.config.AI.Model = "claude-sonnet-4-5-20250929"
		srv.config.AI.AnthropicAPIKey = os.Getenv("ANTHROPIC_API_KEY")
	} else {
		srv.config.AI.OpenAIAPIKey = os.Getenv("OPENAI_API_KEY")
	}
	return true
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestIntegration_ShellCommandFullLifecycle exercises the full shell command workflow:
// add → list → get → execute → get_task_result
func TestIntegration_ShellCommandFullLifecycle(t *testing.T) {
	srv := createIntegrationTestServer(t, integrationOpts{withStore: true})

	task := mustAddShellTask(t, srv, TaskParams{
		Name:     "echo-test",
		Schedule: "0 0 1 1 *",
		Command:  "echo hello",
		Enabled:  false,
	})
	if task.ID == "" {
		t.Fatal("expected non-empty task ID")
	}
	if task.Type != model.TypeShellCommand {
		t.Errorf("expected type %s, got %s", model.TypeShellCommand, task.Type)
	}

	tasks := mustListTasks(t, srv)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	gotTask := mustGetTask(t, srv, task.ID)
	if gotTask.ID != task.ID {
		t.Errorf("expected ID %s, got %s", task.ID, gotTask.ID)
	}
	if gotTask.Command != "echo hello" {
		t.Errorf("expected command 'echo hello', got %q", gotTask.Command)
	}

	mustExecute(t, srv, task.ID, 10*time.Second)

	result := mustGetResult(t, srv, task.ID)
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
	srv := createIntegrationTestServer(t, integrationOpts{withStore: true})

	task := mustAddAITask(t, srv, AITaskParams{
		TaskParams: TaskParams{
			Name:     "ai-math-test",
			Schedule: "0 0 1 1 *",
			Enabled:  false,
		},
		Prompt: "What is 1+1? Answer with just the number",
	})
	if task.Type != model.TypeAI {
		t.Errorf("expected type %s, got %s", model.TypeAI, task.Type)
	}

	tasks := mustListTasks(t, srv)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	gotTask := mustGetTask(t, srv, task.ID)
	if gotTask.Prompt != "What is 1+1? Answer with just the number" {
		t.Errorf("unexpected prompt: %q", gotTask.Prompt)
	}

	updatedTask := mustUpdateTask(t, srv, AITaskParams{
		TaskParams: TaskParams{
			ID:          task.ID,
			Description: "Updated description for AI test",
		},
	})
	if updatedTask.Description != "Updated description for AI test" {
		t.Errorf("expected updated description, got %q", updatedTask.Description)
	}

	// Execute AI task (only if env var is set)
	if configureAIProvider(t, srv) {
		mustExecute(t, srv, task.ID, 30*time.Second)

		result := mustGetResult(t, srv, task.ID)
		if result.ExitCode != 0 {
			t.Errorf("expected exit_code 0, got %d (error: %s)", result.ExitCode, result.Error)
		}
		if !strings.Contains(result.Output, "2") {
			t.Errorf("expected output to contain '2', got %q", result.Output)
		}
	} else {
		t.Log("Skipping AI execution — set MCP_CRON_ENABLE_OPENAI_TESTS=true or MCP_CRON_ENABLE_ANTHROPIC_TESTS=true to run")
	}

	// Remove
	ctx := context.Background()
	_, err := srv.handleRemoveTask(ctx, makeRequest(t, TaskIDParams{ID: task.ID}))
	if err != nil {
		t.Fatalf("handleRemoveTask failed: %v", err)
	}

	// Verify removed
	_, err = srv.handleGetTask(ctx, makeRequest(t, TaskIDParams{ID: task.ID}))
	if err == nil {
		t.Fatal("expected error after removing task, got nil")
	}
}

// TestIntegration_EnableDisableFlow tests enable/disable toggling and idempotency.
func TestIntegration_EnableDisableFlow(t *testing.T) {
	srv := createIntegrationTestServer(t)

	task := mustAddShellTask(t, srv, TaskParams{
		Name:     "toggle-test",
		Schedule: "0 0 1 1 *",
		Command:  "echo toggle",
		Enabled:  false,
	})

	enabled := mustEnableTask(t, srv, task.ID)
	if !enabled.Enabled {
		t.Error("expected task to be enabled")
	}

	disabled := mustDisableTask(t, srv, task.ID)
	if disabled.Enabled {
		t.Error("expected task to be disabled")
	}
	if disabled.Status != model.StatusDisabled {
		t.Errorf("expected status %s, got %s", model.StatusDisabled, disabled.Status)
	}

	reEnabled := mustEnableTask(t, srv, task.ID)
	if !reEnabled.Enabled {
		t.Error("expected task to be re-enabled")
	}

	reDisabled := mustDisableTask(t, srv, task.ID)
	if reDisabled.Enabled {
		t.Error("expected task to be re-disabled")
	}
}

// TestIntegration_ShellCommandFailure verifies that a failing command produces the correct error result.
func TestIntegration_ShellCommandFailure(t *testing.T) {
	srv := createIntegrationTestServer(t, integrationOpts{withStore: true})

	task := mustAddShellTask(t, srv, TaskParams{
		Name:     "fail-test",
		Schedule: "0 0 1 1 *",
		Command:  "exit 1",
		Enabled:  false,
	})

	// Execute — may return an error for failed commands, that's expected
	execTask, _ := srv.scheduler.GetTask(task.ID)
	if err := srv.Execute(context.Background(), execTask, 10*time.Second); err != nil {
		t.Logf("Execute returned error (expected): %v", err)
	}

	result := mustGetResult(t, srv, task.ID)
	if result.ExitCode == 0 {
		t.Error("expected non-zero exit_code for failed command")
	}
}

// TestIntegration_ErrorCases tests error handling for invalid requests.
func TestIntegration_ErrorCases(t *testing.T) {
	srv := createIntegrationTestServer(t)
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
	srv := createIntegrationTestServer(t, integrationOpts{withStore: true})

	taskIDs := make([]string, 3)
	for i := 0; i < 3; i++ {
		task := mustAddShellTask(t, srv, TaskParams{
			Name:     "isolation-test-" + string(rune('A'+i)),
			Schedule: "0 0 1 1 *",
			Command:  "echo task-" + string(rune('A'+i)),
			Enabled:  false,
		})
		taskIDs[i] = task.ID
	}

	tasks := mustListTasks(t, srv)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	mustExecute(t, srv, taskIDs[1], 10*time.Second)

	result := mustGetResult(t, srv, taskIDs[1])
	if !strings.Contains(result.Output, "task-B") {
		t.Errorf("expected output to contain 'task-B', got %q", result.Output)
	}

	// The other tasks should NOT have results
	for _, idx := range []int{0, 2} {
		_, err := srv.handleGetTaskResult(context.Background(), makeRequest(t, TaskResultParams{ID: taskIDs[idx]}))
		if err == nil {
			t.Errorf("expected no result for task %d, but got one", idx)
		}
	}
}

// TestIntegration_OnDemandTask tests creating and running a task without a schedule.
func TestIntegration_OnDemandTask(t *testing.T) {
	srv := createIntegrationTestServer(t, integrationOpts{withStore: true})

	task := mustAddShellTask(t, srv, TaskParams{
		Name:    "on-demand-echo",
		Command: "echo on-demand",
		Enabled: true,
	})
	if task.ID == "" {
		t.Fatal("expected non-empty task ID")
	}
	if task.Schedule != "" {
		t.Errorf("expected empty schedule for on-demand task, got %q", task.Schedule)
	}

	gotTask := mustGetTask(t, srv, task.ID)
	if !gotTask.Enabled {
		t.Error("expected task to be enabled")
	}

	mustExecute(t, srv, task.ID, 10*time.Second)

	result := mustGetResult(t, srv, task.ID)
	if result.ExitCode != 0 {
		t.Errorf("expected exit_code 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Output, "on-demand") {
		t.Errorf("expected output to contain 'on-demand', got %q", result.Output)
	}
}

// TestIntegration_RunTask tests the run_task handler.
func TestIntegration_RunTask(t *testing.T) {
	srv := createIntegrationTestServer(t)

	task := mustAddShellTask(t, srv, TaskParams{
		Name:    "run-task-test",
		Command: "echo triggered",
		Enabled: false,
	})

	// run_task on a disabled task should fail
	ctx := context.Background()
	_, err := srv.handleRunTask(ctx, makeRequest(t, TaskIDParams{ID: task.ID}))
	if err == nil {
		t.Fatal("expected error when running disabled task, got nil")
	}

	mustEnableTask(t, srv, task.ID)

	// run_task on an enabled task should succeed
	_, err = srv.handleRunTask(ctx, makeRequest(t, TaskIDParams{ID: task.ID}))
	if err != nil {
		t.Fatalf("handleRunTask failed: %v", err)
	}

	// run_task on nonexistent task should fail
	_, err = srv.handleRunTask(ctx, makeRequest(t, TaskIDParams{ID: "nonexistent-id"}))
	if err == nil {
		t.Fatal("expected error for nonexistent task, got nil")
	}

	// run_task with missing ID should fail
	_, err = srv.handleRunTask(ctx, makeRequest(t, TaskIDParams{}))
	if err == nil {
		t.Fatal("expected error for missing ID, got nil")
	}
}

// TestIntegration_OnDemandAITask tests creating an on-demand AI task.
func TestIntegration_OnDemandAITask(t *testing.T) {
	srv := createIntegrationTestServer(t)

	task := mustAddAITask(t, srv, AITaskParams{
		TaskParams: TaskParams{
			Name:    "on-demand-ai",
			Enabled: true,
		},
		Prompt: "What is 2+2?",
	})
	if task.Schedule != "" {
		t.Errorf("expected empty schedule for on-demand AI task, got %q", task.Schedule)
	}
	if task.Type != model.TypeAI {
		t.Errorf("expected type %s, got %s", model.TypeAI, task.Type)
	}
}

// TestIntegration_OnDemandRunTaskLifecycle exercises the full on-demand lifecycle
// through the poll loop: add task (no schedule) → run_task → poll executes → get_task_result → verify idle.
// Both shell and AI variants are tested as subtests.
func TestIntegration_OnDemandRunTaskLifecycle(t *testing.T) {
	type testCase struct {
		name       string
		addTask    func(t *testing.T, srv *MCPServer) model.Task
		requiresAI bool
		wantOutput string
		timeout    time.Duration
	}

	cases := []testCase{
		{
			name: "shell",
			addTask: func(t *testing.T, srv *MCPServer) model.Task {
				t.Helper()
				return mustAddShellTask(t, srv, TaskParams{
					Name:    "on-demand-poll-test",
					Command: "echo hello-from-on-demand",
					Enabled: true,
				})
			},
			wantOutput: "hello-from-on-demand",
			timeout:    5 * time.Second,
		},
		{
			name: "AI",
			addTask: func(t *testing.T, srv *MCPServer) model.Task {
				t.Helper()
				return mustAddAITask(t, srv, AITaskParams{
					TaskParams: TaskParams{
						Name:    "on-demand-ai-poll-test",
						Enabled: true,
					},
					Prompt: "What is 1+1? Answer with just the number.",
				})
			},
			requiresAI: true,
			wantOutput: "2",
			timeout:    60 * time.Second,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := createIntegrationTestServer(t, integrationOpts{
				withStore:    true,
				pollInterval: 200 * time.Millisecond,
			})
			if tc.requiresAI {
				if !configureAIProvider(t, srv) {
					t.Skip("Skipping — set MCP_CRON_ENABLE_OPENAI_TESTS=true or MCP_CRON_ENABLE_ANTHROPIC_TESTS=true to run")
				}
			}

			task := tc.addTask(t, srv)
			if task.Schedule != "" {
				t.Errorf("expected empty schedule, got %q", task.Schedule)
			}

			gotTask := mustGetTask(t, srv, task.ID)
			if !gotTask.Enabled {
				t.Error("expected task to be enabled")
			}

			resp := mustRunTask(t, srv, task.ID)
			if resp["success"] != true {
				t.Errorf("expected success=true, got %v", resp["success"])
			}

			result := waitForResult(t, srv, task.ID, tc.timeout)
			if result.ExitCode != 0 {
				t.Errorf("expected exit_code 0, got %d (error: %s)", result.ExitCode, result.Error)
			}
			if !strings.Contains(result.Output, tc.wantOutput) {
				t.Errorf("expected output to contain %q, got %q", tc.wantOutput, result.Output)
			}

			// Verify task went back to idle (NextRun cleared)
			time.Sleep(500 * time.Millisecond)
			afterTask := mustGetTask(t, srv, task.ID)
			if !afterTask.NextRun.IsZero() {
				t.Errorf("expected on-demand task to return to idle (zero NextRun), got %v", afterTask.NextRun)
			}
		})
	}
}

// TestIntegration_ScheduledRunTaskResumesSchedule exercises:
// add task (with schedule) → run_task → poll executes → get_task_result → verify schedule resumes.
// Both shell and AI variants are tested as subtests.
func TestIntegration_ScheduledRunTaskResumesSchedule(t *testing.T) {
	type testCase struct {
		name       string
		addTask    func(t *testing.T, srv *MCPServer) model.Task
		requiresAI bool
		wantOutput string
		timeout    time.Duration
	}

	cases := []testCase{
		{
			name: "shell",
			addTask: func(t *testing.T, srv *MCPServer) model.Task {
				t.Helper()
				return mustAddShellTask(t, srv, TaskParams{
					Name:     "scheduled-run-task-test",
					Schedule: "0 0 1 1 *",
					Command:  "echo scheduled-run",
					Enabled:  true,
				})
			},
			wantOutput: "scheduled-run",
			timeout:    5 * time.Second,
		},
		{
			name: "AI",
			addTask: func(t *testing.T, srv *MCPServer) model.Task {
				t.Helper()
				return mustAddAITask(t, srv, AITaskParams{
					TaskParams: TaskParams{
						Name:     "scheduled-ai-run-task-test",
						Schedule: "0 0 1 1 *",
						Enabled:  true,
					},
					Prompt: "What is 2+2? Answer with just the number.",
				})
			},
			requiresAI: true,
			wantOutput: "4",
			timeout:    60 * time.Second,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := createIntegrationTestServer(t, integrationOpts{
				withStore:    true,
				pollInterval: 200 * time.Millisecond,
			})
			if tc.requiresAI {
				if !configureAIProvider(t, srv) {
					t.Skip("Skipping — set MCP_CRON_ENABLE_OPENAI_TESTS=true or MCP_CRON_ENABLE_ANTHROPIC_TESTS=true to run")
				}
			}

			task := tc.addTask(t, srv)

			originalNextRun := task.NextRun
			if originalNextRun.IsZero() {
				t.Fatal("expected non-zero NextRun for scheduled task")
			}
			if !originalNextRun.After(time.Now()) {
				t.Fatalf("expected NextRun in the future, got %v", originalNextRun)
			}

			mustRunTask(t, srv, task.ID)

			// Verify NextRun was moved to approximately now
			triggeredTask := mustGetTask(t, srv, task.ID)
			if triggeredTask.NextRun.After(time.Now().Add(2 * time.Second)) {
				t.Errorf("expected NextRun ~now after run_task, got %v", triggeredTask.NextRun)
			}

			result := waitForResult(t, srv, task.ID, tc.timeout)
			if result.ExitCode != 0 {
				t.Errorf("expected exit_code 0, got %d (error: %s)", result.ExitCode, result.Error)
			}
			if !strings.Contains(result.Output, tc.wantOutput) {
				t.Errorf("expected output to contain %q, got %q", tc.wantOutput, result.Output)
			}

			// Verify schedule resumed — NextRun should be back in the future
			time.Sleep(500 * time.Millisecond)
			afterTask := mustGetTask(t, srv, task.ID)

			if afterTask.NextRun.IsZero() {
				t.Error("expected NextRun to be set after execution (schedule should resume)")
			}
			if !afterTask.NextRun.After(time.Now()) {
				t.Errorf("expected NextRun in the future (schedule resumed), got %v", afterTask.NextRun)
			}
			if !afterTask.NextRun.Equal(originalNextRun) {
				t.Errorf("expected NextRun to match original %v, got %v", originalNextRun, afterTask.NextRun)
			}
			if !afterTask.Enabled {
				t.Error("expected task to remain enabled after execution")
			}
		})
	}
}

// TestIntegration_AITaskGetTaskResult verifies that an AI task can call
// get_task_result (injected internally) to read its own prior output and
// build on it across executions (n → n+1 pattern).
func TestIntegration_AITaskGetTaskResult(t *testing.T) {
	srv := createIntegrationTestServer(t, integrationOpts{
		withStore:    true,
		pollInterval: 200 * time.Millisecond,
	})
	if !configureAIProvider(t, srv) {
		t.Skip("Skipping — set MCP_CRON_ENABLE_OPENAI_TESTS=true or MCP_CRON_ENABLE_ANTHROPIC_TESTS=true to run")
	}

	// Add a scheduled AI task that fires every 30 seconds.
	// We'll use a placeholder prompt first, then update it with the actual task ID.
	task := mustAddAITask(t, srv, AITaskParams{
		TaskParams: TaskParams{
			Name:     "ai-get-result-test",
			Schedule: "*/30 * * * * *",
			Enabled:  true,
		},
		Prompt: "placeholder",
	})

	// Update the prompt to include the task's own ID
	prompt := `You have a tool called get_task_result. Call it with id '` + task.ID + `'. ` +
		`If no result exists or it errors, respond with just the number 1. ` +
		`If a result exists, parse the number from the output and respond with that number plus 1. ` +
		`Respond ONLY with the number, nothing else.`

	mustUpdateTask(t, srv, AITaskParams{
		TaskParams: TaskParams{
			ID: task.ID,
		},
		Prompt: prompt,
	})

	// Wait for at least 2 results (2 scheduled executions)
	results := waitForNResults(t, srv, task.ID, 2, 180*time.Second)

	// Results are newest-first from the store. Reverse to get chronological order.
	// results[0] = newest (should be "2"), results[1] = oldest (should be "1")
	first := results[len(results)-1]  // oldest
	second := results[len(results)-2] // second execution

	t.Logf("First execution output: %q", first.Output)
	t.Logf("Second execution output: %q", second.Output)

	if !strings.Contains(first.Output, "1") {
		t.Errorf("expected first execution output to contain '1', got %q", first.Output)
	}
	if !strings.Contains(second.Output, "2") {
		t.Errorf("expected second execution output to contain '2', got %q", second.Output)
	}
}
