// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jolks/mcp-cron/internal/config"
)

func TestBuildToolsFromConfig(t *testing.T) {
	// Create a temporary directory for test files
	tempDir, err := os.MkdirTemp("", "mcp-tools-test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

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

	// This test covers the syntax of the buildToolsFromConfig function
	// and basic error handling, but for a complete test we would need
	// to mock the MCP server or use a test server.
}

// Normally we might also have a TestDispatcher function, but we would need
// to mock the MCP server for this, so for now we'll leave it out.

// Test dummy parameter workaround for empty schemas
func TestEmptySchemaFix(t *testing.T) {
	// We can't easily test this function directly, but we can verify
	// that empty schemas are fixed when the dispatcher is created.
	// This would require extensive mocking of the modelcontextprotocol/go-sdk library.
	t.Skip("This test requires mocking the MCP server")
}
