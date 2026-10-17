package ctl

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type lifecycleRuntime struct {
	store *core.Store
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
func (r *lifecycleRuntime) Cancel(ctx context.Context, _ string, _ core.Worker, attempt core.Phase4Attempt) error {
	r.add("cancel")
	return r.publishCanceled(ctx, attempt)
}
func (r *lifecycleRuntime) publishCanceled(ctx context.Context, attempt core.Phase4Attempt) error {
	if r.store == nil {
		return nil
	}
	_, _, _, err := r.store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{Status: core.OutcomeCanceled, Classification: core.OutcomeFinal, Summary: "Native fixture canceled"})
	return err
}

type recordingLifecycleRuntime struct {
	lifecycleRuntime
	dispatchIDs []string
	resumeIDs   []string
	resumeTexts []string
}

func (r *recordingLifecycleRuntime) Dispatch(_ context.Context, commandID string, _ core.Worker, _ core.Turn, _ core.Phase4Attempt, _ core.DispatchResolution) error {
	r.add("dispatch")
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dispatchIDs = append(r.dispatchIDs, commandID)
	return nil
}

func (r *recordingLifecycleRuntime) Resume(_ context.Context, commandID string, _ core.Worker, _ core.Turn, _ core.Phase4Attempt, text string) error {
	r.add("resume")
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resumeIDs = append(r.resumeIDs, commandID)
	r.resumeTexts = append(r.resumeTexts, text)
	return nil
}

func (r *recordingLifecycleRuntime) dispatchCommands() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.dispatchIDs...)
}

func (r *recordingLifecycleRuntime) resumeCommands() ([]string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.resumeIDs...), append([]string(nil), r.resumeTexts...)
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

type blockingLifecycleRuntime struct {
	lifecycleRuntime
	respondStarted chan struct{}
	respondRelease chan struct{}
	cancelStarted  chan struct{}
	cancelRelease  chan struct{}
}

func (r *blockingLifecycleRuntime) Respond(context.Context, string, core.Worker, core.Phase4Attempt, string, string) error {
	r.add("respond")
	close(r.respondStarted)
	<-r.respondRelease
	return nil
}

func (r *blockingLifecycleRuntime) Cancel(ctx context.Context, _ string, _ core.Worker, attempt core.Phase4Attempt) error {
	r.add("cancel")
	close(r.cancelStarted)
	<-r.cancelRelease
	return r.publishCanceled(ctx, attempt)
}

func newWorkerService(t *testing.T) (context.Context, *core.Store, WorkerService, core.Project) {
	t.Helper()
	return newWorkerServiceAt(t, filepath.Join(t.TempDir(), "secretary.db"))
}

func newWorkerServiceAt(t *testing.T, path string) (context.Context, *core.Store, WorkerService, core.Project) {
	t.Helper()
	ctx := context.Background()
	store, err := core.Open(ctx, path)
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
	runtime := &lifecycleRuntime{store: store}
	return ctx, store, WorkerService{Store: store, PersonID: person.ID, Capability: capability, Runtime: runtime, WorkerPolicy: core.HarnessPolicy{DefaultHarness: core.HarnessFX}, WorkerProfileSource: syntheticWorkerTemplateSource()}, project
}

func syntheticWorkerTemplateSource() func() (node.ManagedProfile, error) {
	return func() (node.ManagedProfile, error) {
		return node.ManagedProfile{Version: "synthetic-worker-template-v1", Name: "worker", Content: "Synthetic Worker template fixture.", AllowTools: []string{"read"}, Hash: "synthetic-worker-template-source", Runtime: string(core.HarnessFX)}, nil
	}
}

func spawnLifecycleWorker(t *testing.T, ctx context.Context, service WorkerService, project core.Project) core.WorkerDetails {
	t.Helper()
	details, err := service.SpawnWorker(ctx, SpawnWorkerRequest{Intent: "inspect", ProjectID: project.ID, IdempotencyKey: "spawn"})
	if err != nil {
		t.Fatal(err)
	}
	if len(details.Turns) != 1 || len(details.Attempts) != 1 || details.Worker.NodeID != "node" || details.Worker.HarnessInstanceID != "node/fx" || details.ActionTurnID != details.Turns[0].ID {
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
	if err != nil || len(active.Turns) != 1 || runtime.count("steer") != 1 || active.ActionTurnID != first.TurnID {
		t.Fatalf("active=%#v err=%v calls=%d", active, err, runtime.count("steer"))
	}
	if _, err := store.SetPhase4AttemptNeedsInput(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	input, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "yes", RequestID: "ask-1"})
	if err != nil || len(input.Turns) != 1 || runtime.count("respond") != 1 || input.Worker.Status != core.WorkerWorking || input.ActionTurnID != first.TurnID {
		t.Fatalf("input=%#v err=%v", input, err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, first.ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "done"}); err != nil {
		t.Fatal(err)
	}
	idle, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "one more", IdempotencyKey: "follow-up"})
	if err != nil || len(idle.Turns) != 2 || len(idle.Attempts) != 2 || runtime.count("dispatch") != 2 || idle.ActionTurnID != idle.Worker.CurrentTurnID {
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
	if err != nil || interrupted.Worker.Status != core.WorkerQueued || runtime.count("resume") != 1 || interrupted.ActionTurnID != interrupted.Worker.CurrentTurnID {
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
	firstRuntime := &lifecycleRuntime{store: store}
	first := WorkerService{Store: store, PersonID: person.ID, Capability: capability, Runtime: firstRuntime, WorkerPolicy: core.HarnessPolicy{DefaultHarness: core.HarnessFX}, WorkerProfileSource: syntheticWorkerTemplateSource()}
	details := spawnLifecycleWorker(t, ctx, first, project)
	if _, err := store.SetPhase4AttemptActive(ctx, details.Attempts[0].ID); err != nil {
		t.Fatal(err)
	}
	other, err := core.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	secondRuntime := &lifecycleRuntime{store: other}
	second := WorkerService{Store: other, PersonID: person.ID, Capability: capability, Runtime: secondRuntime, WorkerPolicy: core.HarnessPolicy{DefaultHarness: core.HarnessFX}, WorkerProfileSource: syntheticWorkerTemplateSource()}
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

func TestWorkerServiceSpawnReplayRedeliversPendingInitialDispatchAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secretary.db")
	ctx, store, service, project := newWorkerServiceAt(t, path)
	conversation, err := service.authorize(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key := "crash-before-dispatch-claim"
	worker, turn, attempt, resolution, err := store.ResolveAndCreateWorker(ctx, conversation.ID, "inspect", core.DispatchResolutionRequest{ProjectID: project.ID, WorkerPolicy: core.HarnessPolicy{DefaultHarness: core.HarnessFX}}, key)
	if err != nil {
		t.Fatal(err)
	}
	intent, found, err := store.FindWorkerCommand(ctx, "dispatch", "attempt", worker.ID, attempt.ID)
	if err != nil || !found || intent.State != core.WorkerCommandPending || !intent.LeaseUntil.IsZero() || worker.Status != core.WorkerQueued {
		t.Fatalf("worker=%#v intent=%#v found=%v err=%v", worker, intent, found, err)
	}
	if resolution.Queued {
		t.Fatalf("new resolution=%#v", resolution)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := core.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	_, _, _, replayResolution, found, err := reopened.ReplayWorkerCreation(ctx, key)
	if err != nil || !found || !replayResolution.Queued {
		t.Fatalf("replay resolution=%#v found=%v err=%v", replayResolution, found, err)
	}
	runtime := &recordingLifecycleRuntime{}
	service.Store = reopened
	service.Runtime = runtime
	replayed, err := service.SpawnWorker(ctx, SpawnWorkerRequest{Intent: "inspect", ProjectID: project.ID, IdempotencyKey: key})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.Worker.ID != worker.ID || replayed.Turns[0].ID != turn.ID || replayed.Attempts[0].ID != attempt.ID {
		t.Fatalf("replayed=%#v want worker=%s turn=%s attempt=%s", replayed, worker.ID, turn.ID, attempt.ID)
	}
	if got := runtime.dispatchCommands(); len(got) != 1 || got[0] != intent.ID {
		t.Fatalf("dispatch command IDs=%v, want [%s]", got, intent.ID)
	}
}

func TestWorkerServiceQueuedResumeReplayUsesDurableTurnInput(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	if _, err := store.SetPhase4AttemptActive(ctx, details.Attempts[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.InterruptPhase4Attempt(ctx, details.Attempts[0].ID, "lost", "lost session"); err != nil {
		t.Fatal(err)
	}
	const durableInput = "the durable resume direction"
	turn, attempt, err := store.CreateTurn(ctx, details.Worker.ID, core.TurnSpec{Input: durableInput, IdempotencyKey: "resume-after-crash", CommandKind: "resume"})
	if err != nil {
		t.Fatal(err)
	}
	intent, found, err := store.FindWorkerCommand(ctx, "resume", "attempt", details.Worker.ID, attempt.ID)
	if err != nil || !found || intent.State != core.WorkerCommandPending || !intent.LeaseUntil.IsZero() {
		t.Fatalf("intent=%#v found=%v err=%v", intent, found, err)
	}
	runtime := &recordingLifecycleRuntime{}
	service.Runtime = runtime
	replayed, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: durableInput, IdempotencyKey: "resume-after-crash"})
	if err != nil {
		t.Fatal(err)
	}
	if replayed.CurrentAttempt() == nil || replayed.CurrentAttempt().ID != attempt.ID || replayed.Turns[len(replayed.Turns)-1].ID != turn.ID {
		t.Fatalf("replayed=%#v want turn=%s attempt=%s", replayed, turn.ID, attempt.ID)
	}
	ids, texts := runtime.resumeCommands()
	if len(ids) != 1 || ids[0] != intent.ID || len(texts) != 1 || texts[0] != durableInput {
		t.Fatalf("resume IDs=%v texts=%v, want ID=%s text=%q", ids, texts, intent.ID, durableInput)
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
	if err != nil || replayed.Worker.ID != first.Worker.ID || replayed.Turns[0].ID != first.Turns[0].ID || replayed.Attempts[0].ID != first.Attempts[0].ID || replayed.Worker.HarnessInstanceID != first.Worker.HarnessInstanceID || replayed.ActionTurnID != first.Turns[0].ID {
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

func TestWorkerServiceReplaysDeliveredNeedsInputResponseAndResumesWithoutRespond(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptNeedsInput(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	const requestID = "question-after-crash"
	const idempotencyKey = "answer-after-crash"
	command, duplicate, err := store.ClaimWorkerCommand(ctx, "respond", "key:"+idempotencyKey, details.Worker.ID, attempt.ID)
	if err != nil || duplicate {
		t.Fatalf("claimed=%#v duplicate=%v err=%v", command, duplicate, err)
	}
	runtime := service.Runtime.(*lifecycleRuntime)
	if err := runtime.Respond(ctx, command.ID, details.Worker, attempt, requestID, "answer"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkWorkerCommandDelivered(ctx, command.ID); err != nil {
		t.Fatal(err)
	}

	replayed, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "answer", RequestID: requestID, IdempotencyKey: idempotencyKey})
	if err != nil || replayed.Worker.Status != core.WorkerWorking || runtime.count("respond") != 1 {
		t.Fatalf("replayed=%#v err=%v responds=%d", replayed, err, runtime.count("respond"))
	}
}

func TestWorkerServiceConcurrentDeliveredNeedsInputReplayIsIdempotent(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptNeedsInput(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	const requestID = "question-concurrent-replay"
	const idempotencyKey = "answer-concurrent-replay"
	command, duplicate, err := store.ClaimWorkerCommand(ctx, "respond", "key:"+idempotencyKey, details.Worker.ID, attempt.ID)
	if err != nil || duplicate {
		t.Fatalf("claimed=%#v duplicate=%v err=%v", command, duplicate, err)
	}
	runtime := service.Runtime.(*lifecycleRuntime)
	if err := runtime.Respond(ctx, command.ID, details.Worker, attempt, requestID, "answer"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkWorkerCommandDelivered(ctx, command.ID); err != nil {
		t.Fatal(err)
	}

	var group sync.WaitGroup
	errs := make(chan error, 2)
	statuses := make(chan core.WorkerStatus, 2)
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			replayed, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "answer", RequestID: requestID, IdempotencyKey: idempotencyKey})
			if err == nil {
				statuses <- replayed.Worker.Status
			}
			errs <- err
		}()
	}
	group.Wait()
	close(errs)
	close(statuses)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for status := range statuses {
		if status != core.WorkerWorking {
			t.Fatalf("replayed worker status=%s", status)
		}
	}
	if runtime.count("respond") != 1 {
		t.Fatalf("responds=%d, want 1", runtime.count("respond"))
	}
}

func TestWorkerServiceDuplicateNeedsInputResponseByRequestIDIsNoOp(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptNeedsInput(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	request := MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "answer", RequestID: "question-1"}
	if _, err := service.MessageWorker(ctx, request); err != nil {
		t.Fatal(err)
	}
	duplicate, err := service.MessageWorker(ctx, request)
	runtime := service.Runtime.(*lifecycleRuntime)
	if err != nil || duplicate.Worker.Status != core.WorkerWorking || runtime.count("respond") != 1 || runtime.count("steer") != 0 || runtime.count("resume") != 0 {
		t.Fatalf("duplicate=%#v err=%v responds=%d steers=%d resumes=%d", duplicate, err, runtime.count("respond"), runtime.count("steer"), runtime.count("resume"))
	}
	if _, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "different request", RequestID: "question-2"}); err != nil {
		t.Fatal(err)
	}
	if runtime.count("respond") != 1 || runtime.count("steer") != 1 || runtime.count("resume") != 0 {
		t.Fatalf("different request responds=%d steers=%d resumes=%d", runtime.count("respond"), runtime.count("steer"), runtime.count("resume"))
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

func TestWorkerServiceReclaimsStalePendingRespondBeforeResumingAttempt(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptNeedsInput(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	command, duplicate, err := store.ClaimWorkerCommand(ctx, "respond", "request:question-1", details.Worker.ID, attempt.ID)
	if err != nil || duplicate {
		t.Fatalf("claimed=%#v duplicate=%v err=%v", command, duplicate, err)
	}
	runtime := &lifecycleRuntime{}
	service.Runtime = runtime
	service.commandNow = func() time.Time { return command.LeaseUntil }
	response, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "answer", RequestID: "question-1"})
	if err != nil || response.Worker.Status != core.WorkerWorking || runtime.count("respond") != 1 {
		t.Fatalf("response=%#v err=%v responds=%d", response, err, runtime.count("respond"))
	}
	recovered, found, err := store.FindWorkerCommand(ctx, "respond", "request:question-1", details.Worker.ID, attempt.ID)
	if err != nil || !found || recovered.ID != command.ID || recovered.State != core.WorkerCommandDelivered {
		t.Fatalf("recovered=%#v found=%v err=%v", recovered, found, err)
	}
}

func TestWorkerServicePendingRespondDoesNotResumeUntilDelivered(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptNeedsInput(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	runtime := &blockingLifecycleRuntime{lifecycleRuntime: lifecycleRuntime{store: store}, respondStarted: make(chan struct{}), respondRelease: make(chan struct{}), cancelStarted: make(chan struct{}), cancelRelease: make(chan struct{})}
	service.Runtime = runtime
	done := make(chan error, 1)
	go func() {
		_, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, Text: "answer", RequestID: "question-1"})
		done <- err
	}()
	<-runtime.respondStarted
	pending, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil || pending.Worker.Status != core.WorkerNeedsInput {
		t.Fatalf("pending=%#v err=%v", pending, err)
	}
	close(runtime.respondRelease)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	resumed, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil || resumed.Worker.Status != core.WorkerWorking {
		t.Fatalf("resumed=%#v err=%v", resumed, err)
	}
}

func TestWorkerServicePendingCancelDoesNotFinalizeUntilDelivered(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	runtime := &blockingLifecycleRuntime{lifecycleRuntime: lifecycleRuntime{store: store}, respondStarted: make(chan struct{}), respondRelease: make(chan struct{}), cancelStarted: make(chan struct{}), cancelRelease: make(chan struct{})}
	service.Runtime = runtime
	first := make(chan error, 1)
	go func() {
		_, err := service.CancelWorker(ctx, details.Worker.WorkerRef)
		first <- err
	}()
	<-runtime.cancelStarted
	pending, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil || pending.Attempts[0].State != core.AttemptActive || len(pending.Outcomes) != 0 || len(pending.Results) != 0 {
		t.Fatalf("pending=%#v err=%v", pending, err)
	}
	if _, err := service.CancelWorker(ctx, details.Worker.WorkerRef); !errors.Is(err, ErrWorkerCommandPending) {
		t.Fatalf("concurrent cancel error=%v", err)
	}
	close(runtime.cancelRelease)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	canceled, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil || !canceled.Attempts[0].State.Terminal() || len(canceled.Outcomes) != 1 || len(canceled.Results) != 1 || runtime.count("cancel") != 1 {
		t.Fatalf("canceled=%#v err=%v cancels=%d", canceled, err, runtime.count("cancel"))
	}
}

func TestWorkerServiceRejectsInvalidSelectionWithoutFXFallback(t *testing.T) {
	ctx, _, service, project := newWorkerService(t)
	if _, err := service.SpawnWorker(ctx, SpawnWorkerRequest{Intent: "inspect", ProjectID: project.ID, HarnessInstance: "node/fx", ModelID: "invalid"}); !errors.Is(err, core.ErrInvalidDispatchPin) {
		t.Fatalf("error=%v", err)
	}
}

type errorSteerLifecycleRuntime struct {
	lifecycleRuntime
	steerErr error
}

func (r *errorSteerLifecycleRuntime) Steer(context.Context, string, core.Worker, core.Phase4Attempt, string) error {
	r.add("steer")
	return r.steerErr
}

func TestWorkerServiceSteeringFailure_IdempotentReplayPreservesTypedFailure(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}

	runtime := &errorSteerLifecycleRuntime{
		steerErr: &node.NodeCommandError{
			Outcome: node.CommandOutcome{
				State:           node.CommandFailed,
				TurnID:          attempt.TurnID,
				AttemptID:       attempt.ID,
				ErrorCode:       "usage_limit_exceeded",
				ErrorMessage:    "Usage limit reached",
				FailureCategory: core.FailureCategoryUsageLimit,
				ProviderCode:    "usage_limit_exceeded",
				Technical:       "acp rpc -32603: Internal error",
			},
		},
	}
	service.Runtime = runtime

	request := MessageWorkerRequest{
		WorkerRef:      details.Worker.WorkerRef,
		Text:           "steer instructions",
		IdempotencyKey: "steer-err-key",
	}

	_, firstErr := service.MessageWorker(ctx, request)
	if firstErr == nil {
		t.Fatal("expected error on first steering call, got nil")
	}
	var firstCmdErr *node.NodeCommandError
	if !errors.As(firstErr, &firstCmdErr) {
		t.Fatalf("expected *node.NodeCommandError on first call, got: %T (%v)", firstErr, firstErr)
	}
	if firstCmdErr.Outcome.FailureCategory != core.FailureCategoryUsageLimit {
		t.Fatalf("first category = %q, want %q", firstCmdErr.Outcome.FailureCategory, core.FailureCategoryUsageLimit)
	}
	if firstCmdErr.Outcome.ProviderCode != "usage_limit_exceeded" {
		t.Fatalf("first provider_code = %q, want 'usage_limit_exceeded'", firstCmdErr.Outcome.ProviderCode)
	}

	_, replayErr := service.MessageWorker(ctx, request)
	if replayErr == nil {
		t.Fatal("expected error on replay call, got nil")
	}
	var replayCmdErr *node.NodeCommandError
	if !errors.As(replayErr, &replayCmdErr) {
		t.Fatalf("expected *node.NodeCommandError on replay call, got: %T (%v)", replayErr, replayErr)
	}
	if replayCmdErr.Outcome.FailureCategory != core.FailureCategoryUsageLimit {
		t.Fatalf("replay category = %q, want %q", replayCmdErr.Outcome.FailureCategory, core.FailureCategoryUsageLimit)
	}
	if replayCmdErr.Outcome.ProviderCode != "usage_limit_exceeded" {
		t.Fatalf("replay provider_code = %q, want 'usage_limit_exceeded'", replayCmdErr.Outcome.ProviderCode)
	}
	if replayCmdErr.Outcome.Technical != "acp rpc -32603: Internal error" {
		t.Fatalf("replay technical = %q", replayCmdErr.Outcome.Technical)
	}
	if replayCmdErr.Outcome.ErrorMessage != "Usage limit reached" {
		t.Fatalf("replay message = %q", replayCmdErr.Outcome.ErrorMessage)
	}

	attemptRecord, err := store.Phase4Attempt(ctx, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if attemptRecord.State != core.AttemptActive {
		t.Fatalf("attempt state = %q, want %q", attemptRecord.State, core.AttemptActive)
	}

	_, terminalResult, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{
		Status:         core.OutcomeSucceeded,
		Classification: core.OutcomeFinal,
		Summary:        "completed successfully after steering rejection",
	})
	if err != nil || terminalResult == nil {
		t.Fatalf("failed to record terminal outcome: result=%#v err=%v", terminalResult, err)
	}
	if terminalResult.Status != core.ResultSucceeded {
		t.Fatalf("terminal result status = %q, want %q", terminalResult.Status, core.ResultSucceeded)
	}

	idleDetails, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if idleDetails.Worker.Status != core.WorkerIdle {
		t.Fatalf("worker status = %q, want %q", idleDetails.Worker.Status, core.WorkerIdle)
	}

	_, idleReplayErr := service.MessageWorker(ctx, request)
	if idleReplayErr == nil {
		t.Fatal("expected error on idle replay call, got nil")
	}
	var idleCmdErr *node.NodeCommandError
	if !errors.As(idleReplayErr, &idleCmdErr) {
		t.Fatalf("expected *node.NodeCommandError on idle replay call, got: %T (%v)", idleReplayErr, idleReplayErr)
	}
	if idleCmdErr.Outcome.FailureCategory != core.FailureCategoryUsageLimit || idleCmdErr.Outcome.ProviderCode != "usage_limit_exceeded" {
		t.Fatalf("idle replay category=%q code=%q", idleCmdErr.Outcome.FailureCategory, idleCmdErr.Outcome.ProviderCode)
	}
	afterIdleReplay, err := service.GetWorker(ctx, details.Worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterIdleReplay.Turns) != 1 {
		t.Fatalf("turns count = %d, want 1 (idle replay must not create Turn)", len(afterIdleReplay.Turns))
	}
	if runtime.count("dispatch") != 0 {
		t.Fatalf("dispatch count = %d, want 0 (idle replay must not dispatch)", runtime.count("dispatch"))
	}

	turn2, err := service.MessageWorker(ctx, MessageWorkerRequest{
		WorkerRef:      details.Worker.WorkerRef,
		Text:           "start second turn",
		IdempotencyKey: "turn-2-key",
	})
	if err != nil {
		t.Fatalf("failed to start turn 2: %v", err)
	}
	if len(turn2.Attempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(turn2.Attempts))
	}
	attempt2 := turn2.Attempts[1]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt2.ID); err != nil {
		t.Fatal(err)
	}

	steerCountBefore := runtime.count("steer")
	_, activeReplayErr := service.MessageWorker(ctx, request)
	if activeReplayErr == nil {
		t.Fatal("expected error on active replay call, got nil")
	}
	var activeCmdErr *node.NodeCommandError
	if !errors.As(activeReplayErr, &activeCmdErr) {
		t.Fatalf("expected *node.NodeCommandError on active replay call, got: %T (%v)", activeReplayErr, activeReplayErr)
	}
	if activeCmdErr.Outcome.FailureCategory != core.FailureCategoryUsageLimit || activeCmdErr.Outcome.ProviderCode != "usage_limit_exceeded" {
		t.Fatalf("active replay category=%q code=%q", activeCmdErr.Outcome.FailureCategory, activeCmdErr.Outcome.ProviderCode)
	}
	if runtime.count("steer") != steerCountBefore {
		t.Fatalf("steer was executed again on new attempt (%d != %d)", runtime.count("steer"), steerCountBefore)
	}

	_, foundOnAttempt2, err := store.FindWorkerCommand(ctx, "steering", "key:steer-err-key", details.Worker.ID, attempt2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if foundOnAttempt2 {
		t.Fatal("steering command was erroneously created for new attempt 2")
	}
}

func TestWorkerServiceSteeringCancellationRemainsUncertain(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			ctx, store, service, project := newWorkerService(t)
			details := spawnLifecycleWorker(t, ctx, service, project)
			attempt := details.Attempts[0]
			if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
				t.Fatal(err)
			}
			command, _, err := store.ClaimWorkerCommand(ctx, "steering", "key:cancelled-steer", details.Worker.ID, attempt.ID)
			if err != nil {
				t.Fatal(err)
			}
			local := node.NewLocal(canceledSteeringRuntime{err: cause})
			if _, err := local.Dispatch(ctx, node.StartRequest{WorkerRef: details.Worker.WorkerRef}); err != nil {
				t.Fatal(err)
			}
			runtime := NodeRuntime{Local: local}
			steering := node.Command{Kind: node.CommandSteering, Steering: &node.SteeringCommand{
				Metadata: core.CommandMetadata{
					CommandID: command.ID, Node: "local", HarnessInstanceID: "local/fx",
					WorkerRef: details.Worker.WorkerRef, TurnID: attempt.TurnID, AttemptID: attempt.ID,
				},
				Text: "steer",
			}}
			err = service.deliverCommand(ctx, command, func(string) error {
				_, sendErr := runtime.LocalCommand(ctx, steering)
				return sendErr
			})
			if !errors.Is(err, node.ErrCommandOutcomeUnknown) || !errors.Is(err, cause) {
				t.Fatalf("steering cancellation lost uncertainty/cause: %v", err)
			}
			command, found, err := store.FindWorkerCommand(ctx, "steering", "key:cancelled-steer", details.Worker.ID, attempt.ID)
			if err != nil || !found || command.State != core.WorkerCommandUncertain {
				t.Fatalf("command state=%#v found=%v err=%v, want uncertain", command, found, err)
			}
			if _, err := store.WorkerCommandReceiptOutcome(ctx, command.ID); !errors.Is(err, core.ErrNotFound) {
				t.Fatalf("local cancellation was persisted as an authoritative Node refusal: %v", err)
			}
		})
	}
}

func TestWorkerServiceDoesNotHideSteeringReceiptStorageFailure(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	ctx, store, service, project := newWorkerServiceAt(t, path)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	command, _, err := store.ClaimWorkerCommand(ctx, "steering", "key:store-error", details.Worker.ID, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	var handoffs int
	outcomeErr := &node.NodeCommandError{Outcome: node.CommandOutcome{
		CommandID: command.ID, Kind: node.CommandSteering, State: node.CommandFailed,
		TurnID: attempt.TurnID, AttemptID: attempt.ID, ErrorCode: "steering_failed",
		ErrorMessage: "provider refusal", FailureCategory: core.FailureCategoryInternalError,
	}}
	err = service.deliverCommand(ctx, command, func(string) error {
		handoffs++
		if err := store.Close(); err != nil {
			t.Fatalf("close Core store to inject receipt failure: %v", err)
		}
		return outcomeErr
	})
	if err == nil || !errors.Is(err, node.ErrCommandOutcomeUnknown) {
		t.Fatalf("receipt storage failure was hidden as a definitive refusal: %v", err)
	}
	if handoffs != 1 {
		t.Fatalf("handoffs=%d, want one", handoffs)
	}
	reopened, err := core.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	service.Store = reopened
	current, found, err := reopened.FindWorkerCommand(ctx, "steering", "key:store-error", details.Worker.ID, attempt.ID)
	if err != nil || !found || current.State != core.WorkerCommandUncertain {
		t.Fatalf("storage failure did not leave a durable uncertain command: command=%#v found=%v err=%v", current, found, err)
	}
	if _, err := reopened.WorkerCommandReceiptOutcome(ctx, command.ID); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("failed storage write created an authoritative refusal: %v", err)
	}
	_, send, err := service.claimCommand(ctx, "steering", "key:store-error", details.Worker, attempt, nil)
	if !errors.Is(err, node.ErrCommandOutcomeUnknown) || send {
		t.Fatalf("same command was eligible for repeat delivery: send=%v err=%v", send, err)
	}
	if handoffs != 1 {
		t.Fatalf("storage failure caused a repeated side effect: handoffs=%d", handoffs)
	}
}

func TestWorkerServiceSteeringFailure_AuthenticatedOriginBinding(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}

	conversation, err := service.authorize(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, service.PersonID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{Version: "test-v1", Harness: "fx", Model: "secretary", Reasoning: "high", ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "synthetic"}); err != nil {
		t.Fatal(err)
	}
	origin, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "steer origin")
	if err != nil {
		t.Fatal(err)
	}
	origin, err = store.StartSecretaryTurn(ctx, origin.ID)
	if err != nil {
		t.Fatal(err)
	}

	runtime := &errorSteerLifecycleRuntime{
		steerErr: &node.NodeCommandError{
			Outcome: node.CommandOutcome{
				State:           node.CommandFailed,
				TurnID:          attempt.TurnID,
				AttemptID:       attempt.ID,
				ErrorCode:       "usage_limit_exceeded",
				ErrorMessage:    "Usage limit reached",
				FailureCategory: core.FailureCategoryUsageLimit,
				ProviderCode:    "usage_limit_exceeded",
			},
		},
	}
	service.Runtime = runtime

	request := MessageWorkerRequest{
		WorkerRef:       details.Worker.WorkerRef,
		Text:            "steer with origin",
		IdempotencyKey:  "steer-origin-key",
		SecretaryTurnID: origin.ID,
		InputID:         origin.InputID,
	}

	_, err = service.MessageWorker(ctx, request)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	_, err = service.MessageWorker(ctx, request)
	if err == nil {
		t.Fatal("expected error on replay, got nil")
	}
	var cmdErr *node.NodeCommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("expected *node.NodeCommandError, got: %T (%v)", err, err)
	}

	forgedRequest := request
	forgedRequest.SecretaryTurnID = "foreign-turn"
	_, err = service.MessageWorker(ctx, forgedRequest)
	if !errors.Is(err, core.ErrInvalidSecretaryOrigin) {
		t.Fatalf("expected ErrInvalidSecretaryOrigin for mismatched origin, got: %v", err)
	}

	unauthenticatedRequest := request
	unauthenticatedRequest.SecretaryTurnID = ""
	unauthenticatedRequest.InputID = ""
	_, err = service.MessageWorker(ctx, unauthenticatedRequest)
	if !errors.Is(err, core.ErrInvalidSecretaryOrigin) {
		t.Fatalf("expected ErrInvalidSecretaryOrigin for unauthenticated replay, got: %v", err)
	}

	if _, _, err := store.FinishSecretaryTurnWithOutput(ctx, origin.ID, origin.InputID, core.SecretaryTurnSucceeded, "", "Turn finished", nil); err != nil {
		t.Fatal(err)
	}

	_, err = service.MessageWorker(ctx, request)
	if err == nil {
		t.Fatal("expected error on replay after turn completed, got nil")
	}
	var completedCmdErr *node.NodeCommandError
	if !errors.As(err, &completedCmdErr) {
		t.Fatalf("expected *node.NodeCommandError after completed turn, got: %T (%v)", err, err)
	}

	_, err = service.MessageWorker(ctx, forgedRequest)
	if !errors.Is(err, core.ErrInvalidSecretaryOrigin) {
		t.Fatalf("expected ErrInvalidSecretaryOrigin for forged request after turn completed, got: %v", err)
	}
}

func TestWorkerServiceUncertainSteeringReplayValidatesOrigin(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	conversation, err := service.authorize(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, service.PersonID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{Version: "test-v1", Harness: "fx", Model: "secretary", Reasoning: "high", ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "synthetic"}); err != nil {
		t.Fatal(err)
	}
	origin, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "uncertain steering origin")
	if err != nil {
		t.Fatal(err)
	}
	origin, err = store.StartSecretaryTurn(ctx, origin.ID)
	if err != nil {
		t.Fatal(err)
	}
	runtime := &errorSteerLifecycleRuntime{steerErr: &node.NodeCommandError{Outcome: node.CommandOutcome{
		State: node.CommandFailed, TurnID: attempt.TurnID, AttemptID: attempt.ID,
		ErrorCode: "execution_state_unknown", ErrorMessage: "steering acceptance is unknown", AcceptanceUnknown: true,
	}}}
	service.Runtime = runtime
	request := MessageWorkerRequest{
		WorkerRef: details.Worker.WorkerRef, Text: "continue", IdempotencyKey: "uncertain-steer-origin",
		SecretaryTurnID: origin.ID, InputID: origin.InputID,
	}
	if _, err := service.MessageWorker(ctx, request); !errors.Is(err, node.ErrCommandOutcomeUnknown) {
		t.Fatalf("first uncertain handoff err=%v", err)
	}
	if runtime.count("steer") != 1 {
		t.Fatalf("initial native handoffs=%d, want 1", runtime.count("steer"))
	}
	for name, forged := range map[string]MessageWorkerRequest{
		"missing origin": {WorkerRef: request.WorkerRef, Text: request.Text, IdempotencyKey: request.IdempotencyKey},
		"foreign turn":   {WorkerRef: request.WorkerRef, Text: request.Text, IdempotencyKey: request.IdempotencyKey, SecretaryTurnID: "foreign-turn", InputID: origin.InputID},
		"foreign input":  {WorkerRef: request.WorkerRef, Text: request.Text, IdempotencyKey: request.IdempotencyKey, SecretaryTurnID: origin.ID, InputID: "foreign-input"},
	} {
		if _, err := service.MessageWorker(ctx, forged); !errors.Is(err, core.ErrInvalidSecretaryOrigin) {
			t.Errorf("%s replay err=%v, want invalid authenticated origin", name, err)
		}
	}
	if _, err := service.MessageWorker(ctx, request); !errors.Is(err, node.ErrCommandOutcomeUnknown) {
		t.Fatalf("identical uncertain replay err=%v", err)
	}
	if runtime.count("steer") != 1 {
		t.Fatalf("uncertain replay repeated native steering: %d calls", runtime.count("steer"))
	}
	if _, _, err := store.FinishSecretaryTurnWithOutput(ctx, origin.ID, origin.InputID, core.SecretaryTurnSucceeded, "", "finished", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.MessageWorker(ctx, request); !errors.Is(err, node.ErrCommandOutcomeUnknown) {
		t.Fatalf("historical exact-origin replay err=%v", err)
	}
	if runtime.count("steer") != 1 {
		t.Fatalf("historical replay repeated native steering: %d calls", runtime.count("steer"))
	}
}

func TestWorkerServiceSteering_LegacyReplayAndPendingOriginBinding(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	details := spawnLifecycleWorker(t, ctx, service, project)
	attempt := details.Attempts[0]
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}

	identity, err := store.GetSecretaryIdentity(ctx, service.PersonID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{Version: "test-v1", Harness: "fx", Model: "secretary", Reasoning: "high", ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "synthetic"}); err != nil {
		t.Fatal(err)
	}
	origin, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "valid origin")
	if err != nil {
		t.Fatal(err)
	}
	origin, err = store.StartSecretaryTurn(ctx, origin.ID)
	if err != nil {
		t.Fatal(err)
	}

	runtime := &errorSteerLifecycleRuntime{
		steerErr: &node.NodeCommandError{
			Outcome: node.CommandOutcome{
				State:           node.CommandFailed,
				TurnID:          attempt.TurnID,
				AttemptID:       attempt.ID,
				ErrorCode:       "usage_limit_exceeded",
				ErrorMessage:    "Usage limit reached",
				FailureCategory: core.FailureCategoryUsageLimit,
				ProviderCode:    "usage_limit_exceeded",
			},
		},
	}
	service.Runtime = runtime

	legacyRequest := MessageWorkerRequest{
		WorkerRef:      details.Worker.WorkerRef,
		Text:           "legacy steer without origin",
		IdempotencyKey: "legacy-steer-key",
	}
	_, err = service.MessageWorker(ctx, legacyRequest)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var cmdErr *node.NodeCommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("expected *node.NodeCommandError, got: %T (%v)", err, err)
	}

	_, err = service.MessageWorker(ctx, legacyRequest)
	if err == nil {
		t.Fatal("expected error on legacy replay, got nil")
	}
	if !errors.As(err, &cmdErr) {
		t.Fatalf("expected *node.NodeCommandError on legacy replay, got: %T (%v)", err, err)
	}

	fabricatedRequest := legacyRequest
	fabricatedRequest.SecretaryTurnID = "fabricated-turn"
	fabricatedRequest.InputID = "fabricated-input"
	_, err = service.MessageWorker(ctx, fabricatedRequest)
	if !errors.Is(err, core.ErrInvalidSecretaryOrigin) {
		t.Fatalf("expected ErrInvalidSecretaryOrigin for fabricated origin on legacy replay, got: %v", err)
	}

	validOriginRequest := legacyRequest
	validOriginRequest.SecretaryTurnID = origin.ID
	validOriginRequest.InputID = origin.InputID
	_, err = service.MessageWorker(ctx, validOriginRequest)
	if !errors.Is(err, core.ErrInvalidSecretaryOrigin) {
		t.Fatalf("expected ErrInvalidSecretaryOrigin for rebinding legacy command, got: %v", err)
	}

	pendingCmd, _, err := store.ClaimWorkerCommand(ctx, "steering", "pending-dedupe-key", details.Worker.ID, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ValidateWorkerCommandOrigin(ctx, pendingCmd.ID, &core.SecretaryOriginIdentity{
		PersonID:        service.PersonID,
		Capability:      "invalid-cap",
		SecretaryTurnID: "fabricated-turn",
		InputID:         "fabricated-input",
	}, true); !errors.Is(err, core.ErrInvalidSecretaryOrigin) {
		t.Fatalf("expected ErrInvalidSecretaryOrigin for fabricated origin on pending command, got: %v", err)
	}

	if err := store.ValidateWorkerCommandOrigin(ctx, pendingCmd.ID, &core.SecretaryOriginIdentity{
		PersonID:        service.PersonID,
		Capability:      service.Capability,
		SecretaryTurnID: origin.ID,
		InputID:         origin.InputID,
	}, true); !errors.Is(err, core.ErrInvalidSecretaryOrigin) {
		t.Fatalf("expected ErrInvalidSecretaryOrigin for rebinding pending command, got: %v", err)
	}
}

func TestMessageWorkerIdleFollowupsRespectNodeCapacity(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	record, err := store.NodeRecord(ctx, "node")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNodeHeartbeat(ctx, "node", record.Inventory, core.NodeHeartbeat{Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	refs := make([]string, 2)
	for i := range refs {
		details, err := service.SpawnWorker(ctx, SpawnWorkerRequest{
			Intent: "initial work", ProjectID: project.ID, IdempotencyKey: "initial-" + string(rune('a'+i)),
		})
		if err != nil {
			t.Fatal(err)
		}
		current := details.CurrentAttempt()
		if current == nil {
			t.Fatal("missing initial Attempt")
		}
		if _, _, _, err := store.RecordAttemptOutcome(ctx, current.ID, core.AttemptOutcomeInput{
			Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "idle",
		}); err != nil {
			t.Fatal(err)
		}
		refs[i] = details.Worker.WorkerRef
	}
	// Both idle Workers race to start a new command on one available slot.
	start := make(chan struct{})
	type result struct {
		ref string
		details core.WorkerDetails
		err error
	}
	completed := make(chan result, 2)
	for _, ref := range refs {
		go func(ref string) {
			<-start
			details, err := service.MessageWorker(ctx, MessageWorkerRequest{
				WorkerRef: ref, Text: "follow up", IdempotencyKey: "parallel-followup",
			})
			completed <- result{ref: ref, details: details, err: err}
		}(ref)
	}
	close(start)
	var accepted, denied int
	for i := 0; i < 2; i++ {
		got := <-completed
		if got.err == nil {
			accepted++
			if got.details.ActionTurnID == "" {
				t.Fatalf("accepted Follow-up missing Turn: %#v", got.details)
			}
		} else if errors.Is(got.err, core.ErrSelectedNodeUnavailable) {
			denied++
			remaining, err := service.GetWorker(ctx, got.ref)
			if err != nil || remaining.Worker.Status != core.WorkerIdle || len(remaining.Attempts) != 1 {
				t.Fatalf("denied Follow-up created an Attempt: %#v err=%v", remaining, err)
			}
		} else {
			t.Fatalf("unexpected Follow-up outcome: %v", got.err)
		}
	}
	if accepted != 1 || denied != 1 {
		t.Fatalf("capacity=1 accepted=%d rejected=%d", accepted, denied)
	}
}

func TestQueuedFollowupsWaitForCapacityInsteadOfOverbooking(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	record, err := store.NodeRecord(ctx, "node")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNodeHeartbeat(ctx, "node", record.Inventory, core.NodeHeartbeat{Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	refs := make([]string, 2)
	for i := range refs {
		created, err := service.SpawnWorker(ctx, SpawnWorkerRequest{
			Intent: "initial", ProjectID: project.ID, IdempotencyKey: "queue-initial-" + string(rune('a'+i)),
		})
		if err != nil {
			t.Fatal(err)
		}
		attempt := created.CurrentAttempt()
		if attempt == nil {
			t.Fatal("missing initial Attempt")
		}
		if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{
			Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "idle",
		}); err != nil {
			t.Fatal(err)
		}
		refs[i] = created.Worker.WorkerRef
		if _, err := service.MessageWorker(ctx, MessageWorkerRequest{
			WorkerRef: refs[i], Text: "/q new task", IdempotencyKey: "queue-followup",
		}); err != nil {
			t.Fatal(err)
		}
	}
	runtime := service.Runtime.(*lifecycleRuntime)
	before := runtime.count("dispatch")
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatalf("capacity exhaustion must backpressure, not fail or block queue: %v", err)
	}
	if runtime.count("dispatch") != before+1 {
		t.Fatalf("capacity=1 allowed %d queued Follow-up dispatches", runtime.count("dispatch")-before)
	}
	var dispatched, waiting int
	var activeID string
	for _, ref := range refs {
		details, err := service.GetWorker(ctx, ref)
		if err != nil {
			t.Fatal(err)
		}
		if len(details.QueuedMessages) != 1 {
			t.Fatalf("missing queued message: %#v", details)
		}
		if details.QueuedMessages[0].State == "delivered" {
			dispatched++
			activeID = details.CurrentAttempt().ID
		} else if details.QueuedMessages[0].State == "delivering" {
			waiting++
		} else {
			t.Fatalf("capacity backpressure changed queue state: %#v", details.QueuedMessages)
		}
	}
	if dispatched != 1 || waiting != 1 {
		t.Fatalf("queued dispatch overbooked: delivered=%d waiting=%d", dispatched, waiting)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, activeID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "finished",
	}); err != nil {
		t.Fatal(err)
	}
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatal(err)
	}
	if runtime.count("dispatch") != before+2 {
		t.Fatalf("released capacity did not allow second queued Follow-up: dispatched=%d", runtime.count("dispatch")-before)
	}
}

func TestSpawnWorkerIdempotentReplayAfterTerminalAttempt(t *testing.T) {
	ctx, store, service, project := newWorkerService(t)
	record, err := store.NodeRecord(ctx, "node")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNodeHeartbeat(ctx, "node", record.Inventory, core.NodeHeartbeat{Capacity: 1}); err != nil {
		t.Fatal(err)
	}

	request := SpawnWorkerRequest{Intent: "complete once", ProjectID: project.ID, IdempotencyKey: "terminal-spawn-replay"}
	initial, err := service.SpawnWorker(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	initialAttempt := initial.CurrentAttempt()
	if initialAttempt == nil {
		t.Fatal("initial Spawn did not create an Attempt")
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, initialAttempt.ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "finished",
	}); err != nil {
		t.Fatal(err)
	}

	// Occupy the only Node slot with a different Worker. Replaying the
	// completed Spawn must not require capacity or dispatch again.
	other, err := service.SpawnWorker(ctx, SpawnWorkerRequest{
		Intent: "occupy node", ProjectID: project.ID, IdempotencyKey: "other-active-spawn",
	})
	if err != nil {
		t.Fatal(err)
	}
	if other.Worker.ID == initial.Worker.ID {
		t.Fatal("second Spawn unexpectedly reused original Worker")
	}
	runtime := service.Runtime.(*lifecycleRuntime)
	before := runtime.count("dispatch")
	replayed, err := service.SpawnWorker(ctx, request)
	if err != nil {
		t.Fatalf("completed idempotent Spawn should replay, got %v", err)
	}
	if replayed.Worker.ID != initial.Worker.ID ||
		replayed.Worker.WorkerRef != initial.Worker.WorkerRef ||
		replayed.ActionTurnID != initial.ActionTurnID ||
		len(replayed.Turns) != 1 || len(replayed.Attempts) != 1 ||
		len(replayed.Results) != 1 {
		t.Fatalf("completed Spawn returned changed Worker or lost result: %#v", replayed)
	}
	if replayed.CurrentAttempt().ID != initialAttempt.ID || !replayed.CurrentAttempt().State.Terminal() {
		t.Fatalf("replayed Attempt is not the terminal original: %#v", replayed.CurrentAttempt())
	}
	if runtime.count("dispatch") != before {
		t.Fatalf("completed Spawn was dispatched again: before=%d after=%d", before, runtime.count("dispatch"))
	}
}
