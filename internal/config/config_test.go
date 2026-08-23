// SPDX-License-Identifier: AGPL-3.0-only
package config

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	// Test Server defaults
	if cfg.Server.Address != "localhost" {
		t.Errorf("Expected default server address to be 'localhost', got '%s'", cfg.Server.Address)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Expected default server port to be 8080, got %d", cfg.Server.Port)
	}
	if cfg.Server.TransportMode != TransportHTTP {
		t.Errorf("Expected default transport mode to be '%s', got '%s'", TransportHTTP, cfg.Server.TransportMode)
	}

	// Test Scheduler defaults
	if cfg.Scheduler.DefaultTimeout != 10*time.Minute {
		t.Errorf("Expected default timeout to be 10 minutes, got %s", cfg.Scheduler.DefaultTimeout)
	}

	// Test Logging defaults
	if cfg.Logging.Level != "info" {
		t.Errorf("Expected default logging level to be 'info', got '%s'", cfg.Logging.Level)
	}
	if cfg.Logging.FilePath != "" {
		t.Errorf("Expected default log file path to be empty, got '%s'", cfg.Logging.FilePath)
	}

	// Test Store defaults
	expectedDBPath := filepath.Join(os.Getenv("HOME"), ".mcp-cron", "results.db")
	if cfg.Store.DBPath != expectedDBPath {
		t.Errorf("Expected default DB path to be '%s', got '%s'", expectedDBPath, cfg.Store.DBPath)
	}

	// Test AI defaults
	if cfg.AI.Provider != "openai" {
		t.Errorf("Expected default provider to be 'openai', got '%s'", cfg.AI.Provider)
	}
	if cfg.AI.BaseURL != "" {
		t.Errorf("Expected default base URL to be empty, got '%s'", cfg.AI.BaseURL)
	}
	if cfg.AI.APIKey != "" {
		t.Errorf("Expected default API key to be empty, got '%s'", cfg.AI.APIKey)
	}
	if cfg.AI.OpenAIAPIKey != "" {
		t.Errorf("Expected default OpenAI API key to be empty, got '%s'", cfg.AI.OpenAIAPIKey)
	}
	if cfg.AI.AnthropicAPIKey != "" {
		t.Errorf("Expected default Anthropic API key to be empty, got '%s'", cfg.AI.AnthropicAPIKey)
	}
	if cfg.AI.Model != "gpt-4o" {
		t.Errorf("Expected default AI model to be 'gpt-4o', got '%s'", cfg.AI.Model)
	}
	if cfg.AI.MaxToolIterations != 20 {
		t.Errorf("Expected default max tool iterations to be 20, got %d", cfg.AI.MaxToolIterations)
	}

	expectedPath := filepath.Join(os.Getenv("HOME"), ".cursor", "mcp.json")
	if cfg.AI.MCPConfigFilePath != expectedPath {
		t.Errorf("Expected default MCP config file path to be '%s', got '%s'", expectedPath, cfg.AI.MCPConfigFilePath)
	}
}

func TestValidate(t *testing.T) {
	// Test valid config
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Errorf("Default config should be valid, got error: %v", err)
	}

	// Test invalid port (negative)
	invalidPort := DefaultConfig()
	invalidPort.Server.Port = -1
	if err := invalidPort.Validate(); err == nil {
		t.Error("Expected error for negative port, got nil")
	}

	// Test invalid port (too large)
	invalidLargePort := DefaultConfig()
	invalidLargePort.Server.Port = 70000
	if err := invalidLargePort.Validate(); err == nil {
		t.Error("Expected error for port > 65535, got nil")
	}

	// Test invalid transport mode
	invalidTransport := DefaultConfig()
	invalidTransport.Server.TransportMode = "invalid"
	if err := invalidTransport.Validate(); err == nil {
		t.Error("Expected error for invalid transport mode, got nil")
	}

	// Test invalid default timeout (too short)
	invalidTimeout := DefaultConfig()
	invalidTimeout.Scheduler.DefaultTimeout = time.Millisecond * 500
	if err := invalidTimeout.Validate(); err == nil {
		t.Error("Expected error for timeout < 1 second, got nil")
	}

	// Test invalid log level
	invalidLogLevel := DefaultConfig()
	invalidLogLevel.Logging.Level = "invalid"
	if err := invalidLogLevel.Validate(); err == nil {
		t.Error("Expected error for invalid log level, got nil")
	}

	// Test invalid max tool iterations (zero)
	invalidMaxIterations := DefaultConfig()
	invalidMaxIterations.AI.MaxToolIterations = 0
	if err := invalidMaxIterations.Validate(); err == nil {
		t.Error("Expected error for zero max tool iterations, got nil")
	}

	// Non-positive poll interval would panic time.NewTicker at run time
	for _, pi := range []time.Duration{0, -time.Second} {
		invalidPoll := DefaultConfig()
		invalidPoll.Scheduler.PollInterval = pi
		if err := invalidPoll.Validate(); err == nil {
			t.Errorf("Expected error for poll interval %v, got nil", pi)
		}
	}

	// Address must be a host only; the port is a separate setting
	for _, addr := range []string{"127.0.0.1:9000", "[::1]:9000", "localhost:9000"} {
		withPort := DefaultConfig()
		withPort.Server.Address = addr
		err := withPort.Validate()
		if err == nil || !strings.Contains(err.Error(), "must not include a port") {
			t.Errorf("Expected embedded-port error for address %q, got %v", addr, err)
		}
	}
	// ...but only for HTTP, where the address is used
	stdioWithPort := DefaultConfig()
	stdioWithPort.Server.TransportMode = TransportStdio
	stdioWithPort.Server.Address = "127.0.0.1:9000"
	if err := stdioWithPort.Validate(); err != nil {
		t.Errorf("Expected no address error in stdio mode, got %v", err)
	}
}

func TestIsLoopbackAddress(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"localhost", true},
		{"LOCALHOST", false}, // go-sdk's Host-header check is case-sensitive; accepting it here would 403 every request
		{"127.0.0.1", true},
		{"127.0.0.2", true},
		{"::1", true},
		{"[::1]", true},
		{"::1%lo0", true},
		{"[::1%lo0]", true},
		{"[127.0.0.1]", true},
		{"", false},
		{"0.0.0.0", false},
		{"::", false},
		{"[::]", false},
		{"[", false},
		{"192.168.1.5", false},
		{"example.com", false},
	}
	for _, tt := range tests {
		if got := IsLoopbackAddress(tt.addr); got != tt.want {
			t.Errorf("IsLoopbackAddress(%q) = %v, want %v", tt.addr, got, tt.want)
		}
	}
}

func TestListenAddr(t *testing.T) {
	tests := []struct {
		host string
		port int
		want string
	}{
		{"localhost", 8080, "localhost:8080"},
		{"127.0.0.1", 8080, "127.0.0.1:8080"},
		{"::1", 8080, "[::1]:8080"},
		{"[::1]", 8080, "[::1]:8080"},
		{"::1%lo0", 8080, "[::1%lo0]:8080"},
		{"0.0.0.0", 8080, "0.0.0.0:8080"},
		{"", 8080, ":8080"},
	}
	for _, tt := range tests {
		s := ServerConfig{Address: tt.host, Port: tt.port}
		if got := s.ListenAddr(); got != tt.want {
			t.Errorf("ListenAddr(%q, %d) = %q, want %q", tt.host, tt.port, got, tt.want)
		}
	}

	// The IPv6 form must actually be listenable: this is the regression
	// ("::1:8080" → "too many colons") that bracketing fixes.
	ln, err := net.Listen("tcp", (&ServerConfig{Address: "::1"}).ListenAddr())
	if err != nil {
		t.Skipf("IPv6 loopback unavailable on this host: %v", err)
	}
	_ = ln.Close()
}

func TestValidateAuth(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*ServerConfig)
		wantErr bool
	}{
		{"http non-loopback without token", func(s *ServerConfig) { s.Address = "0.0.0.0" }, true},
		{"http non-loopback with token", func(s *ServerConfig) { s.Address = "0.0.0.0"; s.AuthToken = "secret" }, false},
		{"http non-loopback with explicit override", func(s *ServerConfig) { s.Address = "0.0.0.0"; s.AllowUnauthenticated = true }, false},
		{"stdio is unaffected by the address", func(s *ServerConfig) { s.TransportMode = TransportStdio; s.Address = "0.0.0.0" }, false},
		// Tokens that can never match an HTTP request are rejected at startup
		{"token with internal space", func(s *ServerConfig) { s.AuthToken = "sec ret" }, true},
		{"token with trailing newline", func(s *ServerConfig) { s.AuthToken = "secret\n" }, true},
		{"token with tab", func(s *ServerConfig) { s.AuthToken = "\tsecret" }, true},
		{"token with control char", func(s *ServerConfig) { s.AuthToken = "sec\x00ret" }, true},
		// The token is ignored by the stdio transport, so its format is not checked there
		{"stdio ignores a malformed token", func(s *ServerConfig) { s.TransportMode = TransportStdio; s.AuthToken = "sec ret" }, false},
		// Ordinary tokens, including ones outside the b64token alphabet, are fine
		{"plain token", func(s *ServerConfig) { s.AuthToken = "secret" }, false},
		{"b64token alphabet", func(s *ServerConfig) { s.AuthToken = "s3cr3t-token_with.dots~and+slashes/==" }, false},
		{"punctuation token", func(s *ServerConfig) { s.AuthToken = "p@ss:w0rd!" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tc.mutate(&cfg.Server)
			err := cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if err != nil && cfg.Server.AuthToken != "" && strings.Contains(err.Error(), cfg.Server.AuthToken) {
				t.Errorf("validation error must not echo the token; got %q", err)
			}
		})
	}
}

func TestFromEnvIgnoresUnparseable(t *testing.T) {
	// Every non-string env var goes through envParse: a bad value is logged
	// and ignored, leaving the prior value in place.
	t.Setenv("MCP_CRON_SERVER_PORT", "eighty")
	t.Setenv("MCP_CRON_POLL_INTERVAL", "1sec")
	t.Setenv("MCP_CRON_AI_MAX_TOOL_ITERATIONS", "many")
	cfg := DefaultConfig()
	want := *cfg
	FromEnv(cfg)
	if cfg.Server.Port != want.Server.Port || cfg.Scheduler.PollInterval != want.Scheduler.PollInterval || cfg.AI.MaxToolIterations != want.AI.MaxToolIterations {
		t.Errorf("unparseable env values were applied: port=%d poll=%v iterations=%d", cfg.Server.Port, cfg.Scheduler.PollInterval, cfg.AI.MaxToolIterations)
	}
}

func TestFromEnvPreventSleep(t *testing.T) {
	for val, want := range map[string]bool{"true": true, "1": true, "TRUE": true, "0": false, "false": false} {
		t.Setenv("MCP_CRON_PREVENT_SLEEP", val)
		cfg := DefaultConfig()
		FromEnv(cfg)
		if cfg.PreventSleep != want {
			t.Errorf("MCP_CRON_PREVENT_SLEEP=%q: got %v, want %v", val, cfg.PreventSleep, want)
		}
	}
}

func TestFromEnvAuth(t *testing.T) {
	t.Setenv("MCP_CRON_SERVER_AUTH_TOKEN", "env-secret")
	cfg := DefaultConfig()
	FromEnv(cfg)
	if cfg.Server.AuthToken != "env-secret" {
		t.Errorf("Expected auth token 'env-secret', got '%s'", cfg.Server.AuthToken)
	}

	// strconv.ParseBool forms are accepted, not just the literal "true"
	for val, want := range map[string]bool{"TRUE": true, "1": true, "t": true, "True": true, "false": false, "0": false, "f": false, "FALSE": false} {
		t.Setenv("MCP_CRON_SERVER_ALLOW_UNAUTHENTICATED", val)
		cfg = DefaultConfig()
		FromEnv(cfg)
		if cfg.Server.AllowUnauthenticated != want {
			t.Errorf("MCP_CRON_SERVER_ALLOW_UNAUTHENTICATED=%q: got %v, want %v", val, cfg.Server.AllowUnauthenticated, want)
		}
	}

	// An unparseable value is ignored (leaves the prior value), not treated as false
	t.Setenv("MCP_CRON_SERVER_ALLOW_UNAUTHENTICATED", "yes")
	cfg = DefaultConfig()
	cfg.Server.AllowUnauthenticated = true
	FromEnv(cfg)
	if !cfg.Server.AllowUnauthenticated {
		t.Error("Expected unparseable MCP_CRON_SERVER_ALLOW_UNAUTHENTICATED=yes to be ignored, but it was applied as false")
	}

	// Surrounding whitespace (e.g. a trailing newline from `echo` into a
	// secret file) is trimmed so the token actually matches requests
	t.Setenv("MCP_CRON_SERVER_AUTH_TOKEN", " env-secret\n")
	cfg = DefaultConfig()
	FromEnv(cfg)
	if cfg.Server.AuthToken != "env-secret" {
		t.Errorf("Expected trimmed auth token 'env-secret', got %q", cfg.Server.AuthToken)
	}

	// Whitespace-only is treated as unset
	t.Setenv("MCP_CRON_SERVER_AUTH_TOKEN", "  \n")
	cfg = DefaultConfig()
	FromEnv(cfg)
	if cfg.Server.AuthToken != "" {
		t.Errorf("Expected whitespace-only token to be treated as unset, got %q", cfg.Server.AuthToken)
	}
}

func TestIsResponsesAPICapable(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		want    bool
	}{
		{"empty (direct openai default)", "", true},
		{"direct openai", "https://api.openai.com/v1", true},
		{"litellm proxy", "https://litellm.example.com", false},
		{"kilo gateway", "https://api.kilo.ai/api/gateway", false},
		{"gemini", "https://generativelanguage.googleapis.com/v1beta/openai", false},
		{"ollama", "http://localhost:11434/v1", false},
		{"groq", "https://api.groq.com/openai/v1", false},
		{"spoofed openai subdomain", "https://api.openai.com.evil.com/v1", false},
		{"azure openai", "https://myresource.openai.azure.com/openai/v1/", true},
		{"azure openai other resource", "https://contoso.openai.azure.com/openai/v1/", true},
		{"spoofed azure suffix", "https://openai.azure.com.evil.com/v1", false},
		{"spoofed azure no dot prefix", "https://fakeopenai.azure.com/v1", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsResponsesAPICapable(tt.baseURL)
			if got != tt.want {
				t.Errorf("IsResponsesAPICapable(%q) = %v, want %v", tt.baseURL, got, tt.want)
			}
		})
	}
}

func TestFromEnv(t *testing.T) {
	// Save current environment variables
	originalVars := map[string]string{
		"MCP_CRON_SERVER_ADDRESS":            os.Getenv("MCP_CRON_SERVER_ADDRESS"),
		"MCP_CRON_SERVER_PORT":               os.Getenv("MCP_CRON_SERVER_PORT"),
		"MCP_CRON_SERVER_TRANSPORT":          os.Getenv("MCP_CRON_SERVER_TRANSPORT"),
		"MCP_CRON_SCHEDULER_DEFAULT_TIMEOUT": os.Getenv("MCP_CRON_SCHEDULER_DEFAULT_TIMEOUT"),
		"MCP_CRON_LOGGING_LEVEL":             os.Getenv("MCP_CRON_LOGGING_LEVEL"),
		"MCP_CRON_LOGGING_FILE":              os.Getenv("MCP_CRON_LOGGING_FILE"),
		"MCP_CRON_AI_PROVIDER":               os.Getenv("MCP_CRON_AI_PROVIDER"),
		"MCP_CRON_AI_BASE_URL":               os.Getenv("MCP_CRON_AI_BASE_URL"),
		"MCP_CRON_AI_API_KEY":                os.Getenv("MCP_CRON_AI_API_KEY"),
		"OPENAI_API_KEY":                     os.Getenv("OPENAI_API_KEY"),
		"ANTHROPIC_API_KEY":                  os.Getenv("ANTHROPIC_API_KEY"),
		"MCP_CRON_AI_MODEL":                  os.Getenv("MCP_CRON_AI_MODEL"),
		"MCP_CRON_AI_MAX_TOOL_ITERATIONS":    os.Getenv("MCP_CRON_AI_MAX_TOOL_ITERATIONS"),
		"MCP_CRON_MCP_CONFIG_FILE_PATH":      os.Getenv("MCP_CRON_MCP_CONFIG_FILE_PATH"),
		"MCP_CRON_STORE_DB_PATH":             os.Getenv("MCP_CRON_STORE_DB_PATH"),
	}

	// Restore environment variables after test
	defer func() {
		for key, value := range originalVars {
			if value != "" {
				_ = os.Setenv(key, value)
			} else {
				_ = os.Unsetenv(key)
			}
		}
	}()

	// Clear all relevant environment variables
	for key := range originalVars {
		_ = os.Unsetenv(key)
	}

	// Set test values
	_ = os.Setenv("MCP_CRON_SERVER_ADDRESS", "127.0.0.1")
	_ = os.Setenv("MCP_CRON_SERVER_PORT", "9090")
	_ = os.Setenv("MCP_CRON_SERVER_TRANSPORT", "stdio")
	_ = os.Setenv("MCP_CRON_SCHEDULER_DEFAULT_TIMEOUT", "5m")
	_ = os.Setenv("MCP_CRON_LOGGING_LEVEL", "debug")
	_ = os.Setenv("MCP_CRON_LOGGING_FILE", "/tmp/test.log")
	_ = os.Setenv("MCP_CRON_AI_PROVIDER", "anthropic")
	_ = os.Setenv("MCP_CRON_AI_BASE_URL", "http://localhost:11434/v1")
	_ = os.Setenv("MCP_CRON_AI_API_KEY", "generic-key")
	_ = os.Setenv("OPENAI_API_KEY", "test-key")
	_ = os.Setenv("ANTHROPIC_API_KEY", "anthropic-key")
	_ = os.Setenv("MCP_CRON_AI_MODEL", "gpt-4-turbo")
	_ = os.Setenv("MCP_CRON_AI_MAX_TOOL_ITERATIONS", "30")
	_ = os.Setenv("MCP_CRON_MCP_CONFIG_FILE_PATH", "/tmp/mcp.json")
	_ = os.Setenv("MCP_CRON_STORE_DB_PATH", "/tmp/custom-results.db")

	// Create a new config and apply environment variables
	cfg := DefaultConfig()
	FromEnv(cfg)

	// Verify values were loaded from environment
	if cfg.Server.Address != "127.0.0.1" {
		t.Errorf("Expected server address '127.0.0.1', got '%s'", cfg.Server.Address)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("Expected server port 9090, got %d", cfg.Server.Port)
	}
	if cfg.Server.TransportMode != TransportStdio {
		t.Errorf("Expected transport mode '%s', got '%s'", TransportStdio, cfg.Server.TransportMode)
	}
	if cfg.Scheduler.DefaultTimeout != 5*time.Minute {
		t.Errorf("Expected default timeout 5m, got %s", cfg.Scheduler.DefaultTimeout)
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Expected logging level 'debug', got '%s'", cfg.Logging.Level)
	}
	if cfg.Logging.FilePath != "/tmp/test.log" {
		t.Errorf("Expected log file path '/tmp/test.log', got '%s'", cfg.Logging.FilePath)
	}
	if cfg.AI.Provider != "anthropic" {
		t.Errorf("Expected provider 'anthropic', got '%s'", cfg.AI.Provider)
	}
	if cfg.AI.BaseURL != "http://localhost:11434/v1" {
		t.Errorf("Expected base URL 'http://localhost:11434/v1', got '%s'", cfg.AI.BaseURL)
	}
	if cfg.AI.APIKey != "generic-key" {
		t.Errorf("Expected generic API key 'generic-key', got '%s'", cfg.AI.APIKey)
	}
	if cfg.AI.OpenAIAPIKey != "test-key" {
		t.Errorf("Expected OpenAI API key 'test-key', got '%s'", cfg.AI.OpenAIAPIKey)
	}
	if cfg.AI.AnthropicAPIKey != "anthropic-key" {
		t.Errorf("Expected Anthropic API key 'anthropic-key', got '%s'", cfg.AI.AnthropicAPIKey)
	}
	if cfg.AI.Model != "gpt-4-turbo" {
		t.Errorf("Expected AI model 'gpt-4-turbo', got '%s'", cfg.AI.Model)
	}
	if cfg.AI.MaxToolIterations != 30 {
		t.Errorf("Expected max tool iterations 30, got %d", cfg.AI.MaxToolIterations)
	}
	if cfg.AI.MCPConfigFilePath != "/tmp/mcp.json" {
		t.Errorf("Expected MCP config file path '/tmp/mcp.json', got '%s'", cfg.AI.MCPConfigFilePath)
	}
	if cfg.Store.DBPath != "/tmp/custom-results.db" {
		t.Errorf("Expected store DB path '/tmp/custom-results.db', got '%s'", cfg.Store.DBPath)
	}

	// Test invalid port format
	_ = os.Setenv("MCP_CRON_SERVER_PORT", "invalid")
	cfg = DefaultConfig()
	FromEnv(cfg)
	if cfg.Server.Port != 8080 {
		t.Errorf("Expected server port to remain 8080 for invalid input, got %d", cfg.Server.Port)
	}

	// Test invalid timeout format
	_ = os.Setenv("MCP_CRON_SCHEDULER_DEFAULT_TIMEOUT", "invalid")
	cfg = DefaultConfig()
	FromEnv(cfg)
	if cfg.Scheduler.DefaultTimeout != 10*time.Minute {
		t.Errorf("Expected default timeout to remain 10m for invalid input, got %s", cfg.Scheduler.DefaultTimeout)
	}

	// Test invalid max tool iterations format
	_ = os.Setenv("MCP_CRON_AI_MAX_TOOL_ITERATIONS", "invalid")
	cfg = DefaultConfig()
	FromEnv(cfg)
	if cfg.AI.MaxToolIterations != 20 {
		t.Errorf("Expected max tool iterations to remain 20 for invalid input, got %d", cfg.AI.MaxToolIterations)
	}
}
