package app

import (
	"context"
	"fmt"
	"os"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type Dispatcher struct {
	Store *core.Store
	Node  *node.LocalNode
}

func (d Dispatcher) Dispatch(ctx context.Context, task core.Task, workerRef string) (core.WorkerBinding, core.Attempt, error) {
	workspace, err := os.MkdirTemp("", "secretary-worker-")
	if err != nil {
		return core.WorkerBinding{}, core.Attempt{}, err
	}
	session, err := d.Node.Dispatch(ctx, node.StartRequest{WorkerRef: workerRef, Task: task.Text, Workspace: workspace})
	if err != nil {
		_, transitionErr := d.Store.MarkDispatchFailed(ctx, task.ID)
		if transitionErr != nil {
			return core.WorkerBinding{}, core.Attempt{}, fmt.Errorf("start runtime: %w; mark failed: %v", err, transitionErr)
		}
		return core.WorkerBinding{}, core.Attempt{}, err
	}
	_, binding, attempt, err := d.Store.AcceptDispatch(ctx, task.ID, workerRef, "local", session.ID(), workspace)
	if err != nil {
		_ = d.Node.Remove(workerRef)
		return core.WorkerBinding{}, core.Attempt{}, err
	}
	attempt, err = d.Store.SetAttemptActive(ctx, attempt.ID)
	if err != nil {
		_ = d.Node.Remove(workerRef)
		return core.WorkerBinding{}, core.Attempt{}, err
	}
	go d.persistResults(task.ID, workerRef, session)
	return binding, attempt, nil
}

func (d Dispatcher) persistResults(taskID, workerRef string, session node.Session) {
	for result := range session.Result() {
		attempt, err := d.Store.ActiveAttemptForWorker(context.Background(), workerRef)
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
		_, _, _ = d.Store.CompleteAttempt(context.Background(), attempt.ID, status, result.Summary)
		_, _ = d.Store.FinishClosingTask(context.Background(), taskID)
	}
}
