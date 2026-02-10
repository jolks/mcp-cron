# CLAUDE.md

Go MCP server for cron task scheduling (shell commands and AI prompts).

## Build & Test

```bash
go build ./...                # build all packages
go test ./...                 # run all tests
go test ./... -cover          # run tests with coverage
golangci-lint run             # lint (CI uses vendor mode: --modules-download-mode vendor)
```

## Project Structure

```
cmd/mcp-cron/          # Entry point — flag parsing, wiring, graceful shutdown
internal/
  agent/               # AI task executor (OpenAI chat completions + MCP tool loop)
  command/             # Shell command executor (exec.CommandContext with timeout)
  config/              # Config structs, defaults, env var loading, validation
  errors/              # Typed errors: NotFound, AlreadyExists, InvalidInput, Internal
  logging/             # Leveled logger (Debug/Info/Warn/Error/Fatal), file + stdout
  model/               # Core types: Task, Result, TaskType, TaskStatus, Executor interface
  scheduler/           # Cron scheduling via robfig/cron, in-memory task storage
  server/              # MCP server, tool registration, HTTP/stdio transport, handlers
  utils/               # JSON unmarshal helper
```

## Key Conventions

- **License header**: Every Go file starts with `// SPDX-License-Identifier: AGPL-3.0-only`
- **Handler signature**: `func (s *MCPServer) handle<Name>(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error)`
- **Task types**: `shell_command` (runs a command) and `AI` (runs an LLM prompt)
- **Task statuses**: pending, running, completed, failed, disabled
- **Storage**: In-memory maps — no database
- **Transport**: SSE (HTTP, default) or stdio (for CLI/Docker integration)

## MCP Tools Exposed

list_tasks, get_task, add_task, add_ai_task, update_task, remove_task, enable_task, disable_task

## Dependencies

- `github.com/modelcontextprotocol/go-sdk` — Official MCP Go SDK
- `github.com/openai/openai-go` — OpenAI API client (for AI tasks)
- `github.com/robfig/cron/v3` — Cron expression parsing and scheduling

## CI

GitHub Actions (`.github/workflows/go-test.yml`): runs `golangci-lint` + `go test ./... -cover` on pushes and PRs to main.
