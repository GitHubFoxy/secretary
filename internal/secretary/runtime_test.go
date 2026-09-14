package secretary

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type fakeRuntime struct {
	session *fakeSession
	request node.StartRequest
}
type fakeSession struct {
	id           string
	steerable    bool
	promptErr    error
	promptResult bool
	prompts      chan string
	activities   chan node.Activity
	results      chan node.Result
	responses    chan string
	closed       bool
}

func (r *fakeRuntime) Start(_ context.Context, request node.StartRequest) (node.Session, error) {
	r.request = request
	r.session = &fakeSession{id: "secretary-session", steerable: true, promptResult: true, prompts: make(chan string, 4), activities: make(chan node.Activity, 4), results: make(chan node.Result, 4)}
	return r.session, nil
}
func (s *fakeSession) ID() string { return s.id }
func (s *fakeSession) Prompt(_ context.Context, text string) error {
	s.prompts <- text
	if s.promptErr != nil {
		return s.promptErr
	}
	if s.promptResult {
		s.results <- node.Result{Status: "succeeded", Summary: "turn done"}
	}
	return nil
}
func (s *fakeSession) Steer(_ context.Context, text string) (bool, error) {
	if !s.steerable {
		return false, nil
	}
	s.activities <- node.Activity{Kind: node.ActivityText, Text: text}
	return true, nil
}
func (s *fakeSession) Cancel(context.Context) error {
	s.results <- node.Result{Status: "canceled", Summary: "stopped"}
	return nil
}
func (s *fakeSession) Respond(_ context.Context, requestID, response string) error {
	s.responses <- requestID + ":" + response
	return nil
}
func (s *fakeSession) Activity() <-chan node.Activity { return s.activities }
func (s *fakeSession) Result() <-chan node.Result     { return s.results }
func (s *fakeSession) Close() error                   { s.closed = true; return nil }

func setRuntimeTestPolicy(t *testing.T, store *core.Store) {
	t.Helper()
	if err := store.SetSecretaryPolicySnapshot(context.Background(), core.SecretaryPolicySnapshot{
		Version: "test-v1", Harness: "fx", Model: "secretary", Reasoning: "high",
		ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "test-hash", ProfileContent: "test policy",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRuntimeIgnoresResultFromReplacedSession(t *testing.T) {
	oldRuntime := &fakeRuntime{}
	runtime := NewRuntime(node.NewLocal(oldRuntime), "cap")
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	current := &fakeSession{results: make(chan node.Result, 1)}
	runtime.mu.Lock()
	runtime.session = current
	runtime.busy = true
	runtime.activeTurnID = "current-turn"
	runtime.mu.Unlock()
	oldRuntime.session.results <- node.Result{Status: "canceled", Summary: "old session stopped"}
	time.Sleep(20 * time.Millisecond)
	runtime.mu.Lock()
	busy, turnID := runtime.busy, runtime.activeTurnID
	runtime.mu.Unlock()
	if !busy || turnID != "current-turn" {
		t.Fatalf("old session result changed current turn: busy=%v turn=%q", busy, turnID)
	}
}

func TestRuntimeFailsClosedOnSecretaryInteraction(t *testing.T) {
	session := &fakeSession{activities: make(chan node.Activity, 2), responses: make(chan string, 2)}
	runtime := NewRuntime(nil, "cap")
	runtime.session = session
	go runtime.consumeActivity(session)
	session.activities <- node.Activity{Kind: node.ActivityPermission, RequestID: "permission-1"}
	session.activities <- node.Activity{Kind: node.ActivityUserInput, RequestID: "input-1"}
	for _, want := range []string{"permission-1:denied", "input-1:cancel"} {
		select {
		case got := <-session.responses:
			if got != want {
				t.Fatalf("response=%q want=%q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("missing response %q", want)
		}
	}
	close(session.activities)
}

func TestRuntimePublishesNormalizedThinkingAndToolActivity(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary-activity.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "durable user"); err != nil {
		t.Fatal(err)
	}
	setRuntimeTestPolicy(t, store)
	turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "inspect")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, turn.ID); err != nil {
		t.Fatal(err)
	}
	session := &fakeSession{activities: make(chan node.Activity, 3), results: make(chan node.Result, 1)}
	runtime := NewRuntime(nil, "cap")
	runtime.AttachConversation(store, conversation.ID)
	runtime.AttachIdentity(identity)
	runtime.session = session
	runtime.activeTurnID = turn.ID
	go runtime.consumeActivity(session)
	session.activities <- node.Activity{Kind: node.ActivityThinkingSummary, Summary: "Checking the project."}
	session.activities <- node.Activity{Kind: node.ActivityToolCall, Tool: "list_workers", Arguments: json.RawMessage(`{"scope":"current","api_token":"do-not-store","nested":{"analysis":"raw-analysis","safe":"keep","thought":"raw-thought"}}`)}
	session.activities <- node.Activity{Kind: node.ActivityToolResult, Tool: "list_workers", Result: `{"workers":[{"worker_ref":"w1"}],"nested":{"reasoning":"raw-reasoning","chain_of_thought":"raw-chain","safe":"keep-result"}}`, Status: "ok"}
	close(session.activities)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		events, readErr := store.SecretaryEvents(ctx, turn.ID, 0, 20)
		if readErr == nil && len(events) == 5 {
			if events[2].Kind != core.SecretaryThinkingSummaryEvent || events[3].Kind != core.SecretaryToolCallEvent || events[4].Kind != core.SecretaryToolResultEvent {
				t.Fatalf("events=%#v", events)
			}
			encoded, _ := json.Marshal(events[3].Payload)
			if strings.Contains(string(encoded), "do-not-store") || strings.Contains(string(encoded), "raw-analysis") || strings.Contains(string(encoded), "raw-thought") {
				t.Fatalf("tool call unsafe content leaked into Secretary event: %s", encoded)
			}
			resultJSON, _ := json.Marshal(events[4].Payload)
			if strings.Contains(string(resultJSON), "raw-reasoning") || strings.Contains(string(resultJSON), "raw-chain") {
				t.Fatalf("tool result unsafe content leaked into Secretary event: %s", resultJSON)
			}
			if !strings.Contains(string(encoded), "keep") || !strings.Contains(string(resultJSON), "keep-result") {
				t.Fatalf("ordinary tool payload was not preserved: call=%s result=%s", encoded, resultJSON)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	events, _ := store.SecretaryEvents(ctx, turn.ID, 0, 20)
	t.Fatalf("normalized Secretary activity was not published: %#v", events)
}

func TestDurableRuntimePromptErrorReturnsClaimedResultToNextTurn(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "durable user"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{
		Version: "test-v1", Harness: "fx", Model: "secretary", Reasoning: "high",
		ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "test-hash", ProfileContent: "test policy",
	}); err != nil {
		t.Fatal(err)
	}
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "prompt-error-result", Intent: "result", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "worker-policy"}, core.TurnSpec{Input: "result"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	_, result, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "same result"})
	if err != nil || result == nil {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	first, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	fake := &fakeRuntime{}
	runtime := NewRuntime(node.NewLocal(fake), "cap")
	runtime.AttachConversation(store, conversation.ID)
	runtime.AttachIdentity(identity)
	runtime.session = &fakeSession{promptErr: errors.New("prompt rejected"), prompts: make(chan string, 1), results: make(chan node.Result, 1)}
	runtime.busy = true
	runtime.activeTurnID = first.ID
	runtime.runPrompt(ctx, runtime.session, first.Input)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		stored, readErr := store.SecretaryTurn(ctx, first.ID)
		if readErr == nil && stored.State == core.SecretaryTurnFailed {
			break
		}
		time.Sleep(time.Millisecond)
	}
	failed, err := store.SecretaryTurn(ctx, first.ID)
	if err != nil || failed.State != core.SecretaryTurnFailed {
		t.Fatalf("failed turn=%#v err=%v", failed, err)
	}
	second, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "second")
	if err != nil {
		t.Fatal(err)
	}
	started, err := store.StartSecretaryTurn(ctx, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot core.SecretaryContext
	if err := json.Unmarshal([]byte(started.ContextSnapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.UnseenWorkerResults) != 1 || snapshot.UnseenWorkerResults[0].ID != result.ID {
		t.Fatalf("result after Prompt error=%#v", snapshot.UnseenWorkerResults)
	}
	if _, err := store.FinishSecretaryTurn(ctx, second.ID, core.SecretaryTurnSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	third, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "third")
	if err != nil {
		t.Fatal(err)
	}
	started, err = store.StartSecretaryTurn(ctx, third.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(started.ContextSnapshot), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.UnseenWorkerResults) != 0 {
		t.Fatalf("Prompt failure result appeared more than once=%#v", snapshot.UnseenWorkerResults)
	}
}

func TestDurableRuntimeDoesNotPromptWithoutPolicySnapshot(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "durable user"); err != nil {
		t.Fatal(err)
	}
	fake := &fakeRuntime{}
	runtime := NewRuntime(node.NewLocal(fake), "cap")
	runtime.AttachConversation(store, conversation.ID)
	runtime.AttachIdentity(identity)
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	fake.session.results <- node.Result{Status: "succeeded", Summary: "ready"}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		runtime.mu.Lock()
		busy := runtime.busy
		runtime.mu.Unlock()
		if !busy {
			break
		}
		time.Sleep(time.Millisecond)
	}
	queued, err := runtime.QueueMessage(ctx, "must not prompt")
	if err != nil {
		t.Fatal(err)
	}
	runtime.startNextDurable(ctx)
	select {
	case prompt := <-fake.session.prompts:
		t.Fatalf("prompt sent without policy snapshot: %q", prompt)
	case <-time.After(100 * time.Millisecond):
	}
	stored, err := store.SecretaryTurn(ctx, queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != core.SecretaryTurnQueued {
		t.Fatalf("missing policy changed turn state: %#v", stored)
	}
}

func TestDurableRuntimeRunsMessageQueuedBeforeSessionStart(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary-prestart.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	setRuntimeTestPolicy(t, store)
	dataDir := t.TempDir()
	if _, err := store.SaveUserDocument(ctx, filepath.Join(dataDir, "user.md"), "durable user"); err != nil {
		t.Fatal(err)
	}
	fake := &fakeRuntime{}
	runtime := NewRuntime(node.NewLocal(fake), "cap")
	runtime.AttachMCP("", dataDir)
	runtime.AttachConversation(store, conversation.ID)
	runtime.AttachIdentity(identity)
	if err := runtime.HandleMessage(ctx, "arrived before Node readiness"); err != nil {
		t.Fatalf("pre-start durable message: %v", err)
	}
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		turns, readErr := store.SecretaryTurns(ctx, identity.ID)
		if readErr == nil && len(turns) == 1 && turns[0].State == core.SecretaryTurnSucceeded {
			return
		}
		time.Sleep(time.Millisecond * 5)
	}
	turns, _ := store.SecretaryTurns(ctx, identity.ID)
	t.Fatalf("pre-start durable turn did not finish: %#v", turns)
}

func TestDurableRuntimeDrainsPreStartBacklogAfterFailedTurn(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary-backlog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	setRuntimeTestPolicy(t, store)
	dataDir := t.TempDir()
	if _, err := store.SaveUserDocument(ctx, filepath.Join(dataDir, "user.md"), "durable user"); err != nil {
		t.Fatal(err)
	}
	fake := &fakeRuntime{}
	runtime := NewRuntime(node.NewLocal(fake), "cap")
	runtime.AttachMCP("", dataDir)
	runtime.AttachConversation(store, conversation.ID)
	runtime.AttachIdentity(identity)
	calls := 0
	runtime.turnLoader = func(ctx context.Context, id string) (core.SecretaryTurn, error) {
		calls++
		if calls == 1 {
			return core.SecretaryTurn{}, errors.New("injected context failure")
		}
		return store.SecretaryTurn(ctx, id)
	}
	for _, input := range []string{"first", "second"} {
		if err := runtime.HandleMessage(ctx, input); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		turns, readErr := store.SecretaryTurns(ctx, identity.ID)
		if readErr == nil && len(turns) == 2 && turns[0].State == core.SecretaryTurnFailed && turns[1].State == core.SecretaryTurnSucceeded {
			return
		}
		time.Sleep(time.Millisecond * 5)
	}
	turns, _ := store.SecretaryTurns(ctx, identity.ID)
	t.Fatalf("startup backlog was not drained: %#v", turns)
}

func TestDurableRuntimeQueuesAndFinishesSecretaryTurn(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	setRuntimeTestPolicy(t, store)
	fake := &fakeRuntime{}
	runtime := NewRuntime(node.NewLocal(fake), "cap")
	runtime.AttachConversation(store, conversation.ID)
	runtime.AttachIdentity(identity)
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	fake.session.results <- node.Result{Status: "succeeded", Summary: "ready"}
	if err := runtime.HandleMessage(ctx, "queued input"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	turnID := ""
	for time.Now().Before(deadline) {
		if events, readErr := store.EventsAfter(ctx, time.Time{}, 100); readErr == nil {
			for _, event := range events {
				if event.Kind == core.SecretaryTurnQueuedEvent {
					turnID = event.AggregateID
				}
			}
			if turnID != "" {
				stream, streamErr := store.SecretaryEvents(ctx, turnID, 0, 100)
				if streamErr == nil && len(stream) == 3 && stream[2].Kind == core.SecretaryTurnFinishedEvent {
					return
				}
			}
		}
		time.Sleep(time.Millisecond * 5)
	}
	t.Fatal("durable Secretary turn did not finish")
}

func TestRuntimeUsesCapabilityForSecretarySession(t *testing.T) {
	rt := &fakeRuntime{}
	runtime := node.NewLocal(rt)
	secretary := NewRuntime(runtime, "cap-123")
	if err := secretary.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if rt.request.WorkerRef != "secretary" || rt.request.Task == "" || contains(rt.request.Task, "cap-123") || !rt.request.DeferInitialPrompt {
		t.Fatalf("request=%#v", rt.request)
	}
	if err := secretary.HandleMessage(context.Background(), "change direction"); err != nil {
		t.Fatal(err)
	}
	select {
	case prompt := <-rt.session.prompts:
		if prompt != "change direction" {
			t.Fatalf("prompt=%q", prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("prompt was not delivered")
	}
}

func TestRuntimeFailsClosedWhenCanonicalSnapshotIsEmpty(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	setRuntimeTestPolicy(t, store)
	turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "must not prompt")
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.StartSecretaryTurn(ctx, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeRuntime{}
	runtime := NewRuntime(node.NewLocal(fake), "cap")
	runtime.AttachConversation(store, conversation.ID)
	runtime.AttachIdentity(identity)
	runtime.turnLoader = func(context.Context, string) (core.SecretaryTurn, error) {
		active.ContextSnapshot = ""
		return active, nil
	}
	runtime.session = &fakeSession{prompts: make(chan string, 1), results: make(chan node.Result, 1)}
	runtime.busy = true
	runtime.activeTurnID = turn.ID
	runtime.runPrompt(ctx, runtime.session, active.Input)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		stored, readErr := store.SecretaryTurn(ctx, turn.ID)
		if readErr == nil && stored.State == core.SecretaryTurnFailed {
			if stored.Error != "canonical Secretary context unavailable: empty canonical Secretary context snapshot" {
				t.Fatalf("turn error=%q", stored.Error)
			}
			select {
			case prompt := <-runtime.session.(*fakeSession).prompts:
				t.Fatalf("raw prompt was sent: %q", prompt)
			default:
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("empty canonical snapshot did not fail closed")
}

func TestRuntimeFailsClosedWhenCanonicalSnapshotIsInvalid(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	setRuntimeTestPolicy(t, store)
	queued, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "must not prompt")
	if err != nil {
		t.Fatal(err)
	}
	active, err := store.StartSecretaryTurn(ctx, queued.ID)
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeRuntime{}
	runtime := NewRuntime(node.NewLocal(fake), "cap")
	runtime.AttachConversation(store, conversation.ID)
	runtime.AttachIdentity(identity)
	runtime.turnLoader = func(context.Context, string) (core.SecretaryTurn, error) {
		active.ContextSnapshot = `{"identity":{"id":"` + identity.ID + `","conversation_id":"` + conversation.ID + `"}}`
		return active, nil
	}
	runtime.session = &fakeSession{prompts: make(chan string, 1), results: make(chan node.Result, 1)}
	runtime.busy = true
	runtime.activeTurnID = active.ID
	runtime.runPrompt(ctx, runtime.session, active.Input)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		stored, readErr := store.SecretaryTurn(ctx, active.ID)
		if readErr == nil && stored.State == core.SecretaryTurnFailed {
			select {
			case prompt := <-runtime.session.(*fakeSession).prompts:
				t.Fatalf("raw prompt was sent: %q", prompt)
			default:
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("invalid canonical snapshot did not fail closed")
}

func TestRuntimePersistsResponseEntries(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rt := &fakeRuntime{}
	secretary := NewRuntime(node.NewLocal(rt), "cap")
	secretary.AttachConversation(store, conversation.ID)
	if err := secretary.Start(ctx); err != nil {
		t.Fatal(err)
	}
	rt.session.steerable = false
	if err := secretary.HandleMessage(ctx, "answer this"); err != nil {
		t.Fatal(err)
	}
	rt.session.results <- node.Result{Status: "succeeded", Summary: "initial ready"}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		entries, readErr := store.EntriesAfter(ctx, conversation.ID, 0)
		if readErr == nil && len(entries) == 1 && entries[0].Kind == core.EntrySecretary && entries[0].Body == "turn done" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	entries, _ := store.EntriesAfter(ctx, conversation.ID, 0)
	t.Fatalf("Secretary response was not persisted: %#v", entries)
}

func TestRuntimeQueuesFollowUpUntilIdle(t *testing.T) {
	rt := &fakeRuntime{}
	rt.session = nil
	local := node.NewLocal(rt)
	secretary := NewRuntime(local, "cap")
	if err := secretary.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	rt.session.steerable = false
	if err := secretary.HandleMessage(context.Background(), "/q follow up"); err != nil {
		t.Fatal(err)
	}
	// Initial Secretary turn reaches its safe boundary here.
	rt.session.results <- node.Result{Status: "succeeded", Summary: "ready"}
	select {
	case prompt := <-rt.session.prompts:
		if prompt != "follow up" {
			t.Fatalf("prompt=%q", prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("queued prompt was not delivered")
	}
}

func contains(text, part string) bool {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
