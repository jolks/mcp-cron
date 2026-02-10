// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type toolCaller func(context.Context, ToolCall) (string, error)

func buildToolsFromConfig(sysCfg *config.Config) ([]ToolDefinition, toolCaller, error) {
	// Parse the config file
	// TODO: support Env
	var cfg struct {
		MCP map[string]struct {
			Command string   `json:"command,omitempty"`
			Args    []string `json:"args,omitempty"`
			URL     string   `json:"url,omitempty"`
		} `json:"mcpServers"`
	}
	raw, err := os.ReadFile(sysCfg.AI.MCPConfigFilePath)
	if err != nil {
		return nil, nil, err
	}
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return nil, nil, err
	}

	// Create a go-sdk client per server and collect its tools
	var tools []ToolDefinition
	sessionBySrv := map[string]*mcp.ClientSession{}
	tool2srv := map[string]string{} // toolName -> serverName

	for name, spec := range cfg.MCP {
		var tp mcp.Transport
		switch {
		case spec.Command != "":
			tp = &mcp.CommandTransport{Command: exec.Command(spec.Command, spec.Args...)}
		case spec.URL != "":
			tp = &mcp.SSEClientTransport{Endpoint: spec.URL}
		default:
			continue
		}

		cli := mcp.NewClient(&mcp.Implementation{Name: "mcp-cron", Version: "1.0.0"}, nil)
		session, err := cli.Connect(context.Background(), tp, nil)
		if err != nil {
			log.Printf("Failed to connect to server %s: %v\n", name, err)
			continue
		}
		sessionBySrv[name] = session

		resp, err := session.ListTools(context.Background(), nil)
		if err != nil {
			log.Printf("Failed to list tools for server %s: %v\n", name, err)
			continue
		}
		for _, tl := range resp.Tools {
			// Extract the raw JSON-schema
			var rawSchema []byte
			if tl.InputSchema != nil {
				if b, err := json.Marshal(tl.InputSchema); err == nil {
					rawSchema = b
				} else {
					log.Printf("Failed to marshal input schema for tool %s: %v\n", tl.Name, err)
					continue
				}
			}
			// Unmarshal into map[string]interface{} for the SDK
			var params map[string]interface{}
			if err := json.Unmarshal(rawSchema, &params); err != nil {
				log.Printf("Failed to unmarshal input schema for tool %s: %v\n", tl.Name, err)
				continue
			}

			// WORKAROUND: Fix empty parameter schemas to avoid OpenAI API errors.
			// Check if this is an empty schema (no properties).
			if params["type"] == "object" && (params["properties"] == nil || len(params["properties"].(map[string]interface{})) == 0) {
				// Add a dummy property to satisfy OpenAI API requirements
				props := map[string]interface{}{
					"random_string": map[string]interface{}{
						"type":        "string",
						"description": "Dummy parameter for no-parameter tools",
					},
				}
				params["properties"] = props
				params["required"] = []string{"random_string"}
				log.Printf("Added dummy parameter to empty schema for tool %s\n", tl.Name)
			}

			tools = append(tools, ToolDefinition{
				Name:        tl.Name,
				Description: tl.Description,
				Parameters:  params,
			})
			tool2srv[tl.Name] = name
		}
	}
	// No tools. Fallback to LLM
	if len(tools) == 0 {
		return nil, nil, nil
	}
	// Dispatcher to route model's tool calls to the correct MCP server
	dispatcher := func(ctx context.Context, call ToolCall) (string, error) {
		// Parse arguments JSON string into a map
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return "", fmt.Errorf("failed to unmarshal arguments: %w", err)
		}

		// Check if tool name exists in mapping
		serverName, ok := tool2srv[call.Name]
		if !ok {
			return "", fmt.Errorf("unknown tool: %s", call.Name)
		}

		// Check if session exists in mapping
		session, ok := sessionBySrv[serverName]
		if !ok {
			return "", fmt.Errorf("server not found for tool: %s", call.Name)
		}

		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      call.Name,
			Arguments: args,
		})
		if err != nil {
			return "", err
		}
		// Flatten the tool response into a single string
		out, _ := json.Marshal(res.Content)
		return string(out), nil
	}
	return tools, dispatcher, nil
}
