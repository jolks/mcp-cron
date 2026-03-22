// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBuildToolsFromConfig(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "mcp-tools-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Suppress expected log output from connection failures
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)

	// Create a valid MCP config file
	validConfig := `{
		"mcpServers": {
			"test-server": {
				"url": "http://localhost:8080"
			},
			"stdio-server": {
				"command": "echo",
				"args": ["hello"]
			},
			"invalid-server": {
			}
		}
	}`
	validConfigPath := filepath.Join(tempDir, "valid-config.json")
	if err := os.WriteFile(validConfigPath, []byte(validConfig), 0644); err != nil {
		t.Fatalf("Failed to write valid config file: %v", err)
	}

	// Create an invalid MCP config file
	invalidConfig := `{
		"mcpServers": {
			"test-server": {
				"url": "http://localhost:8080",
	}`
	invalidConfigPath := filepath.Join(tempDir, "invalid-config.json")
	if err := os.WriteFile(invalidConfigPath, []byte(invalidConfig), 0644); err != nil {
		t.Fatalf("Failed to write invalid config file: %v", err)
	}

	// Test with valid config
	// Since we can't easily mock the MCP server, we'll test the error case
	// where the server doesn't exist or isn't accessible
	cfg := &config.Config{
		AI: config.AIConfig{
			MCPConfigFilePath: validConfigPath,
		},
	}

	tools, dispatcher, closeFn, err := buildToolsFromConfig(cfg)
	if closeFn != nil {
		defer closeFn()
	}
	// We expect no error but also no tools since the servers aren't available
	if err != nil {
		t.Errorf("buildToolsFromConfig with valid config should not return error: %v", err)
	}
	if len(tools) != 0 {
		t.Errorf("Expected 0 tools (since servers aren't available), got %d", len(tools))
	}
	if dispatcher != nil {
		t.Error("Expected nil dispatcher (since no tools), got non-nil")
	}

	// Test with invalid config file
	invalidCfg := &config.Config{
		AI: config.AIConfig{
			MCPConfigFilePath: invalidConfigPath,
		},
	}
	_, _, _, err = buildToolsFromConfig(invalidCfg)
	if err == nil {
		t.Error("Expected error for invalid config file, got nil")
	}

	// Test with non-existent file
	nonExistentCfg := &config.Config{
		AI: config.AIConfig{
			MCPConfigFilePath: filepath.Join(tempDir, "non-existent.json"),
		},
	}
	_, _, _, err = buildToolsFromConfig(nonExistentCfg)
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}

func TestSelfReferenceDetection(t *testing.T) {
	// Start a test MCP server that identifies as "mcp-cron" (self-reference)
	srv := mcp.NewServer(&mcp.Implementation{Name: "mcp-cron", Version: "1.0.0"}, nil)
	srv.AddTool(&mcp.Tool{
		Name:        "list_tasks",
		Description: "Lists all tasks",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	go func() {
		_ = srv.Run(context.Background(), serverTransport)
	}()

	// Connect and verify the server identifies as "mcp-cron"
	cli := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	session, err := cli.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("Failed to connect to test server: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	res := session.InitializeResult()
	if res == nil || res.ServerInfo == nil {
		t.Fatal("No server info returned from handshake")
	}
	if res.ServerInfo.Name != "mcp-cron" {
		t.Fatalf("Expected server name 'mcp-cron', got %q", res.ServerInfo.Name)
	}

	// Verify the self-reference check would match
	if res.ServerInfo.Name != config.ServerName {
		t.Errorf("Server name %q should match config.ServerName %q", res.ServerInfo.Name, config.ServerName)
	}
}

func TestStreamableHTTPTransport(t *testing.T) {
	// Create a test MCP server with a tool
	srv := mcp.NewServer(&mcp.Implementation{Name: "test-http-server", Version: "1.0.0"}, nil)
	srv.AddTool(&mcp.Tool{
		Name:        "greet",
		Description: "Returns a greeting",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Name to greet",
				},
			},
			"required": []string{"name"},
		},
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Hello, " + args.Name + "!"}},
		}, nil
	})

	// Wrap with StreamableHTTPHandler and serve via httptest
	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return srv
	}, nil)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	// Connect using StreamableClientTransport (same path buildToolsFromConfig takes for URL specs)
	tp := &mcp.StreamableClientTransport{Endpoint: ts.URL}
	cli := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	session, err := cli.Connect(context.Background(), tp, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	// Verify tool discovery
	resp, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(resp.Tools))
	}
	if resp.Tools[0].Name != "greet" {
		t.Fatalf("Expected tool name 'greet', got %q", resp.Tools[0].Name)
	}

	// Call the tool and verify the result
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "greet",
		Arguments: map[string]any{"name": "World"},
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty content")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Expected TextContent, got %T", result.Content[0])
	}
	if text.Text != "Hello, World!" {
		t.Errorf("Expected 'Hello, World!', got %q", text.Text)
	}
}

func TestEnvPassedToCommand(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mcp-env-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)

	// Config with env vars — the server won't connect (not a real MCP server),
	// but we verify the env parsing works by checking the JSON round-trip
	envConfig := `{
		"mcpServers": {
			"env-server": {
				"command": "echo",
				"args": ["hello"],
				"env": {
					"MY_CUSTOM_VAR": "custom_value",
					"ANOTHER_VAR": "another_value"
				}
			}
		}
	}`
	configPath := filepath.Join(tempDir, "env-config.json")
	if err := os.WriteFile(configPath, []byte(envConfig), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Verify the env field is parsed by re-reading with the same struct
	var cfg struct {
		MCP map[string]struct {
			Command string            `json:"command,omitempty"`
			Args    []string          `json:"args,omitempty"`
			URL     string            `json:"url,omitempty"`
			Env     map[string]string `json:"env,omitempty"`
		} `json:"mcpServers"`
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}
	spec, ok := cfg.MCP["env-server"]
	if !ok {
		t.Fatal("Expected 'env-server' in config")
	}
	if len(spec.Env) != 2 {
		t.Fatalf("Expected 2 env vars, got %d", len(spec.Env))
	}
	if spec.Env["MY_CUSTOM_VAR"] != "custom_value" {
		t.Errorf("Expected MY_CUSTOM_VAR=custom_value, got %q", spec.Env["MY_CUSTOM_VAR"])
	}
	if spec.Env["ANOTHER_VAR"] != "another_value" {
		t.Errorf("Expected ANOTHER_VAR=another_value, got %q", spec.Env["ANOTHER_VAR"])
	}

	// Verify exec.Command gets env vars when built the same way as buildToolsFromConfig
	cmd := exec.Command(spec.Command, spec.Args...)
	if len(spec.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range spec.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	found := 0
	for _, e := range cmd.Env {
		if e == "MY_CUSTOM_VAR=custom_value" || e == "ANOTHER_VAR=another_value" {
			found++
		}
	}
	if found != 2 {
		t.Errorf("Expected both env vars in cmd.Env, found %d", found)
	}
}

func TestNonSelfServerNotSkipped(t *testing.T) {
	// Start a test MCP server with a different name
	srv := mcp.NewServer(&mcp.Implementation{Name: "other-server", Version: "1.0.0"}, nil)
	srv.AddTool(&mcp.Tool{
		Name:        "some_tool",
		Description: "A tool from another server",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	go func() {
		_ = srv.Run(context.Background(), serverTransport)
	}()

	// Connect and verify it would NOT be skipped
	cli := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	session, err := cli.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	res := session.InitializeResult()
	if res == nil || res.ServerInfo == nil {
		t.Fatal("No server info returned")
	}

	if res.ServerInfo.Name == config.ServerName {
		t.Error("Non-self server should not match mcp-cron server name")
	}

	// Verify tools are listed (would be included, not skipped)
	resp, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(resp.Tools) != 1 || resp.Tools[0].Name != "some_tool" {
		t.Errorf("Expected 1 tool named 'some_tool', got %d tools", len(resp.Tools))
	}
}


