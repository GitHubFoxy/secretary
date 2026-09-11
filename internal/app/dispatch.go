package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type Dispatcher struct {
	Store               *core.Store
	Node                *node.LocalNode
	Profile             func() core.BindingProfile
	ManagedProfile      func() node.ManagedProfile
	ChildProfile        func() core.BindingProfile
	ManagedChildProfile func() node.ManagedProfile
	MCPCommand          string
	MCPDataDir          string
}

func (d Dispatcher) Dispatch(ctx context.Context, task core.Task, workerRef string) (core.WorkerBinding, core.Attempt, error) {
	_, _ = d.Store.RecordEvent(ctx, "worker.dispatch_started", workerRef, "", "", map[string]string{"task_id": task.ID})
	isChild := task.ParentTaskID != ""
	var managedProfile node.ManagedProfile
	if isChild {
		if d.ManagedChildProfile != nil {
			managedProfile = d.ManagedChildProfile()
		}
	} else if d.ManagedProfile != nil {
		managedProfile = d.ManagedProfile()
	}
	var profile core.BindingProfile
	if managedProfile.Name != "" {
		profile = core.BindingProfile{Version: managedProfile.Version, Name: managedProfile.Name, Hash: managedProfile.Hash, Runtime: managedProfile.Runtime, Model: managedProfile.Model, Reasoning: managedProfile.Reasoning, Tools: strings.Join(managedProfile.AllowTools, ","), Delivery: managedProfile.Delivery}
	} else if isChild {
		if d.ChildProfile != nil {
			profile = d.ChildProfile()
		}
	} else if d.Profile != nil {
		profile = d.Profile()
	}
	if managedProfile.Name != "" {
		_, _ = d.Store.RecordEvent(ctx, "worker.profile_delivery", workerRef, "", "", map[string]string{
			"task_id": task.ID, "profile": managedProfile.Name, "profile_version": managedProfile.Version,
			"profile_hash": managedProfile.Hash, "delivery": managedProfile.Delivery, "runtime": managedProfile.Runtime,
		})
	}
	var workerCapability string
	var err error
	if d.MCPCommand != "" && !isChild {
		workerCapability, err = d.Store.IssueWorkerCapability(ctx, task.ID, workerRef)
		if err != nil {
			_, _ = d.Store.RecordEvent(ctx, "worker.dispatch_failed", workerRef, "", "", map[string]string{"task_id": task.ID, "error": err.Error()})
			_, _ = d.Store.MarkDispatchFailed(ctx, task.ID)
			return core.WorkerBinding{}, core.Attempt{}, err
		}
	}
	workspace, err := os.MkdirTemp("", "secretary-worker-")
	if err != nil {
		if !isChild {
			_ = d.Store.RevokeWorkerCapability(ctx, workerRef)
		}
		_, _ = d.Store.MarkDispatchFailed(ctx, task.ID)
		return core.WorkerBinding{}, core.Attempt{}, err
	}
	prompt := task.Text
	if managedProfile.Name != "" && managedProfile.Delivery != "native" {
		prompt = "Read and follow the managed AGENTS.md before doing this task. Do not replace or weaken its instructions.\n\nTask:\n" + task.Text
	}
	request := node.StartRequest{WorkerRef: workerRef, Task: prompt, Workspace: workspace, Profile: managedProfile}
	if d.MCPCommand != "" {
		role := "worker"
		if isChild {
			role = "child_worker"
		}
		request.MCPServers = []node.MCPServer{node.SecretaryMCPServer(d.MCPCommand, d.MCPDataDir, role, workerCapability, workerRef)}
	}
	session, err := d.Node.Dispatch(ctx, request)
	if err != nil {
		if !isChild {
			_ = d.Store.RevokeWorkerCapability(ctx, workerRef)
		}
		_, _ = d.Store.RecordEvent(ctx, "worker.dispatch_failed", workerRef, "", "", map[string]string{"task_id": task.ID, "error": err.Error()})
		_, transitionErr := d.Store.MarkDispatchFailed(ctx, task.ID)
		if transitionErr != nil {
			return core.WorkerBinding{}, core.Attempt{}, fmt.Errorf("start runtime: %w; mark failed: %v", err, transitionErr)
		}
		return core.WorkerBinding{}, core.Attempt{}, err
	}
	_, binding, attempt, err := d.Store.AcceptDispatchWithProfile(ctx, task.ID, workerRef, "local", session.ID(), workspace, profile)
	if err != nil {
		_ = d.Node.Remove(workerRef)
		if !isChild {
			_ = d.Store.RevokeWorkerCapability(ctx, workerRef)
		}
		return core.WorkerBinding{}, core.Attempt{}, err
	}
	attempt, err = d.Store.SetAttemptActive(ctx, attempt.ID)
	if err == nil {
		_, _ = d.Store.RecordEvent(ctx, "worker.attempt_active", workerRef, attempt.ID, session.ID(), map[string]string{"task_id": task.ID})
	}
	if err != nil {
		_ = d.Node.Remove(workerRef)
		if !isChild {
			_ = d.Store.RevokeWorkerCapability(ctx, workerRef)
		}
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
		_, _ = d.Store.RecordEvent(context.Background(), "worker.attempt_finished", workerRef, attempt.ID, session.ID(), map[string]string{"task_id": taskID, "status": string(status)})
		_, _ = d.Store.FinishClosingTask(context.Background(), taskID)
	}
}
