package app

import (
	"context"
	"fmt"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type Dispatcher struct {
	Store *core.Store
	Node  *node.LocalNode
}

func (d Dispatcher) Dispatch(ctx context.Context, task core.Task, workerRef string) (core.WorkerBinding, core.Attempt, error) {
	session, err := d.Node.Dispatch(ctx, node.StartRequest{WorkerRef: workerRef, Task: task.Text})
	if err != nil {
		_, transitionErr := d.Store.MarkDispatchFailed(ctx, task.ID)
		if transitionErr != nil {
			return core.WorkerBinding{}, core.Attempt{}, fmt.Errorf("start runtime: %w; mark failed: %v", err, transitionErr)
		}
		return core.WorkerBinding{}, core.Attempt{}, err
	}
	_, binding, attempt, err := d.Store.AcceptDispatch(ctx, task.ID, workerRef, "local", session.ID())
	if err != nil {
		_ = d.Node.Remove(workerRef)
		return core.WorkerBinding{}, core.Attempt{}, err
	}
	attempt, err = d.Store.SetAttemptActive(ctx, attempt.ID)
	if err != nil {
		_ = d.Node.Remove(workerRef)
		return core.WorkerBinding{}, core.Attempt{}, err
	}
	go d.persistResult(task.ID, attempt.ID, session)
	return binding, attempt, nil
}

func (d Dispatcher) persistResult(taskID, attemptID string, session node.Session) {
	result := <-session.Result()
	status := core.ResultSucceeded
	if result.Status == "failed" {
		status = core.ResultFailed
	}
	if result.Status == "canceled" {
		status = core.ResultCanceled
	}
	_, _, _ = d.Store.CompleteAttempt(context.Background(), attemptID, status, result.Summary)
	_ = taskID
}
