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
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/jolks/mcp-cron/internal/scheduler"
	"github.com/jolks/mcp-cron/internal/server"
	"github.com/jolks/mcp-cron/internal/store"
)

var (
	address         = flag.String("address", "", "The address to bind the server to")
	port            = flag.Int("port", 0, "The port to bind the server to")
	transport       = flag.String("transport", "", "Transport mode: sse or stdio")
	logLevel        = flag.String("log-level", "", "Logging level: debug, info, warn, error, fatal")
	logFile         = flag.String("log-file", "", "Log file path (default: stdout)")
	version         = flag.Bool("version", false, "Show version information and exit")
	aiProvider      = flag.String("ai-provider", "", "AI provider: openai or anthropic (default: openai)")
	aiBaseURL       = flag.String("ai-base-url", "", "Custom base URL for OpenAI-compatible endpoints (e.g. Ollama, vLLM, Groq, LiteLLM)")
	aiModel         = flag.String("ai-model", "", "AI model to use for AI tasks (default: gpt-4o)")
	aiMaxIterations = flag.Int("ai-max-iterations", 0, "Maximum iterations for tool-enabled AI tasks (default: 20)")
	mcpConfigPath   = flag.String("mcp-config-path", "", "Path to MCP configuration file (default: ~/.cursor/mcp.json)")
	dbPath          = flag.String("db-path", "", "Path to SQLite database for result history (default: ~/.mcp-cron/results.db)")
)

func main() {
	flag.Parse()

	// Load configuration
	cfg := loadConfig()

	// Show version and exit if requested
	if *version {
		log.Printf("%s version %s", cfg.Server.Name, cfg.Server.Version)
		os.Exit(0)
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
	waitForShutdown(cancel, app)
}

// loadConfig loads configuration from environment and command line flags
func loadConfig() *config.Config {
	// Start with defaults
	cfg := config.DefaultConfig()

	// Override with environment variables
	config.FromEnv(cfg)

	// Override with command-line flags
	applyCommandLineFlagsToConfig(cfg)

	// Validate the configuration
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	return cfg
}

// applyCommandLineFlagsToConfig applies command line flags to the configuration
func applyCommandLineFlagsToConfig(cfg *config.Config) {
	if *address != "" {
		cfg.Server.Address = *address
	}
	if *port != 0 {
		cfg.Server.Port = *port
	}
	if *transport != "" {
		cfg.Server.TransportMode = *transport
	}
	if *logLevel != "" {
		cfg.Logging.Level = *logLevel
	}
	if *logFile != "" {
		cfg.Logging.FilePath = *logFile
	}
	if *aiProvider != "" {
		cfg.AI.Provider = *aiProvider
	}
	if *aiBaseURL != "" {
		cfg.AI.BaseURL = *aiBaseURL
	}
	if *aiModel != "" {
		cfg.AI.Model = *aiModel
	}
	if *aiMaxIterations > 0 {
		cfg.AI.MaxToolIterations = *aiMaxIterations
	}
	if *mcpConfigPath != "" {
		cfg.AI.MCPConfigFilePath = *mcpConfigPath
	}
	if *dbPath != "" {
		cfg.Store.DBPath = *dbPath
	}
}

// Application represents the running application
type Application struct {
	scheduler     *scheduler.Scheduler
	cmdExecutor   *command.CommandExecutor
	agentExecutor *agent.AgentExecutor
	resultStore   model.ResultStore
	server        *server.MCPServer
	logger        *logging.Logger
}

// createApp creates a new application instance
func createApp(cfg *config.Config) (*Application, error) {
	// Create result store
	resultStore, err := store.NewSQLiteStore(cfg.Store.DBPath)
	if err != nil {
		return nil, fmt.Errorf("create result store: %w", err)
	}

	// Create components
	cmdExec := command.NewCommandExecutor(resultStore)
	agentExec := agent.NewAgentExecutor(cfg, resultStore)
	sched := scheduler.NewScheduler(&cfg.Scheduler)
	sched.SetTaskStore(resultStore)

	// Create the MCP server
	mcpServer, err := server.NewMCPServer(cfg, sched, cmdExec, agentExec, resultStore)
	if err != nil {
		_ = resultStore.Close()
		return nil, err
	}

	// Get the default logger that was configured by the server
	logger := logging.GetDefaultLogger()

	// Create the application
	app := &Application{
		scheduler:     sched,
		cmdExecutor:   cmdExec,
		agentExecutor: agentExec,
		resultStore:   resultStore,
		server:        mcpServer,
		logger:        logger,
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

	return nil
}

// waitForShutdown waits for termination signals or server exit and performs cleanup
func waitForShutdown(cancel context.CancelFunc, app *Application) {
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-signalCh:
		app.logger.Infof("Received termination signal, shutting down...")
	case <-app.server.Done():
		app.logger.Infof("Server transport exited, shutting down...")
	}

	// Cancel the context to initiate shutdown
	cancel()

	// Stop the application with a timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
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
