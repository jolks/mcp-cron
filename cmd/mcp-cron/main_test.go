// SPDX-License-Identifier: AGPL-3.0-only
package main

import (
	"testing"

	"github.com/jolks/mcp-cron/internal/agent"
	"github.com/jolks/mcp-cron/internal/command"
	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/scheduler"
	"github.com/jolks/mcp-cron/internal/server"
)

// TestMCPServerCreation tests server creation with custom configs
func TestMCPServerCreation(t *testing.T) {
	// Test creating MCP server with custom config

	// Import the config package from the same repo
	cfg := &config.Config{
		Server: config.ServerConfig{
			Address:       "127.0.0.1",
			Port:          9999,
			TransportMode: "stdio", // Use stdio to avoid network binding
		},
		Scheduler: config.SchedulerConfig{
			DefaultTimeout: config.DefaultConfig().Scheduler.DefaultTimeout,
		},
	}

	// Create a scheduler and executors first
	cronScheduler := scheduler.NewScheduler(&cfg.Scheduler)
	commandExecutor := command.NewCommandExecutor(nil)

	// Create agent executor with config
	agentExecutor := agent.NewAgentExecutor(cfg, nil)

	// Create the server with custom config
	mcpServer, err := server.NewMCPServer(cfg, cronScheduler, commandExecutor, agentExecutor, nil)

	if err != nil {
		t.Fatalf("Failed to create MCP server: %v", err)
	}

	if mcpServer == nil {
		t.Fatal("NewMCPServer returned nil server")
	}
}
