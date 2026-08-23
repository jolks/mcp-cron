// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jolks/mcp-cron/internal/agent"
	"github.com/jolks/mcp-cron/internal/command"
	"github.com/jolks/mcp-cron/internal/config"
	httpexec "github.com/jolks/mcp-cron/internal/http"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/jolks/mcp-cron/internal/scheduler"
	"github.com/jolks/mcp-cron/internal/server"
	"github.com/jolks/mcp-cron/internal/singleton"
	"github.com/jolks/mcp-cron/internal/sleep"
	"github.com/jolks/mcp-cron/internal/store"
)

func main() {
	// Load configuration
	cfg := loadConfig()

	// Try to become the primary instance for this db-path.
	// Primary: enters keep-alive mode after transport exits (scheduler continues).
	// Secondary: exits after transport closes (avoids lingering processes).
	lock, isPrimary, err := singleton.TryAcquire(cfg.Store.DBPath)
	if err != nil {
		log.Fatalf("Failed to acquire singleton lock: %v", err)
	}
	if isPrimary {
		defer func() { _ = lock.Release() }()
	}

	// Create a context that will be cancelled on interrupt signal
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize the application
	app, err := createApp(cfg)
	if err != nil {
		log.Fatalf("Failed to create application: %v", err)
	}

	// Start the application
	if err := app.Start(ctx); err != nil {
		log.Fatalf("Failed to start application: %v", err)
	}

	// Wait for termination signal or server exit (e.g. stdin closed in stdio mode)
	waitForShutdown(cancel, app, isPrimary)
}

// loadConfig resolves the configuration from defaults, environment variables
// and command-line flags, handles --version, and validates the result.
func loadConfig() *config.Config {
	cfg, showVersion := parseConfig(flag.CommandLine, os.Args[1:])

	// Before validation, so --version works even under an invalid configuration
	if showVersion {
		log.Printf("%s version %s", config.ServerName, config.Version)
		os.Exit(0)
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}
	return cfg
}

// parseConfig applies the precedence defaults < environment < flags. Each
// flag is bound directly to its config field with the env-resolved value as
// the flag default, so an explicitly passed flag always wins (including
// --allow-unauthenticated=false), there is a single source of defaults, and
// -h shows the effective values.
//
// --auth-token is the one exception: it is parsed into a local and applied
// via SetAuthToken so that the env-supplied token never appears as a flag
// default in -h output. Consequently `--auth-token ""` does not clear an
// env token; unset the variable instead.
func parseConfig(fs *flag.FlagSet, args []string) (cfg *config.Config, showVersion bool) {
	cfg = config.DefaultConfig()
	config.FromEnv(cfg)

	var authToken string
	fs.StringVar(&cfg.Server.Address, "address", cfg.Server.Address, "The address to bind the server to (host only; see --port)")
	fs.IntVar(&cfg.Server.Port, "port", cfg.Server.Port, "The port to bind the server to")
	fs.StringVar(&cfg.Server.TransportMode, "transport", cfg.Server.TransportMode, "Transport mode: http or stdio")
	fs.StringVar(&authToken, "auth-token", "", "Bearer token required for HTTP transport requests (prefer MCP_CRON_SERVER_AUTH_TOKEN to keep it out of process listings)")
	fs.BoolVar(&cfg.Server.AllowUnauthenticated, "allow-unauthenticated", cfg.Server.AllowUnauthenticated, "Allow HTTP transport on a non-loopback address without an auth token (dangerous)")
	fs.StringVar(&cfg.Logging.Level, "log-level", cfg.Logging.Level, "Logging level: debug, info, warn, error, fatal")
	fs.StringVar(&cfg.Logging.FilePath, "log-file", cfg.Logging.FilePath, "Log file path (default: stdout)")
	fs.BoolVar(&showVersion, "version", false, "Show version information and exit")
	fs.StringVar(&cfg.AI.Provider, "ai-provider", cfg.AI.Provider, "AI provider: openai or anthropic")
	fs.StringVar(&cfg.AI.BaseURL, "ai-base-url", cfg.AI.BaseURL, "Custom base URL for OpenAI-compatible endpoints (e.g. Ollama, vLLM, Groq, LiteLLM)")
	fs.StringVar(&cfg.AI.Model, "ai-model", cfg.AI.Model, "AI model to use for AI tasks")
	fs.IntVar(&cfg.AI.MaxToolIterations, "ai-max-iterations", cfg.AI.MaxToolIterations, "Maximum iterations for tool-enabled AI tasks")
	fs.StringVar(&cfg.AI.MCPConfigFilePath, "mcp-config-path", cfg.AI.MCPConfigFilePath, "Path to MCP configuration file")
	fs.StringVar(&cfg.Store.DBPath, "db-path", cfg.Store.DBPath, "Path to SQLite database for result history")
	fs.BoolVar(&cfg.PreventSleep, "prevent-sleep", cfg.PreventSleep, "Prevent system from sleeping while mcp-cron is running (macOS and Windows only)")
	fs.DurationVar(&cfg.Scheduler.PollInterval, "poll-interval", cfg.Scheduler.PollInterval, "How often to check for due tasks")
	_ = fs.Parse(args) // flag.CommandLine exits on error; a ContinueOnError set (tests) reports via the config

	cfg.Server.SetAuthToken(authToken)
	return cfg, showVersion
}

// Application represents the running application
type Application struct {
	scheduler     *scheduler.Scheduler
	cmdExecutor   *command.CommandExecutor
	agentExecutor *agent.AgentExecutor
	httpExecutor  *httpexec.HTTPExecutor
	resultStore   model.ResultStore
	server        *server.MCPServer
	logger        *logging.Logger
	releaseSleep  func()
	taskTimeout   time.Duration // used to derive shutdown deadline
}

// createApp creates a new application instance
func createApp(cfg *config.Config) (*Application, error) {
	// Create logger first so all components can use it
	logger, err := server.CreateLogger(cfg)
	if err != nil {
		return nil, err
	}
	logging.SetDefaultLogger(logger)

	// Create result store
	resultStore, err := store.NewSQLiteStore(cfg.Store.DBPath)
	if err != nil {
		return nil, fmt.Errorf("create result store: %w", err)
	}

	// Create components
	cmdExec := command.NewCommandExecutor(resultStore, logger)
	agentExec := agent.NewAgentExecutor(cfg, resultStore, logger)
	httpExec := httpexec.NewHTTPExecutor(resultStore, logger)
	sched := scheduler.NewScheduler(&cfg.Scheduler, logger)
	sched.SetTaskStore(resultStore)

	// Create the MCP server
	mcpServer, err := server.NewMCPServer(cfg, sched, cmdExec, agentExec, httpExec, resultStore, logger)
	if err != nil {
		_ = resultStore.Close()
		return nil, err
	}

	// Create the application
	app := &Application{
		scheduler:     sched,
		cmdExecutor:   cmdExec,
		agentExecutor: agentExec,
		httpExecutor:  httpExec,
		resultStore:   resultStore,
		server:        mcpServer,
		logger:        logger,
		taskTimeout:   cfg.Scheduler.DefaultTimeout,
	}

	// Prevent system sleep if configured
	if cfg.PreventSleep {
		release, err := sleep.Prevent()
		if err != nil {
			logger.Warnf("Failed to prevent system sleep: %v", err)
		} else {
			app.releaseSleep = release
			logger.Infof("System sleep prevention enabled")
		}
	}

	return app, nil
}

// Start starts the application
func (a *Application) Start(ctx context.Context) error {
	// Start the scheduler
	a.scheduler.Start(ctx)
	a.logger.Infof("Task scheduler started")

	// Restore persisted tasks from the database
	if err := a.scheduler.LoadTasks(); err != nil {
		a.logger.Errorf("Failed to load persisted tasks: %v", err)
	} else {
		a.logger.Infof("Persisted tasks loaded")
	}

	// Start the MCP server
	if err := a.server.Start(ctx); err != nil {
		return err
	}
	a.logger.Infof("MCP server started")

	return nil
}

// Stop stops the application
func (a *Application) Stop() error {
	// Stop the scheduler
	err := a.scheduler.Stop()
	if err != nil {
		return err
	}
	a.logger.Infof("Task scheduler stopped")

	// Stop the server
	if err := a.server.Stop(); err != nil {
		a.logger.Errorf("Error stopping MCP server: %v", err)
		return err
	}
	a.logger.Infof("MCP server stopped")

	// Close the result store last, after all components that use it have stopped
	if a.resultStore != nil {
		if err := a.resultStore.Close(); err != nil {
			a.logger.Warnf("Error closing result store: %v", err)
		}
	}

	// Release sleep prevention
	if a.releaseSleep != nil {
		a.releaseSleep()
		a.logger.Infof("System sleep prevention released")
	}

	return nil
}

// waitForShutdown waits for termination signals or server exit and performs cleanup.
// Primary instances enter keep-alive mode after transport exit (scheduler continues).
// Secondary instances shut down after transport exit (avoids lingering processes).
func waitForShutdown(cancel context.CancelFunc, app *Application, isPrimary bool) {
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-signalCh:
		app.logger.Infof("Received termination signal, shutting down...")
	case <-app.server.Done():
		if !isPrimary {
			// Secondary instance — shut down after transport closes.
			// The primary instance handles the scheduler.
			app.logger.Infof("Server transport exited, shutting down (secondary instance)")
			break
		}

		// Primary instance — keep the scheduler running so scheduled
		// tasks continue to fire on time.
		app.logger.Infof("Server transport exited, scheduler continues running")

		// MCP clients send SIGTERM after closing stdin to clean up the
		// server process. Ignore it so the scheduler survives. Only
		// SIGINT (kill -INT / Ctrl+C) or SIGKILL triggers shutdown.
		signal.Stop(signalCh)
		signal.Ignore(syscall.SIGTERM)
		intCh := make(chan os.Signal, 1)
		signal.Notify(intCh, syscall.SIGINT)
		<-intCh
		app.logger.Infof("Received interrupt signal, shutting down...")
	}

	// Cancel the context to stop the poll loop from scheduling new tasks
	cancel()

	// Stop the application. The scheduler waits for in-flight tasks to finish
	// (bounded by each task's own timeout), so the outer deadline must exceed it.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), app.taskTimeout+1*time.Minute)
	defer shutdownCancel()

	shutdownDone := make(chan struct{})
	go func() {
		if err := app.Stop(); err != nil {
			app.logger.Errorf("Error during shutdown: %v", err)
		}
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		app.logger.Infof("Graceful shutdown completed")
	case <-shutdownCtx.Done():
		app.logger.Warnf("Shutdown timed out")
	}
}
