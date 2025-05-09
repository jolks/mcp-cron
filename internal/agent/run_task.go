// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"fmt"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// RunTask executes an AI task using the OpenAI API
func RunTask(ctx context.Context, t *model.Task, cfg *config.Config) (string, error) {
	logger := logging.GetDefaultLogger().WithField("task_id", t.ID)
	logger.Infof("Running AI task: %s", t.Name)

	// Get tools for the AI agent
	tools, dispatcher, err := buildToolsFromConfig(cfg)
	if err != nil {
		logger.Errorf("Failed to build tools: %v", err)
		return "", err
	}

	// Check for API key
	apiKey := cfg.AI.OpenAIAPIKey
	if apiKey == "" {
		logger.Errorf("OpenAI API key is not set in configuration")
		return "", fmt.Errorf("OpenAI API key is not set in configuration")
	}

	// Create OpenAI client
	client := openai.NewClient(option.WithAPIKey(apiKey))
	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage(t.Prompt),
	}

	// Fallback to LLM if no tools
	if len(tools) == 0 {
		logger.Infof("No tools available, using basic chat completion")
		resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model:    cfg.AI.Model,
			Messages: msgs,
		})
		if err != nil {
			logger.Errorf("Chat completion failed: %v", err)
			return "", err
		}
		result := resp.Choices[0].Message.Content
		logger.Infof("AI task completed successfully")
		return result, nil
	}

	// Tool-enabled loop
	maxIterations := cfg.AI.MaxToolIterations
	logger.Infof("Starting tool-enabled AI task with max %d iterations", maxIterations)

	for i := 0; i < maxIterations; i++ {
		logger.Debugf("AI task iteration %d", i+1)
		resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model:    cfg.AI.Model,
			Messages: msgs,
			Tools:    tools,
		})
		if err != nil {
			logger.Errorf("Chat completion failed on iteration %d: %v", i+1, err)
			return "", err
		}

		m := resp.Choices[0].Message

		// If no tool calls, return the content
		if len(m.ToolCalls) == 0 {
			logger.Infof("AI task completed successfully with %d iterations", i+1)
			return m.Content, nil
		}

		// Add the assistant message with tool calls to the conversation first
		msgs = append(msgs, m.ToParam())

		// Process tool calls
		logger.Debugf("Processing %d tool calls in iteration %d", len(m.ToolCalls), i+1)
		for j, call := range m.ToolCalls {
			logger.Debugf("Tool call %d: %s", j+1, call.Function.Name)
			out, err := dispatcher(ctx, call)
			if err != nil {
				logger.Warnf("Tool call error: %v", err)
				out = "ERROR: " + err.Error()
			}
			msgs = append(msgs, openai.ToolMessage(out, call.ID))
		}
	}

	logger.Errorf("AI task exceeded maximum iterations (%d)", maxIterations)
	return "", fmt.Errorf("tool loop exceeded maximum iterations (%d)", maxIterations)
}
