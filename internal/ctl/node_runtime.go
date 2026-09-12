package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

// NodeRuntime is the server-side WorkerRuntime adapter. It translates only
// server-owned Worker/Turn/Attempt fields into typed Node commands. Native
// harness session IDs and callback capabilities never enter this boundary.
type NodeRuntime struct {
	Manager *node.ServerManager
}

func (r NodeRuntime) send(ctx context.Context, nodeID core.NodeReference, command node.Command) error {
	if r.Manager == nil {
		return ErrWorkerRuntimeUnavailable
	}
	return r.Manager.SendCommand(ctx, nodeID, command)
}

func (r NodeRuntime) Dispatch(ctx context.Context, commandID string, worker core.Worker, turn core.Turn, attempt core.Phase4Attempt, resolution core.DispatchResolution) error {
	envelope, err := r.envelope(worker, turn, attempt, resolution.ProjectDispatch)
	if err != nil {
		return err
	}
	return r.send(ctx, core.NodeReference(attempt.NodeID), node.Command{Kind: node.CommandDispatch, Dispatch: &node.DispatchCommand{Metadata: r.metadata(commandID, worker, turn, attempt, resolution.HarnessInstance.ID), Envelope: envelope}})
}

func (r NodeRuntime) Resume(ctx context.Context, commandID string, worker core.Worker, turn core.Turn, attempt core.Phase4Attempt, _ string) error {
	binding, err := bindingFromWorker(worker)
	if err != nil {
		return err
	}
	envelope, err := r.envelope(worker, turn, attempt, binding)
	if err != nil {
		return err
	}
	return r.send(ctx, core.NodeReference(attempt.NodeID), node.Command{Kind: node.CommandResume, Resume: &node.ResumeCommand{Metadata: r.metadata(commandID, worker, turn, attempt, envelope.HarnessInstance.ID), Envelope: envelope}})
}

func (r NodeRuntime) Steer(ctx context.Context, commandID string, worker core.Worker, attempt core.Phase4Attempt, text string) error {
	return r.send(ctx, core.NodeReference(attempt.NodeID), node.Command{Kind: node.CommandSteering, Steering: &node.SteeringCommand{Metadata: r.metadata(commandID, worker, core.Turn{ID: attempt.TurnID}, attempt, core.HarnessInstanceID(worker.HarnessInstanceID)), Text: text}})
}

func (r NodeRuntime) Respond(ctx context.Context, commandID string, worker core.Worker, attempt core.Phase4Attempt, requestID, response string) error {
	return r.send(ctx, core.NodeReference(attempt.NodeID), node.Command{Kind: node.CommandRespondWorker, RespondWorker: &node.RespondWorkerCommand{Metadata: r.metadata(commandID, worker, core.Turn{ID: attempt.TurnID}, attempt, core.HarnessInstanceID(worker.HarnessInstanceID)), RequestID: requestID, Response: response}})
}

func (r NodeRuntime) Cancel(ctx context.Context, commandID string, worker core.Worker, attempt core.Phase4Attempt) error {
	return r.send(ctx, core.NodeReference(attempt.NodeID), node.Command{Kind: node.CommandCancel, Cancel: &node.CancelCommand{Metadata: r.metadata(commandID, worker, core.Turn{ID: attempt.TurnID}, attempt, core.HarnessInstanceID(worker.HarnessInstanceID)), Reason: "owner requested cancel"}})
}

func (r NodeRuntime) metadata(commandID string, worker core.Worker, turn core.Turn, attempt core.Phase4Attempt, harness core.HarnessInstanceID) core.CommandMetadata {
	return core.CommandMetadata{CommandID: commandID, Node: core.NodeReference(attempt.NodeID), HarnessInstanceID: harness, WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, CorrelationID: attempt.CorrelationID, IssuedAt: time.Now().UTC()}
}

func (r NodeRuntime) envelope(worker core.Worker, turn core.Turn, attempt core.Phase4Attempt, binding core.ProjectDispatch) (node.WorkerEnvelope, error) {
	if binding.HarnessInstance.ID == "" {
		return node.WorkerEnvelope{}, errors.New("worker: immutable HarnessInstance binding is missing")
	}
	policy := binding.Snapshot.Policy
	model := policy.ModelPin()
	reasoning := policy.Reasoning
	approvalPolicy := ""
	if policy.EffectiveExecution().RequireApproval {
		approvalPolicy = "required"
	}
	return node.WorkerEnvelope{WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, OriginalUserIntent: worker.Intent, NormalizedGoal: turn.NormalizedIntent, ProjectID: worker.ProjectID, ProjectSnapshot: binding.Snapshot, Workspace: binding.Workspace, HarnessInstance: binding.HarnessInstance, Model: model, Reasoning: reasoning, ApprovalPolicy: approvalPolicy}, nil
}

func bindingFromWorker(worker core.Worker) (core.ProjectDispatch, error) {
	var snapshot core.ProjectSnapshot
	if strings.TrimSpace(worker.ProjectSnapshot) == "" {
		return core.ProjectDispatch{}, errors.New("worker: Project snapshot is missing")
	}
	if err := json.Unmarshal([]byte(worker.ProjectSnapshot), &snapshot); err != nil {
		return core.ProjectDispatch{}, err
	}
	return core.ProjectDispatch{Project: core.Project{ID: worker.ProjectID, Revision: snapshot.Revision}, Node: core.NodeReference(worker.NodeID), Workspace: worker.Workspace, HarnessInstance: snapshot.HarnessInstance, Snapshot: snapshot}, nil
}

var _ WorkerRuntime = NodeRuntime{}
