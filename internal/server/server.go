// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
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
	httpexec "github.com/jolks/mcp-cron/internal/http"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/jolks/mcp-cron/internal/scheduler"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Make os.OpenFile mockable for testing
var osOpenFile = os.OpenFile

// TaskParams holds parameters for various task operations
type TaskParams struct {
	ID          string            `json:"id,omitempty" description:"task ID"`
	Name        string            `json:"name" description:"task name (required)"`
	Schedule    string            `json:"schedule,omitempty" description:"cron expression for recurring execution (e.g. '*/5 * * * *', '@hourly'). Omit to create an on-demand task triggered via run_task."`
	Type        string            `json:"type,omitempty" description:"task type: 'shell_command', 'AI', or 'http'"`
	Command     string            `json:"command,omitempty" description:"shell command to execute (required for shell_command tasks)"`
	URL         string            `json:"url,omitempty" description:"URL to request (required for http tasks)"`
	Method      string            `json:"method,omitempty" description:"HTTP method for http tasks (default POST)"`
	Headers     map[string]string `json:"headers,omitempty" description:"HTTP request headers for http tasks"`
	Body        string            `json:"body,omitempty" description:"HTTP request body for http tasks"`
	Description string            `json:"description,omitempty" description:"task description"`
	Enabled     bool              `json:"enabled,omitempty" description:"whether the task is enabled (defaults to false; set to true to activate immediately)"`
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

// QueryTaskResultParams holds parameters for the query_task_result tool
type QueryTaskResultParams struct {
	SQL string `json:"sql" description:"SQL SELECT query to execute against the database"`
}

// AITaskParams combines task parameters with AI-specific parameters
type AITaskParams struct {
	TaskParams
	Prompt string `json:"prompt" description:"prompt for the AI to execute (required for AI tasks)"`
}

// MCPServer represents the MCP scheduler server
type MCPServer struct {
	scheduler      *scheduler.Scheduler
	cmdExecutor    *command.CommandExecutor
	agentExecutor  *agent.AgentExecutor
	httpExecutor   *httpexec.HTTPExecutor
	resultStore    model.ResultStore
	server         *mcp.Server
	httpServer     *http.Server
	listener       net.Listener
	cancel         context.CancelFunc
	stopCh         chan struct{}
	wg             sync.WaitGroup
	config         *config.Config
	logger         *logging.Logger
	shutdownMutex  sync.Mutex
	isShuttingDown bool
}

// CreateLogger creates a logger appropriate for the given configuration.
// For stdio transport, it directs output to a log file to avoid corrupting
// the JSON-RPC stream on stdout.
func CreateLogger(cfg *config.Config) (*logging.Logger, error) {
	if cfg.Logging.FilePath != "" {
		logger, err := logging.FileLogger(cfg.Logging.FilePath, parseLogLevel(cfg.Logging.Level))
		if err != nil {
			return nil, fmt.Errorf("failed to create file logger: %w", err)
		}
		return logger, nil
	}

	if cfg.Server.TransportMode == config.TransportStdio {
		// For stdio transport, all logging must go to a file to avoid
		// corrupting the JSON-RPC stream on stdout
		execPath, err := os.Executable()
		if err != nil {
			execPath = config.ServerName
		}
		execDir := filepath.Dir(execPath)
		logFilename := fmt.Sprintf("%s.log", config.ServerName)
		logPath := filepath.Join(execDir, logFilename)

		logFile, err := osOpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err == nil {
			log.SetOutput(logFile)
			return logging.New(logging.Options{
				Output: logFile,
				Level:  parseLogLevel(cfg.Logging.Level),
			}), nil
		}
		// Fall back to stderr to avoid corrupting stdout
		log.SetOutput(os.Stderr)
		return logging.New(logging.Options{
			Output: os.Stderr,
			Level:  parseLogLevel(cfg.Logging.Level),
		}), nil
	}

	return logging.New(logging.Options{
		Level: parseLogLevel(cfg.Logging.Level),
	}), nil
}

// NewMCPServer creates a new MCP scheduler server.
//
// httpExecutor is optional: passing nil disables the http task type at
// execution time (handlers still accept add_http_task and the scheduler
// will route it, but Execute will return an error).
func NewMCPServer(cfg *config.Config, scheduler *scheduler.Scheduler, cmdExecutor *command.CommandExecutor, agentExecutor *agent.AgentExecutor, httpExecutor *httpexec.HTTPExecutor, resultStore model.ResultStore, logger *logging.Logger) (*MCPServer, error) {
	// Create default config if not provided
	if cfg == nil {
		cfg = config.DefaultConfig()
	}

	// Validate transport mode
	switch cfg.Server.TransportMode {
	case config.TransportStdio:
		logger.Infof("Using stdio transport")
	case config.TransportHTTP:
		logger.Infof("Using Streamable HTTP transport on %s", cfg.Server.ListenAddr())
	default:
		return nil, errors.InvalidInput(fmt.Sprintf("unsupported transport mode: %s", cfg.Server.TransportMode))
	}

	// Create MCP server
	mcpSrv := mcp.NewServer(&mcp.Implementation{
		Name:    config.ServerName,
		Version: config.Version,
	}, nil)

	// Create MCP Server
	mcpServer := &MCPServer{
		scheduler:     scheduler,
		cmdExecutor:   cmdExecutor,
		agentExecutor: agentExecutor,
		httpExecutor:  httpExecutor,
		resultStore:   resultStore,
		server:        mcpSrv,
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
	case config.TransportStdio:
		if s.config.Server.AuthEnabled() {
			s.logger.Warnf("An auth token is configured but the stdio transport has no HTTP authentication; it is ignored")
		}
		runCtx, cancel := context.WithCancel(ctx)
		s.cancel = cancel
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := s.server.Run(runCtx, &mcp.StdioTransport{}); err != nil {
				s.logger.Errorf("Error running MCP server: %v", err)
			}
			// Signal that stdio transport has exited (e.g. stdin closed)
			close(s.stopCh)
		}()
	case config.TransportHTTP:
		// Enforce the fail-closed rule here as well as in config.Validate():
		// this is the layer that opens the socket, and Validate() only runs
		// from the CLI entry point.
		if err := s.config.Server.CheckAuthPolicy(); err != nil {
			return err
		}
		addr := s.config.Server.ListenAddr()
		var handler http.Handler = mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
			return s.server
		}, nil)
		if s.config.Server.AuthEnabled() {
			handler = requireBearerToken(s.config.Server.AuthToken, handler)
			s.logger.Infof("HTTP bearer-token authentication enabled")
		} else if s.config.Server.UnauthenticatedNonLoopback() {
			s.logger.Warnf("HTTP transport serving on non-loopback address %s WITHOUT authentication", addr)
		}
		// Bind synchronously so a port clash or bad address fails Start()
		// instead of being logged from a goroutine after startup "succeeded".
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to listen on %s: %w", addr, err)
		}
		s.listener = ln
		s.httpServer = &http.Server{
			Handler: handler,
			// Bounds only the header-read phase, which runs before the auth
			// middleware — without it an unauthenticated client trickling
			// header bytes holds a connection (and goroutine) open forever.
			// ReadTimeout/WriteTimeout must stay 0: SSE streams are long-lived.
			ReadHeaderTimeout: 10 * time.Second,
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
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

// ListenAddr returns the address the HTTP transport is bound to. It is nil
// in stdio mode and until Start() has successfully bound the socket, so
// tests that pass port 0 can discover the chosen port.
func (s *MCPServer) ListenAddr() net.Addr {
	if s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// Done returns a channel that is closed when the server's transport exits.
// For stdio mode, this fires when stdin is closed (parent process exited).
func (s *MCPServer) Done() <-chan struct{} {
	return s.stopCh
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
		return nil, err
	}

	s.logger.Debugf("Handling get_task request for task %s", taskID)

	// Get the task
	task, err := s.scheduler.GetTask(taskID)
	if err != nil {
		return nil, err
	}

	return createTaskResponse(task)
}

// handleAddTask adds a new shell command task
func (s *MCPServer) handleAddTask(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract parameters
	var params TaskParams

	if err := extractParams(request, &params); err != nil {
		return nil, err
	}

	// Validate parameters
	if err := validateShellTaskParams(params.Name, params.Command); err != nil {
		return nil, err
	}

	s.logger.Debugf("Handling add_task request for task %s", params.Name)

	// Create task
	task := createBaseTask(params.Name, params.Schedule, params.Description, params.Enabled)
	task.Type = model.TypeShellCommand
	task.Command = params.Command

	// Add task to scheduler
	if err := s.scheduler.AddTask(task); err != nil {
		return nil, err
	}

	return createTaskResponse(task)
}

func (s *MCPServer) handleAddAITask(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract parameters
	var params AITaskParams

	if err := extractParams(request, &params); err != nil {
		return nil, err
	}

	// Validate parameters
	if err := validateAITaskParams(params.Name, params.Prompt); err != nil {
		return nil, err
	}

	s.logger.Debugf("Handling add_ai_task request for task %s", params.Name)

	// Create task
	task := createBaseTask(params.Name, params.Schedule, params.Description, params.Enabled)
	task.Type = model.TypeAI
	task.Prompt = params.Prompt

	// Add task to scheduler
	if err := s.scheduler.AddTask(task); err != nil {
		return nil, err
	}

	return createTaskResponse(task)
}

// handleAddHTTPTask adds a new HTTP/webhook task. Headers and body are optional;
// method defaults to POST when omitted.
func (s *MCPServer) handleAddHTTPTask(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var params TaskParams

	if err := extractParams(request, &params); err != nil {
		return nil, err
	}

	if err := validateHTTPTaskParams(params.Name, params.URL); err != nil {
		return nil, err
	}

	s.logger.Debugf("Handling add_http_task request for task %s", params.Name)

	task := createBaseTask(params.Name, params.Schedule, params.Description, params.Enabled)
	task.Type = model.TypeHTTP
	task.URL = params.URL
	task.Method = params.Method
	task.Headers = params.Headers
	task.Body = params.Body

	if err := s.scheduler.AddTask(task); err != nil {
		return nil, err
	}

	return createTaskResponse(task)
}

// createBaseTask creates a base task with common fields initialized
func createBaseTask(name, schedule, description string, enabled bool) *model.Task {
	now := time.Now()
	var b [8]byte
	_, _ = rand.Read(b[:])
	taskID := "task_" + hex.EncodeToString(b[:])

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
		return nil, err
	}

	if params.ID == "" {
		return nil, errors.InvalidInput("task ID is required")
	}

	s.logger.Debugf("Handling update_task request for task %s", params.ID)

	// Get existing task
	existingTask, err := s.scheduler.GetTask(params.ID)
	if err != nil {
		return nil, err
	}

	// Update fields with provided values
	updateTaskFields(existingTask, params, request.Params.Arguments)

	// Update task in scheduler
	if err := s.scheduler.UpdateTask(existingTask); err != nil {
		return nil, err
	}

	return createTaskResponse(existingTask)
}

// updateTaskFields updates task fields with provided values
func updateTaskFields(task *model.Task, params AITaskParams, rawJSON []byte) {
	// Update non-empty string fields
	if params.Name != "" {
		task.Name = params.Name
	}
	if params.Command != "" {
		task.Command = params.Command
	}
	if params.Prompt != "" {
		task.Prompt = params.Prompt
	}
	if params.URL != "" {
		task.URL = params.URL
	}
	if params.Method != "" {
		task.Method = params.Method
	}
	if params.Headers != nil {
		task.Headers = params.Headers
	}
	if params.Body != "" {
		task.Body = params.Body
	}
	if params.Description != "" {
		task.Description = params.Description
	}

	// Update task type if provided
	if params.Type != "" {
		switch {
		case strings.EqualFold(params.Type, string(model.TypeAI)):
			task.Type = model.TypeAI
		case strings.EqualFold(params.Type, string(model.TypeShellCommand)):
			task.Type = model.TypeShellCommand
		case strings.EqualFold(params.Type, string(model.TypeHTTP)):
			task.Type = model.TypeHTTP
		}
	}

	// Only update Schedule and Enabled if explicitly in the JSON,
	// since their zero values ("" and false) are valid updates.
	var rawParams map[string]interface{}
	if err := json.Unmarshal(rawJSON, &rawParams); err == nil {
		if _, exists := rawParams["schedule"]; exists {
			task.Schedule = params.Schedule
		}
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
		return nil, err
	}

	s.logger.Debugf("Handling remove_task request for task %s", taskID)

	// Remove task
	if err := s.scheduler.RemoveTask(taskID); err != nil {
		return nil, err
	}

	return createSuccessResponse(fmt.Sprintf("Task %s removed successfully", taskID))
}

// handleEnableTask enables a task
func (s *MCPServer) handleEnableTask(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract task ID
	taskID, err := extractTaskIDParam(request)
	if err != nil {
		return nil, err
	}

	s.logger.Debugf("Handling enable_task request for task %s", taskID)

	// Enable task
	if err := s.scheduler.EnableTask(taskID); err != nil {
		return nil, err
	}

	// Get updated task
	task, err := s.scheduler.GetTask(taskID)
	if err != nil {
		return nil, err
	}

	return createTaskResponse(task)
}

// handleDisableTask disables a task
func (s *MCPServer) handleDisableTask(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract task ID
	taskID, err := extractTaskIDParam(request)
	if err != nil {
		return nil, err
	}

	s.logger.Debugf("Handling disable_task request for task %s", taskID)

	// Disable task
	if err := s.scheduler.DisableTask(taskID); err != nil {
		return nil, err
	}

	// Get updated task
	task, err := s.scheduler.GetTask(taskID)
	if err != nil {
		return nil, err
	}

	return createTaskResponse(task)
}

// handleRunTask triggers immediate execution of a task and waits for the result.
func (s *MCPServer) handleRunTask(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	taskID, err := extractTaskIDParam(request)
	if err != nil {
		return nil, err
	}

	s.logger.Debugf("Handling run_task request for task %s", taskID)

	// Snapshot latest result time so we can detect the new one
	var beforeTime time.Time
	if latest, found := s.GetTaskResult(taskID); found {
		beforeTime = latest.EndTime
	}

	// Trigger immediate execution
	if err := s.scheduler.RunTaskNow(taskID); err != nil {
		return nil, err
	}

	// Poll until a new result appears
	timeout := time.After(s.config.Scheduler.DefaultTimeout)
	ticker := time.NewTicker(s.config.Scheduler.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return createSuccessResponse(fmt.Sprintf(
				"Task %s triggered but did not complete within %s. Use get_task_result to check later.", taskID, s.config.Scheduler.DefaultTimeout))
		case <-ticker.C:
			if result, found := s.GetTaskResult(taskID); found && result.EndTime.After(beforeTime) {
				return createResultResponse(result)
			}
		}
	}
}

// handleGetTaskResult returns execution results for a task
func (s *MCPServer) handleGetTaskResult(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var params TaskResultParams
	if err := extractParams(request, &params); err != nil {
		return nil, err
	}

	if params.ID == "" {
		return nil, errors.InvalidInput("task ID is required")
	}

	s.logger.Debugf("Handling get_task_result request for task %s (limit=%d)", params.ID, params.Limit)

	limit := params.Limit
	if limit <= 0 {
		limit = 1
	}

	if limit == 1 {
		result, found := s.GetTaskResult(params.ID)
		if !found {
			return nil, errors.NotFound("result", params.ID)
		}
		return createResultResponse(result)
	}

	results, err := s.GetTaskResults(params.ID, limit)
	if err != nil {
		return nil, errors.Internal(fmt.Errorf("failed to get results: %w", err))
	}
	if len(results) == 0 {
		return nil, errors.NotFound("result", params.ID)
	}
	return createResultsResponse(results)
}

// handleQueryTaskResult executes a read-only SQL query against the database
func (s *MCPServer) handleQueryTaskResult(ctx context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var params QueryTaskResultParams
	if err := extractParams(request, &params); err != nil {
		return nil, err
	}

	if params.SQL == "" {
		return nil, errors.InvalidInput("sql is required")
	}

	s.logger.Debugf("Handling query_task_result request: %s", params.SQL)

	rows, err := s.resultStore.QueryDB(ctx, params.SQL)
	if err != nil {
		if strings.HasPrefix(err.Error(), "invalid input:") {
			return nil, err
		}
		return nil, errors.Internal(err)
	}

	responseJSON, err := json.Marshal(rows)
	if err != nil {
		return nil, errors.Internal(fmt.Errorf("failed to marshal query results: %w", err))
	}

	result := &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(responseJSON),
			},
		},
	}
	if len(rows) >= model.MaxQueryRows {
		result.Content = append(result.Content, &mcp.TextContent{
			Text: fmt.Sprintf("Warning: Results truncated to %d rows. Use LIMIT or WHERE clauses to narrow your query.", model.MaxQueryRows),
		})
	}
	return result, nil
}

// Execute implements the taskexec.Executor interface by routing tasks to the appropriate executor
func (s *MCPServer) Execute(ctx context.Context, task *model.Task, timeout time.Duration) error {
	// Get the task type
	taskType := task.Type

	// Route to the appropriate executor based on task type
	s.logger.Debugf("Executing task with type: %s", taskType)

	switch taskType {
	case model.TypeAI:
		// Use the agent executor for AI tasks
		s.logger.Infof("Routing to AgentExecutor for AI task")
		return s.agentExecutor.Execute(ctx, task, timeout)

	case model.TypeHTTP:
		s.logger.Infof("Routing to HTTPExecutor for HTTP task")
		if s.httpExecutor == nil {
			return fmt.Errorf("http executor not configured")
		}
		return s.httpExecutor.Execute(ctx, task, timeout)

	case model.TypeShellCommand, "":
		// Use the command executor for shell command tasks or when type is not specified
		s.logger.Infof("Routing to CommandExecutor for shell command task")
		return s.cmdExecutor.Execute(ctx, task, timeout)

	default:
		// Unknown task type
		return fmt.Errorf("unknown task type: %s", taskType)
	}
}

// GetTaskResult retrieves the latest execution result for a task.
func (s *MCPServer) GetTaskResult(taskID string) (*model.Result, bool) {
	if s.resultStore == nil {
		return nil, false
	}
	result, err := s.resultStore.GetLatestResult(taskID)
	if err != nil || result == nil {
		return nil, false
	}
	return result, true
}

// GetTaskResults retrieves multiple execution results for a task.
func (s *MCPServer) GetTaskResults(taskID string, limit int) ([]*model.Result, error) {
	if s.resultStore == nil {
		return nil, nil
	}
	return s.resultStore.GetResults(taskID, limit)
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
