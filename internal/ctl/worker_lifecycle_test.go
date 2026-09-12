package ctl

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

type lifecycleRuntime struct {
	mu    sync.Mutex
	calls []string
}

func (r *lifecycleRuntime) add(call string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call)
}
func (r *lifecycleRuntime) Dispatch(context.Context, string, core.Worker, core.Turn, core.Phase4Attempt, core.DispatchResolution) error {
	r.add("dispatch")
	return nil
}
func (r *lifecycleRuntime) Steer(context.Context, string, core.Worker, core.Phase4Attempt, string) error {
	r.add("steer")
	return nil
}
func (r *lifecycleRuntime) Respond(context.Context, string, core.Worker, core.Phase4Attempt, string, string) error {
	r.add("respond")
	return nil
}
func (r *lifecycleRuntime) Resume(context.Context, string, core.Worker, core.Turn, core.Phase4Attempt, string) error {
	r.add("resume")
	return nil
}
func (r *lifecycleRuntime) Cancel(context.Context, string, core.Worker, core.Phase4Attempt) error {
	r.add("cancel")
	return nil
}

type unavailableCancelRuntime struct{ lifecycleRuntime }

func (r *unavailableCancelRuntime) Cancel(context.Context, string, core.Worker, core.Phase4Attempt) error {
	r.add("cancel")
	return errors.New("node offline")
}

func (r *lifecycleRuntime) count(want string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, call := range r.calls {
		if call == want {
			count++
		}
	}
	return count
}

func newWorkerService(t *testing.T) (context.Context, *core.Store, WorkerService, core.Project) {
	t.Helper()
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	person, _, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, core.ProjectSpec{ID: "repo", Name: "Repo", Mappings: []core.ProjectPathMapping{{Node: "node", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	instance := core.HarnessInstance{ID: "node/fx", Node: "node", Kind: core.HarnessFX, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}, Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityShell}}}
	if _, err := store.EnrollNode(ctx, "node"); err != nil && !errors.Is(err, core.ErrNodeAlreadyEnrolled) {
		t.Fatal(err)
	}
	if err := store.UpdateNodeHeartbeat(ctx, "node", core.HarnessInventorySnapshot{Node: "node", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{instance}}, core.NodeHeartbeat{Capacity: 2}); err != nil {
		t.Fatal(err)
	}
	runtime := &lifecycleRuntime{}
	return ctx, store, WorkerService{Store: store, PersonID: person.ID, Capability: capability, Runtime: runtime}, project
}

func spawnLifecycleWorker(t *testing.T, ctx context.Context, service WorkerService, project core.Project) core.WorkerDetails {
	t.Helper()
	details, err := service.SpawnWorker(ctx, SpawnWorkerRequest{Intent: "inspect", ProjectID: project.ID, IdempotencyKey: "spawn"})
	if err != nil {
		t.Fatal(err)
	}
	if len(details.Turns) != 1 || len(details.Attempts) != 1 || details.Worker.NodeID != "node" || details.Worker.HarnessInstanceID != "node/fx" {
		t.Fatalf("spawned=%#v", details)
	}
	return details
}

func TestWorkerServiceMessageLifecycleMatrix(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	runtime := service.Runtime.(*lifecycleRuntime)
	first := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, first.ID); err != nil {
		t.Fatal(err)
	}

	active, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "prioritize tests"})
	if err != nil || len(active.Turns) != 1 || runtime.count("steer") != 1 {
		t.Fatalf("active=%#v err=%v calls=%d", active, err, runtime.count("steer"))
	}
	if _, err := store.SetPhase4AttemptNeedsInput(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	input, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "yes", RequestID: "ask-1"})
	if err != nil || len(input.Turns) != 1 || runtime.count("respond") != 1 || input.Worker.Status != core.WorkerWorking {
		t.Fatalf("input=%#v err=%v", input, err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, first.ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	idle, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "one more", IdempotencyKey: "follow-up"})
	if err != nil || len(idle.Turns) != 2 || len(idle.Attempts) != 2 || runtime.count("dispatch") != 2 {
		t.Fatalf("idle=%#v err=%v", idle, err)
	}
	second := idle.CurrentAttempt()
	if second == nil || second.NodeID != first.NodeID || second.HarnessInstanceID != first.HarnessInstanceID {
		t.Fatalf("binding changed: %#v", second)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.InterruptPhase4Attempt(ctx, second.ID, "lost", "lost session"); err != nil {
		t.Fatal(err)
	}
	interrupted, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "resume"})
	if err != nil || interrupted.Worker.Status != core.WorkerQueued || runtime.count("resume") != 1 {
		t.Fatalf("interrupted=%#v err=%v", interrupted, err)
	}
}

func TestWorkerServiceSteeringDedupeKeepsDistinctMessages(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	if _, err := store.SetPhase4AttemptActive(ctx, details.Attempts[0].ID); err != nil {
		t.Fatal(err)
	}
	runtime := service.Runtime.(*lifecycleRuntime)
	for _, text := range []string{"first direction", "second direction"} {
		if _, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.count("steer") != 2 {
		t.Fatalf("distinct steering calls=%d, want 2", runtime.count("steer"))
	}
	request := MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "deduplicated", IdempotencyKey: "steer-key"}
	if _, err := service.MessageWorker(ctx, request); err != nil {
		t.Fatal(err)
	}
	request.Text = "duplicate delivery with changed text"
	if _, err := service.MessageWorker(ctx, request); err != nil {
		t.Fatal(err)
	}
	if runtime.count("steer") != 3 {
		t.Fatalf("same-key steering calls=%d, want 3", runtime.count("steer"))
	}
}

func TestWorkerServiceCancelAndCloseAreIdempotentAndDoNotCreateTask(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	if _, err := store.SetPhase4AttemptActive(ctx, details.Attempts[0].ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := service.CancelWorker(ctx, details.Worker.WorkerRef); err != nil {
			t.Fatal(err)
		}
	}
	canceled, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil || canceled.CurrentAttempt() == nil || !canceled.CurrentAttempt().State.Terminal() || len(canceled.Outcomes) != 1 || len(canceled.Results) != 1 {
		t.Fatalf("canceled=%#v err=%v", canceled, err)
	}
	for range 2 {
		closed, err := service.CloseWorker(ctx, details.Worker.WorkerRef)
		if err != nil || closed.Worker.Status != core.WorkerClosed || closed.CurrentAttempt() == nil && len(closed.Attempts) != 1 {
			t.Fatalf("closed=%#v err=%v", closed, err)
		}
	}
	workers, err := service.ListWorkers(ctx)
	if err != nil || len(workers) != 1 {
		t.Fatalf("workers=%#v err=%v", workers, err)
	}
}

func TestWorkerServiceConcurrentCancelCreatesOneFinalOutcome(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	if _, err := store.SetPhase4AttemptActive(ctx, details.Attempts[0].ID); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := service.CancelWorker(ctx, details.Worker.WorkerRef)
			errs <- err
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, ErrWorkerCommandPending) {
			t.Fatal(err)
		}
	}
	current, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil || len(current.Outcomes) != 1 || len(current.Results) != 1 || !current.Attempts[0].State.Terminal() || service.Runtime.(*lifecycleRuntime).count("cancel") != 1 {
		t.Fatalf("current=%#v err=%v", current, err)
	}
}

func TestWorkerServiceConcurrentCancelAcrossStoresSendsOneCommand(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	store, err := core.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, _, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	project, err := store.CreateProject(ctx, core.ProjectSpec{ID: "repo", Name: "Repo", Mappings: []core.ProjectPathMapping{{Node: "node", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	instance := core.HarnessInstance{ID: "node/fx", Node: "node", Kind: core.HarnessFX, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}, Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityShell}}}
	if _, err := store.EnrollNode(ctx, "node"); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNodeHeartbeat(ctx, "node", core.HarnessInventorySnapshot{Node: "node", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{instance}}, core.NodeHeartbeat{Capacity: 2}); err != nil {
		t.Fatal(err)
	}
	firstRuntime := &lifecycleRuntime{}
	first := WorkerService{Store: store, PersonID: person.ID, Capability: capability, Runtime: firstRuntime}
	details := spawnLifecycleWorker(t, ctx, first, project)
	if _, err := store.SetPhase4AttemptActive(ctx, details.Attempts[0].ID); err != nil {
		t.Fatal(err)
	}
	other, err := core.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	secondRuntime := &lifecycleRuntime{}
	second := WorkerService{Store: other, PersonID: person.ID, Capability: capability, Runtime: secondRuntime}
	var group sync.WaitGroup
	errs := make(chan error, 2)
	for _, service := range []WorkerService{first, second} {
		group.Add(1)
		go func(service WorkerService) {
			defer group.Done()
			_, err := service.CancelWorker(ctx, details.Worker.WorkerRef)
			errs <- err
		}(service)
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, ErrWorkerCommandPending) {
			t.Fatal(err)
		}
	}
	if firstRuntime.count("cancel")+secondRuntime.count("cancel") != 1 {
		t.Fatalf("cancel sends=%d", firstRuntime.count("cancel")+secondRuntime.count("cancel"))
	}
}

func TestWorkerServiceDoesNotFinalizeCancelOrCloseWhenNodeIsUnavailable(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	if _, err := store.SetPhase4AttemptActive(ctx, details.Attempts[0].ID); err != nil {
		t.Fatal(err)
	}
	unavailable := &unavailableCancelRuntime{}
	service.Runtime = unavailable
	if _, err := service.CancelWorker(ctx, details.Worker.WorkerRef); err == nil {
		t.Fatal("expected unavailable Node error")
	}
	if _, err := service.CloseWorker(ctx, details.Worker.WorkerRef); err == nil {
		t.Fatal("expected unavailable Node error")
	}
	attempt, err := store.Phase4Attempt(ctx, details.Attempts[0].ID)
	if err != nil || attempt.State != core.AttemptActive || unavailable.count("cancel") != 1 {
		t.Fatalf("attempt=%#v err=%v cancels=%d", attempt, err, unavailable.count("cancel"))
	}
}

func TestWorkerServiceDuplicateSpawnSendsOneDispatch(t *testing.T) {
	ctx, _, service, project := newWorkerService(t)
	request := SpawnWorkerRequest{Intent: "inspect", ProjectID: project.ID, IdempotencyKey: "same-spawn"}
	first, err := service.SpawnWorker(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.SpawnWorker(ctx, request)
	if err != nil || second.Worker.ID != first.Worker.ID || service.Runtime.(*lifecycleRuntime).count("dispatch") != 1 {
		t.Fatalf("first=%#v second=%#v err=%v dispatches=%d", first, second, err, service.Runtime.(*lifecycleRuntime).count("dispatch"))
	}
}

func TestWorkerServiceSpawnReplayAfterProjectDeleteUsesDurableBinding(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	request := SpawnWorkerRequest{Intent: "inspect", ProjectID: project.ID, IdempotencyKey: "durable-spawn"}
	first, err := service.SpawnWorker(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteProject(ctx, project.ID, project.Revision); err != nil {
		t.Fatal(err)
	}
	service.Runtime = nil
	replayed, err := service.SpawnWorker(ctx, request)
	if err != nil || replayed.Worker.ID != first.Worker.ID || replayed.Turns[0].ID != first.Turns[0].ID || replayed.Attempts[0].ID != first.Attempts[0].ID || replayed.Worker.HarnessInstanceID != first.Worker.HarnessInstanceID {
		t.Fatalf("replayed=%#v first=%#v err=%v", replayed, first, err)
	}
}

func TestWorkerServiceFailsClosedWithoutRuntimeBeforeSpawnSideEffects(t *testing.T) {
	ctx, _, service, project := newWorkerService(t)
	service.Runtime = nil
	if _, err := service.SpawnWorker(ctx, SpawnWorkerRequest{Intent: "inspect", ProjectID: project.ID, IdempotencyKey: "no-runtime"}); !errors.Is(err, ErrWorkerRuntimeUnavailable) {
		t.Fatalf("spawn error=%v", err)
	}
	workers, err := service.ListWorkers(ctx)
	if err != nil || len(workers) != 0 {
		t.Fatalf("workers=%#v err=%v", workers, err)
	}
}

func TestWorkerServiceDuplicateNeedsInputResponseIsNoOp(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptNeedsInput(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	request := MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "answer", RequestID: "question-1", IdempotencyKey: "answer-1"}
	if _, err := service.MessageWorker(ctx, request); err != nil {
		t.Fatal(err)
	}
	duplicate, err := service.MessageWorker(ctx, request)
	if err != nil || duplicate.Worker.Status != core.WorkerWorking || service.Runtime.(*lifecycleRuntime).count("respond") != 1 || service.Runtime.(*lifecycleRuntime).count("steer") != 0 {
		t.Fatalf("duplicate=%#v err=%v responds=%d steers=%d", duplicate, err, service.Runtime.(*lifecycleRuntime).count("respond"), service.Runtime.(*lifecycleRuntime).count("steer"))
	}
}

func TestWorkerServiceNeedsInputRequiresRequestID(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	if _, err := store.SetPhase4AttemptActive(ctx, details.Attempts[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptNeedsInput(ctx, details.Attempts[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "answer"}); err == nil {
		t.Fatal("expected missing request_id error")
	}
}

func TestWorkerServiceRejectsInvalidSelectionWithoutFXFallback(t *testing.T) {
	ctx, _, service, project := newWorkerService(t)
	if _, err := service.SpawnWorker(ctx, SpawnWorkerRequest{Intent: "inspect", ProjectID: project.ID, HarnessInstance: "node/fx", ModelID: "invalid"}); !errors.Is(err, core.ErrInvalidDispatchPin) {
		t.Fatalf("error=%v", err)
	}
}
