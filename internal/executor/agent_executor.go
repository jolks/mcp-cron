package executor

import (
	"context"
	"github.com/jolks/mcp-cron/internal/scheduler"
	"time"
)

type AgentExecutor struct{}

func NewAgentExecutor() *AgentExecutor {
	return &AgentExecutor{}
}

func (ae *AgentExecutor) Execute(ctx context.Context, t *scheduler.Task, timeout time.Duration) *Result {

}
