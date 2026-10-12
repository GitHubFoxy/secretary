package ctl

import (
	"context"
	"encoding/json"
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
	r.resumeCalls++
	r.resumeTexts = append(r.resumeTexts, prompt)
	r.mu.Unlock()
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
	instance := core.HarnessInstance{ID: "local/codex", Node: "local", Kind: core.HarnessCodex}
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
	store, service, runtime, worker, conversation, _, _ := setupLimitedWorker(t, 10*time.Second) // future reset
	service.commandNow = func() time.Time { return time.Now().UTC().Add(2 * time.Minute) } // simulate clock advancing past due
	defer store.Close()

	// Enqueue a FIFO message (/q)
	_, err := service.MessageWorker(ctx, MessageWorkerRequest{
		WorkerRef:      worker.WorkerRef,
		Text:           "/q next task",
		IdempotencyKey: "q-1",
	})
	if err != nil {
		t.Fatalf("enqueue /q failed: %v", err)
	}

	// Verify the message is pending in queue
	details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(details.QueuedMessages) != 1 || details.QueuedMessages[0].State != "pending" {
		t.Fatalf("expected pending queued message, got %#v", details.QueuedMessages)
	}

	// Pump due schedules and queued worker messages
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatalf("process queued worker messages: %v", err)
	}

	// Runtime should receive Resume with ContinuationPrompt, NOT the queued message
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

	// The queued message MUST still be pending because the turn was suspended and not finished
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

	// Set node offline / not ready
	runtime.mu.Lock()
	runtime.ready = false
	runtime.mu.Unlock()

	// Run process loop
	if err := service.ProcessQueuedWorkerMessages(ctx); err != nil {
		t.Fatal(err)
	}

	// Runtime was not called
	runtime.mu.Lock()
	calls := runtime.resumeCalls
	runtime.mu.Unlock()
	if calls != 0 {
		t.Fatalf("expected 0 resume calls on offline node, got %d", calls)
	}

	// Schedule remains untouched in pending state
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
	store, service, runtime, worker, _, _, _ := setupLimitedWorker(t, 10*time.Minute) // not due yet
	defer store.Close()

	// Call ResumeWorker (which backs both POST /v1/workers/{ref}/resume and Telegram /resume)
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

	// Idempotent second call
	_, err2 := service.ResumeWorker(ctx, worker.WorkerRef)
	if err2 != nil {
		t.Fatalf("idempotent ResumeWorker failed: %v", err2)
	}
	_ = updated
}

func TestLimitedWorker_RaceManualAndAutomatic(t *testing.T) {
	ctx := context.Background()
	store, service, runtime, worker, _, _, _ := setupLimitedWorker(t, -1*time.Minute) // due now
	defer store.Close()

	var wg sync.WaitGroup
	wg.Add(2)

	// One goroutine calls manual ResumeWorker
	go func() {
		defer wg.Done()
		_, _ = service.ResumeWorker(ctx, worker.WorkerRef)
	}()

	// Another goroutine calls ProcessQueuedWorkerMessages
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

	// User sends direct input (not /q)
	details, err := service.MessageWorker(ctx, MessageWorkerRequest{
		WorkerRef:      worker.WorkerRef,
		Text:           "start completely new task",
		IdempotencyKey: "new-input-1",
	})
	if err != nil {
		t.Fatalf("MessageWorker failed: %v", err)
	}

	// Verify old schedule was canceled
	_, found, err := store.FindActiveScheduleForWorker(ctx, worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("expected active schedule to be canceled and not found")
	}

	// Suspended turn was finalized with canceled Result, and new turn was created
	if len(details.Turns) < 2 {
		t.Fatalf("expected at least 2 turns (canceled turn + new turn), got %d", len(details.Turns))
	}
	firstTurn := details.Turns[0]
	if firstTurn.State != core.TurnCanceled {
		t.Fatalf("expected first turn canceled, got %s", firstTurn.State)
	}
	newTurn := details.Turns[1]
	if newTurn.Input != "start completely new task" {
		t.Fatalf("expected new turn input %q, got %q", "start completely new task", newTurn.Input)
	}
}
