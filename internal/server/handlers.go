// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jolks/mcp-cron/internal/errors"
	"github.com/jolks/mcp-cron/internal/model"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// extractParams extracts parameters from a tool request
func extractParams(request *mcp.CallToolRequest, params interface{}) error {
	if err := json.Unmarshal(request.Params.Arguments, params); err != nil {
		return errors.InvalidInput(fmt.Sprintf("invalid parameters: %v", err))
	}
	return nil
}

// extractTaskIDParam extracts the task ID parameter from a request
func extractTaskIDParam(request *mcp.CallToolRequest) (string, error) {
	var params TaskIDParams
	if err := extractParams(request, &params); err != nil {
		return "", err
	}

	if params.ID == "" {
		return "", errors.InvalidInput("task ID is required")
	}

	return params.ID, nil
}

// createSuccessResponse creates a success response
func createSuccessResponse(message string) (*mcp.CallToolResult, error) {
	response := map[string]interface{}{
		"success": true,
		"message": message,
	}

	responseJSON, err := json.Marshal(response)
	if err != nil {
		return nil, errors.Internal(fmt.Errorf("failed to marshal response: %w", err))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(responseJSON),
			},
		},
	}, nil
}

// createTaskResponse creates a response with a single task
func createTaskResponse(task *model.Task) (*mcp.CallToolResult, error) {
	taskJSON, err := json.Marshal(task)
	if err != nil {
		return nil, errors.Internal(fmt.Errorf("failed to marshal task: %w", err))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(taskJSON),
			},
		},
	}, nil
}

// createTasksResponse creates a response with multiple tasks
func createTasksResponse(tasks []*model.Task) (*mcp.CallToolResult, error) {
	tasksJSON, err := json.Marshal(tasks)
	if err != nil {
		return nil, errors.Internal(fmt.Errorf("failed to marshal tasks: %w", err))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(tasksJSON),
			},
		},
	}, nil
}

// createResultResponse creates a response with a single result
func createResultResponse(result *model.Result) (*mcp.CallToolResult, error) {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, errors.Internal(fmt.Errorf("failed to marshal result: %w", err))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(resultJSON),
			},
		},
	}, nil
}

// createResultsResponse creates a response with multiple results
func createResultsResponse(results []*model.Result) (*mcp.CallToolResult, error) {
	resultsJSON, err := json.Marshal(results)
	if err != nil {
		return nil, errors.Internal(fmt.Errorf("failed to marshal results: %w", err))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: string(resultsJSON),
			},
		},
	}, nil
}

// validateTaskParams validates the common task parameters
func validateTaskParams(name string) error {
	if name == "" {
		return errors.InvalidInput("missing required field: name is required")
	}
	return nil
}

func parseRunAt(value *string) (*time.Time, error) {
	if value == nil {
		return nil, nil
	}

	runAt, err := time.Parse(time.RFC3339, *value)
	if err != nil {
		return nil, errors.InvalidInput("run_at must be an RFC 3339 timestamp")
	}
	runAt = runAt.UTC()
	return &runAt, nil
}

func validateTaskSchedule(schedule string, runAt *time.Time) error {
	if schedule != "" && runAt != nil {
		return errors.InvalidInput("schedule and run_at are mutually exclusive")
	}
	return nil
}

// validateShellTaskParams validates the parameters specific to shell tasks
func validateShellTaskParams(name, command string) error {
	if err := validateTaskParams(name); err != nil {
		return err
	}

	if command == "" {
		return errors.InvalidInput("missing required field: command is required for shell tasks")
	}

	return nil
}

// validateAITaskParams validates the parameters specific to AI tasks
func validateAITaskParams(name, prompt string) error {
	if err := validateTaskParams(name); err != nil {
		return err
	}

	if prompt == "" {
		return errors.InvalidInput("missing required field: prompt is required for AI tasks")
	}

	return nil
}

// validateHTTPTaskParams validates the parameters specific to HTTP tasks
func validateHTTPTaskParams(name, url string) error {
	if err := validateTaskParams(name); err != nil {
		return err
	}

	if url == "" {
		return errors.InvalidInput("missing required field: url is required for http tasks")
	}

	return nil
}
