package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

type testUsageLimitRuntime struct {
	lifecycleRuntime
	mu          sync.Mutex
	ready       bool
	resumeErr   error
	resumeCalls int
	resumeTexts []string
}

func (r *testUsageLimitRuntime) CommandReady(context.Context, core.NodeReference) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ready, nil
}

func (r *testUsageLimitRuntime) Resume(ctx context.Context, commandID string, worker core.Worker, turn core.Turn, attempt core.Phase4Attempt, prompt string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resumeCalls++
	r.resumeTexts = append(r.resumeTexts, prompt)
	if r.resumeErr != nil {
		return r.resumeErr
	}
	return nil
}

func setupLimitedWorker(t *testing.T, resetOffset time.Duration) (*core.Store, WorkerService, *testUsageLimitRuntime, core.Worker, core.Conversation, core.Turn, core.Phase4Attempt) {
	t.Helper()
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "ctl-limit.db"))
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

	projectDir := t.TempDir()
	project, err := store.CreateProject(ctx, core.ProjectSpec{
		ID:       "proj",
		Name:     "Project",
		Mappings: []core.ProjectPathMapping{{Node: "local", Path: projectDir}},
	})
	if err != nil {
		t.Fatal(err)
	}
	instance := core.HarnessInstance{ID: "local/codex", Node: "local", Kind: core.HarnessCodex, Version: "1", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}}
	if _, err := store.EnrollNode(ctx, "local"); err != nil && !errors.Is(err, core.ErrNodeAlreadyEnrolled) {
		t.Fatal(err)
	}
	if err := store.UpdateNodeHeartbeat(ctx, "local", core.HarnessInventorySnapshot{
		Node: "local", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{instance},
	}, core.NodeHeartbeat{Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	snapshot := project.Snapshot("local", projectDir, instance)
	snapshotBytes, _ := json.Marshal(snapshot)

	runtime := &testUsageLimitRuntime{ready: true}
	service := WorkerService{
		Store:      store,
		Runtime:    runtime,
		PersonID:   person.ID,
		Capability: capability,
	}

	worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		Intent:            "work",
		ProjectID:         "proj",
		NodeID:            "local",
		HarnessInstanceID: "local/codex",
		ProjectSnapshot:   string(snapshotBytes),
	}, core.TurnSpec{Input: "do work"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}

	resetTime := time.Now().UTC().Add(resetOffset)
	if _, _, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{
		Status:          core.OutcomeFailed,
		Classification:  core.OutcomeRetryable,
		FailureCategory: core.FailureCategoryUsageLimit,
		FailureCode:     "usage_limit_exceeded",
		Summary:         "usage limit reached",
		ResetsAt:        &resetTime,
	}); err != nil {
		t.Fatal(err)
	}

	return store, service, runtime, worker, conversation, turn, attempt
}

func TestScheduledResume_RunsDueScheduleBeforeFIFO(t *testing.T) {
	ctx := context.Background()
	store, service, runtime, worker, conversation, _, _ := setupLimitedWorker(t, 10*time.Second)
	service.commandNow = func() time.Time { return time.Now().UTC().Add(2 * time.Minute) }
	defer store.Close()

	_, err := service.MessageWorker(ctx, MessageWorkerRequest{
		WorkerRef:      worker.WorkerRef,
		Text:           "/q next task",
		IdempotencyKey: "q-1",
	})
	if err != nil {
		t.Fatalf("enqueue /q failed: %v", err)
	}

	details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(details.QueuedMessages) != 1 || details.QueuedMessages[0].State != "pending" {
		t.Fatalf("expected pending queued message, got %#v", details.QueuedMessages)
	}

	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatalf("process queued worker messages: %v", err)
	}

	runtime.mu.Lock()
	calls := runtime.resumeCalls
	var text string
	if len(runtime.resumeTexts) > 0 {
		text = runtime.resumeTexts[0]
	}
	runtime.mu.Unlock()

	if calls != 1 {
		t.Fatalf("expected 1 resume call, got %d", calls)
	}
	if text != core.ContinuationPrompt {
		t.Fatalf("expected continuation prompt %q, got %q", core.ContinuationPrompt, text)
	}

	detailsAfter, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(detailsAfter.QueuedMessages) != 1 || detailsAfter.QueuedMessages[0].State != "pending" {
		t.Fatalf("queued message should remain pending while turn is running, got %s", detailsAfter.QueuedMessages[0].State)
	}
}

func TestScheduledResume_OfflineLeavesPendingUntouched(t *testing.T) {
	ctx := context.Background()
	store, service, runtime, worker, _, _, _ := setupLimitedWorker(t, 10*time.Second)
	service.commandNow = func() time.Time { return time.Now().UTC().Add(2 * time.Minute) }
	defer store.Close()

	runtime.mu.Lock()
	runtime.ready = false
	runtime.mu.Unlock()

	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatal(err)
	}

	runtime.mu.Lock()
	calls := runtime.resumeCalls
	runtime.mu.Unlock()
	if calls != 0 {
		t.Fatalf("expected 0 resume calls on offline node, got %d", calls)
	}

	sched, found, err := store.FindActiveScheduleForWorker(ctx, worker.ID)
	if err != nil || !found {
		t.Fatalf("expected schedule, err=%v, found=%v", err, found)
	}
	if sched.State != core.SchedulePending {
		t.Fatalf("expected schedule still pending, got %s", sched.State)
	}
}

func TestManualResume_OwnerAPIAndTelegram(t *testing.T) {
	ctx := context.Background()
	store, service, runtime, worker, _, _, _ := setupLimitedWorker(t, 10*time.Minute)
	defer store.Close()

	updated, err := service.ResumeWorker(ctx, worker.WorkerRef)
	if err != nil {
		t.Fatalf("ResumeWorker failed: %v", err)
	}

	runtime.mu.Lock()
	calls := runtime.resumeCalls
	var text string
	if len(runtime.resumeTexts) > 0 {
		text = runtime.resumeTexts[0]
	}
	runtime.mu.Unlock()

	if calls != 1 {
		t.Fatalf("expected 1 resume call, got %d", calls)
	}
	if text != core.ContinuationPrompt {
		t.Fatalf("expected continuation prompt %q, got %q", core.ContinuationPrompt, text)
	}

	_, err2 := service.ResumeWorker(ctx, worker.WorkerRef)
	if err2 != nil {
		t.Fatalf("idempotent ResumeWorker failed: %v", err2)
	}
	_ = updated
}

func TestLimitedWorker_RaceManualAndAutomatic(t *testing.T) {
	ctx := context.Background()
	store, service, runtime, worker, _, _, _ := setupLimitedWorker(t, -1*time.Minute)
	defer store.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = service.ResumeWorker(ctx, worker.WorkerRef)
	}()

	go func() {
		defer wg.Done()
		_ = service.ProcessQueuedWorkerMessages(ctx)
	}()

	wg.Wait()

	runtime.mu.Lock()
	calls := runtime.resumeCalls
	runtime.mu.Unlock()

	if calls != 1 {
		t.Fatalf("expected exactly 1 resume execution across race, got %d", calls)
	}
}

func TestLimitedWorker_NewDirectInputCancelsSchedule(t *testing.T) {
	ctx := context.Background()
	store, service, _, worker, _, _, _ := setupLimitedWorker(t, 10*time.Minute)
	defer store.Close()

	details, err := service.MessageWorker(ctx, MessageWorkerRequest{
		WorkerRef:      worker.WorkerRef,
		Text:           "start completely new task",
		IdempotencyKey: "new-input-1",
	})
	if err != nil {
		t.Fatalf("MessageWorker failed: %v", err)
	}

	_, found, err := store.FindActiveScheduleForWorker(ctx, worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("expected active schedule to be canceled and not found")
	}

	if len(details.Turns) < 2 {
		t.Fatalf("expected at least 2 turns (canceled turn + new turn), got %d", len(details.Turns))
	}
	firstTurn := details.Turns[0]
	if firstTurn.State != core.TurnCanceled {
		t.Fatalf("expected first turn canceled, got %s", firstTurn.State)
	}
	if firstTurn.ResultID == "" {
		t.Fatal("expected first turn to have canceled ResultID")
	}
	res, resErr := store.Phase4Result(ctx, firstTurn.ID)
	if resErr != nil {
		t.Fatalf("load turn result: %v", resErr)
	}
	if res.Status != core.ResultCanceled {
		t.Fatalf("expected canceled result status, got %s", res.Status)
	}
	newTurn := details.Turns[1]
	if newTurn.Input != "start completely new task" {
		t.Fatalf("expected new turn input %q, got %q", "start completely new task", newTurn.Input)
	}
}

func TestLimitedWorker_CancelWorker_AtomicAndIdempotent(t *testing.T) {
	ctx := context.Background()
	store, service, _, worker, _, _, _ := setupLimitedWorker(t, 10*time.Minute)
	defer store.Close()

	canceled, err := service.CancelWorker(ctx, worker.WorkerRef)
	if err != nil {
		t.Fatalf("CancelWorker failed: %v", err)
	}

	if canceled.Worker.Status != core.WorkerIdle {
		t.Fatalf("expected worker status idle, got %s", canceled.Worker.Status)
	}
	if len(canceled.Turns) == 0 {
		t.Fatal("expected turns to be present")
	}
	turn := canceled.Turns[0]
	if turn.State != core.TurnCanceled {
		t.Fatalf("expected turn canceled, got %s", turn.State)
	}
	if turn.ResultID == "" {
		t.Fatal("expected turn to have result_id populated")
	}
	res, resErr := store.Phase4Result(ctx, turn.ID)
	if resErr != nil {
		t.Fatalf("load result: %v", resErr)
	}
	if res.Status != core.ResultCanceled {
		t.Fatalf("expected result status canceled, got %s", res.Status)
	}

	_, found, err := store.FindActiveScheduleForWorker(ctx, worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("expected no active schedule after cancellation")
	}

	canceled2, err2 := service.CancelWorker(ctx, worker.WorkerRef)
	if err2 != nil {
		t.Fatalf("second CancelWorker failed: %v", err2)
	}
	if canceled2.Worker.Status != core.WorkerIdle {
		t.Fatalf("expected worker idle on duplicate cancel, got %s", canceled2.Worker.Status)
	}
}

func TestLimitedWorker_ResumeWorkerOffline_NoClaim(t *testing.T) {
	ctx := context.Background()
	store, service, runtime, worker, _, _, _ := setupLimitedWorker(t, 10*time.Minute)
	defer store.Close()

	runtime.mu.Lock()
	runtime.ready = false
	runtime.mu.Unlock()

	_, err := service.ResumeWorker(ctx, worker.WorkerRef)
	if err == nil {
		t.Fatal("expected error when resuming offline worker")
	}

	sched, found, schedErr := store.FindActiveScheduleForWorker(ctx, worker.ID)
	if schedErr != nil || !found {
		t.Fatalf("expected schedule to remain, err=%v, found=%v", schedErr, found)
	}
	if sched.State != core.SchedulePending {
		t.Fatalf("expected schedule to remain pending, got %s", sched.State)
	}
}

func TestScheduledResume_DeliverCommandFailureReleasesSchedule(t *testing.T) {
	ctx := context.Background()
	store, service, runtime, worker, _, _, _ := setupLimitedWorker(t, 10*time.Second)
	service.commandNow = func() time.Time { return time.Now().UTC().Add(2 * time.Minute) }
	defer store.Close()

	runtime.mu.Lock()
	runtime.resumeErr = errors.New("simulated runtime connection failure")
	runtime.mu.Unlock()

	_ = service.ProcessQueuedWorkerMessages(ctx)

	sched, found, schedErr := store.FindActiveScheduleForWorker(ctx, worker.ID)
	if schedErr != nil || !found {
		t.Fatalf("expected schedule, err=%v, found=%v", schedErr, found)
	}
	if sched.State != core.SchedulePending {
		t.Fatalf("expected schedule released to pending, got %s", sched.State)
	}
}

func TestManualResumeDoesNotOvercommitNodeCapacity(t *testing.T) {
	ctx := context.Background()
	store, service, runtime, limited, conversation, _, _ := setupLimitedWorker(t, 10*time.Minute)
	defer store.Close()

	schedule, found, err := store.FindActiveScheduleForWorker(ctx, limited.ID)
	if err != nil || !found {
		t.Fatalf("missing limited Worker schedule: found=%v err=%v", found, err)
	}
	persisted, err := store.Worker(ctx, limited.ID)
	if err != nil {
		t.Fatal(err)
	}
	busy, _, busyAttempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		Intent: "occupy single slot", ProjectID: persisted.ProjectID,
		NodeID: persisted.NodeID, HarnessInstanceID: persisted.HarnessInstanceID,
		ProjectSnapshot: persisted.ProjectSnapshot, ReserveNodeCapacity: true,
	}, core.TurnSpec{Input: "keep node occupied"})
	if err != nil {
		t.Fatal(err)
	}
	if busy.ID == limited.ID {
		t.Fatal("busy Worker accidentally reused limited Worker")
	}

	_, err = service.ResumeWorker(ctx, limited.WorkerRef)
	if !errors.Is(err, core.ErrNodeCapacityExhausted) {
		t.Fatalf("manual resume should reject a full Node: %v", err)
	}
	runtime.mu.Lock()
	resumeCalls := runtime.resumeCalls
	runtime.mu.Unlock()
	if resumeCalls != 0 {
		t.Fatalf("manual Resume dispatched on an occupied Node: %d calls", resumeCalls)
	}
	after, found, err := store.FindActiveScheduleForWorker(ctx, limited.ID)
	if err != nil || !found || after.ID != schedule.ID || after.State != schedule.State {
		t.Fatalf("capacity error lost scheduled retry: %#v found=%v err=%v", after, found, err)
	}
	command, found, err := store.FindWorkerCommand(ctx, "resume", "attempt", limited.ID, schedule.AttemptID)
	// Usage-limit scheduling may persist a pending command intent before
	// admission. The denial must not claim/deliver that intent.
	if err != nil || (found && (command.State != core.WorkerCommandPending || !command.LeaseUntil.IsZero())) {
		t.Fatalf("capacity denial claimed or delivered resume command: %#v found=%v err=%v", command, found, err)
	}

	if _, _, _, err := store.RecordAttemptOutcome(ctx, busyAttempt.ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "slot freed",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResumeWorker(ctx, limited.WorkerRef); err != nil {
		t.Fatalf("retry after capacity is freed: %v", err)
	}
	runtime.mu.Lock()
	resumeCalls = runtime.resumeCalls
	runtime.mu.Unlock()
	if resumeCalls != 1 {
		t.Fatalf("expected exactly one resumed Attempt, got %d", resumeCalls)
	}
	if _, found, err := store.FindActiveScheduleForWorker(ctx, limited.ID); err != nil || found {
		t.Fatalf("resumed schedule remained active: found=%v err=%v", found, err)
	}
	if _, err := service.ResumeWorker(ctx, limited.WorkerRef); err != nil {
		t.Fatalf("idempotent manual replay failed: %v", err)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.resumeCalls != 1 {
		t.Fatalf("idempotent manual replay dispatched twice: %d", runtime.resumeCalls)
	}
}

func TestManualResumeDeliveredCommandReplayDoesNotNeedFreshCapacity(t *testing.T) {
	ctx := context.Background()
	store, service, runtime, limited, conversation, _, _ := setupLimitedWorker(t, 10*time.Minute)
	defer store.Close()
	schedule, found, err := store.FindActiveScheduleForWorker(ctx, limited.ID)
	if err != nil || !found {
		t.Fatalf("missing limited schedule: found=%v err=%v", found, err)
	}
	persisted, err := store.Worker(ctx, limited.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		Intent: "occupy single slot", ProjectID: persisted.ProjectID,
		NodeID: persisted.NodeID, HarnessInstanceID: persisted.HarnessInstanceID,
		ProjectSnapshot: persisted.ProjectSnapshot, ReserveNodeCapacity: true,
	}, core.TurnSpec{Input: "busy"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a resume accepted by Node before the schedule was completed.
	command, duplicate, err := store.ClaimWorkerCommand(ctx, "resume", "attempt", limited.ID, schedule.AttemptID)
	if err != nil || duplicate {
		t.Fatalf("failed to persist prior resume: %#v dup=%v err=%v", command, duplicate, err)
	}
	if _, err := store.MarkWorkerCommandDelivered(ctx, command.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResumeWorker(ctx, limited.WorkerRef); err != nil {
		t.Fatalf("already-delivered resume must complete schedule without capacity: %v", err)
	}
	if _, found, err := store.FindActiveScheduleForWorker(ctx, limited.ID); err != nil || found {
		t.Fatalf("delivered command schedule not completed: found=%v err=%v", found, err)
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.resumeCalls != 0 {
		t.Fatalf("delivered command unexpectedly resent: %d calls", runtime.resumeCalls)
	}
}
