# MCP Cron

Model Context Protocol (MCP) server for scheduling and managing tasks through a standardized API. The server provides task scheduling capabilities supporting both shell commands and AI-powered tasks, all accessible via the MCP protocol.

## Features

- Schedule shell command or prompt to AI tasks using cron expressions
- AI can have access to MCP servers 
- [Manage tasks](#available-mcp-tools) via MCP protocol
- Task execution with command output capture
- Task persistence across restarts (SQLite)
- Multi-instance safe — multiple instances can share the same database without duplicate execution
- Support multiple isolated instances with different `--db-path`

## Installation

### npm (recommended)

```bash
npx -y mcp-cron
```

#### Claude Code
```bash
claude mcp add mcp-cron -- npx -y mcp-cron
```

#### Cursor / Claude Desktop
```json
{
  "mcpServers": {
    "mcp-cron": {
      "command": "npx",
      "args": ["-y", "mcp-cron", "--transport", "stdio"]
    }
  }
}
```

#### Recommended Configuration

A more complete setup with AI provider, model selection, and sleep prevention:

```json
{
  "mcpServers": {
    "mcp-cron": {
      "command": "npx",
      "args": [
        "-y", "mcp-cron",
        "--transport", "stdio",
        "--prevent-sleep",
        "--ai-provider", "anthropic",
        "--ai-model", "claude-sonnet-4-5-20250929"
      ],
      "env": {
        "ANTHROPIC_API_KEY": "your-api-key"
      }
    }
  }
}
```

#### Using LiteLLM

To route AI tasks through a [LiteLLM](https://docs.litellm.ai/) proxy:

```json
{
  "mcpServers": {
    "mcp-cron": {
      "command": "npx",
      "args": [
        "-y", "mcp-cron",
        "--transport", "stdio",
        "--prevent-sleep",
        "--ai-base-url", "https://litellm.yourcompany.com",
        "--ai-model", "claude-sonnet-4-5-20250929"
      ],
      "env": {
        "MCP_CRON_AI_API_KEY": "sk-your-litellm-key"
      }
    }
  }
}
```

> The `--ai-model` value should match a model name in your LiteLLM proxy config. LiteLLM exposes an OpenAI-compatible API, so `--ai-provider` can be omitted (defaults to `openai`).

> See [Command Line Arguments](#command-line-arguments) and [Environment Variables](#environment-variables) for all available options.

### Building from Source

#### Prerequisites
- Go 1.24.0 or higher

```bash
# Clone the repository
git clone https://github.com/jolks/mcp-cron.git
cd mcp-cron

# Build the application as mcp-cron binary
go build -o mcp-cron cmd/mcp-cron/main.go
```

## Usage
The server supports two transport modes:
- **SSE (Server-Sent Events)**: Default HTTP-based transport for browser and network clients
- **stdio**: Standard input/output transport for direct piping and inter-process communication

| Client | Config File Location |
|--------|----------------------|
| Cursor | `~/.cursor/mcp.json` |
| Claude Desktop (Mac) | `~/Library/Application Support/Claude/claude_desktop_config.json`|
| Claude Desktop (Windows) | `%APPDATA%\Claude\claude_desktop_config.json` |

### SSE

```bash
# Start the server with HTTP SSE transport (default mode)
# Default to localhost:8080
./mcp-cron

# Start with custom address and port
./mcp-cron --address 127.0.0.1 --port 9090
```
Config file example
```json
{
  "mcpServers": {
    "mcp-cron": {
      "url": "http://localhost:8080/sse"
    }
  }
}
```

### stdio
The stdio transport is particularly useful for:
- Direct piping to/from other processes
- Integration with CLI tools
- Testing in environments without HTTP
- Docker container integration

Upon starting Cursor IDE and Claude Desktop, it will **automatically** start the server

Config file example
```json
{
  "mcpServers": {
    "mcp-cron": {
      "command": "<path to where mcp-cron binary is located>/mcp-cron",
      "args": ["--transport", "stdio"]
    }
  }
}
```

### Command Line Arguments

The following command line arguments are supported:

| Argument | Description | Default |
|----------|-------------|---------|
| `--address` | The address to bind the server to | `localhost` |
| `--port` | The port to bind the server to | `8080` |
| `--transport` | Transport mode: `sse` or `stdio` | `sse` |
| `--log-level` | Logging level: `debug`, `info`, `warn`, `error`, `fatal` | `info` |
| `--log-file` | Log file path | stdout |
| `--version` | Show version information and exit | `false` |
| `--ai-provider` | AI provider: `openai` or `anthropic` | `openai` |
| `--ai-base-url` | Custom base URL for OpenAI-compatible endpoints (e.g. Ollama, vLLM, Groq, LiteLLM) | Not set |
| `--ai-model` | AI model to use for AI tasks | `gpt-4o` |
| `--ai-max-iterations` | Maximum iterations for tool-enabled AI tasks | `20` |
| `--mcp-config-path` | Path to MCP configuration file | `~/.cursor/mcp.json` |
| `--db-path` | Path to SQLite database for result history | `~/.mcp-cron/results.db` |
| `--prevent-sleep` | Prevent system from sleeping while mcp-cron is running (macOS and Windows) | `false` |
| `--poll-interval` | How often to check for due tasks | `1s` |

### Environment Variables

The following environment variables are supported:

| Environment Variable | Description | Default |
|----------------------|-------------|---------|
| `MCP_CRON_SERVER_ADDRESS` | The address to bind the server to | `localhost` |
| `MCP_CRON_SERVER_PORT` | The port to bind the server to | `8080` |
| `MCP_CRON_SERVER_TRANSPORT` | Transport mode: `sse` or `stdio` | `sse` |
| `MCP_CRON_SERVER_NAME` | **Deprecated** — ignored; the server name is fixed to ensure self-reference detection works correctly | - |
| `MCP_CRON_SERVER_VERSION` | **Deprecated** — ignored; version is set at build time via ldflags | - |
| `MCP_CRON_SCHEDULER_DEFAULT_TIMEOUT` | Default timeout for task execution | `10m` |
| `MCP_CRON_LOGGING_LEVEL` | Logging level: `debug`, `info`, `warn`, `error`, `fatal` | `info` |
| `MCP_CRON_LOGGING_FILE` | Log file path | stdout |
| `MCP_CRON_AI_PROVIDER` | AI provider: `openai` or `anthropic` | `openai` |
| `MCP_CRON_AI_BASE_URL` | Custom base URL for OpenAI-compatible endpoints (e.g. Ollama, vLLM, Groq, LiteLLM) | Not set |
| `MCP_CRON_AI_API_KEY` | Generic fallback API key (used when provider-specific key is not set) | Not set |
| `OPENAI_API_KEY` | OpenAI API key for AI tasks | Not set |
| `ANTHROPIC_API_KEY` | Anthropic API key for AI tasks | Not set |
| `MCP_CRON_ENABLE_OPENAI_TESTS` | Enable OpenAI integration tests | `false` |
| `MCP_CRON_AI_MODEL` | LLM model to use for AI tasks | `gpt-4o` |
| `MCP_CRON_AI_MAX_TOOL_ITERATIONS` | Maximum iterations for tool-enabled tasks | `20` |
| `MCP_CRON_MCP_CONFIG_FILE_PATH` | Path to MCP configuration file | `~/.cursor/mcp.json` |
| `MCP_CRON_STORE_DB_PATH` | Path to SQLite database for result history | `~/.mcp-cron/results.db` |
| `MCP_CRON_PREVENT_SLEEP` | Prevent system from sleeping while mcp-cron is running (macOS and Windows) | `false` |
| `MCP_CRON_POLL_INTERVAL` | How often to check for due tasks (Go duration format) | `1s` |

### Sleep Prevention

On laptops, the system may go to sleep and prevent scheduled tasks from running on time. Use the `--prevent-sleep` flag to keep the system awake while mcp-cron is running:

```bash
mcp-cron --prevent-sleep --transport stdio
```

Or via environment variable:
```bash
MCP_CRON_PREVENT_SLEEP=true mcp-cron --transport stdio
```

| Platform | Mechanism | Notes |
|----------|-----------|-------|
| macOS | `caffeinate` | Prevents idle sleep; automatically cleans up on exit |
| Windows | `SetThreadExecutionState` | Prevents idle sleep; automatically cleans up on exit |
| Linux | Not supported | Linux servers typically do not auto-sleep |

> **Note:** This prevents idle sleep only. It does not prevent sleep from closing the laptop lid or pressing the power button.

### Logging

When running with the default SSE transport, logs are output to the console. 

When running with stdio transport, logs are redirected to a `mcp-cron.log` log file to prevent interference with the JSON-RPC protocol:
- Log file location: Same location as `mcp-cron` binary.
- Task outputs, execution details, and server diagnostics are written to this file.
- The stdout/stderr streams are kept clean for protocol messages only.

### Available MCP Tools

The server exposes several tools through the MCP protocol:

1. `list_tasks` - Lists all tasks (scheduled and on-demand)
2. `get_task` - Gets a specific task by ID
3. `add_task` - Adds a new shell command task (provide `schedule` for recurring, or omit for on-demand)
4. `add_ai_task` - Adds a new AI (LLM) task with a prompt (provide `schedule` for recurring, or omit for on-demand)
5. `update_task` - Updates an existing task
6. `remove_task` - Removes a task by ID
7. `run_task` - Immediately executes a task by ID (for on-demand tasks or ad-hoc runs of scheduled tasks)
8. `enable_task` - Enables a task so it runs on its schedule or can be triggered via `run_task`
9. `disable_task` - Disables a task so it stops running and cannot be triggered
10. `get_task_result` - Gets execution results for a task (latest by default, or recent history with `limit`)

### Task Format

Tasks have the following structure:

```json
{
  "id": "task_1234567890",
  "name": "Example Task",
  "schedule": "0 */5 * * * *",
  "command": "echo 'Task executed!'",
  "prompt": "Analyze yesterday's sales data and provide a summary",
  "type": "shell_command",
  "description": "An example task that runs every 5 minutes",
  "enabled": true,
  "lastRun": "2025-01-01T12:00:00Z",
  "nextRun": "2025-01-01T12:05:00Z",
  "status": "completed",
  "createdAt": "2025-01-01T00:00:00Z",
  "updatedAt": "2025-01-01T12:00:00Z"
}
```

For shell command tasks, use the `command` field to specify the command to execute.
For AI tasks, use the `prompt` field to specify what the AI should do.
The `type` field can be either `shell_command` (default) or `AI`.

**Scheduled vs on-demand tasks:**
- **Scheduled**: Provide a `schedule` (cron expression) — the task runs automatically on that schedule.
- **On-demand**: Omit `schedule` — the task sits idle until triggered via `run_task`.

`run_task` also works on scheduled tasks for ad-hoc execution outside their normal schedule. After execution, scheduled tasks resume their normal schedule; on-demand tasks return to idle.

### Task Status

The tasks can have the following status values:
- `pending` - Task has not been run yet
- `running` - Task is currently running
- `completed` - Task has successfully completed
- `failed` - Task has failed during execution
- `disabled` - Task is disabled and won't run on schedule

### Cron Expression Format

Cron expressions are required for scheduled tasks and omitted for on-demand tasks. The scheduler uses the [github.com/robfig/cron/v3](https://github.com/robfig/cron) library for parsing. The format includes seconds:

```
┌───────────── second (0 - 59) (Optional)
│ ┌───────────── minute (0 - 59)
│ │ ┌───────────── hour (0 - 23)
│ │ │ ┌───────────── day of the month (1 - 31)
│ │ │ │ ┌───────────── month (1 - 12)
│ │ │ │ │ ┌───────────── day of the week (0 - 6) (Sunday to Saturday)
│ │ │ │ │ │
│ │ │ │ │ │
* * * * * *
```

Examples:
- `0 */5 * * * *` - Every 5 minutes (at 0 seconds)
- `0 0 * * * *` - Every hour
- `0 0 0 * * *` - Every day at midnight
- `0 0 12 * * MON-FRI` - Every weekday at noon

## Development

### Building

```bash
go build -o mcp-cron cmd/mcp-cron/main.go
```

### Testing

See [docs/testing.md](docs/testing.md) for the full testing guide, including integration tests and AI task tests.

## Acknowledgments

- [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) - Official Go SDK for the Model Context Protocol
- [robfig/cron](https://github.com/robfig/cron) - Cron expression parsing for Go
