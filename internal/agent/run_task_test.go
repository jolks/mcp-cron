// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
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
