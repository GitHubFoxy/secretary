package app

import (
	"context"
	"errors"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

var ErrRuntimeSessionUnavailable = errors.New("runtime_session_unavailable")

// WorkerController is the server-facing boundary for observer commands. It
// records Follow-up state before handing input to the runtime.
type WorkerController struct {
	Store          *core.Store
	Node           *node.LocalNode
	ManagedProfile func(core.BindingProfile) node.ManagedProfile
}

func (c *WorkerController) Session(workerRef string) (node.Session, bool) {
	if c == nil || c.Node == nil {
		return nil, false
	}
	return c.Node.Session(workerRef)
}

func (c *WorkerController) Steer(ctx context.Context, workerRef, text string) (bool, error) {
	if c.Store != nil && c.Store.LegacyTasksReadOnly(ctx) {
		return false, core.ErrLegacyTaskReadOnly
	}
	session, ok := c.Session(workerRef)
	if !ok {
		return false, core.ErrNotFound
	}
	if steerer, ok := session.(node.InterruptAndContinueSteerer); ok {
		if c.Store == nil {
			return false, errors.New("worker controller: store is required for interrupt steering")
		}
		task, err := c.Store.TaskForWorker(ctx, workerRef)
		if err != nil {
			return false, err
		}
		return steerer.InterruptAndContinue(ctx, text, func() error {
			return c.createFollowUpAfterInterrupt(ctx, task.ID, text)
		})
	}
	return session.Steer(ctx, text)
}

func (c *WorkerController) createFollowUpAfterInterrupt(ctx context.Context, taskID, text string) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, err := c.Store.CreateFollowUpAttempt(ctx, taskID, text)
		if err == nil {
			return nil
		}
		if !errors.Is(err, core.ErrInvalidTransition) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (c *WorkerController) Queue(ctx context.Context, workerRef, text string) (core.Attempt, error) {
	if c.Store == nil {
		return core.Attempt{}, errors.New("worker controller: store is required")
	}
	if c.Store.LegacyTasksReadOnly(ctx) {
		return core.Attempt{}, core.ErrLegacyTaskReadOnly
	}
	session, ok := c.Session(workerRef)
	task, err := c.Store.TaskForWorker(ctx, workerRef)
	if err != nil {
		return core.Attempt{}, err
	}
	if !ok {
		details, detailsErr := c.Store.TaskDetails(ctx, task.ID)
		if detailsErr != nil {
			return core.Attempt{}, detailsErr
		}
		if details.Binding == nil || len(details.Attempts) == 0 || details.Attempts[len(details.Attempts)-1].State != core.AttemptInterrupted {
			return core.Attempt{}, core.ErrNotFound
		}
		request := node.StartRequest{WorkerRef: workerRef, Workspace: details.Binding.Workspace}
		if c.ManagedProfile != nil {
			request.Profile = c.ManagedProfile(details.Binding.Profile)
		}
		session, err = c.Node.Resume(ctx, request, details.Binding.RuntimeSessionID)
		if err != nil {
			return core.Attempt{}, ErrRuntimeSessionUnavailable
		}
		go c.persistResults(task.ID, workerRef, session)
	}
	queue, ok := session.(node.Queueer)
	if !ok {
		return core.Attempt{}, errors.New("worker controller: runtime does not support queued input")
	}
	attempt, err := c.Store.CreateFollowUpAttempt(ctx, task.ID, text)
	if err != nil {
		return core.Attempt{}, err
	}
	if err := queue.Queue(ctx, text); err != nil {
		return attempt, err
	}
	return attempt, nil
}

func (c *WorkerController) persistResults(taskID, workerRef string, session node.Session) {
	for result := range session.Result() {
		attempt, err := c.Store.ActiveAttemptForWorker(context.Background(), workerRef)
		if err != nil {
			continue
		}
		status := core.ResultSucceeded
		if result.Status == "failed" {
			status = core.ResultFailed
		}
		if result.Status == "canceled" {
			status = core.ResultCanceled
		}
		_, _, _ = c.Store.CompleteAttempt(context.Background(), attempt.ID, status, result.Summary)
		_, _ = c.Store.FinishClosingTask(context.Background(), taskID)
	}
}

func (c *WorkerController) Stop(ctx context.Context, workerRef string) error {
	if c.Store != nil && c.Store.LegacyTasksReadOnly(ctx) {
		return core.ErrLegacyTaskReadOnly
	}
	session, ok := c.Session(workerRef)
	if !ok {
		return core.ErrNotFound
	}
	return session.Cancel(ctx)
}
