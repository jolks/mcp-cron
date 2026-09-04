// SPDX-License-Identifier: AGPL-3.0-only
package server

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolks/mcp-cron/internal/agent"
	"github.com/jolks/mcp-cron/internal/command"
	"github.com/jolks/mcp-cron/internal/config"
	"github.com/jolks/mcp-cron/internal/scheduler"
)

// newHTTPTestServer builds an MCPServer in HTTP mode on an ephemeral port.
func newHTTPTestServer(t *testing.T, mutate func(*config.Config)) *MCPServer {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Server.TransportMode = config.TransportHTTP
	cfg.Server.Address = "127.0.0.1"
	cfg.Server.Port = 0
	cfg.AI.MCPConfigFilePath = filepath.Join(t.TempDir(), "mcp.json")
	if mutate != nil {
		mutate(cfg)
	}
	logger := testLogger()
	sched := scheduler.NewScheduler(&cfg.Scheduler, logger)
	srv, err := NewMCPServer(cfg, sched, command.NewCommandExecutor(nil, logger), agent.NewAgentExecutor(cfg, nil, logger), nil, nil, logger)
	if err != nil {
		t.Fatalf("NewMCPServer: %v", err)
	}
	return srv
}

const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"auth-test","version":"1.0"}}}`

func postInitialize(t *testing.T, url, authorization string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(initializeBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// TestStartRefusesUnauthenticatedNonLoopback proves the fail-closed rule is
// enforced by the server itself, not only by config.Validate() in main.
func TestStartRefusesUnauthenticatedNonLoopback(t *testing.T) {
	srv := newHTTPTestServer(t, func(cfg *config.Config) {
		cfg.Server.Address = "0.0.0.0"
	})
	err := srv.Start(context.Background())
	if err == nil {
		_ = srv.Stop()
		t.Fatal("expected Start to refuse unauthenticated non-loopback bind, got nil")
	}
	if !strings.Contains(err.Error(), "refusing to serve http") {
		t.Errorf("unexpected error: %v", err)
	}
	if srv.ListenAddr() != nil {
		t.Error("server must not bind a socket when refusing to start")
	}
}

// TestHTTPTransportBearerTokenEndToEnd exercises the real HTTP transport with
// a token configured: missing and wrong tokens are rejected with 401 and a
// WWW-Authenticate challenge; the correct token reaches the MCP handler.
func TestHTTPTransportBearerTokenEndToEnd(t *testing.T) {
	srv := newHTTPTestServer(t, func(cfg *config.Config) {
		cfg.Server.AuthToken = "s3cret"
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	url := "http://" + srv.ListenAddr().String() + "/"

	cases := []struct {
		name          string
		authorization string
		wantStatus    int
	}{
		{"missing token", "", http.StatusUnauthorized},
		{"wrong token", "Bearer nope", http.StatusUnauthorized},
		{"basic scheme", "Basic czNjcmV0", http.StatusUnauthorized},
		{"correct token", "Bearer s3cret", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := postInitialize(t, url, tc.authorization)
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusUnauthorized && resp.Header.Get("WWW-Authenticate") != "Bearer" {
				t.Errorf("WWW-Authenticate = %q, want \"Bearer\"", resp.Header.Get("WWW-Authenticate"))
			}
		})
	}
}

// TestHTTPTransportLoopbackWithoutToken confirms the zero-config loopback
// path still serves without authentication.
func TestHTTPTransportLoopbackWithoutToken(t *testing.T) {
	srv := newHTTPTestServer(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := srv.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = srv.Stop() })

	resp := postInitialize(t, "http://"+srv.ListenAddr().String()+"/", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 for loopback without token", resp.StatusCode)
	}
}

// TestStartRefusesMalformedToken proves the token-format rule is enforced by
// the server itself: a token no client could present must not produce a
// server that starts cleanly and 401s everyone.
func TestStartRefusesMalformedToken(t *testing.T) {
	srv := newHTTPTestServer(t, func(cfg *config.Config) {
		cfg.Server.AuthToken = "abc def"
	})
	err := srv.Start(context.Background())
	if err == nil {
		_ = srv.Stop()
		t.Fatal("expected Start to refuse a token containing whitespace, got nil")
	}
	if !strings.Contains(err.Error(), "auth token") {
		t.Errorf("unexpected error: %v", err)
	}
	if srv.ListenAddr() != nil {
		t.Error("server must not bind a socket when refusing to start")
	}
}

// TestStartRefusesAddressWithPort proves the host-only address rule is
// enforced at the socket layer, like the rest of CheckAuthPolicy.
func TestStartRefusesAddressWithPort(t *testing.T) {
	srv := newHTTPTestServer(t, func(cfg *config.Config) {
		cfg.Server.Address = "localhost:9000"
		cfg.Server.AuthToken = "s3cret"
	})
	err := srv.Start(context.Background())
	if err == nil {
		_ = srv.Stop()
		t.Fatal("expected Start to refuse an address with an embedded port, got nil")
	}
	if !strings.Contains(err.Error(), "must not include a port") {
		t.Errorf("unexpected error: %v", err)
	}
}
