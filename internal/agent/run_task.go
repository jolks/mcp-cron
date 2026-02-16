// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
)

// buildSystemMessage constructs a system prompt that tells the model about its
// task identity, how to retrieve previous results, and the MCP namespace mapping.
// The most important instructions (task ID + get_task_result) come first, before
// the full tool list, so they aren't lost in a long list of tools.
func buildSystemMessage(taskID string, tools []ToolDefinition, hasResultStore bool) string {
	if len(tools) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("You are an AI assistant executing a scheduled task.")
	if taskID != "" {
		sb.WriteString(" Your task ID is \"")
		sb.WriteString(taskID)
		sb.WriteString("\".")
	}

	// Put the key get_task_result instruction FIRST, before the tool list,
	// so it isn't buried after dozens of tool descriptions.
	if hasResultStore && taskID != "" {
		sb.WriteString("\n\nYou have a tool called \"get_task_result\" that retrieves output from previous executions. To get your own previous results, call it with id=\"")
		sb.WriteString(taskID)
		sb.WriteString("\". Use limit > 1 for multiple past results.")
	}

	sb.WriteString("\n\nIf the task references tools with MCP namespace prefixes (e.g. \"mcp__cron__get_task_result\"), use the matching tool by stripping the prefix.")
	return sb.String()
}

// newChatProvider builds the appropriate ChatProvider based on cfg.AI.Provider.
func newChatProvider(cfg *config.Config) (ChatProvider, error) {
	provider := strings.ToLower(cfg.AI.Provider)
	switch provider {
	case "anthropic":
		apiKey := cfg.AI.AnthropicAPIKey
		if apiKey == "" {
			apiKey = cfg.AI.APIKey
		}
		if apiKey == "" {
			return nil, fmt.Errorf("anthropic API key is not set in configuration")
		}
		return NewAnthropicProvider(apiKey), nil
	default: // "openai" or empty
		apiKey := cfg.AI.OpenAIAPIKey
		if apiKey == "" {
			apiKey = cfg.AI.APIKey
		}
		if apiKey == "" {
			return nil, fmt.Errorf("OpenAI API key is not set in configuration")
		}
		return NewOpenAIProvider(apiKey, cfg.AI.BaseURL), nil
	}
}

// internalGetTaskResultTool is the tool definition for the internal get_task_result tool
// injected into AI tasks so they can read past execution results without spawning
// a second mcp-cron process.
var internalGetTaskResultTool = ToolDefinition{
	Name:        "get_task_result",
	Description: "Gets execution results for a task. Returns the latest result by default, or recent history when limit > 1.",
	Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"id": map[string]interface{}{
				"type":        "string",
				"description": "the ID of the task to get results for",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "number of recent results to return (default 1, max 100)",
			},
		},
		"required": []string{"id"},
	},
}

// handleInternalGetTaskResult dispatches an internal get_task_result call
// against the provided ResultStore.
func handleInternalGetTaskResult(store model.ResultStore, call ToolCall) (string, error) {
	var params struct {
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(call.Arguments), &params); err != nil {
		return "", fmt.Errorf("failed to parse get_task_result arguments: %w", err)
	}
	if params.ID == "" {
		return "", fmt.Errorf("task ID is required")
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 1
	}

	if limit == 1 {
		result, err := store.GetLatestResult(params.ID)
		if err != nil || result == nil {
			return "", fmt.Errorf("no result found for task %s", params.ID)
		}
		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("failed to marshal result: %w", err)
		}
		return string(out), nil
	}

	results, err := store.GetResults(params.ID, limit)
	if err != nil {
		return "", fmt.Errorf("failed to get results: %w", err)
	}
	if len(results) == 0 {
		return "", fmt.Errorf("no results found for task %s", params.ID)
	}
	out, err := json.Marshal(results)
	if err != nil {
		return "", fmt.Errorf("failed to marshal results: %w", err)
	}
	return string(out), nil
}

// RunTask executes an AI task using the configured LLM provider.
func RunTask(ctx context.Context, t *model.Task, cfg *config.Config, resultStore model.ResultStore) (string, error) {
	logger := logging.GetDefaultLogger().WithField("task_id", t.ID)
	logger.Infof("Running AI task: %s", t.Name)

	// Get tools for the AI agent from MCP config
	tools, mcpDispatcher, err := buildToolsFromConfig(cfg)
	if err != nil {
		logger.Warnf("Failed to build MCP tools (continuing with internal tools only): %v", err)
		tools = nil
		mcpDispatcher = nil
	}

	// Inject internal get_task_result tool if a result store is available
	if resultStore != nil {
		tools = append(tools, internalGetTaskResultTool)
	}

	// Build combined dispatcher: internal tools take priority, then MCP tools
	dispatcher := func(ctx context.Context, call ToolCall) (string, error) {
		if call.Name == "get_task_result" && resultStore != nil {
			return handleInternalGetTaskResult(resultStore, call)
		}
		if mcpDispatcher != nil {
			return mcpDispatcher(ctx, call)
		}
		return "", fmt.Errorf("unknown tool: %s", call.Name)
	}

	// Build the provider
	provider, err := newChatProvider(cfg)
	if err != nil {
		logger.Errorf("Failed to create chat provider: %v", err)
		return "", err
	}

	// Build system message listing available tools and namespace mapping
	systemMsg := buildSystemMessage(t.ID, tools, resultStore != nil)
	logger.Infof("System message length: %d chars, tools: %d, resultStore: %v", len(systemMsg), len(tools), resultStore != nil)
	logger.Debugf("System message:\n%s", systemMsg)

	msgs := []Message{
		{Role: "user", Content: t.Prompt},
	}

	// Fallback to LLM if no tools
	if len(tools) == 0 {
		logger.Infof("No tools available, using basic chat completion")
		resp, err := provider.CreateCompletion(ctx, cfg.AI.Model, "", msgs, nil)
		if err != nil {
			logger.Errorf("Chat completion failed: %v", err)
			return "", err
		}
		logger.Infof("AI task completed successfully")
		return resp.Content, nil
	}

	// Tool-enabled loop
	maxIterations := cfg.AI.MaxToolIterations
	logger.Infof("Starting tool-enabled AI task with max %d iterations", maxIterations)

	for i := 0; i < maxIterations; i++ {
		logger.Debugf("AI task iteration %d", i+1)
		resp, err := provider.CreateCompletion(ctx, cfg.AI.Model, systemMsg, msgs, tools)
		if err != nil {
			logger.Errorf("Chat completion failed on iteration %d: %v", i+1, err)
			return "", err
		}

		// If no tool calls, return the content
		if len(resp.ToolCalls) == 0 {
			logger.Infof("AI task completed successfully with %d iterations", i+1)
			return resp.Content, nil
		}

		// Add the assistant message with tool calls to the conversation
		msgs = append(msgs, *resp)

		// Process tool calls
		logger.Debugf("Processing %d tool calls in iteration %d", len(resp.ToolCalls), i+1)
		for j, call := range resp.ToolCalls {
			logger.Debugf("Tool call %d: %s", j+1, call.Name)
			out, err := dispatcher(ctx, call)
			if err != nil {
				logger.Warnf("Tool call error: %v", err)
				out = "ERROR: " + err.Error()
			}
			msgs = append(msgs, Message{
				Role:       "tool",
				Content:    out,
				ToolCallID: call.ID,
			})
		}
	}

	logger.Errorf("AI task exceeded maximum iterations (%d)", maxIterations)
	return "", fmt.Errorf("tool loop exceeded maximum iterations (%d)", maxIterations)
}
