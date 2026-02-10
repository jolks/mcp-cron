// SPDX-License-Identifier: AGPL-3.0-only
package command

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNewCommandExecutor(t *testing.T) {
	executor := NewCommandExecutor(nil)
	if executor == nil {
		t.Fatal("NewCommandExecutor(nil) returned nil")
	}
	if executor.results == nil {
		t.Error("CommandExecutor.results is nil")
	}
}

func TestExecuteCommand(t *testing.T) {
	executor := NewCommandExecutor(nil)
	ctx := context.Background()

	// Execute a simple echo command
	result := executor.ExecuteCommand(ctx, "test-task", "echo 'hello world'", 5*time.Second)

	if result == nil {
		t.Fatal("ExecuteCommand returned nil result")
	}
	if result.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Output, "hello world") {
		t.Errorf("Expected output to contain 'hello world', got: %s", result.Output)
	}
	if result.Error != "" {
		t.Errorf("Expected no error, got: %v", result.Error)
	}
}

func TestExecuteInvalidCommand(t *testing.T) {
	executor := NewCommandExecutor(nil)
	ctx := context.Background()

	// Execute an invalid command
	result := executor.ExecuteCommand(ctx, "test-task", "command_that_does_not_exist", 5*time.Second)

	if result == nil {
		t.Fatal("ExecuteCommand returned nil result")
	}
	if result.ExitCode == 0 {
		t.Error("Expected non-zero exit code for invalid command")
	}
	if result.Error == "" {
		t.Error("Expected error for invalid command, got empty string")
	}
}

func TestCommandTimeout(t *testing.T) {
	executor := NewCommandExecutor(nil)
	ctx := context.Background()

	// Execute a command that should timeout
	result := executor.ExecuteCommand(ctx, "timeout-task", "sleep 5", 1*time.Second)

	if result == nil {
		t.Fatal("ExecuteCommand returned nil result")
	}
	if result.ExitCode == 0 {
		t.Error("Expected non-zero exit code for timed out command")
	}
	if result.Error == "" {
		t.Error("Expected error for timed out command, got empty string")
	}
}
