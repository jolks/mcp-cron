// SPDX-License-Identifier: AGPL-3.0-only
package config

import (
	"fmt"
	"log"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// ServerName is the fixed server name used for MCP identity and self-reference detection.
const ServerName = "mcp-cron"

// Transport mode constants.
const (
	TransportHTTP  = "http"
	TransportStdio = "stdio"
)

// responsesAPIHosts and responsesAPIHostSuffixes identify servers known to
// support the OpenAI Responses API. All other custom base URLs default to the
// Chat Completions API, which is the universally supported format across
// third-party proxies (LiteLLM, Ollama, vLLM, Groq, etc.) and translates
// correctly to non-OpenAI backends like Anthropic.
var responsesAPIHosts = []string{
	"api.openai.com",
}

// responsesAPIHostSuffixes lists hostname suffixes for wildcard matching.
// Each entry matches any hostname ending with the suffix (e.g., ".openai.azure.com"
// matches "myresource.openai.azure.com"). The leading dot prevents false positives
// like "fakeopenai.azure.com".
var responsesAPIHostSuffixes = []string{
	".openai.azure.com",
}

// IsResponsesAPICapable returns true if baseURL points to a server known to
// support the OpenAI Responses API. When baseURL is empty (direct OpenAI
// default), matches a known host exactly, or matches a known hostname suffix
// (e.g., Azure OpenAI), this returns true.
func IsResponsesAPICapable(baseURL string) bool {
	if baseURL == "" {
		return true
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	hostname := u.Hostname()
	for _, host := range responsesAPIHosts {
		if hostname == host {
			return true
		}
	}
	for _, suffix := range responsesAPIHostSuffixes {
		if strings.HasSuffix(hostname, suffix) {
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

	// AuthToken, when set, requires HTTP requests to carry
	// an "Authorization: Bearer <token>" header
	AuthToken string

	// AllowUnauthenticated permits serving HTTP on a non-loopback
	// address without an auth token (dangerous)
	AllowUnauthenticated bool
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
			DefaultTimeout: 10 * time.Minute,
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

// IsLoopbackAddress reports whether addr only binds loopback interfaces.
// It accepts "localhost" (case-insensitive) and any loopback IP literal,
// including bracketed ("[::1]") and zoned ("::1%lo0") IPv6 forms — the same
// rule the go-sdk applies for its own localhost protection. The empty string
// (all interfaces) and unparseable hostnames are treated as non-loopback
// (fail closed).
func IsLoopbackAddress(addr string) bool {
	host := hostLiteral(addr)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

// hostLiteral strips the optional IPv6 brackets from a configured address so
// that "[::1]" and "::1" are treated identically.
func hostLiteral(addr string) string {
	return strings.Trim(addr, "[]")
}

// ListenAddr returns the address the HTTP transport should bind, bracketing
// IPv6 literals so that "::1" becomes "[::1]:8080" (which net.Listen requires)
// without double-wrapping already-bracketed input.
func (s *ServerConfig) ListenAddr() string {
	return net.JoinHostPort(hostLiteral(s.Address), strconv.Itoa(s.Port))
}

// SetAuthToken applies a token from any source (env var, flag), trimming
// surrounding whitespace — typically a trailing newline from a secret file
// created with `echo` — and treating a blank value as "not set" so it does
// not clear a token supplied elsewhere.
func (s *ServerConfig) SetAuthToken(raw string) {
	if token := strings.TrimSpace(raw); token != "" {
		s.AuthToken = token
	}
}

// validateAuthToken rejects tokens that can never be presented by a client.
// RFC 6750's b64token grammar has no whitespace, and HTTP header values
// cannot carry control characters, so a token containing either would
// silently lock every caller out with 401. SetAuthToken trims the edges;
// anything left over is a configuration error. The token itself is never
// included in the message.
func (s *ServerConfig) validateAuthToken() error {
	if i := strings.IndexFunc(s.AuthToken, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }); i >= 0 {
		return fmt.Errorf("auth token contains whitespace or a control character at byte offset %d; bearer tokens cannot include these (check for a trailing newline in the secret source)", i)
	}
	return nil
}

// AuthEnabled reports whether bearer-token authentication is configured.
func (s *ServerConfig) AuthEnabled() bool {
	return s.AuthToken != ""
}

// UnauthenticatedNonLoopback reports whether this configuration would serve
// the HTTP transport on a non-loopback address without authentication.
func (s *ServerConfig) UnauthenticatedNonLoopback() bool {
	return s.TransportMode == TransportHTTP && !s.AuthEnabled() && !IsLoopbackAddress(s.Address)
}

// CheckAuthPolicy enforces the fail-closed rule: serving the HTTP transport
// on a non-loopback address without a token is refused unless the operator
// explicitly opted in with AllowUnauthenticated. It is the single definition
// of that rule, called from both Validate() (CLI entry point) and
// MCPServer.Start() (the layer that opens the socket).
func (s *ServerConfig) CheckAuthPolicy() error {
	if s.UnauthenticatedNonLoopback() && !s.AllowUnauthenticated {
		return fmt.Errorf("refusing to serve http on non-loopback address %q without authentication: set --auth-token (or MCP_CRON_SERVER_AUTH_TOKEN), or pass --allow-unauthenticated to accept the risk", s.Address)
	}
	return nil
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

	if err := c.Server.validateAuthToken(); err != nil {
		return err
	}

	if err := c.Server.CheckAuthPolicy(); err != nil {
		return err
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

	if port, ok := envParse("MCP_CRON_SERVER_PORT", strconv.Atoi); ok {
		config.Server.Port = port
	}

	if val := os.Getenv("MCP_CRON_SERVER_TRANSPORT"); val != "" {
		config.Server.TransportMode = val
	}

	config.Server.SetAuthToken(os.Getenv("MCP_CRON_SERVER_AUTH_TOKEN"))

	if val, ok := envParse("MCP_CRON_SERVER_ALLOW_UNAUTHENTICATED", strconv.ParseBool); ok {
		config.Server.AllowUnauthenticated = val
	}

	if os.Getenv("MCP_CRON_SERVER_NAME") != "" {
		log.Printf("WARN: MCP_CRON_SERVER_NAME is deprecated and ignored; the server name is fixed to %q to ensure self-reference detection works correctly", ServerName)
	}

	if os.Getenv("MCP_CRON_SERVER_VERSION") != "" {
		log.Printf("WARN: MCP_CRON_SERVER_VERSION is deprecated and ignored; version is set at build time via ldflags")
	}

	// Scheduler configuration
	if duration, ok := envParse("MCP_CRON_SCHEDULER_DEFAULT_TIMEOUT", time.ParseDuration); ok {
		config.Scheduler.DefaultTimeout = duration
	}

	if duration, ok := envParse("MCP_CRON_POLL_INTERVAL", time.ParseDuration); ok {
		config.Scheduler.PollInterval = duration
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

	if iterations, ok := envParse("MCP_CRON_AI_MAX_TOOL_ITERATIONS", strconv.Atoi); ok {
		config.AI.MaxToolIterations = iterations
	}

	if val := os.Getenv("MCP_CRON_MCP_CONFIG_FILE_PATH"); val != "" {
		config.AI.MCPConfigFilePath = val
	}

	if val, ok := envParse("MCP_CRON_PREVENT_SLEEP", strconv.ParseBool); ok {
		config.PreventSleep = val
	}
}

// envParse reads a non-string environment variable through parse (e.g.
// strconv.Atoi, strconv.ParseBool, time.ParseDuration). It returns ok=false
// when the variable is unset or blank. A value that does not parse is logged
// and ignored rather than silently dropped, so a typo — especially in a
// security-sensitive opt-in such as MCP_CRON_SERVER_ALLOW_UNAUTHENTICATED —
// is visible instead of surfacing as a confusing downstream error.
func envParse[T any](name string, parse func(string) (T, error)) (value T, ok bool) {
	val := strings.TrimSpace(os.Getenv(name))
	if val == "" {
		return value, false
	}
	parsed, err := parse(val)
	if err != nil {
		log.Printf("WARN: ignoring %s=%q: %v", name, val, err)
		return value, false
	}
	return parsed, true
}
