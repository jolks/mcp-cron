// SPDX-License-Identifier: AGPL-3.0-only
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Version is the default version, overridden at build time via:
//
//	-ldflags "-X github.com/jolks/mcp-cron/internal/config.Version=1.2.3"
var Version = "dev"

// Config holds the application configuration
type Config struct {
	// Server configuration
	Server ServerConfig

	// Scheduler configuration
	Scheduler SchedulerConfig

	// Logging configuration
	Logging LoggingConfig

	// AI configuration
	AI AIConfig

	// Store configuration
	Store StoreConfig

	// PreventSleep prevents the system from sleeping while mcp-cron is running
	PreventSleep bool
}

// ServerConfig holds server-specific configuration
type ServerConfig struct {
	// Address to bind to
	Address string

	// Port to listen on
	Port int

	// Transport mode (sse, stdio)
	TransportMode string

	// Server name
	Name string

	// Server version
	Version string
}

// SchedulerConfig holds scheduler-specific configuration
type SchedulerConfig struct {
	// Default task timeout
	DefaultTimeout time.Duration
}

// LoggingConfig holds logging-specific configuration
type LoggingConfig struct {
	// Log level (debug, info, warn, error, fatal)
	Level string

	// Log file path (optional)
	FilePath string
}

// StoreConfig holds result store configuration
type StoreConfig struct {
	// DBPath is the path to the SQLite database file
	DBPath string
}

// AIConfig holds AI-specific configuration
type AIConfig struct {
	// Provider selects the LLM backend: "openai" (default) or "anthropic"
	Provider string

	// BaseURL overrides the API endpoint for OpenAI-compatible providers
	// (e.g. Ollama, vLLM, Groq). Empty means use the default.
	BaseURL string

	// APIKey is a generic fallback API key used when the provider-specific
	// key is not set. Loaded from MCP_CRON_AI_API_KEY.
	APIKey string

	// OpenAI API key
	OpenAIAPIKey string

	// AnthropicAPIKey is the API key for the Anthropic provider.
	// Loaded from ANTHROPIC_API_KEY.
	AnthropicAPIKey string

	// Enable OpenAI integration tests
	EnableOpenAITests bool

	// LLM model to use for AI tasks
	Model string

	// Maximum iterations for tool-enabled tasks
	MaxToolIterations int

	// File path for the MCP configuration
	MCPConfigFilePath string
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}

	return &Config{
		Server: ServerConfig{
			Address:       "localhost",
			Port:          8080,
			TransportMode: "sse",
			Name:          "mcp-cron",
			Version:       Version,
		},
		Scheduler: SchedulerConfig{
			DefaultTimeout: 10 * time.Minute,
		},
		Logging: LoggingConfig{
			Level:    "info",
			FilePath: "",
		},
		Store: StoreConfig{
			DBPath: filepath.Join(home, ".mcp-cron", "results.db"),
		},
		AI: AIConfig{
			Provider:          "openai",
			BaseURL:           "",
			APIKey:            "",
			OpenAIAPIKey:      "",
			AnthropicAPIKey:   "",
			EnableOpenAITests: false,
			Model:             "gpt-4o",
			MaxToolIterations: 20,
			MCPConfigFilePath: filepath.Join(home, ".cursor", "mcp.json"),
		},
	}
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	// Validate server config
	if c.Server.Port < 0 || c.Server.Port > 65535 {
		return fmt.Errorf("server port must be between 0 and 65535")
	}

	if c.Server.TransportMode != "sse" && c.Server.TransportMode != "stdio" {
		return fmt.Errorf("transport mode must be either 'sse' or 'stdio'")
	}

	// Validate scheduler config
	if c.Scheduler.DefaultTimeout < time.Second {
		return fmt.Errorf("default timeout must be at least 1 second")
	}

	// Validate logging config
	switch strings.ToLower(c.Logging.Level) {
	case "debug", "info", "warn", "error", "fatal":
		// Valid log level
	default:
		return fmt.Errorf("log level must be one of: debug, info, warn, error, fatal")
	}

	// Validate AI config
	if c.AI.MaxToolIterations < 1 {
		return fmt.Errorf("max tool iterations must be at least 1")
	}

	return nil
}

// FromEnv loads configuration from environment variables
func FromEnv(config *Config) {
	// Server configuration
	if val := os.Getenv("MCP_CRON_SERVER_ADDRESS"); val != "" {
		config.Server.Address = val
	}

	if val := os.Getenv("MCP_CRON_SERVER_PORT"); val != "" {
		if port, err := strconv.Atoi(val); err == nil {
			config.Server.Port = port
		}
	}

	if val := os.Getenv("MCP_CRON_SERVER_TRANSPORT"); val != "" {
		config.Server.TransportMode = val
	}

	if val := os.Getenv("MCP_CRON_SERVER_NAME"); val != "" {
		config.Server.Name = val
	}

	if val := os.Getenv("MCP_CRON_SERVER_VERSION"); val != "" {
		config.Server.Version = val
	}

	// Scheduler configuration
	if val := os.Getenv("MCP_CRON_SCHEDULER_DEFAULT_TIMEOUT"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			config.Scheduler.DefaultTimeout = duration
		}
	}

	// Logging configuration
	if val := os.Getenv("MCP_CRON_LOGGING_LEVEL"); val != "" {
		config.Logging.Level = val
	}

	if val := os.Getenv("MCP_CRON_LOGGING_FILE"); val != "" {
		config.Logging.FilePath = val
	}

	// Store configuration
	if val := os.Getenv("MCP_CRON_STORE_DB_PATH"); val != "" {
		config.Store.DBPath = val
	}

	// AI configuration
	if val := os.Getenv("MCP_CRON_AI_PROVIDER"); val != "" {
		config.AI.Provider = val
	}

	if val := os.Getenv("MCP_CRON_AI_BASE_URL"); val != "" {
		config.AI.BaseURL = val
	}

	if val := os.Getenv("MCP_CRON_AI_API_KEY"); val != "" {
		config.AI.APIKey = val
	}

	if val := os.Getenv("OPENAI_API_KEY"); val != "" {
		config.AI.OpenAIAPIKey = val
	}

	if val := os.Getenv("ANTHROPIC_API_KEY"); val != "" {
		config.AI.AnthropicAPIKey = val
	}

	if val := os.Getenv("MCP_CRON_ENABLE_OPENAI_TESTS"); val != "" {
		config.AI.EnableOpenAITests = strings.ToLower(val) == "true"
	}

	if val := os.Getenv("MCP_CRON_AI_MODEL"); val != "" {
		config.AI.Model = val
	}

	if val := os.Getenv("MCP_CRON_AI_MAX_TOOL_ITERATIONS"); val != "" {
		if iterations, err := strconv.Atoi(val); err == nil {
			config.AI.MaxToolIterations = iterations
		}
	}

	if val := os.Getenv("MCP_CRON_MCP_CONFIG_FILE_PATH"); val != "" {
		config.AI.MCPConfigFilePath = val
	}

	if val := os.Getenv("MCP_CRON_PREVENT_SLEEP"); val != "" {
		config.PreventSleep = strings.ToLower(val) == "true"
	}
}
