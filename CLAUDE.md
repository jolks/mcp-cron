# CLAUDE.md

Go MCP server for cron task scheduling (shell commands and AI prompts).

## Build & Test

```bash
go build ./...                # build all packages
go test ./...                 # run all tests
go test ./... -cover          # run tests with coverage
go tool golangci-lint run     # lint (installed as go tool dependency in go.mod)
go test ./internal/server/ -run TestIntegration -v  # integration tests only
MCP_CRON_ENABLE_OPENAI_TESTS=true go test ./...     # include AI integration tests (requires OPENAI_API_KEY)
MCP_CRON_ENABLE_ANTHROPIC_TESTS=true go test ./...  # include AI integration tests (requires ANTHROPIC_API_KEY)
```

## Project Structure

```
cmd/mcp-cron/          # Entry point — flag parsing, wiring, graceful shutdown
internal/
  agent/               # AI task executor (multi-provider: OpenAI, Anthropic, OpenAI-compatible) + MCP tool loop
  command/             # Shell command executor (exec.CommandContext with timeout)
  config/              # Config structs, defaults, env var loading, validation
  errors/              # Typed errors: NotFound, AlreadyExists, InvalidInput, Internal
  logging/             # Leveled logger (Debug/Info/Warn/Error/Fatal), file + stdout
  model/               # Core types: Task, Result, TaskType, TaskStatus, Executor, ResultStore interfaces
  scheduler/           # Cron scheduling via robfig/cron, in-memory task storage with SQLite write-through
  server/              # MCP server, tool registration, HTTP/stdio transport, handlers
  sleep/               # Platform-specific system sleep prevention (macOS, Windows)
  store/               # SQLite store (persistent task definitions + result history, schema migrations)
  utils/               # JSON unmarshal helper
npm/
  mcp-cron/            # Main npm package — JS wrapper that spawns the platform binary
  mcp-cron-{os}-{arch}/ # Platform-specific packages (darwin/linux/windows × amd64/arm64)
scripts/
  build-npm.sh         # Cross-compile Go binaries for all platforms, optional version update
  publish-npm.sh       # Publish all 7 npm packages (platform packages first, then main)
```

## Key Conventions

- **Versioning**: `config.Version` defaults to `"dev"` and is injected at build time via `-ldflags "-X github.com/jolks/mcp-cron/internal/config.Version=X.Y.Z"`. The build script and CI handle this automatically from the git tag — never hardcode a version in source.
- **Vendor directory**: `vendor/` is gitignored — do NOT commit it. Dependencies are tracked via `go.mod` + `go.sum`; run `go mod vendor` locally to recreate.
- **License header**: Every Go file starts with `// SPDX-License-Identifier: AGPL-3.0-only`
- **Handler signature**: `func (s *MCPServer) handle<Name>(_ context.Context, request *mcp.CallToolRequest) (*mcp.CallToolResult, error)`
- **Task types**: `shell_command` (runs a command) and `AI` (runs an LLM prompt)
- **Task statuses**: pending, running, completed, failed, disabled
- **Storage**: In-memory maps with SQLite write-through for task definitions; SQLite for persistent result history (`modernc.org/sqlite`, pure Go)
- **Transport**: SSE (HTTP, default) or stdio (for CLI/Docker integration). Stdio mode auto-exits on stdin EOF via `server.Done()` channel.

## MCP Tools Exposed

list_tasks, get_task, get_task_result, add_task, add_ai_task, update_task, remove_task, enable_task, disable_task

## Dependencies

- `github.com/modelcontextprotocol/go-sdk` — Official MCP Go SDK
- `github.com/openai/openai-go` — OpenAI API client (for AI tasks)
- `github.com/anthropics/anthropic-sdk-go` — Anthropic API client (for AI tasks)
- `github.com/robfig/cron/v3` — Cron expression parsing and scheduling
- `modernc.org/sqlite` — Pure-Go SQLite driver (no CGo) for persistent result history

## npm Packaging

Uses the `optionalDependencies` pattern (same as esbuild): one main package (`mcp-cron`) with a JS wrapper + 6 platform-specific packages containing pre-built Go binaries. npm installs only the matching platform package via `os`/`cpu` fields.

- **Build**: `./scripts/build-npm.sh [version]` — cross-compiles all platforms, optionally updates version
- **Publish**: `./scripts/publish-npm.sh [--dry-run]` — publishes platform packages first, then main
- **Versions**: All 7 `package.json` files must have matching versions (build script handles this)

## CI

- `.github/workflows/go-test.yml`: runs `golangci-lint` + `go test ./... -cover` on pushes and PRs to main
- `.github/workflows/npm-publish.yml`: triggered on `v*` tags — cross-compiles, then publishes all npm packages
