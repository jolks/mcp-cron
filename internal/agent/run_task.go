package agent

import (
	"context"
	"encoding/json"
	"github.com/jolks/mcp-cron/internal/scheduler"
	"github.com/jolks/mcp-cron/internal/server"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"time"
)

func RunTask(ctx context.Context, t *scheduler.Task) (string, error) {
	if err := json.Unmarshal([]byte(t.Description), &server.AIParams{}); err != nil {
		return "", err
	}
	tools, dispatcher, err := buildToolsFromConfig()
	if err != nil {
		return "", err
	}
	client := openai.NewClient(option.WithTimeout(10 * time.Second))
	// Fallback to LLM
	if len(tools) == 0 {

	}
}
