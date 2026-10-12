package ctl

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type canceledSteeringRuntime struct {
	err    error
	steers *int
}

func (r canceledSteeringRuntime) Start(context.Context, node.StartRequest) (node.Session, error) {
	return canceledSteeringSession{err: r.err, steers: r.steers}, nil
}

type canceledSteeringSession struct {
	err    error
	steers *int
}

func (canceledSteeringSession) ID() string                           { return "local-session" }
func (canceledSteeringSession) Prompt(context.Context, string) error { return nil }
func (s canceledSteeringSession) Steer(context.Context, string) (bool, error) {
	if s.steers != nil {
		(*s.steers)++
	}
	return false, s.err
}
func (canceledSteeringSession) Cancel(context.Context) error   { return nil }
func (canceledSteeringSession) Activity() <-chan node.Activity { return nil }
func (canceledSteeringSession) Result() <-chan node.Result     { return nil }
func (canceledSteeringSession) Close() error                   { return nil }

func TestLocalCommandPreservesSteeringCancellationCause(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			local := node.NewLocal(canceledSteeringRuntime{err: cause})
			if _, err := local.Dispatch(context.Background(), node.StartRequest{WorkerRef: "cancel-worker"}); err != nil {
				t.Fatal(err)
			}
			command := node.Command{Kind: node.CommandSteering, Steering: &node.SteeringCommand{
				Metadata: core.CommandMetadata{CommandID: "cancel-command", WorkerRef: "cancel-worker", TurnID: "turn", AttemptID: "attempt"},
				Text:     "continue",
			}}
			_, err := (NodeRuntime{Local: local}).LocalCommand(context.Background(), command)
			var commandErr *node.NodeCommandError
			if !errors.As(err, &commandErr) || !errors.Is(err, cause) {
				t.Fatalf("local command error lost cancellation: type=%T err=%v", err, err)
			}
		})
	}
}

func TestLocalSteeringUnknownReplayDoesNotRepeatAndReceiptResolves(t *testing.T) {
	ctx, store, service, _ := newWorkerService(t)
	if _, err := store.EnrollNode(ctx, "local"); err != nil && !errors.Is(err, core.ErrNodeAlreadyEnrolled) {
		t.Fatal(err)
	}
	instance := core.HarnessInstance{
		ID: "local/fx", Node: "local", Kind: core.HarnessFX, Version: "1", Status: core.HarnessReady,
		Authentication: core.HarnessAuthentication{Authenticated: true},
		Capabilities:   core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityShell}},
	}
	if err := store.UpdateNodeHeartbeat(ctx, "local", core.HarnessInventorySnapshot{Node: "local", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{instance}}, core.NodeHeartbeat{Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, core.ProjectSpec{ID: "local-repo", Name: "Local Repo", Mappings: []core.ProjectPathMapping{{Node: "local", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	steerCalls := 0
	local := node.NewLocal(canceledSteeringRuntime{err: context.Canceled, steers: &steerCalls})
	service.Runtime = NodeRuntime{Local: local}
	details, err := service.SpawnWorker(ctx, SpawnWorkerRequest{Intent: "test local steering", ProjectID: project.ID, IdempotencyKey: "local-spawn"})
	if err != nil {
		t.Fatal(err)
	}
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	request := MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "continue", IdempotencyKey: "local-uncertain-steer"}
	if _, err := service.MessageWorker(ctx, request); !errors.Is(err, node.ErrCommandOutcomeUnknown) {
		t.Fatalf("local cancellation was not exposed as unknown acceptance: %v", err)
	}
	command, found, err := store.FindWorkerCommandByWorkerAndDedupeKey(ctx, "steering", "key:local-uncertain-steer", details.Worker.ID)
	if err != nil || !found || command.State != core.WorkerCommandUncertain {
		t.Fatalf("local command state=%#v found=%v err=%v, want uncertain", command, found, err)
	}
	if _, err := service.MessageWorker(ctx, request); !errors.Is(err, node.ErrCommandOutcomeUnknown) {
		t.Fatalf("same-key replay was not kept uncertain: %v", err)
	}
	if steerCalls != 1 {
		t.Fatalf("local native Session.Steer ran %d times, want once", steerCalls)
	}
	receipt := core.WorkerCommandReceipt{CommandID: command.ID, Kind: "steering", TurnID: attempt.TurnID, AttemptID: attempt.ID, State: core.WorkerCommandReceiptAccepted}
	reconciled, err := store.ReconcileWorkerCommandReceipt(ctx, "local", receipt)
	if err != nil || reconciled.State != core.WorkerCommandDelivered {
		t.Fatalf("accepted local receipt did not reconcile: command=%#v err=%v", reconciled, err)
	}
	if _, err := service.MessageWorker(ctx, request); err != nil {
		t.Fatalf("reconciled same-key replay: %v", err)
	}
	if steerCalls != 1 {
		t.Fatalf("local native Session.Steer ran %d times after receipt, want once", steerCalls)
	}
}
