// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jolks/mcp-cron/internal/agent"
	"github.com/jolks/mcp-cron/internal/command"
	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/errors"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/jolks/mcp-cron/internal/scheduler"
	"github.com/jolks/mcp-cron/internal/utils"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// TaskResultParams holds parameters for the get_task_result tool
type TaskResultParams struct {
	ID    string `json:"id" description:"the ID of the task to get results for"`
	Limit int    `json:"limit,omitempty" description:"number of recent results to return (default 1, max 100)"`
}

// AITaskParams combines task parameters with AI parameters
type AITaskParams struct {
	ID          string `json:"id,omitempty" description:"task ID"`
	Name        string `json:"name,omitempty" description:"task name"`
	Schedule    string `json:"schedule,omitempty" description:"cron schedule expression"`
	Type        string `json:"type,omitempty" description:"task type"`
	Command     string `json:"command,omitempty" description:"command to execute"`
	Description string `json:"description,omitempty" description:"task description"`
	Enabled     bool   `json:"enabled,omitempty" description:"whether the task is enabled"`
	// LLM Prompt
	Prompt string `json:"prompt,omitempty" description:"prompt to use for AI"`
}

// MCPServer represents the MCP scheduler server
type MCPServer struct {
	scheduler      *scheduler.Scheduler
	cmdExecutor    *command.CommandExecutor
	agentExecutor  *agent.AgentExecutor
	resultStore    model.ResultStore
	server         *mcp.Server
	httpServer     *http.Server
	cancel         context.CancelFunc
	address        string
	port           int
	stopCh         chan struct{}
	wg             sync.WaitGroup
	config         *config.Config
	logger         *logging.Logger
	shutdownMutex  sync.Mutex
	isShuttingDown bool
}

// NewMCPServer creates a new MCP scheduler server
func NewMCPServer(cfg *config.Config, scheduler *scheduler.Scheduler, cmdExecutor *command.CommandExecutor, agentExecutor *agent.AgentExecutor, resultStore model.ResultStore) (*MCPServer, error) {
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
	} else if cfg.Server.TransportMode == "stdio" {
		// For stdio transport, all logging must go to a file to avoid
		// corrupting the JSON-RPC stream on stdout
		execPath, err := os.Executable()
		if err != nil {
			execPath = cfg.Server.Name
		}
		execDir := filepath.Dir(execPath)
		logFilename := fmt.Sprintf("%s.log", cfg.Server.Name)
		logPath := filepath.Join(execDir, logFilename)

		logFile, err := osOpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err == nil {
			log.SetOutput(logFile)
			logger = logging.New(logging.Options{
				Output: logFile,
				Level:  parseLogLevel(cfg.Logging.Level),
			})
		} else {
			// Fall back to stderr to avoid corrupting stdout
			log.SetOutput(os.Stderr)
			logger = logging.New(logging.Options{
				Output: os.Stderr,
				Level:  parseLogLevel(cfg.Logging.Level),
			})
		}
	} else {
		logger = logging.New(logging.Options{
			Level: parseLogLevel(cfg.Logging.Level),
		})
	}

	// Set as the default logger
	logging.SetDefaultLogger(logger)

	// Validate transport mode
	switch cfg.Server.TransportMode {
	case "stdio":
		logger.Infof("Using stdio transport")
	case "sse":
		logger.Infof("Using SSE transport on %s:%d", cfg.Server.Address, cfg.Server.Port)
	default:
		return nil, errors.InvalidInput(fmt.Sprintf("unsupported transport mode: %s", cfg.Server.TransportMode))
	}

	// Create MCP server
	mcpSrv := mcp.NewServer(&mcp.Implementation{
		Name:    cfg.Server.Name,
		Version: cfg.Server.Version,
	}, nil)

	// Create MCP Server
	mcpServer := &MCPServer{
		scheduler:     scheduler,
		cmdExecutor:   cmdExecutor,
		agentExecutor: agentExecutor,
		resultStore:   resultStore,
		server:        mcpSrv,
		address:       cfg.Server.Address,
		port:          cfg.Server.Port,
		stopCh:        make(chan struct{}),
		config:        cfg,
		logger:        logger,
	}

	// Set up task routing
	scheduler.SetTaskExecutor(mcpServer)

	return mcpServer, nil
}

// Start starts the MCP server
func (s *MCPServer) Start(ctx context.Context) error {
	// Register all tools
	s.registerToolsDeclarative()

	switch s.config.Server.TransportMode {
	case "stdio":
		runCtx, cancel := context.WithCancel(ctx)
		s.cancel = cancel
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := s.server.Run(runCtx, &mcp.StdioTransport{}); err != nil {
				s.logger.Errorf("Error running MCP server: %v", err)
			}
		}()
	case "sse":
		addr := fmt.Sprintf("%s:%d", s.address, s.port)
		handler := mcp.NewSSEHandler(func(_ *http.Request) *mcp.Server {
			return s.server
		}, nil)
		s.httpServer = &http.Server{Addr: addr, Handler: handler}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				s.logger.Errorf("Error running MCP server: %v", err)
			}
		}()
	}

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
	s.shutdownMutex.Lock()
	defer s.shutdownMutex.Unlock()

	// Return early if server is already being shut down
	if s.isShuttingDown {
		s.logger.Debugf("Stop called but server is already shutting down, ignoring")
		return nil
	}

	s.isShuttingDown = true

	if s.cancel != nil {
		s.cancel()
	}

	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(ctx); err != nil {
			return errors.Internal(fmt.Errorf("error shutting down MCP server: %w", err))
		}
	}

	// Close the result store
	if s.resultStore != nil {
		if err := s.resultStore.Close(); err != nil {
			s.logger.Warnf("Error closing result store: %v", err)
		}
	}

	// Only close stopCh if it hasn't been closed yet
	select {
	case <-s.stopCh:
		// Channel is already closed, do nothing
	default:
		close(s.stopCh)
	}

	s.wg.Wait()
	return nil
}

// handleListTasks lists all tasks
func (s *MCPServer) handleListTasks(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.logger.Debugf("Handling list_tasks request")

	// Get all tasks
	tasks := s.scheduler.ListTasks()

	return createTasksResponse(tasks)
}

// handleGetTask gets a specific task by ID
func (s *MCPServer) handleGetTask(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
func (s *MCPServer) handleAddTask(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract parameters
	var params TaskParams

	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	// Validate parameters
	if err := validateShellTaskParams(params.Name, params.Schedule, params.Command); err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling add_task request for task %s", params.Name)

	// Create task
	task := createBaseTask(params.Name, params.Schedule, params.Description, params.Enabled)
	task.Type = model.TypeShellCommand.String()
	task.Command = params.Command

	// Add task to scheduler
	if err := s.scheduler.AddTask(task); err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(task)
}

func (s *MCPServer) handleAddAITask(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract parameters
	var params AITaskParams

	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	// Validate parameters
	if err := validateAITaskParams(params.Name, params.Schedule, params.Prompt); err != nil {
		return createErrorResponse(err)
	}

	s.logger.Debugf("Handling add_ai_task request for task %s", params.Name)

	// Create task
	task := createBaseTask(params.Name, params.Schedule, params.Description, params.Enabled)
	task.Type = model.TypeAI.String()
	task.Prompt = params.Prompt

	// Add task to scheduler
	if err := s.scheduler.AddTask(task); err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(task)
}

// createBaseTask creates a base task with common fields initialized
func createBaseTask(name, schedule, description string, enabled bool) *model.Task {
	now := time.Now()
	taskID := fmt.Sprintf("task_%d", now.UnixNano())

	return &model.Task{
		ID:          taskID,
		Name:        name,
		Schedule:    schedule,
		Description: description,
		Enabled:     enabled,
		Status:      model.StatusPending,
		LastRun:     now,
		NextRun:     now,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
}

// handleUpdateTask updates an existing task
func (s *MCPServer) handleUpdateTask(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract parameters
	var params AITaskParams

	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	if params.ID == "" {
		return createErrorResponse(errors.InvalidInput("task ID is required"))
	}

	s.logger.Debugf("Handling update_task request for task %s", params.ID)

	// Get existing task
	existingTask, err := s.scheduler.GetTask(params.ID)
	if err != nil {
		return createErrorResponse(err)
	}

	// Update fields with provided values
	updateTaskFields(existingTask, params, request.Params.Arguments)

	// Update task in scheduler
	if err := s.scheduler.UpdateTask(existingTask); err != nil {
		return createErrorResponse(err)
	}

	return createTaskResponse(existingTask)
}

// updateTaskFields updates task fields with provided values
func updateTaskFields(task *model.Task, params AITaskParams, rawJSON []byte) {
	// Update non-empty string fields
	if params.Name != "" {
		task.Name = params.Name
	}
	if params.Schedule != "" {
		task.Schedule = params.Schedule
	}
	if params.Command != "" {
		task.Command = params.Command
	}
	if params.Prompt != "" {
		task.Prompt = params.Prompt
	}
	if params.Description != "" {
		task.Description = params.Description
	}

	// Update task type if provided
	if params.Type != "" {
		if strings.EqualFold(params.Type, model.TypeAI.String()) {
			task.Type = model.TypeAI.String()
		} else if strings.EqualFold(params.Type, model.TypeShellCommand.String()) {
			task.Type = model.TypeShellCommand.String()
		}
	}

	// Only update Enabled if it's explicitly in the JSON
	var rawParams map[string]interface{}
	if err := utils.JsonUnmarshal(rawJSON, &rawParams); err == nil {
		if _, exists := rawParams["enabled"]; exists {
			task.Enabled = params.Enabled
		}
	}

	task.UpdatedAt = time.Now()
}

// handleRemoveTask removes a task
func (s *MCPServer) handleRemoveTask(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
func (s *MCPServer) handleEnableTask(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
func (s *MCPServer) handleDisableTask(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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

// handleGetTaskResult returns execution results for a task
func (s *MCPServer) handleGetTaskResult(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var params TaskResultParams
	if err := extractParams(request, &params); err != nil {
		return createErrorResponse(err)
	}

	if params.ID == "" {
		return createErrorResponse(errors.InvalidInput("task ID is required"))
	}

	s.logger.Debugf("Handling get_task_result request for task %s (limit=%d)", params.ID, params.Limit)

	limit := params.Limit
	if limit <= 0 {
		limit = 1
	}

	if limit == 1 {
		result, found := s.GetTaskResult(params.ID)
		if !found {
			return createErrorResponse(errors.NotFound("result", params.ID))
		}
		return createResultResponse(result)
	}

	results, err := s.GetTaskResults(params.ID, limit)
	if err != nil {
		return createErrorResponse(errors.Internal(fmt.Errorf("failed to get results: %w", err)))
	}
	if len(results) == 0 {
		return createErrorResponse(errors.NotFound("result", params.ID))
	}
	return createResultsResponse(results)
}

// Execute implements the taskexec.Executor interface by routing tasks to the appropriate executor
func (s *MCPServer) Execute(ctx context.Context, task *model.Task, timeout time.Duration) error {
	// Get the task type
	taskType := task.Type

	// Route to the appropriate executor based on task type
	s.logger.Debugf("Executing task with type: %s", taskType)

	switch taskType {
	case model.TypeAI.String():
		// Use the agent executor for AI tasks
		s.logger.Infof("Routing to AgentExecutor for AI task")
		return s.agentExecutor.Execute(ctx, task, timeout)

	case model.TypeShellCommand.String(), "":
		// Use the command executor for shell command tasks or when type is not specified
		s.logger.Infof("Routing to CommandExecutor for shell command task")
		return s.cmdExecutor.Execute(ctx, task, timeout)

	default:
		// Unknown task type
		return fmt.Errorf("unknown task type: %s", taskType)
	}
}

// GetTaskResult retrieves execution result for a task regardless of executor type.
// It checks the persistent store first, then falls back to in-memory.
func (s *MCPServer) GetTaskResult(taskID string) (*model.Result, bool) {
	// Try the persistent store first.
	if s.resultStore != nil {
		if result, err := s.resultStore.GetLatestResult(taskID); err == nil && result != nil {
			return result, true
		}
	}

	// Fall back to in-memory results.
	if result, exists := s.agentExecutor.GetTaskResult(taskID); exists {
		return result, true
	}

	return s.cmdExecutor.GetTaskResult(taskID)
}

// GetTaskResults retrieves multiple execution results for a task.
func (s *MCPServer) GetTaskResults(taskID string, limit int) ([]*model.Result, error) {
	if s.resultStore != nil {
		return s.resultStore.GetResults(taskID, limit)
	}

	// Fall back to in-memory (only latest available).
	if result, exists := s.agentExecutor.GetTaskResult(taskID); exists {
		return []*model.Result{result}, nil
	}
	if result, exists := s.cmdExecutor.GetTaskResult(taskID); exists {
		return []*model.Result{result}, nil
	}
	return nil, nil
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
