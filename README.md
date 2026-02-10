# MCP Cron

Model Context Protocol (MCP) server for scheduling and managing tasks through a standardized API. The server provides task scheduling capabilities supporting both shell commands and AI-powered tasks, all accessible via the MCP protocol.

## Features

- Schedule shell command or prompt to AI tasks using cron expressions
- AI can have access to MCP servers 
- [Manage tasks](#available-mcp-tools) via MCP protocol
- Task execution with command output capture

## Installation

### Building from Source

#### Prerequisites
- Go 1.23.0 or higher

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
| `--ai-model` | AI model to use for AI tasks | `gpt-4o` |
| `--ai-max-iterations` | Maximum iterations for tool-enabled AI tasks | `20` |
| `--mcp-config-path` | Path to MCP configuration file | `~/.cursor/mcp.json` |

### Environment Variables

The following environment variables are supported:

| Environment Variable | Description | Default |
|----------------------|-------------|---------|
| `MCP_CRON_SERVER_ADDRESS` | The address to bind the server to | `localhost` |
| `MCP_CRON_SERVER_PORT` | The port to bind the server to | `8080` |
| `MCP_CRON_SERVER_TRANSPORT` | Transport mode: `sse` or `stdio` | `sse` |
| `MCP_CRON_SERVER_NAME` | Server name | `mcp-cron` |
| `MCP_CRON_SERVER_VERSION` | Server version | `0.1.0` |
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

### Logging

When running with the default SSE transport, logs are output to the console. 

When running with stdio transport, logs are redirected to a `mcp-cron.log` log file to prevent interference with the JSON-RPC protocol:
- Log file location: Same location as `mcp-cron` binary.
- Task outputs, execution details, and server diagnostics are written to this file.
- The stdout/stderr streams are kept clean for protocol messages only.

### Available MCP Tools

The server exposes several tools through the MCP protocol:

1. `list_tasks` - Lists all scheduled tasks
2. `get_task` - Gets a specific task by ID
3. `add_task` - Adds a new scheduled task
4. `add_ai_task` - Adds a new scheduled AI (LLM) task with a prompt
5. `update_task` - Updates an existing task
6. `remove_task` - Removes a task by ID
7. `enable_task` - Enables a disabled task
8. `disable_task` - Disables an enabled task

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

### Task Status

The tasks can have the following status values:
- `pending` - Task has not been run yet
- `running` - Task is currently running
- `completed` - Task has successfully completed
- `failed` - Task has failed during execution
- `disabled` - Task is disabled and won't run on schedule

### Cron Expression Format

The scheduler uses the [github.com/robfig/cron/v3](https://github.com/robfig/cron) library for parsing cron expressions. The format includes seconds:

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

### Project Structure

```
mcp-cron/
├── cmd/
│   └── mcp-cron/        # Main application entry point
├── internal/
│   ├── agent/           # AI agent execution functionality 
│   ├── command/         # Command execution functionality
│   ├── config/          # Configuration handling
│   ├── errors/          # Error types and handling
│   ├── logging/         # Logging utilities
│   ├── model/           # Data models and types
│   ├── scheduler/       # Task scheduling
│   ├── server/          # MCP server implementation
│   └── utils/           # Miscellanous utilities
├── scripts/             # Utility scripts
├── go.mod               # Go modules definition
├── go.sum               # Go modules checksums
└── README.md            # Project documentation
```

### Building and Testing

```bash
# Build the application
go build -o mcp-cron cmd/mcp-cron/main.go

# Run tests
go test ./...

# Run tests and check coverage
go test ./... -cover
```

## Acknowledgments

- [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) - Official Go SDK for the Model Context Protocol
- [robfig/cron](https://github.com/robfig/cron) - Cron library for Go 