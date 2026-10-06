package ctl

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type workerTemplateDispatchObservation struct {
	worker     core.Worker
	turn       core.Turn
	attempt    core.Phase4Attempt
	resolution core.DispatchResolution
}

type workerTemplateResumeObservation struct {
	worker  core.Worker
	turn    core.Turn
	attempt core.Phase4Attempt
}

type workerTemplateRuntime struct {
	dispatches []workerTemplateDispatchObservation
	resumes    []workerTemplateResumeObservation
}

type legacyWorkerTemplateNodeRuntime struct{ starts []node.StartRequest }

func (r *legacyWorkerTemplateNodeRuntime) Start(_ context.Context, request node.StartRequest) (node.Session, error) {
	r.starts = append(r.starts, request)
	return legacyWorkerTemplateNodeSession{}, nil
}

type legacyWorkerTemplateNodeSession struct{}

func (legacyWorkerTemplateNodeSession) ID() string                                  { return "synthetic-legacy-fx" }
func (legacyWorkerTemplateNodeSession) Prompt(context.Context, string) error        { return nil }
func (legacyWorkerTemplateNodeSession) Steer(context.Context, string) (bool, error) { return true, nil }
func (legacyWorkerTemplateNodeSession) Cancel(context.Context) error                { return nil }
func (legacyWorkerTemplateNodeSession) Activity() <-chan node.Activity              { return nil }
func (legacyWorkerTemplateNodeSession) Result() <-chan node.Result                  { return nil }
func (legacyWorkerTemplateNodeSession) Close() error                                { return nil }

func (r *workerTemplateRuntime) Dispatch(_ context.Context, _ string, worker core.Worker, turn core.Turn, attempt core.Phase4Attempt, resolution core.DispatchResolution) error {
	r.dispatches = append(r.dispatches, workerTemplateDispatchObservation{worker: worker, turn: turn, attempt: attempt, resolution: resolution})
	return nil
}
func (*workerTemplateRuntime) Steer(context.Context, string, core.Worker, core.Phase4Attempt, string) error {
	return nil
}
func (*workerTemplateRuntime) Respond(context.Context, string, core.Worker, core.Phase4Attempt, string, string) error {
	return nil
}
func (r *workerTemplateRuntime) Resume(_ context.Context, _ string, worker core.Worker, turn core.Turn, attempt core.Phase4Attempt, _ string) error {
	r.resumes = append(r.resumes, workerTemplateResumeObservation{worker: worker, turn: turn, attempt: attempt})
	return nil
}
func (*workerTemplateRuntime) Cancel(context.Context, string, core.Worker, core.Phase4Attempt) error {
	return nil
}

func TestNewFXWorkerTemplateWithoutSourceRejectsBeforeBinding(t *testing.T) {
	ctx, _, service, project := newWorkerService(t)
	service.WorkerProfileSource = nil
	_, err := service.SpawnWorker(ctx, SpawnWorkerRequest{
		Intent: "synthetic FX template fixture", ProjectID: project.ID, IdempotencyKey: "new-fx-without-template",
	})
	if !errors.Is(err, ErrWorkerProfileUnavailable) {
		t.Fatalf("new FX Worker without template source rejected=%t", errors.Is(err, ErrWorkerProfileUnavailable))
	}
	workers, err := service.ListWorkers(ctx)
	if err != nil || len(workers) != 0 {
		t.Fatalf("new FX Worker binding exists=%t err_present=%t", len(workers) != 0, err != nil)
	}
}

func TestExistingFXWorkerTemplateBindingSurvivesDispatchFollowUpAndResume(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "legacy-secretary.db")
	store, err := core.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	project, err := store.CreateProject(ctx, core.ProjectSpec{
		ID: "legacy-fx-project", Name: "Legacy FX project", Mappings: []core.ProjectPathMapping{{Node: "local", Path: workspace}},
		Policy: core.ProjectPolicy{AllowedHarnessKinds: []core.HarnessKind{core.HarnessFX}},
	})
	if err != nil {
		t.Fatal(err)
	}
	instance := core.HarnessInstance{
		ID: "local/fx", Node: "local", Kind: core.HarnessFX, Version: "legacy-v1",
		Authentication: core.HarnessAuthentication{Authenticated: true}, Status: core.HarnessReady,
		Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityCancel, core.CapabilityShell}},
	}
	if _, err := store.EnrollNode(ctx, "local"); err != nil && !errors.Is(err, core.ErrNodeAlreadyEnrolled) {
		t.Fatal(err)
	}
	if err := store.UpdateNodeHeartbeat(ctx, "local", core.HarnessInventorySnapshot{
		Node: "local", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{instance},
	}, core.NodeHeartbeat{Capacity: 2}); err != nil {
		t.Fatal(err)
	}
	worker, originalTurn, originalAttempt, resolution, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "existing FX task",
		core.DispatchResolutionRequest{ProjectID: project.ID, WorkerPolicy: core.HarnessPolicy{DefaultHarness: core.HarnessFX}}, "existing-fx-binding")
	if err != nil {
		t.Fatal(err)
	}
	if worker.WorkerTemplateRequired != true || worker.ProfileSnapshot != "" || resolution.HarnessInstance.ID != "local/fx" {
		t.Fatal("fixture did not create the pre-marker binding that models an existing Worker")
	}
	originalProjectSnapshot := worker.ProjectSnapshot
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen a database schema from before the generation marker existed. The
	// existing Worker, Turn, Attempt and binding are left intact.
	legacyDB, err := sql.Open("sqlite", databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := legacyDB.ExecContext(ctx, `ALTER TABLE workers DROP COLUMN worker_template_required`); err != nil {
		_ = legacyDB.Close()
		t.Fatal(err)
	}
	if err := legacyDB.Close(); err != nil {
		t.Fatal(err)
	}
	legacyStore, err := core.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = legacyStore.Close() })
	legacyWorker, err := legacyStore.Worker(ctx, worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if legacyWorker.WorkerTemplateRequired || legacyWorker.ProfileSnapshot != "" || legacyWorker.ProjectSnapshot != originalProjectSnapshot || legacyWorker.HarnessInstanceID != "local/fx" {
		t.Fatal("additive migration did not prove and preserve the existing FX binding")
	}
	originalBinding, err := legacyStore.ResolveWorkerBinding(ctx, worker.ID)
	if err != nil || originalBinding.HarnessInstance.ID != "local/fx" || originalBinding.HarnessInstance.Kind != core.HarnessFX || originalBinding.Snapshot.HarnessInstance.ID != "local/fx" {
		t.Fatal("legacy Worker original HarnessInstance binding did not survive schema reopen")
	}
	legacyRuntime := &legacyWorkerTemplateNodeRuntime{}
	localNode := node.NewLocal(legacyRuntime)
	t.Cleanup(func() { _ = localNode.Close() })
	if err := (NodeRuntime{Local: localNode}).Dispatch(ctx, "legacy-node-runtime-dispatch", legacyWorker, originalTurn, originalAttempt, core.DispatchResolution{ProjectDispatch: originalBinding}); err != nil {
		t.Fatalf("NodeRuntime rejected existing durable FX binding: %v", err)
	}
	if len(legacyRuntime.starts) != 1 || !reflect.DeepEqual(legacyRuntime.starts[0].Profile, node.ManagedProfile{
		Runtime: string(core.HarnessFX), Model: originalBinding.Snapshot.Policy.ModelPin(), Reasoning: originalBinding.Snapshot.Policy.Reasoning,
	}) || legacyRuntime.starts[0].HarnessInstance.ID != "local/fx" || legacyRuntime.starts[0].Workspace != legacyWorker.Workspace {
		t.Fatal("NodeRuntime changed the existing FX binding or synthesized a Worker template")
	}

	runtime := &workerTemplateRuntime{}
	service := WorkerService{
		Store: legacyStore, PersonID: person.ID, Capability: capability, Runtime: runtime,
		WorkerPolicy:        core.HarnessPolicy{DefaultHarness: core.HarnessOpenCode, ModelID: "new-default/model", Reasoning: "xhigh"},
		WorkerProfileSource: nil,
	}
	spawned, err := service.SpawnWorker(ctx, SpawnWorkerRequest{
		Intent: "existing FX task", ProjectID: project.ID, IdempotencyKey: "existing-fx-binding",
	})
	if err != nil || spawned.Worker.ID != worker.ID || len(runtime.dispatches) != 1 {
		t.Fatalf("existing FX dispatch preserved=%t dispatches=%d err_present=%t", spawned.Worker.ID == worker.ID, len(runtime.dispatches), err != nil)
	}
	assertExistingFXTemplateBinding(t, runtime.dispatches[0].worker, runtime.dispatches[0].resolution.ProjectDispatch, originalProjectSnapshot)

	if _, err := legacyStore.SetPhase4AttemptActive(ctx, originalAttempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := legacyStore.RecordAttemptOutcome(ctx, originalAttempt.ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "synthetic FX completion",
	}); err != nil {
		t.Fatal(err)
	}
	followUp, err := service.MessageWorker(ctx, MessageWorkerRequest{
		WorkerRef: worker.WorkerRef, Text: "continue this existing FX task", IdempotencyKey: "existing-fx-follow-up",
	})
	if err != nil || len(runtime.dispatches) != 2 || followUp.ActionTurnID == originalTurn.ID {
		t.Fatalf("existing FX Follow-up dispatches=%d new_turn=%t err_present=%t", len(runtime.dispatches), followUp.ActionTurnID != originalTurn.ID, err != nil)
	}
	assertExistingFXTemplateBinding(t, runtime.dispatches[1].worker, runtime.dispatches[1].resolution.ProjectDispatch, originalProjectSnapshot)
	followUpAttempt := followUp.CurrentAttempt()
	if followUpAttempt == nil || followUpAttempt.HarnessInstanceID != "local/fx" {
		t.Fatal("Follow-up changed the existing FX HarnessInstance")
	}
	if _, err := legacyStore.SetPhase4AttemptActive(ctx, followUpAttempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := legacyStore.InterruptPhase4Attempt(ctx, followUpAttempt.ID, "synthetic_interrupted", "synthetic interruption"); err != nil {
		t.Fatal(err)
	}
	resumed, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: worker.WorkerRef, Text: "resume existing FX task"})
	if err != nil || len(runtime.resumes) != 1 || resumed.CurrentAttempt() == nil || resumed.CurrentAttempt().HarnessInstanceID != "local/fx" {
		t.Fatalf("existing FX Resume preserved=%t resumes=%d err_present=%t", resumed.CurrentAttempt() != nil && resumed.CurrentAttempt().HarnessInstanceID == "local/fx", len(runtime.resumes), err != nil)
	}
	assertExistingFXTemplateBinding(t, runtime.resumes[0].worker, core.ProjectDispatch{HarnessInstance: originalBinding.HarnessInstance, Snapshot: originalBinding.Snapshot}, originalProjectSnapshot)
}

func assertExistingFXTemplateBinding(t *testing.T, worker core.Worker, binding core.ProjectDispatch, originalProjectSnapshot string) {
	t.Helper()
	if worker.WorkerTemplateRequired || worker.ProfileSnapshot != "" || worker.NodeID != "local" || worker.HarnessInstanceID != "local/fx" ||
		worker.ProjectSnapshot != originalProjectSnapshot || binding.HarnessInstance.ID != "local/fx" || binding.HarnessInstance.Kind != core.HarnessFX ||
		binding.Snapshot.HarnessInstance.ID != "local/fx" || len(binding.Snapshot.Policy.AllowedHarnessKinds) != 1 || binding.Snapshot.Policy.AllowedHarnessKinds[0] != core.HarnessFX {
		t.Fatal("existing FX Worker binding or empty legacy template changed")
	}
}
