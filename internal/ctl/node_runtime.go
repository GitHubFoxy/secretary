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
	Local   *node.LocalNode
}

func (r NodeRuntime) send(ctx context.Context, nodeID core.NodeReference, command node.Command) error {
	if r.Local != nil && nodeID == "local" {
		_, err := r.LocalCommand(ctx, command)
		return err
	}
	if r.Manager == nil {
		return ErrWorkerRuntimeUnavailable
	}
	return r.Manager.SendCommand(ctx, nodeID, command)
}

// LocalCommand is the trusted-local equivalent of the authenticated Node
// protocol. It keeps the old local daemon path while exposing one runtime
// adapter to WorkerService.
func (r NodeRuntime) LocalCommand(ctx context.Context, command node.Command) (node.CommandOutcome, error) {
	if r.Local == nil {
		return node.CommandOutcome{}, ErrWorkerRuntimeUnavailable
	}
	metadata := command.Metadata()
	session, ok := r.Local.Session(metadata.WorkerRef)
	if command.Kind == node.CommandDispatch {
		if command.Dispatch == nil {
			return node.CommandOutcome{}, errors.New("worker: local dispatch command is missing")
		}
		var err error
		session, err = r.Local.Dispatch(ctx, node.StartRequest{WorkerRef: command.Dispatch.Envelope.WorkerRef, Task: command.Dispatch.Envelope.OriginalUserIntent, Workspace: command.Dispatch.Envelope.Workspace, Profile: command.Dispatch.Envelope.Profile, HarnessInstance: command.Dispatch.Envelope.HarnessInstance, Model: command.Dispatch.Envelope.Model, Reasoning: command.Dispatch.Envelope.Reasoning, ApprovalPolicy: command.Dispatch.Envelope.ApprovalPolicy})
		if err != nil {
			return node.CommandOutcome{}, err
		}
		return node.CommandOutcome{CommandID: metadata.CommandID, Kind: command.Kind, State: node.CommandAccepted}, nil
	}
	if !ok {
		return node.CommandOutcome{}, ErrWorkerRuntimeUnavailable
	}
	switch command.Kind {
	case node.CommandRespondWorker:
		responder, ok := session.(node.Responder)
		if !ok || command.RespondWorker == nil {
			return node.CommandOutcome{}, ErrWorkerRuntimeUnavailable
		}
		err := responder.Respond(ctx, command.RespondWorker.RequestID, command.RespondWorker.Response)
		if err != nil {
			return node.CommandOutcome{}, err
		}
	case node.CommandSteering:
		if command.Steering == nil {
			return node.CommandOutcome{}, errors.New("worker: local steering command is missing")
		}
		_, err := session.Steer(ctx, command.Steering.Text)
		if err != nil {
			return node.CommandOutcome{}, err
		}
	case node.CommandCancel:
		if err := session.Cancel(ctx); err != nil {
			return node.CommandOutcome{}, err
		}
	default:
		return node.CommandOutcome{}, ErrWorkerRuntimeUnavailable
	}
	return node.CommandOutcome{CommandID: metadata.CommandID, Kind: command.Kind, State: node.CommandAccepted}, nil
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
	command := node.Command{Kind: node.CommandRespondWorker, RespondWorker: &node.RespondWorkerCommand{Metadata: r.metadata(commandID, worker, core.Turn{ID: attempt.TurnID}, attempt, core.HarnessInstanceID(worker.HarnessInstanceID)), RequestID: requestID, Response: response}}
	if r.Local != nil && core.NodeReference(attempt.NodeID) == "local" {
		outcome, err := r.LocalCommand(ctx, command)
		if err != nil {
			return err
		}
		if outcome.State != node.CommandAccepted {
			return errors.New("worker: local response was not accepted")
		}
		return nil
	}
	if r.Manager == nil {
		return ErrWorkerRuntimeUnavailable
	}
	return r.Manager.SendCommandAndWait(ctx, core.NodeReference(attempt.NodeID), command)
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
