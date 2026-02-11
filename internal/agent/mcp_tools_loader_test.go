// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"io"
	"log"
	"os"
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
				"url": "http://localhost:8080/sse"
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
				"url": "http://localhost:8080/sse",
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

	tools, dispatcher, err := buildToolsFromConfig(cfg)
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
	_, _, err = buildToolsFromConfig(invalidCfg)
	if err == nil {
		t.Error("Expected error for invalid config file, got nil")
	}

	// Test with non-existent file
	nonExistentCfg := &config.Config{
		AI: config.AIConfig{
			MCPConfigFilePath: filepath.Join(tempDir, "non-existent.json"),
		},
	}
	_, _, err = buildToolsFromConfig(nonExistentCfg)
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
	defer func() { _ = session.Close() }()

	res := session.InitializeResult()
	if res == nil || res.ServerInfo == nil {
		t.Fatal("No server info returned from handshake")
	}
	if res.ServerInfo.Name != "mcp-cron" {
		t.Fatalf("Expected server name 'mcp-cron', got %q", res.ServerInfo.Name)
	}

	// Verify the self-reference check would match
	cfg := config.DefaultConfig()
	if res.ServerInfo.Name != cfg.Server.Name {
		t.Errorf("Server name %q should match config server name %q", res.ServerInfo.Name, cfg.Server.Name)
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
	defer func() { _ = session.Close() }()

	res := session.InitializeResult()
	if res == nil || res.ServerInfo == nil {
		t.Fatal("No server info returned")
	}

	cfg := config.DefaultConfig()
	if res.ServerInfo.Name == cfg.Server.Name {
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

func TestSelfReferenceWithCustomServerName(t *testing.T) {
	// Test that self-reference detection works with a custom server name
	customName := "my-custom-cron"

	srv := mcp.NewServer(&mcp.Implementation{Name: customName, Version: "1.0.0"}, nil)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	go func() {
		_ = srv.Run(context.Background(), serverTransport)
	}()

	cli := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	session, err := cli.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() { _ = session.Close() }()

	res := session.InitializeResult()
	if res == nil || res.ServerInfo == nil {
		t.Fatal("No server info returned")
	}

	// With default config name "mcp-cron", this should NOT match
	cfg := config.DefaultConfig()
	if res.ServerInfo.Name == cfg.Server.Name {
		t.Error("Custom-named server should not match default mcp-cron name")
	}

	// With matching config name, it SHOULD match
	cfg.Server.Name = customName
	if res.ServerInfo.Name != cfg.Server.Name {
		t.Error("Server name should match when config name is set to the same value")
	}
}
