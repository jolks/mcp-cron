# Testing

## Unit Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test ./... -cover
```

## Integration Tests

Integration tests exercise the full MCP handler workflow (add → list → get → execute → get_task_result) for both shell command and AI task types.

```bash
# Run integration tests only
go test ./internal/server/ -run TestIntegration -v
```

### AI Integration Tests

AI task execution tests require API keys and are skipped by default. Enable them with environment variables:

```bash
# Run with OpenAI (requires OPENAI_API_KEY)
MCP_CRON_ENABLE_OPENAI_TESTS=true go test ./...

# Run with Anthropic (requires ANTHROPIC_API_KEY)
MCP_CRON_ENABLE_ANTHROPIC_TESTS=true go test ./...
```

### What the Integration Tests Cover

| Test | Description |
|------|-------------|
| `TestIntegration_ShellCommandFullLifecycle` | Add, list, get, execute, and retrieve result for a shell command task |
| `TestIntegration_AITaskFullLifecycle` | Full CRUD + execution for an AI task (execution gated behind env var) |
| `TestIntegration_EnableDisableFlow` | Enable/disable toggling and idempotency |
| `TestIntegration_ShellCommandFailure` | Verifies failed commands produce correct error results |
| `TestIntegration_ErrorCases` | Table-driven tests for missing fields and not-found errors |
| `TestIntegration_MultipleTasksIsolation` | Executing one task does not affect other tasks' results |

## Linting

```bash
go tool golangci-lint run
```

`golangci-lint` is installed as a [Go tool dependency](https://go.dev/blog/toolchain) in `go.mod`.
