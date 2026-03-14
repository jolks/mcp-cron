// SPDX-License-Identifier: AGPL-3.0-only
package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ServerName is the fixed server name used for MCP identity and self-reference detection.
const ServerName = "mcp-cron"

// Transport mode constants.
const (
	TransportHTTP  = "http"
	TransportStdio = "stdio"
)

// ChatCompletionsOnlyGateways lists hostnames of API proxies that support only
// the Chat Completions API (not the Responses API). When the configured base
// URL matches one of these, the agent falls back to the Chat Completions provider.
var ChatCompletionsOnlyGateways = []string{
	"api.kilo.ai",
	"generativelanguage.googleapis.com",
}

// IsChatCompletionsGateway returns true if baseURL points to a known proxy
// that only supports Chat Completions.
func IsChatCompletionsGateway(baseURL string) bool {
	if baseURL == "" {
		return false
	}
	for _, gw := range ChatCompletionsOnlyGateways {
		if strings.Contains(baseURL, gw) {
			return true
		}
	}
	return false
}

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

	// Transport mode (http, stdio)
	TransportMode string
}

// SchedulerConfig holds scheduler-specific configuration
type SchedulerConfig struct {
	// Default task timeout
	DefaultTimeout time.Duration

	// PollInterval is how often the scheduler checks for due tasks
	PollInterval time.Duration
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
			TransportMode: TransportHTTP,
		},
		Scheduler: SchedulerConfig{
			DefaultTimeout: 0,
			PollInterval:   1 * time.Second,
		},
		Logging: LoggingConfig{
			Level:    "info",
			FilePath: "",
		},
		Store: StoreConfig{
			DBPath: filepath.Join(home, ".mcp-cron", "results.db"),
		},
		AI: AIConfig{
			Provider:        "openai",
			BaseURL:         "",
			APIKey:          "",
			OpenAIAPIKey:    "",
			AnthropicAPIKey: "",
			Model:           "gpt-4o",
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

	if c.Server.TransportMode != TransportHTTP && c.Server.TransportMode != TransportStdio {
		return fmt.Errorf("transport mode must be either '%s' or '%s'", TransportHTTP, TransportStdio)
	}

	// Validate scheduler config
	if c.Scheduler.DefaultTimeout != 0 && c.Scheduler.DefaultTimeout < time.Second {
		return fmt.Errorf("default timeout must be 0 (no timeout) or at least 1 second")
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

	if os.Getenv("MCP_CRON_SERVER_NAME") != "" {
		log.Printf("WARN: MCP_CRON_SERVER_NAME is deprecated and ignored; the server name is fixed to %q to ensure self-reference detection works correctly", ServerName)
	}

	if os.Getenv("MCP_CRON_SERVER_VERSION") != "" {
		log.Printf("WARN: MCP_CRON_SERVER_VERSION is deprecated and ignored; version is set at build time via ldflags")
	}

	// Scheduler configuration
	if val := os.Getenv("MCP_CRON_SCHEDULER_DEFAULT_TIMEOUT"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			config.Scheduler.DefaultTimeout = duration
		}
	}

	if val := os.Getenv("MCP_CRON_POLL_INTERVAL"); val != "" {
		if duration, err := time.ParseDuration(val); err == nil {
			config.Scheduler.PollInterval = duration
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
