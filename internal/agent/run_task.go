// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
)

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
			return nil, fmt.Errorf("Anthropic API key is not set in configuration")
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

// RunTask executes an AI task using the configured LLM provider.
func RunTask(ctx context.Context, t *model.Task, cfg *config.Config) (string, error) {
	logger := logging.GetDefaultLogger().WithField("task_id", t.ID)
	logger.Infof("Running AI task: %s", t.Name)

	// Get tools for the AI agent
	tools, dispatcher, err := buildToolsFromConfig(cfg)
	if err != nil {
		logger.Errorf("Failed to build tools: %v", err)
		return "", err
	}

	// Build the provider
	provider, err := newChatProvider(cfg)
	if err != nil {
		logger.Errorf("Failed to create chat provider: %v", err)
		return "", err
	}

	msgs := []Message{
		{Role: "user", Content: t.Prompt},
	}

	// Fallback to LLM if no tools
	if len(tools) == 0 {
		logger.Infof("No tools available, using basic chat completion")
		resp, err := provider.CreateCompletion(ctx, cfg.AI.Model, msgs, nil)
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
		resp, err := provider.CreateCompletion(ctx, cfg.AI.Model, msgs, tools)
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
