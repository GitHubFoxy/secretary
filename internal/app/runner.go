package app

import (
	"context"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

// Runner is the single local dispatch loop. It makes a task created by
// secretaryctl durable before asking the Node to execute it.
type Runner struct {
	Store        *core.Store
	Dispatcher   *Dispatcher
	Conversation string
	Interval     time.Duration
}

func (r Runner) Run(ctx context.Context) {
	interval := r.Interval
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	r.dispatch(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			r.dispatch(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (r Runner) dispatch(ctx context.Context) {
	if r.Store == nil || r.Dispatcher == nil || r.Conversation == "" {
		return
	}
	tasks, err := r.Store.TasksForConversation(ctx, r.Conversation)
	if err != nil {
		return
	}
	for _, task := range tasks {
		if task.State != core.TaskDispatching {
			continue
		}
		_, _, _ = r.Dispatcher.Dispatch(ctx, task, task.ID)
	}
}
