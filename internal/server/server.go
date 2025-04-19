package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ThinkInAIXYZ/go-mcp/protocol"
	"github.com/ThinkInAIXYZ/go-mcp/server"
	"github.com/ThinkInAIXYZ/go-mcp/transport"
	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/errors"
	"github.com/jolks/mcp-cron/internal/executor"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/scheduler"
	"github.com/jolks/mcp-cron/internal/utils"
)

// Make os.OpenFile mockable for testing
var osOpenFile = os.OpenFile

// TaskParams holds parameters for various task operations
type TaskParams struct {
	ID          string `json:"id,omitempty" description:"task ID"`
	Name        string `json:"name,omitempty" description:"task name"`
	Schedule    string `json:"schedule,omitempty" description:"cron schedule expression"`
	Type        string `json:"type,omitempty" description:"task type"`
	Command     string `json:"command,omitempty" description:"command to execute"`
	Description string `json:"description,omitempty" description:"task description"`
	Enabled     bool   `json:"enabled,omitempty" description:"whether the task is enabled"`
}

// TaskIDParams holds the ID parameter used by multiple handlers
type TaskIDParams struct {
	ID string `json:"id" description:"the ID of the task to get/remove/enable/disable"`
}

type AIParams struct {
	Prompt string `json:"prompt,omitempty" description:"prompt to use for AI"`
}

type AITaskParams struct {
	TaskParams
	AIParams
}

// MCPServer represents the MCP scheduler server
type MCPServer struct {
	scheduler *scheduler.Scheduler
	executor  *executor.CommandExecutor
	server    *server.Server
	address   string
	port      int
	stopCh    chan struct{}
	wg        sync.WaitGroup
	config    *config.Config
	logger    *logging.Logger
}

// NewMCPServer creates a new MCP scheduler server
func NewMCPServer(cfg *config.Config, scheduler *scheduler.Scheduler, executor *executor.CommandExecutor) (*MCPServer, error) {
	// Create default config if not provided
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	// Initialize logger
	var logger *logging.Logger

	if cfg.Logging.FilePath != "" {
		var err error
		logger, err = logging.FileLogger(cfg.Logging.FilePath, parseLogLevel(cfg.Logging.Level))
		if err != nil {
			return nil, fmt.Errorf("failed to create file logger: %w", err)
		}
	} else {
		logger = logging.New(logging.Options{
			Level: parseLogLevel(cfg.Logging.Level),
		})
	}

	// Set as the default logger
	logging.SetDefaultLogger(logger)

	// Configure logger based on transport mode
	if cfg.Server.TransportMode == "stdio" {
		// For stdio transport, we need to be careful with logging
		// as it could interfere with JSON-RPC messages
		// Redirect logs to a file instead of stdout

		// Get the executable path
		execPath, err := os.Executable()
		if err != nil {
			logger.Errorf("Failed to get executable path: %v", err)
			execPath = cfg.Server.Name
		}

		// Get the directory containing the executable
		execDir := filepath.Dir(execPath)

		// Set log path in the same directory as the executable
		logFilename := fmt.Sprintf("%s.log", cfg.Server.Name)
		logPath := filepath.Join(execDir, logFilename)

		logFile, err := osOpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err == nil {
			log.SetOutput(logFile)
			logger.Infof("Logging to %s", logPath)
		} else {
			logger.Errorf("Failed to open log file at %s: %v", logPath, err)
		}
	}

	// Create MCP Server
	mcpServer := &MCPServer{
		scheduler: scheduler,
		executor:  executor,
		address:   cfg.Server.Address,
		port:      cfg.Server.Port,
		stopCh:    make(chan struct{}),
		config:    cfg,
		logger:    logger,
	}

	// Set the server as the task executor
	scheduler.SetTaskExecutor(mcpServer)

	// Create transport based on mode
	var svrTransport transport.ServerTransport
	var err error

	switch cfg.Server.TransportMode {
	case "stdio":
		// Create stdio transport
		logger.Infof("Using stdio transport")
		svrTransport = transport.NewStdioServerTransport()
	case "sse":
		// Create HTTP SSE transport
		addr := fmt.Sprintf("%s:%d", cfg.Server.Address, cfg.Server.Port)
		logger.Infof("Using SSE transport on %s", addr)

		// Create SSE transport with the address
		svrTransport, err = transport.NewSSEServerTransport(addr)
		if err != nil {
			return nil, errors.Internal(fmt.Errorf("failed to create SSE transport: %w", err))
		}
	default:
		return nil, errors.InvalidInput(fmt.Sprintf("unsupported transport mode: %s", cfg.Server.TransportMode))
	}

	// Create MCP server with the transport
	mcpServer.server, err = server.NewServer(
		svrTransport,
		server.WithServerInfo(protocol.Implementation{
			Name:    cfg.Server.Name,
			Version: cfg.Server.Version,
		}),
	)
	if err != nil {
		return nil, errors.Internal(fmt.Errorf("failed to create MCP server: %w", err))
	}

	return mcpServer, nil
}

// Start starts the MCP server
func (s *MCPServer) Start(ctx context.Context) error {
	// Register all tools
	s.registerToolsDeclarative()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()

		// Start the server
		if err := s.server.Run(); err != nil {
			s.logger.Errorf("Error running MCP server: %v", err)
			return
		}
	}()

	// Listen for context cancellation
	go func() {
		<-ctx.Done()
		if err := s.Stop(); err != nil {
			s.logger.Errorf("Error stopping MCP server: %v", err)
		}
	}()

	return nil
}

// Stop stops the MCP server
func (s *MCPServer) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		return errors.Internal(fmt.Errorf("error shutting down MCP server: %w", err))
	}

	close(s.stopCh)
	s.wg.Wait()
	return nil
}

// handleListTasks lists all tasks
func (s *MCPServer) handleListTasks(_ *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	s.logger.Debugf("Handling list_tasks request")

	// Get all tasks
	tasks := s.scheduler.ListTasks()

	return createTasksResponse(tasks)
}

// handleGetTask gets a specific task by ID
func (s *MCPServer) handleGetTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	// Extract task ID
	taskID, err := extractTaskIDParam(request)
	if err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling get_task request for task %s", taskID)

	// Get the task
	task, err := s.scheduler.GetTask(taskID)
	if err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(task)
}

// handleAddTask adds a new shell command task
func (s *MCPServer) handleAddTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	// Extract parameters
	var params TaskParams

	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling add_task request for task %s", params.Name)

	// Create task
	taskID := fmt.Sprintf("task_%d", time.Now().UnixNano())
	task := &scheduler.Task{
		ID:          taskID,
		Name:        params.Name,
		Schedule:    params.Schedule,
		Type:        scheduler.TypeShellCommand.String(),
		Command:     params.Command,
		Description: params.Description,
		Enabled:     params.Enabled,
		Status:      scheduler.StatusPending.String(),
		LastRun:     time.Now(),
		NextRun:     time.Now(),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Add task to scheduler
	if err := s.scheduler.AddTask(task); err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(task)
}

func (s *MCPServer) handleAddAITask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	// Extract parameters
	var params AITaskParams

	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling add_ai_task request for task %s", params.Name)

	aiParams, err := json.Marshal(AIParams{
		Prompt: params.Prompt,
	})
	if err != nil {
		return createErrorResponse(err)
	}
	// Create task
	taskID := fmt.Sprintf("task_%d", time.Now().UnixNano())
	task := &scheduler.Task{
		ID:          taskID,
		Name:        params.Name,
		Schedule:    params.Schedule,
		Type:        scheduler.TypeAI.String(),
		Command:     params.Command,
		Description: string(aiParams),
		Enabled:     params.Enabled,
		Status:      scheduler.StatusPending.String(),
		LastRun:     time.Now(),
		NextRun:     time.Now(),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	// Add task to scheduler
	if err := s.scheduler.AddTask(task); err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(task)
}

// handleUpdateTask updates an existing task
func (s *MCPServer) handleUpdateTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	// Extract parameters
	var params TaskParams

	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling update_task request for task %s", params.ID)

	// Get existing task
	existingTask, err := s.scheduler.GetTask(params.ID)
	if err != nil {
		return createErrorResponse(err)
	}

	// Update fields if provided
	if params.Name != "" {
		existingTask.Name = params.Name
	}

	if params.Schedule != "" {
		existingTask.Schedule = params.Schedule
	}

	if params.Command != "" {
		existingTask.Command = params.Command
	}

	if params.Description != "" {
		existingTask.Description = params.Description
	}

	// Only update Enabled if it's explicitly in the JSON
	var rawJSON map[string]interface{}
	if err := utils.JsonUnmarshal(request.RawArguments, &rawJSON); err == nil {
		if _, exists := rawJSON["enabled"]; exists {
			existingTask.Enabled = params.Enabled
		}
	}

	existingTask.UpdatedAt = time.Now()

	// Update task
	if err := s.scheduler.UpdateTask(existingTask); err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(existingTask)
}

// handleRemoveTask removes a task
func (s *MCPServer) handleRemoveTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	// Extract task ID
	taskID, err := extractTaskIDParam(request)
	if err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling remove_task request for task %s", taskID)

	// Remove task
	if err := s.scheduler.RemoveTask(taskID); err != nil {
		return createErrorResponse(err)
	}

	return createSuccessResponse(fmt.Sprintf("Task %s removed successfully", taskID))
}

// handleEnableTask enables a task
func (s *MCPServer) handleEnableTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	// Extract task ID
	taskID, err := extractTaskIDParam(request)
	if err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling enable_task request for task %s", taskID)

	// Enable task
	if err := s.scheduler.EnableTask(taskID); err != nil {
		return createErrorResponse(err)
	}

	// Get updated task
	task, err := s.scheduler.GetTask(taskID)
	if err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(task)
}

// handleDisableTask disables a task
func (s *MCPServer) handleDisableTask(request *protocol.CallToolRequest) (*protocol.CallToolResult, error) {
	// Extract task ID
	taskID, err := extractTaskIDParam(request)
	if err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling disable_task request for task %s", taskID)

	// Disable task
	if err := s.scheduler.DisableTask(taskID); err != nil {
		return createErrorResponse(err)
	}

	// Get updated task
	task, err := s.scheduler.GetTask(taskID)
	if err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(task)
}

// ExecuteTask delegates to correct executor based on task type
func (s *MCPServer) ExecuteTask(task *scheduler.Task) error {
	logger := logging.GetDefaultLogger().WithField("task_id", task.ID)
	logger.Infof("Executing task: %s", task.Name)

	// Execute the command with the configured timeout
	ctx := context.Background()

	if task.Type == scheduler.TypeAI.String() {

	}
	result := s.executor.ExecuteCommand(ctx, task.ID, task.Command, s.config.Scheduler.DefaultTimeout)

	if result.Error != "" {
		logger.Errorf("Task execution failed: %v", result.Error)
		return fmt.Errorf(result.Error)
	}

	logger.Infof("Task completed successfully")
	return nil
}

// Helper function to parse log level
func parseLogLevel(level string) logging.LogLevel {
	switch level {
	case "debug":
		return logging.Debug
	case "info":
		return logging.Info
	case "warn":
		return logging.Warn
	case "error":
		return logging.Error
	case "fatal":
		return logging.Fatal
	default:
		return logging.Info
	}
}
