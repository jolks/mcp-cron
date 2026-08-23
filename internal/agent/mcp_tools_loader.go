// SPDX-License-Identifier: AGPL-3.0-only
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/logging"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type toolCaller func(context.Context, ToolCall) (string, error)

// reservedHeaders are derived from connection state by the go-sdk on every
// request and must never be overridden by a static value from mcp.json; a
// stale session id or protocol version would make the server reject the
// request. This mirrors the TypeScript SDK's RESERVED_REQUEST_HEADER_NAMES.
// Everything else — including Authorization, Accept and Content-Type — is
// left to the user, consistent with the TypeScript and Python SDKs.
var reservedHeaders = map[string]bool{
	"Mcp-Session-Id":       true,
	"Mcp-Protocol-Version": true,
}

// sanitizeHeaders canonicalizes configured header names and drops the
// reserved ones, returning the dropped names so the caller can warn once.
// Doing this at construction keeps RoundTrip a plain copy loop.
func sanitizeHeaders(in map[string]string) (clean map[string]string, dropped []string) {
	clean = make(map[string]string, len(in))
	for k, v := range in {
		canonical := http.CanonicalHeaderKey(k)
		if reservedHeaders[canonical] {
			dropped = append(dropped, k)
			continue
		}
		clean[canonical] = v
	}
	return clean, dropped
}

// headerRoundTripper injects static headers (e.g. an Authorization bearer
// token) into every request to an HTTP MCP server, and refuses to send them
// anywhere else.
//
// It sits *below* http.Client's redirect logic, so the stdlib's protection —
// stripping Authorization when a redirect crosses to another host — would be
// undone: the client strips the header, then this transport adds it back on
// the redirected request. The transport therefore pins itself to the
// configured origin (scheme, host, port) and fails any request to a
// different one before dialing, with a single exception: a same-host
// http→https upgrade on default ports. That is the policy httpx (Python MCP
// SDK) applies and the outcome fetch (TypeScript MCP SDK) produces by
// dropping Authorization on cross-origin redirects. Keeping the check inside
// the type that holds the secrets means it cannot be lost by pairing the
// transport with a different http.Client.
type headerRoundTripper struct {
	base    http.RoundTripper
	origin  *url.URL
	headers map[string]string // canonical keys, reserved names removed
}

func (h *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if !sameOriginOrHTTPSUpgrade(h.origin, req.URL) {
		return nil, fmt.Errorf("refusing redirect from %s://%s to %s://%s: configured headers would be sent to a different origin",
			h.origin.Scheme, h.origin.Host, req.URL.Scheme, req.URL.Host)
	}
	// Clone per the RoundTripper contract: the original request must not be mutated
	req = req.Clone(req.Context())
	for k, v := range h.headers {
		req.Header.Set(k, v)
	}
	return h.base.RoundTrip(req)
}

// newHeaderInjectingClient builds the HTTP client for an MCP server at
// endpoint that needs the given (already sanitized) static headers.
func newHeaderInjectingClient(endpoint string, headers map[string]string) (*http.Client, error) {
	origin, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: &headerRoundTripper{
		base:    http.DefaultTransport,
		origin:  origin,
		headers: headers,
	}}, nil
}

// sameOriginOrHTTPSUpgrade reports whether to is the same origin as from, or
// a same-host http→https upgrade on default ports. url.Parse already
// lowercases the scheme, so only the host needs case folding.
func sameOriginOrHTTPSUpgrade(from, to *url.URL) bool {
	if !strings.EqualFold(from.Hostname(), to.Hostname()) {
		return false
	}
	fromPort, toPort := effectivePort(from), effectivePort(to)
	if from.Scheme == to.Scheme {
		return fromPort == toPort
	}
	return from.Scheme == "http" && to.Scheme == "https" && fromPort == "80" && toPort == "443"
}

func effectivePort(u *url.URL) string {
	if p := u.Port(); p != "" {
		return p
	}
	switch u.Scheme {
	case "http":
		return "80"
	case "https":
		return "443"
	}
	return ""
}

func buildToolsFromConfig(sysCfg *config.Config, logger *logging.Logger) ([]ToolDefinition, toolCaller, func(), error) {
	var cfg struct {
		MCP map[string]struct {
			Command string            `json:"command,omitempty"`
			Args    []string          `json:"args,omitempty"`
			URL     string            `json:"url,omitempty"`
			Env     map[string]string `json:"env,omitempty"`
			Headers map[string]string `json:"headers,omitempty"`
		} `json:"mcpServers"`
	}
	raw, err := os.ReadFile(sysCfg.AI.MCPConfigFilePath)
	if err != nil {
		return nil, nil, nil, err
	}
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return nil, nil, nil, err
	}

	// Create a go-sdk client per server and collect its tools
	var tools []ToolDefinition
	sessionBySrv := map[string]*mcp.ClientSession{}
	tool2srv := map[string]string{} // toolName -> serverName

	for name, spec := range cfg.MCP {
		var tp mcp.Transport
		switch {
		case spec.Command != "":
			cmd := exec.Command(spec.Command, spec.Args...)
			// Inherit the full parent environment so the subprocess gets PATH,
			// HOME, etc. Config values override via last-value-wins semantics.
			if len(spec.Env) > 0 {
				cmd.Env = os.Environ()
				for k, v := range spec.Env {
					cmd.Env = append(cmd.Env, k+"="+v)
				}
			}
			tp = &mcp.CommandTransport{Command: cmd}
		case spec.URL != "":
			st := &mcp.StreamableClientTransport{Endpoint: spec.URL}
			if len(spec.Headers) > 0 {
				headers, dropped := sanitizeHeaders(spec.Headers)
				for _, k := range dropped {
					logger.Warnf("MCP server %q: ignoring configured header %q; it is set per request by the transport", name, k)
				}
				client, err := newHeaderInjectingClient(spec.URL, headers)
				if err != nil {
					logger.Warnf("MCP server %q: invalid url %q: %v", name, spec.URL, err)
					continue
				}
				st.HTTPClient = client
			}
			tp = st
		default:
			continue
		}

		cli := mcp.NewClient(&mcp.Implementation{Name: "mcp-cron", Version: "1.0.0"}, nil)
		session, err := cli.Connect(context.Background(), tp, nil)
		if err != nil {
			logger.Warnf("Failed to connect to MCP server %q; its tools will be unavailable for this AI task: %v", name, err)
			continue
		}

		// Skip mcp-cron itself to prevent recursive task scheduling loops.
		// The MCP handshake returns the server's identity, so we check that
		// rather than relying on the config key name or tool names.
		// We must close the session to kill the spawned child process,
		// otherwise it loads tasks from SQLite and schedules duplicates.
		if res := session.InitializeResult(); res != nil && res.ServerInfo != nil && res.ServerInfo.Name == config.ServerName {
			logger.Infof("Skipping MCP server %q: detected as mcp-cron (self-reference)", name)
			_ = session.Close()
			continue
		}

		sessionBySrv[name] = session

		resp, err := session.ListTools(context.Background(), nil)
		if err != nil {
			logger.Warnf("Failed to list tools for MCP server %q: %v", name, err)
			continue
		}
		for _, tl := range resp.Tools {
			// Extract the raw JSON-schema
			var rawSchema []byte
			if tl.InputSchema != nil {
				if b, err := json.Marshal(tl.InputSchema); err == nil {
					rawSchema = b
				} else {
					logger.Warnf("Failed to marshal input schema for tool %s: %v", tl.Name, err)
					continue
				}
			}
			// Unmarshal into map[string]interface{} for the SDK
			var params map[string]interface{}
			if err := json.Unmarshal(rawSchema, &params); err != nil {
				logger.Warnf("Failed to unmarshal input schema for tool %s: %v", tl.Name, err)
				continue
			}

			// WORKAROUND: Fix empty parameter schemas to avoid OpenAI API errors.
			// Check if this is an empty schema (no properties).
			if params["type"] == "object" && (params["properties"] == nil || len(params["properties"].(map[string]interface{})) == 0) {
				// Add a dummy property to satisfy OpenAI API requirements
				props := map[string]interface{}{
					"random_string": map[string]interface{}{
						"type":        "string",
						"description": "Dummy parameter for no-parameter tools",
					},
				}
				params["properties"] = props
				params["required"] = []string{"random_string"}
				logger.Debugf("Added dummy parameter to empty schema for tool %s", tl.Name)
			}

			tools = append(tools, ToolDefinition{
				Name:        tl.Name,
				Description: tl.Description,
				Parameters:  params,
			})
			tool2srv[tl.Name] = name
		}
	}
	// closeSessions closes all open MCP client sessions.
	closeSessions := func() {
		for _, s := range sessionBySrv {
			_ = s.Close()
		}
	}

	// No tools. Fallback to LLM
	if len(tools) == 0 {
		closeSessions()
		return nil, nil, nil, nil
	}
	// Dispatcher to route model's tool calls to the correct MCP server
	dispatcher := func(ctx context.Context, call ToolCall) (string, error) {
		// Parse arguments JSON string into a map
		var args map[string]interface{}
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			return "", fmt.Errorf("failed to unmarshal arguments: %w", err)
		}

		// Check if tool name exists in mapping
		serverName, ok := tool2srv[call.Name]
		if !ok {
			return "", fmt.Errorf("unknown tool: %s", call.Name)
		}

		// Check if session exists in mapping
		session, ok := sessionBySrv[serverName]
		if !ok {
			return "", fmt.Errorf("server not found for tool: %s", call.Name)
		}

		res, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      call.Name,
			Arguments: args,
		})
		if err != nil {
			return "", err
		}
		// Flatten the tool response into a single string
		out, _ := json.Marshal(res.Content)
		return string(out), nil
	}
	return tools, dispatcher, closeSessions, nil
}
