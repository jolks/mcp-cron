// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"context"
	"reflect"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ToolDefinition represents a tool that can be registered with the MCP server
type ToolDefinition struct {
	// Name is the name of the tool
	Name string

	// Description is a brief description of what the tool does
	Description string

	// Handler is the function that will be called when the tool is invoked
	Handler func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error)

	// Parameters is the parameter schema for the tool (can be a struct)
	Parameters interface{}
}

// registerToolsDeclarative sets up all the MCP tools using a more declarative approach
func (s *MCPServer) registerToolsDeclarative() {
	// Define all the tools in one place
	tools := []ToolDefinition{
		{
			Name:        "list_tasks",
			Description: "Lists all scheduled tasks",
			Handler:     s.handleListTasks,
			Parameters:  struct{}{},
		},
		{
			Name:        "get_task",
			Description: "Gets a specific task by ID",
			Handler:     s.handleGetTask,
			Parameters:  TaskIDParams{},
		},
		{
			Name:        "add_task",
			Description: "Adds a new scheduled shell command task",
			Handler:     s.handleAddTask,
			Parameters:  TaskParams{},
		},
		{
			Name:        "add_ai_task",
			Description: "Adds a new scheduled AI (LLM) task. Use the 'prompt' field to directly specify what the AI should do.",
			Handler:     s.handleAddAITask,
			Parameters:  AITaskParams{},
		},
		{
			Name:        "update_task",
			Description: "Updates an existing task",
			Handler:     s.handleUpdateTask,
			Parameters:  AITaskParams{},
		},
		{
			Name:        "remove_task",
			Description: "Removes a task by ID",
			Handler:     s.handleRemoveTask,
			Parameters:  TaskIDParams{},
		},
		{
			Name:        "enable_task",
			Description: "Enables a disabled task",
			Handler:     s.handleEnableTask,
			Parameters:  TaskIDParams{},
		},
		{
			Name:        "disable_task",
			Description: "Disables an enabled task",
			Handler:     s.handleDisableTask,
			Parameters:  TaskIDParams{},
		},
		{
			Name:        "get_task_result",
			Description: "Gets execution results for a task. Returns the latest result by default, or recent history when limit > 1.",
			Handler:     s.handleGetTaskResult,
			Parameters:  TaskResultParams{},
		},
	}

	// Register all the tools
	for _, tool := range tools {
		registerToolWithError(s.server, tool)
	}
}

// registerToolWithError registers a tool with the MCP server
func registerToolWithError(srv *mcp.Server, def ToolDefinition) {
	schema := buildSchema(def.Parameters)
	tool := &mcp.Tool{
		Name:        def.Name,
		Description: def.Description,
		InputSchema: schema,
	}
	srv.AddTool(tool, def.Handler)
}

// buildSchema converts a Go struct with json and description tags into a JSON Schema object
func buildSchema(params interface{}) map[string]interface{} {
	t := reflect.TypeOf(params)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	properties := map[string]interface{}{}
	var required []string

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		jsonTag := field.Tag.Get("json")
		if jsonTag == "" || jsonTag == "-" {
			continue
		}

		// Parse json tag to get field name and options
		parts := strings.Split(jsonTag, ",")
		fieldName := parts[0]
		omitempty := false
		for _, p := range parts[1:] {
			if p == "omitempty" {
				omitempty = true
			}
		}

		prop := map[string]interface{}{
			"type": goTypeToJSONType(field.Type),
		}

		if desc := field.Tag.Get("description"); desc != "" {
			prop["description"] = desc
		}

		properties[fieldName] = prop

		if !omitempty {
			required = append(required, fieldName)
		}
	}

	schema := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// goTypeToJSONType maps Go types to JSON Schema types
func goTypeToJSONType(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer"
	case reflect.Float32, reflect.Float64:
		return "number"
	default:
		return "string"
	}
}
