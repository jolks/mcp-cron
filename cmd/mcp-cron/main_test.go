// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jolks/mcp-cron/internal/agent"
	"github.com/jolks/mcp-cron/internal/command"
	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/jolks/mcp-cron/internal/scheduler"
	"github.com/jolks/mcp-cron/internal/server"
	"github.com/jolks/mcp-cron/internal/store"
)

// TestMCPServerCreation tests server creation with custom configs
func TestMCPServerCreation(t *testing.T) {
	// Test creating MCP server with custom config

	// Import the config package from the same repo
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address:       "127.0.0.1",
			Port:          9999,
			TransportMode: "stdio", // Use stdio to avoid network binding
		},
		Scheduler: config.SchedulerConfig{
			DefaultTimeout: config.DefaultConfig().Scheduler.DefaultTimeout,
		},
	}

	// Create a scheduler and executors first
	cronScheduler := scheduler.NewScheduler(&cfg.Scheduler)
	commandExecutor := command.NewCommandExecutor(nil)

	// Create agent executor with config
	agentExecutor := agent.NewAgentExecutor(cfg, nil)

	// Create the server with custom config
	mcpServer, err := server.NewMCPServer(cfg, cronScheduler, commandExecutor, agentExecutor, nil)

	if err != nil {
		t.Fatalf("Failed to create MCP server: %v", err)
	}

	if mcpServer == nil {
		t.Fatal("NewMCPServer returned nil server")
	}
}

// mockExecutor implements model.Executor for testing.
type mockExecutor struct {
	executeFunc func(ctx context.Context, task *model.Task, timeout time.Duration) error
}

func (m *mockExecutor) Execute(ctx context.Context, task *model.Task, timeout time.Duration) error {
	return m.executeFunc(ctx, task, timeout)
}

// TestSchedulerContinuesAfterTransportExit verifies that when the MCP
// transport exits (e.g. stdin EOF in stdio mode), the scheduler's poll loop
// keeps running so scheduled tasks continue to fire. Only SIGINT/SIGTERM
// should trigger a full shutdown.
//
// This is a regression test: commit 4e34447 made waitForShutdown exit
// on server.Done(), which killed the scheduler whenever the MCP client
// disconnected.
func TestSchedulerContinuesAfterTransportExit(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a persistent store
	resultStore, err := store.NewSQLiteStore(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Scheduler.PollInterval = 100 * time.Millisecond
	cfg.Server.TransportMode = "stdio"
	cfg.Logging.FilePath = filepath.Join(tmpDir, "test.log")

	// Build all components
	sched := scheduler.NewScheduler(&cfg.Scheduler)
	sched.SetTaskStore(resultStore)

	cmdExec := command.NewCommandExecutor(resultStore)
	agentExec := agent.NewAgentExecutor(cfg, resultStore)

	srv, err := server.NewMCPServer(cfg, sched, cmdExec, agentExec, resultStore)
	if err != nil {
		t.Fatalf("NewMCPServer: %v", err)
	}

	// Override the executor that NewMCPServer set on the scheduler,
	// so we can detect task execution without running real commands.
	var executed atomic.Bool
	sched.SetTaskExecutor(&mockExecutor{
		executeFunc: func(_ context.Context, _ *model.Task, _ time.Duration) error {
			executed.Store(true)
			return nil
		},
	})

	// Start the scheduler (poll loop)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sched.Start(ctx)

	app := &Application{
		scheduler:     sched,
		cmdExecutor:   cmdExec,
		agentExecutor: agentExec,
		resultStore:   resultStore,
		server:        srv,
		logger:        logging.GetDefaultLogger(),
		taskTimeout:   cfg.Scheduler.DefaultTimeout,
	}

	// Run waitForShutdown in the background
	shutdownDone := make(chan struct{})
	go func() {
		waitForShutdown(cancel, app)
		close(shutdownDone)
	}()

	// Simulate the MCP transport exiting (stdin EOF).
	// srv.Stop() closes stopCh, which makes server.Done() fire.
	_ = srv.Stop()

	// Give waitForShutdown time to process the Done event
	time.Sleep(200 * time.Millisecond)

	// The scheduler should still be alive. Add a task and trigger it.
	now := time.Now()
	task := &model.Task{
		ID:        "post-exit-task",
		Name:      "Post Exit Task",
		Type:      model.TypeShellCommand.String(),
		Command:   "echo test",
		Enabled:   true,
		Status:    model.StatusPending,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := sched.AddTask(task); err != nil {
		t.Fatalf("AddTask: %v", err)
	}
	if err := sched.RunTaskNow("post-exit-task"); err != nil {
		t.Fatalf("RunTaskNow: %v", err)
	}

	// Wait for the poll loop to pick up and execute the task
	deadline := time.After(5 * time.Second)
	for !executed.Load() {
		select {
		case <-deadline:
			t.Fatal("Scheduler stopped after transport exit — poll loop should continue running")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Clean up: send SIGTERM to trigger the full shutdown path
	p, _ := os.FindProcess(os.Getpid())
	_ = p.Signal(syscall.SIGTERM)

	select {
	case <-shutdownDone:
	case <-time.After(10 * time.Second):
		t.Fatal("waitForShutdown did not complete after SIGTERM")
	}
}
