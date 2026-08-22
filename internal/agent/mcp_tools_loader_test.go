// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	helperProcessEnvKey = "GO_WANT_HELPER_PROCESS"
	testEnvKey          = "MCP_TEST_ENV_VALUE"
	testEnvValue        = "custom_value"
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
				"url": "http://localhost:8080"
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
				"url": "http://localhost:8080",
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

	tools, dispatcher, closeFn, err := buildToolsFromConfig(cfg)
	if closeFn != nil {
		defer closeFn()
	}
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
	_, _, _, err = buildToolsFromConfig(invalidCfg)
	if err == nil {
		t.Error("Expected error for invalid config file, got nil")
	}

	// Test with non-existent file
	nonExistentCfg := &config.Config{
		AI: config.AIConfig{
			MCPConfigFilePath: filepath.Join(tempDir, "non-existent.json"),
		},
	}
	_, _, _, err = buildToolsFromConfig(nonExistentCfg)
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
	t.Cleanup(func() { _ = session.Close() })

	res := session.InitializeResult()
	if res == nil || res.ServerInfo == nil {
		t.Fatal("No server info returned from handshake")
	}
	if res.ServerInfo.Name != "mcp-cron" {
		t.Fatalf("Expected server name 'mcp-cron', got %q", res.ServerInfo.Name)
	}

	// Verify the self-reference check would match
	if res.ServerInfo.Name != config.ServerName {
		t.Errorf("Server name %q should match config.ServerName %q", res.ServerInfo.Name, config.ServerName)
	}
}

func TestStreamableHTTPTransport(t *testing.T) {
	// Create a test MCP server with a tool
	srv := mcp.NewServer(&mcp.Implementation{Name: "test-http-server", Version: "1.0.0"}, nil)
	srv.AddTool(&mcp.Tool{
		Name:        "greet",
		Description: "Returns a greeting",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Name to greet",
				},
			},
			"required": []string{"name"},
		},
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Hello, " + args.Name + "!"}},
		}, nil
	})

	// Wrap with StreamableHTTPHandler and serve via httptest
	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return srv
	}, nil)
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	// Connect using StreamableClientTransport (same path buildToolsFromConfig takes for URL specs)
	tp := &mcp.StreamableClientTransport{Endpoint: ts.URL}
	cli := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1.0.0"}, nil)
	session, err := cli.Connect(context.Background(), tp, nil)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	// Verify tool discovery
	resp, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools failed: %v", err)
	}
	if len(resp.Tools) != 1 {
		t.Fatalf("Expected 1 tool, got %d", len(resp.Tools))
	}
	if resp.Tools[0].Name != "greet" {
		t.Fatalf("Expected tool name 'greet', got %q", resp.Tools[0].Name)
	}

	// Call the tool and verify the result
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "greet",
		Arguments: map[string]any{"name": "World"},
	})
	if err != nil {
		t.Fatalf("CallTool failed: %v", err)
	}
	if len(result.Content) == 0 {
		t.Fatal("Expected non-empty content")
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Expected TextContent, got %T", result.Content[0])
	}
	if text.Text != "Hello, World!" {
		t.Errorf("Expected 'Hello, World!', got %q", text.Text)
	}
}

func TestHeaderRoundTripperDoesNotMutateOriginal(t *testing.T) {
	var seen http.Header
	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		seen = req.Header
		rec := httptest.NewRecorder()
		rec.WriteHeader(http.StatusOK)
		return rec.Result(), nil
	})
	rt := &headerRoundTripper{base: base, headers: map[string]string{"Authorization": "Bearer secret"}}

	orig := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	resp, err := rt.RoundTrip(orig)
	if err != nil {
		t.Fatalf("RoundTrip failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := seen.Get("Authorization"); got != "Bearer secret" {
		t.Errorf("Expected injected Authorization header, got %q", got)
	}
	if got := orig.Header.Get("Authorization"); got != "" {
		t.Errorf("Original request was mutated: Authorization = %q", got)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// TestHeaderInjectingClientRefusesCrossOriginRedirect guards against token
// leakage: net/http strips Authorization on a cross-host redirect, but
// headerRoundTripper sits below that logic and would re-add it. The client
// must refuse the hop so the configured headers never reach the other origin.
func TestHeaderInjectingClientRefusesCrossOriginRedirect(t *testing.T) {
	var attackerHits int32
	var leaked http.Header
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attackerHits, 1)
		leaked = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(attacker.Close)

	// Both httptest servers listen on 127.0.0.1; different ports make them
	// different origins — exactly the case the policy must reject.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attacker.URL+"/collect", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(origin.Close)

	client := newHeaderInjectingClient(map[string]string{"Authorization": "Bearer secret"})
	resp, err := client.Post(origin.URL+"/mcp", "application/json", strings.NewReader(`{}`))
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected cross-origin redirect to be refused, got nil error")
	}
	if !strings.Contains(err.Error(), "refusing redirect") {
		t.Errorf("unexpected error: %v", err)
	}
	if n := atomic.LoadInt32(&attackerHits); n != 0 {
		t.Errorf("redirect target was contacted %d time(s); leaked headers: %v", n, leaked)
	}
}

func TestHeaderInjectingClientFollowsSameOriginRedirect(t *testing.T) {
	var finalAuth string
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/mcp/", http.StatusPermanentRedirect)
	})
	mux.HandleFunc("/mcp/", func(w http.ResponseWriter, r *http.Request) {
		finalAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client := newHeaderInjectingClient(map[string]string{"Authorization": "Bearer secret"})
	resp, err := client.Post(srv.URL+"/mcp", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("same-origin redirect should be followed: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 after redirect, got %d", resp.StatusCode)
	}
	if finalAuth != "Bearer secret" {
		t.Errorf("expected Authorization to be present on same-origin redirect, got %q", finalAuth)
	}
}

func TestHeaderInjectingClientStopsAfterMaxRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/loop", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)

	client := newHeaderInjectingClient(map[string]string{"X-Test": "1"})
	resp, err := client.Get(srv.URL + "/loop")
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "stopped after 10 redirects") {
		t.Errorf("expected redirect limit error, got %v", err)
	}
}

func TestRefuseCrossOriginRedirectPolicy(t *testing.T) {
	cases := []struct {
		name    string
		from    string
		to      string
		allowed bool
	}{
		{"same origin, different path", "https://mcp.example.com/mcp", "https://mcp.example.com/mcp/", true},
		{"same origin, explicit default port", "https://mcp.example.com/mcp", "https://mcp.example.com:443/mcp", true},
		{"host case-insensitive", "https://MCP.example.com/mcp", "https://mcp.EXAMPLE.com/mcp", true},
		{"http to https upgrade on default ports", "http://mcp.example.com/mcp", "https://mcp.example.com/mcp", true},
		{"https to http downgrade", "https://mcp.example.com/mcp", "http://mcp.example.com/mcp", false},
		{"different host", "https://mcp.example.com/mcp", "https://evil.example/collect", false},
		{"subdomain", "https://example.com/mcp", "https://mcp.example.com/mcp", false},
		{"different port", "http://localhost:8080/mcp", "http://localhost:9090/mcp", false},
		{"http to https on non-default port", "http://localhost:8080/mcp", "https://localhost:8443/mcp", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			from, err := url.Parse(tc.from)
			if err != nil {
				t.Fatal(err)
			}
			to, err := url.Parse(tc.to)
			if err != nil {
				t.Fatal(err)
			}
			err = refuseCrossOriginRedirect(&http.Request{URL: to}, []*http.Request{{URL: from}})
			if tc.allowed && err != nil {
				t.Errorf("expected redirect %s -> %s to be allowed, got %v", tc.from, tc.to, err)
			}
			if !tc.allowed && err == nil {
				t.Errorf("expected redirect %s -> %s to be refused", tc.from, tc.to)
			}
		})
	}
}

func TestStreamableHTTPTransportWithHeaders(t *testing.T) {
	// MCP server behind a bearer-token check
	srv := mcp.NewServer(&mcp.Implementation{Name: "header-test-server", Version: "1.0.0"}, nil)
	srv.AddTool(&mcp.Tool{
		Name:        "ping",
		Description: "Returns pong",
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "pong"}}}, nil
	})
	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return srv
	}, nil)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mcpHandler.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)

	// mcp.json spec with the new headers field
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "mcp.json")
	mcpConfig := fmt.Sprintf(`{
		"mcpServers": {
			"header-server": {
				"url": %q,
				"headers": {"Authorization": "Bearer secret"}
			}
		}
	}`, ts.URL)
	if err := os.WriteFile(configPath, []byte(mcpConfig), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	cfg := &config.Config{AI: config.AIConfig{MCPConfigFilePath: configPath}}
	tools, dispatcher, closeFn, err := buildToolsFromConfig(cfg)
	if closeFn != nil {
		defer closeFn()
	}
	if err != nil {
		t.Fatalf("buildToolsFromConfig failed: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "ping" {
		t.Fatalf("Expected 1 tool 'ping', got %v", tools)
	}
	out, err := dispatcher(context.Background(), ToolCall{Name: "ping", Arguments: `{}`})
	if err != nil {
		t.Fatalf("dispatcher failed: %v", err)
	}
	if !strings.Contains(out, "pong") {
		t.Errorf("Expected tool output to contain 'pong', got %q", out)
	}
}

// TestHelperProcess is a subprocess helper for TestEnvPassedToCommand.
// When GO_WANT_HELPER_PROCESS=1, it serves a minimal MCP server on stdio
// with a check_env tool that returns the value of MCP_TEST_ENV_VALUE.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperProcessEnvKey) != "1" {
		return
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: "env-test-server", Version: "1.0.0"}, nil)
	srv.AddTool(&mcp.Tool{
		Name:        "check_env",
		Description: "Returns " + testEnvKey,
		InputSchema: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
	}, func(_ context.Context, _ *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: os.Getenv(testEnvKey)}},
		}, nil
	})
	_ = srv.Run(context.Background(), &mcp.StdioTransport{})
	os.Exit(0)
}

func TestEnvPassedToCommand(t *testing.T) {
	tempDir := t.TempDir()

	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)

	// Config that spawns this test binary as an MCP server subprocess,
	// passing env vars through the config's env field.
	envConfig := fmt.Sprintf(`{
		"mcpServers": {
			"env-test": {
				"command": %q,
				"args": ["-test.run=^TestHelperProcess$"],
				"env": {
					%q: "1",
					%q: %q
				}
			}
		}
	}`, os.Args[0], helperProcessEnvKey, testEnvKey, testEnvValue)
	configPath := filepath.Join(tempDir, "env-config.json")
	if err := os.WriteFile(configPath, []byte(envConfig), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	cfg := &config.Config{
		AI: config.AIConfig{
			MCPConfigFilePath: configPath,
		},
	}

	tools, dispatcher, closeFn, err := buildToolsFromConfig(cfg)
	if err != nil {
		t.Fatalf("buildToolsFromConfig failed: %v", err)
	}
	if closeFn != nil {
		defer closeFn()
	}

	// The helper process serves check_env; verify it was discovered
	if len(tools) == 0 {
		t.Fatal("Expected tools from helper process, got 0")
	}
	found := false
	for _, tool := range tools {
		if tool.Name == "check_env" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("Expected check_env tool, got: %v", tools)
	}

	// Call the tool through the dispatcher — this proves env vars
	// flowed through buildToolsFromConfig → exec.Command → subprocess.
	// Note: "random_string" is the dummy param added by the OpenAI
	// empty-schema workaround (see buildToolsFromConfig).
	result, err := dispatcher(context.Background(), ToolCall{
		Name:      "check_env",
		Arguments: `{"random_string":"x"}`,
	})
	if err != nil {
		t.Fatalf("dispatcher failed: %v", err)
	}
	if !strings.Contains(result, testEnvValue) {
		t.Errorf("Expected result to contain %q, got: %s", testEnvValue, result)
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
	t.Cleanup(func() { _ = session.Close() })

	res := session.InitializeResult()
	if res == nil || res.ServerInfo == nil {
		t.Fatal("No server info returned")
	}

	if res.ServerInfo.Name == config.ServerName {
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


