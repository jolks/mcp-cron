package executor

import (
	"context"
	"github.com/jolks/mcp-cron/internal/scheduler"
	"time"
)

func (ce *CommandExecutor) Execute(ctx context.Context, t *scheduler.Task, timeout time.Duration) *Result {
	return ce.ExecuteCommand(ctx, t.ID, t.Command, timeout)
}
