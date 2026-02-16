// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"strings"
	"testing"

	"github.com/jolks/mcp-cron/internal/config"
)

func TestNewChatProvider_DefaultIsOpenAI(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.OpenAIAPIKey = "sk-test"

	provider, err := newChatProvider(cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if _, ok := provider.(*OpenAIProvider); !ok {
		t.Errorf("Expected *OpenAIProvider, got %T", provider)
	}
}

func TestNewChatProvider_ExplicitOpenAI(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"
	cfg.AI.OpenAIAPIKey = "sk-test"

	provider, err := newChatProvider(cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if _, ok := provider.(*OpenAIProvider); !ok {
		t.Errorf("Expected *OpenAIProvider, got %T", provider)
	}
}

func TestNewChatProvider_Anthropic(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "anthropic"
	cfg.AI.AnthropicAPIKey = "sk-ant-test"

	provider, err := newChatProvider(cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if _, ok := provider.(*AnthropicProvider); !ok {
		t.Errorf("Expected *AnthropicProvider, got %T", provider)
	}
}

func TestNewChatProvider_AnthropicCaseInsensitive(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "Anthropic"
	cfg.AI.AnthropicAPIKey = "sk-ant-test"

	provider, err := newChatProvider(cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if _, ok := provider.(*AnthropicProvider); !ok {
		t.Errorf("Expected *AnthropicProvider, got %T", provider)
	}
}

func TestNewChatProvider_OpenAIFallbackToGenericKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"
	cfg.AI.OpenAIAPIKey = ""
	cfg.AI.APIKey = "generic-key"

	provider, err := newChatProvider(cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if _, ok := provider.(*OpenAIProvider); !ok {
		t.Errorf("Expected *OpenAIProvider, got %T", provider)
	}
}

func TestNewChatProvider_AnthropicFallbackToGenericKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "anthropic"
	cfg.AI.AnthropicAPIKey = ""
	cfg.AI.APIKey = "generic-key"

	provider, err := newChatProvider(cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if _, ok := provider.(*AnthropicProvider); !ok {
		t.Errorf("Expected *AnthropicProvider, got %T", provider)
	}
}

func TestNewChatProvider_OpenAIMissingKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"
	cfg.AI.OpenAIAPIKey = ""
	cfg.AI.APIKey = ""

	_, err := newChatProvider(cfg)
	if err == nil {
		t.Fatal("Expected error for missing OpenAI API key, got nil")
	}
}

func TestNewChatProvider_AnthropicMissingKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "anthropic"
	cfg.AI.AnthropicAPIKey = ""
	cfg.AI.APIKey = ""

	_, err := newChatProvider(cfg)
	if err == nil {
		t.Fatal("Expected error for missing Anthropic API key, got nil")
	}
}

func TestNewChatProvider_OpenAIKeyTakesPrecedence(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"
	cfg.AI.OpenAIAPIKey = "specific-key"
	cfg.AI.APIKey = "generic-key"

	// Should succeed using the specific key, not fall through to generic
	provider, err := newChatProvider(cfg)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if _, ok := provider.(*OpenAIProvider); !ok {
		t.Errorf("Expected *OpenAIProvider, got %T", provider)
	}
}

func TestBuildSystemMessage_Empty(t *testing.T) {
	msg := buildSystemMessage("task-1", nil, false)
	if msg != "" {
		t.Errorf("Expected empty system message for nil tools, got %q", msg)
	}
}

func TestBuildSystemMessage_WithResultStore(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "get_task_result", Description: "Gets execution results for a task."},
		{Name: "list_tasks", Description: "Lists all tasks"},
	}
	msg := buildSystemMessage("my-task", tools, true)
	if !strings.Contains(msg, "get_task_result") {
		t.Error("Expected system message to mention get_task_result")
	}
	if !strings.Contains(msg, "mcp__") {
		t.Error("Expected system message to contain MCP namespace guidance")
	}
	if !strings.Contains(msg, "my-task") {
		t.Error("Expected system message to contain task ID")
	}
}

func TestBuildSystemMessage_IncludesTaskID(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "get_task_result", Description: "Gets execution results for a task."},
	}
	msg := buildSystemMessage("hn-top-5", tools, true)
	if !strings.Contains(msg, "hn-top-5") {
		t.Error("Expected system message to contain the task ID")
	}
	if !strings.Contains(msg, `id="hn-top-5"`) {
		t.Error("Expected explicit instruction to call get_task_result with the task ID")
	}
}

func TestBuildSystemMessage_NoResultStore(t *testing.T) {
	tools := []ToolDefinition{
		{Name: "get_task_result", Description: "Gets execution results for a task."},
	}
	msg := buildSystemMessage("task-1", tools, false)
	if strings.Contains(msg, "previous") {
		t.Error("Expected no previous-results guidance when result store is unavailable")
	}
}

func TestBuildSystemMessage_NoToolList(t *testing.T) {
	// System message should NOT include a full tool listing (models get
	// tool definitions via the API already).
	tools := []ToolDefinition{
		{Name: "some_tool", Description: "Does something"},
		{Name: "another_tool", Description: "Does another thing"},
	}
	msg := buildSystemMessage("task-1", tools, false)
	if strings.Contains(msg, "some_tool") {
		t.Error("System message should not list individual tool names (API provides them)")
	}
}
