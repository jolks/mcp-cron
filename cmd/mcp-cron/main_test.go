// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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

func testLogger() *logging.Logger {
	return logging.New(logging.Options{Output: io.Discard, Level: logging.Fatal})
}

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
	logger := testLogger()
	cronScheduler := scheduler.NewScheduler(&cfg.Scheduler, logger)
	commandExecutor := command.NewCommandExecutor(nil, logger)

	// Create agent executor with config
	agentExecutor := agent.NewAgentExecutor(cfg, nil, logger)

	// Create the server with custom config
	mcpServer, err := server.NewMCPServer(cfg, cronScheduler, commandExecutor, agentExecutor, nil, logger)

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
	logger := testLogger()
	sched := scheduler.NewScheduler(&cfg.Scheduler, logger)
	sched.SetTaskStore(resultStore)

	cmdExec := command.NewCommandExecutor(resultStore, logger)
	agentExec := agent.NewAgentExecutor(cfg, resultStore, logger)

	srv, err := server.NewMCPServer(cfg, sched, cmdExec, agentExec, resultStore, logger)
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

	// Run waitForShutdown in the background (as primary instance)
	shutdownDone := make(chan struct{})
	go func() {
		waitForShutdown(cancel, app, true)
		close(shutdownDone)
	}()

	// Simulate the MCP transport exiting (stdin EOF).
	// srv.Stop() closes stopCh, which makes server.Done() fire.
	_ = srv.Stop()

	// Give waitForShutdown time to process the Done event
	time.Sleep(200 * time.Millisecond)

	// Simulate the MCP client sending SIGTERM to clean up the server
	// process (Claude Desktop / Cursor do this ~2s after closing stdin).
	// This should be ignored after transport exit.
	p, _ := os.FindProcess(os.Getpid())
	_ = p.Signal(syscall.SIGTERM)

	// Give time for the ignored signal to be processed
	time.Sleep(200 * time.Millisecond)

	// The scheduler should still be alive. Add a task and trigger it.
	now := time.Now()
	task := &model.Task{
		ID:        "post-exit-task",
		Name:      "Post Exit Task",
		Type:      model.TypeShellCommand,
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

	// Clean up: send SIGINT to trigger the full shutdown path
	// (SIGTERM is ignored after transport exit)
	_ = p.Signal(syscall.SIGINT)

	select {
	case <-shutdownDone:
	case <-time.After(10 * time.Second):
		t.Fatal("waitForShutdown did not complete after SIGINT")
	}
}

// buildBinary builds the mcp-cron binary and returns the path. It uses
// sync.Once so the binary is built at most once per test run. The binary
// is placed in os.TempDir (not t.TempDir) so it persists across tests.
var (
	builtBinary     string
	builtBinaryOnce sync.Once
	builtBinaryErr  error
)

func buildTestBinary(t *testing.T) string {
	t.Helper()
	builtBinaryOnce.Do(func() {
		bin := filepath.Join(os.TempDir(), "mcp-cron-test")
		cmd := exec.Command("go", "build", "-o", bin, ".")
		out, err := cmd.CombinedOutput()
		if err != nil {
			builtBinaryErr = err
			t.Logf("build output: %s", out)
		}
		builtBinary = bin
	})
	if builtBinaryErr != nil {
		t.Fatalf("failed to build test binary: %v", builtBinaryErr)
	}
	return builtBinary
}

// processAlive checks if a process is still running.
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}

// TestPrimaryKeepsAliveSecondaryExits verifies that the first instance
// (primary) stays alive after transport exit while a second instance
// (secondary) exits after its transport closes.
func TestPrimaryKeepsAliveSecondaryExits(t *testing.T) {
	bin := buildTestBinary(t)
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Start primary instance.
	cmd1 := exec.Command(bin, "--transport", "stdio", "--db-path", dbPath,
		"--log-file", filepath.Join(tmpDir, "primary.log"))
	stdin1, _ := cmd1.StdinPipe()
	if err := cmd1.Start(); err != nil {
		t.Fatalf("start primary: %v", err)
	}
	defer func() {
		_ = cmd1.Process.Kill()
	}()

	// Wait for it to start and acquire the lock.
	time.Sleep(1 * time.Second)

	// Start secondary instance (same db-path, can't acquire lock).
	cmd2 := exec.Command(bin, "--transport", "stdio", "--db-path", dbPath,
		"--log-file", filepath.Join(tmpDir, "secondary.log"))
	stdin2, _ := cmd2.StdinPipe()
	if err := cmd2.Start(); err != nil {
		t.Fatalf("start secondary: %v", err)
	}

	// Wait for secondary to start.
	time.Sleep(1 * time.Second)

	// Both should be alive (secondary is serving its transport).
	if !processAlive(cmd1.Process.Pid) {
		t.Fatal("primary not running")
	}
	if !processAlive(cmd2.Process.Pid) {
		t.Fatal("secondary not running")
	}

	// Close stdin on both (simulate SDK disconnect).
	_ = stdin1.Close()
	_ = stdin2.Close()

	// Secondary should exit (not primary — it enters keep-alive mode).
	done2 := make(chan error, 1)
	go func() { done2 <- cmd2.Wait() }()
	select {
	case <-done2:
		// OK — secondary exited.
	case <-time.After(15 * time.Second):
		_ = cmd2.Process.Kill()
		t.Fatal("secondary did not exit after stdin closed")
	}

	// Primary should still be alive (keep-alive mode).
	time.Sleep(500 * time.Millisecond)
	if !processAlive(cmd1.Process.Pid) {
		t.Fatal("primary should still be alive in keep-alive mode")
	}

	// Clean up primary.
	_ = cmd1.Process.Signal(syscall.SIGINT)
	_ = cmd1.Wait()
}

// TestDifferentDbPathsBothPrimary verifies that instances with different
// db-paths can both be primary (separate locks).
func TestDifferentDbPathsBothPrimary(t *testing.T) {
	bin := buildTestBinary(t)
	tmpDir := t.TempDir()

	start := func(name, dbPath string) (*exec.Cmd, func()) {
		t.Helper()
		cmd := exec.Command(bin, "--transport", "stdio", "--db-path", dbPath,
			"--log-file", filepath.Join(tmpDir, name+".log"))
		stdin, _ := cmd.StdinPipe()
		if err := cmd.Start(); err != nil {
			t.Fatalf("start %s: %v", name, err)
		}
		return cmd, func() {
			// Send SIGINT while transport is still active — handled by
			// the first select case in waitForShutdown (immediate shutdown).
			_ = cmd.Process.Signal(syscall.SIGINT)
			_ = cmd.Wait()
			_ = stdin.Close()
		}
	}

	cmd1, cleanup1 := start("a", filepath.Join(tmpDir, "a.db"))
	defer cleanup1()

	cmd2, cleanup2 := start("b", filepath.Join(tmpDir, "b.db"))
	defer cleanup2()

	// Wait for both to start.
	time.Sleep(1 * time.Second)

	// Both should be alive simultaneously (different db-paths = separate locks).
	if !processAlive(cmd1.Process.Pid) {
		t.Fatal("instance a not running")
	}
	if !processAlive(cmd2.Process.Pid) {
		t.Fatal("instance b not running")
	}
}

// TestRepeatedSpawnOnlyOnePrimaryAlive simulates the SDK pattern of spawning
// mcp-cron per request and closing stdin after each. Only the first instance
// (primary) should stay alive; subsequent instances (secondary) should exit
// after their transport closes.
func TestRepeatedSpawnOnlyOnePrimaryAlive(t *testing.T) {
	bin := buildTestBinary(t)
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	type inst struct {
		cmd  *exec.Cmd
		done chan error
	}
	var instances []inst

	for i := range 3 {
		cmd := exec.Command(bin, "--transport", "stdio", "--db-path", dbPath,
			"--log-file", filepath.Join(tmpDir, "inst"+string(rune('0'+i))+".log"))
		stdin, _ := cmd.StdinPipe()
		if err := cmd.Start(); err != nil {
			t.Fatalf("spawn %d: %v", i, err)
		}

		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		instances = append(instances, inst{cmd: cmd, done: done})

		// Give it time to start.
		time.Sleep(1 * time.Second)

		// Close stdin to simulate SDK disconnect.
		_ = stdin.Close()

		// Brief pause before next spawn.
		time.Sleep(500 * time.Millisecond)
	}

	// Wait for secondary instances (1 and 2) to exit.
	for i := 1; i < len(instances); i++ {
		select {
		case <-instances[i].done:
			// OK — secondary exited.
		case <-time.After(15 * time.Second):
			_ = instances[i].cmd.Process.Kill()
			t.Fatalf("instance %d (pid %d) did not exit", i, instances[i].cmd.Process.Pid)
		}
	}

	// The first instance (primary) should still be alive.
	first := instances[0]
	if !processAlive(first.cmd.Process.Pid) {
		t.Fatalf("primary instance (pid %d) should be alive but is not", first.cmd.Process.Pid)
	}

	// Clean up.
	_ = first.cmd.Process.Signal(syscall.SIGINT)
	<-first.done
}
