package secretary

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type fakeRuntime struct {
	session  *fakeSession
	request  node.StartRequest
	startErr error
}
type fakeSession struct {
	id           string
	steerable    bool
	promptErr    error
	promptResult bool
	onPrompt     func(context.Context, string) error
	onCancel     func(context.Context) error
	prompts      chan string
	activities   chan node.Activity
	results      chan node.Result
	responses    chan string
	closed       bool
	closeErr     error
}

func (r *fakeRuntime) Start(_ context.Context, request node.StartRequest) (node.Session, error) {
	if r.startErr != nil {
		return nil, r.startErr
	}
	r.request = request
	r.session = &fakeSession{id: "secretary-session", steerable: true, promptResult: true, prompts: make(chan string, 4), activities: make(chan node.Activity, 4), results: make(chan node.Result, 4)}
	return r.session, nil
}
func (s *fakeSession) ID() string { return s.id }
func (s *fakeSession) Prompt(ctx context.Context, text string) error {
	s.prompts <- text
	if s.onPrompt != nil {
		return s.onPrompt(ctx, text)
	}
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
func (s *fakeSession) Cancel(ctx context.Context) error {
	if s.onCancel != nil {
		return s.onCancel(ctx)
	}
	s.results <- node.Result{Status: "canceled", Summary: "stopped"}
	return nil
}
func (s *fakeSession) Respond(_ context.Context, requestID, response string) error {
	s.responses <- requestID + ":" + response
	return nil
}
func (s *fakeSession) Activity() <-chan node.Activity { return s.activities }
func (s *fakeSession) Result() <-chan node.Result     { return s.results }
func (s *fakeSession) Close() error                   { s.closed = true; return s.closeErr }

func setRuntimeTestPolicy(t *testing.T, store *core.Store) {
	t.Helper()
	if err := store.SetSecretaryPolicySnapshot(context.Background(), core.SecretaryPolicySnapshot{
		Version: "test-v1", Harness: "fx", Model: "secretary", Reasoning: "high",
		ProfileVersion: "test-v1", ProfileName: "secretary", ProfileHash: "test-hash", ProfileContent: "test policy",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCancelActivePromptIgnoresStalePromptID(t *testing.T) {
	fake := &fakeRuntime{}
	runtime := NewRuntime(node.NewLocal(fake), "cap")
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	cancelCalls := make(chan struct{}, 1)
	fake.session.onCancel = func(context.Context) error {
		cancelCalls <- struct{}{}
		return nil
	}
	runtime.mu.Lock()
	runtime.busy = true
	stalePromptID := runtime.beginPromptLocked()
	currentPromptID := runtime.beginPromptLocked()
	runtime.promptStartedID = currentPromptID
	runtime.mu.Unlock()
	runtime.cancelActivePrompt(context.Background(), fake.session, stalePromptID)
	select {
	case <-cancelCalls:
		t.Fatal("stale cancellation canceled the current prompt")
	default:
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.activePromptID != currentPromptID || runtime.cancelRequestedID != 0 {
		t.Fatalf("current prompt state changed: active=%d cancelRequested=%d", runtime.activePromptID, runtime.cancelRequestedID)
	}
}

func TestRuntimeStartsOnlyExplicitAddressedReplyProfileWithVersionedMCP(t *testing.T) {
	fake := &fakeRuntime{}
	runtime := NewRuntime(node.NewLocal(fake), "capability")
	runtime.AttachMCPServer("secretary-mcp", "/state", "http://127.0.0.1:8081")
	runtime.AttachProfile(func() node.ManagedProfile {
		return node.ManagedProfile{Name: "secretary", Version: "profile-v1", Hash: "hash-v1", Content: "explicit addressed policy", ReplyContractVersion: core.SecretaryReplyContractAddressedV1}
	})
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !fake.request.DrainOutputBeforeResult || fake.request.Profile.ReplyContractVersion != core.SecretaryReplyContractAddressedV1 {
		t.Fatalf("runtime request omitted the explicit v1 stream contract: %#v", fake.request)
	}
	found := false
	for _, server := range fake.request.MCPServers {
		for _, env := range server.Env {
			if env.Name == "SECRETARY_MCP_REPLY_CONTRACT" && env.Value == core.SecretaryReplyContractAddressedV1 {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("MCP session lacks explicit reply contract environment: %#v", fake.request.MCPServers)
	}
	close(fake.session.activities)
	close(fake.session.results)
}

func TestRuntimeAddressedReplyPreservesPreResultDeltaAndSuppressesPostResultOutput(t *testing.T) {
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
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{Version: "v1", Harness: "fx", Model: "secretary", Reasoning: "high", ProfileVersion: "v1", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "policy", ReplyContractVersion: core.SecretaryReplyContractAddressedV1}); err != nil {
		t.Fatal(err)
	}
	origin, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "dispatch a worker task")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, origin.ID); err != nil {
		t.Fatal(err)
	}
	_, workerTurn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "runtime-origin", Intent: "synthetic", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "synthetic"}, core.TurnSpec{Input: "synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	session := &fakeSession{id: "addressed-session", activities: make(chan node.Activity, 2), results: make(chan node.Result, 1)}
	runtime := NewRuntime(nil, capability)
	runtime.AttachConversation(store, conversation.ID)
	runtime.AttachIdentity(identity)
	runtime.mu.Lock()
	runtime.session = session
	runtime.busy = true
	runtime.activeTurnID = origin.ID
	runtime.activeInputID = origin.InputID
	runtime.replyContractVersion = core.SecretaryReplyContractAddressedV1
	runtime.mu.Unlock()
	go runtime.consumeAddressedSession(session)
	session.activities <- node.Activity{Kind: node.ActivityText, Text: "independent progress before Worker Result"}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		events, eventErr := store.SecretaryEvents(ctx, origin.ID, 0, 20)
		if eventErr == nil && len(events) == 3 && events[2].Kind == core.SecretaryTextDeltaEvent {
			break
		}
		time.Sleep(time.Millisecond)
	}
	preEvents, err := store.SecretaryEvents(ctx, origin.ID, 0, 20)
	if err != nil || len(preEvents) != 3 || preEvents[2].Kind != core.SecretaryTextDeltaEvent {
		t.Fatalf("pre-Result ActivityText was not streamed immediately: events=%#v err=%v", preEvents, err)
	}
	if _, result, _, err := store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "canonical worker result"}); err != nil || result == nil {
		t.Fatalf("record Worker Result: result=%#v err=%v", result, err)
	}
	if _, err := store.LinkSecretaryWorkerTurn(ctx, person.ID, capability, origin.ID, origin.InputID, workerTurn.ID); err != nil {
		t.Fatal(err)
	}
	session.activities <- node.Activity{Kind: node.ActivityText, Text: "unaddressed after Worker Result"}
	session.results <- node.Result{
		Status: "succeeded", Summary: "same result summary echo",
		CompletionEvidence: &node.TerminalCompletionEvidence{
			Contract: node.TerminalCompletionOpenCodeV2, StopReason: node.TerminalStopReasonEndTurn,
			RPCSucceeded: true, DrainCompleted: true, AssistantChunks: 1,
		},
	}
	close(session.results)
	close(session.activities)
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		turn, err := store.SecretaryTurn(ctx, origin.ID)
		if err == nil && turn.State == core.SecretaryTurnFailed && turn.Error == "addressed_reply_missing" {
			entries, listErr := store.EntriesAfter(ctx, conversation.ID, 0)
			if listErr != nil {
				t.Fatal(listErr)
			}
			secretaryEntries, workerResults := 0, 0
			for _, entry := range entries {
				if entry.Kind == core.EntrySecretary {
					secretaryEntries++
				}
				if entry.Kind == core.EntryWorkerResult {
					workerResults++
				}
			}
			if secretaryEntries != 0 || workerResults != 1 {
				t.Fatalf("Runtime produced a duplicate echo: Secretary=%d WorkerResults=%d entries=%#v", secretaryEntries, workerResults, entries)
			}
			events, eventErr := store.SecretaryEvents(ctx, origin.ID, 0, 20)
			streamDeltas := 0
			for _, event := range events {
				if event.Kind == core.SecretaryTextDeltaEvent {
					streamDeltas++
					var payload map[string]any
					if err := json.Unmarshal(event.Payload, &payload); err != nil || payload["text"] != "independent progress before Worker Result" {
						t.Fatalf("Runtime exposed post-Result delta: payload=%#v err=%v", payload, err)
					}
				}
			}
			if eventErr != nil || streamDeltas != 1 {
				t.Fatalf("Runtime stream did not preserve exactly the pre-Result delta: events=%#v err=%v", events, eventErr)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("addressed Runtime turn did not complete")
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
	dataDir := t.TempDir()
	if _, err := store.SaveUserDocument(ctx, filepath.Join(dataDir, "user.md"), "durable user"); err != nil {
		t.Fatal(err)
	}
	fake := &fakeRuntime{}
	runtime := NewRuntime(node.NewLocal(fake), "cap")
	runtime.AttachMCP("", dataDir)
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

func TestDurableRuntimeCancelsActivePromptBeforeNextMessage(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary-interrupt.db"))
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
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	releaseFirst := make(chan struct{})
	cancelEntered := make(chan struct{})
	allowCancel := make(chan struct{})
	firstTerminal := make(chan struct{})
	var cancelCalls atomic.Int32
	fake.session.onPrompt = func(_ context.Context, text string) error {
		if strings.Contains(text, "User turn:\nfirst input\n\nActive reply identity") {
			<-releaseFirst
			close(firstTerminal)
			fake.session.results <- node.Result{Status: "canceled", Summary: "cancelled"}
			return nil
		}
		if strings.Contains(text, "User turn:\nreplacement input\n\nActive reply identity") {
			select {
			case <-firstTerminal:
			default:
				t.Error("replacement prompt started before the canceled prompt was terminal")
			}
			fake.session.results <- node.Result{Status: "succeeded", Summary: "replacement complete"}
			return nil
		}
		if strings.Contains(text, "User turn:\nthird input\n\nActive reply identity") {
			fake.session.results <- node.Result{Status: "succeeded", Summary: "third complete"}
			return nil
		}
		return errors.New("unexpected prompt")
	}
	fake.session.onCancel = func(context.Context) error {
		cancelCalls.Add(1)
		close(cancelEntered)
		<-allowCancel
		close(releaseFirst)
		return nil
	}
	firstEntry, _, err := store.AppendInbound(ctx, conversation.ID, "web", "first", "first input")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.HandleInbound(ctx, firstEntry); err != nil {
		t.Fatal(err)
	}
	select {
	case prompt := <-fake.session.prompts:
		if !strings.Contains(prompt, "first input") {
			t.Fatalf("first prompt = %q", prompt)
		}
	case <-time.After(time.Second):
		select {
		case runtimeErr := <-runtime.Errors():
			t.Fatalf("first prompt did not start: %v", runtimeErr)
		default:
			t.Fatal("first prompt did not start")
		}
	}
	interruptErr := make(chan error, 1)
	replacementEntry, _, err := store.AppendInbound(ctx, conversation.ID, "web", "replacement", "replacement input")
	if err != nil {
		t.Fatal(err)
	}
	go func() { interruptErr <- runtime.HandleInbound(ctx, replacementEntry) }()
	select {
	case <-cancelEntered:
	case <-time.After(time.Second):
		runtime.mu.Lock()
		promptID, startedID, cancelID, busy := runtime.activePromptID, runtime.promptStartedID, runtime.cancelRequestedID, runtime.busy
		runtime.mu.Unlock()
		select {
		case err := <-interruptErr:
			t.Fatalf("active prompt cancellation was not requested (input error %v, prompt=%d started=%d cancel=%d busy=%v)", err, promptID, startedID, cancelID, busy)
		default:
			t.Fatalf("active prompt cancellation was not requested (prompt=%d started=%d cancel=%d busy=%v)", promptID, startedID, cancelID, busy)
		}
	}
	if err := runtime.HandleInbound(ctx, replacementEntry); err != nil {
		t.Fatal(err)
	}
	thirdEntry, _, err := store.AppendInbound(ctx, conversation.ID, "web", "third", "third input")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.HandleInbound(ctx, thirdEntry); err != nil {
		t.Fatal(err)
	}
	close(allowCancel)
	if err := <-interruptErr; err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		turns, readErr := store.SecretaryTurns(ctx, identity.ID)
		if readErr == nil && len(turns) == 3 && turns[0].State == core.SecretaryTurnCanceled && turns[1].State == core.SecretaryTurnSucceeded && turns[2].State == core.SecretaryTurnSucceeded {
			if turns[0].InputEntryID != firstEntry.ID || turns[1].InputEntryID != replacementEntry.ID || turns[2].InputEntryID != thirdEntry.ID || turns[0].InputSeq >= turns[1].InputSeq || turns[1].InputSeq >= turns[2].InputSeq {
				t.Fatalf("durable input identity/sequence was not preserved: %#v", turns)
			}
			if cancelCalls.Load() != 1 {
				t.Fatalf("cancel calls = %d, want 1", cancelCalls.Load())
			}
			select {
			case prompt := <-fake.session.prompts:
				if !strings.Contains(prompt, "replacement input") {
					t.Fatalf("replacement prompt = %q", prompt)
				}
			default:
				t.Fatal("replacement prompt was not observed")
			}
			select {
			case prompt := <-fake.session.prompts:
				if !strings.Contains(prompt, "third input") {
					t.Fatalf("third prompt = %q", prompt)
				}
			default:
				t.Fatal("third prompt was not observed")
			}
			return
		}
		time.Sleep(time.Millisecond * 5)
	}
	turns, _ := store.SecretaryTurns(ctx, identity.ID)
	t.Fatalf("interrupted durable turns did not finish in order: %#v", turns)
}

func TestDurableRuntimeQueuedMessageDoesNotCancelActivePrompt(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary-queued-interrupt.db"))
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
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	releaseFirst := make(chan struct{})
	var cancelCalls atomic.Int32
	fake.session.onPrompt = func(_ context.Context, text string) error {
		if strings.Contains(text, "first input") {
			<-releaseFirst
			fake.session.results <- node.Result{Status: "succeeded", Summary: "first complete"}
			return nil
		}
		fake.session.results <- node.Result{Status: "succeeded", Summary: "queued complete"}
		return nil
	}
	fake.session.onCancel = func(context.Context) error {
		cancelCalls.Add(1)
		close(releaseFirst)
		return nil
	}
	if err := runtime.HandleMessage(ctx, "first input"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fake.session.prompts:
	case <-time.After(time.Second):
		select {
		case runtimeErr := <-runtime.Errors():
			t.Fatalf("first prompt did not start: %v", runtimeErr)
		default:
			t.Fatal("first prompt did not start")
		}
	}
	if err := runtime.HandleMessage(ctx, "/q queued input"); err != nil {
		t.Fatal(err)
	}
	if cancelCalls.Load() != 0 {
		t.Fatalf("explicit queue canceled active prompt %d times", cancelCalls.Load())
	}
	close(releaseFirst)
	deadline := time.After(time.Second)
	for {
		select {
		case prompt := <-fake.session.prompts:
			if strings.Contains(prompt, "queued input") {
				if cancelCalls.Load() != 0 {
					t.Fatalf("explicit queue canceled active prompt %d times", cancelCalls.Load())
				}
				return
			}
		case <-deadline:
			t.Fatal("explicitly queued prompt did not run after active turn")
		}
	}
}

func TestDurableRuntimeCancelFailureIsRecordedAndWaitsForTerminal(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary-cancel-failure.db"))
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
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	releaseFirst := make(chan struct{})
	fake.session.onPrompt = func(_ context.Context, text string) error {
		if strings.Contains(text, "active input") {
			<-releaseFirst
			fake.session.results <- node.Result{Status: "succeeded", Summary: "natural completion"}
			return nil
		}
		fake.session.results <- node.Result{Status: "succeeded", Summary: "queued completion"}
		return nil
	}
	fake.session.onCancel = func(context.Context) error { return errors.New("cancel unavailable") }
	if err := runtime.HandleMessage(ctx, "active input"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fake.session.prompts:
	case <-time.After(time.Second):
		t.Fatal("active prompt did not start")
	}
	if err := runtime.HandleMessage(ctx, "replacement input"); err != nil {
		t.Fatal(err)
	}
	select {
	case runtimeErr := <-runtime.Errors():
		if !strings.Contains(runtimeErr.Error(), "cancel active prompt") {
			t.Fatalf("runtime error = %v", runtimeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel failure was not reported")
	}
	select {
	case prompt := <-fake.session.prompts:
		t.Fatalf("next prompt started before active prompt terminal: %q", prompt)
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseFirst)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		turns, readErr := store.SecretaryTurns(ctx, identity.ID)
		if readErr == nil && len(turns) == 2 && turns[0].State == core.SecretaryTurnSucceeded && turns[1].State == core.SecretaryTurnSucceeded {
			events, eventErr := store.EventsAfter(ctx, time.Time{}, 100)
			if eventErr != nil {
				t.Fatal(eventErr)
			}
			for _, event := range events {
				if event.Kind == "secretary.interruption_failed" && event.AggregateID == turns[0].ID {
					return
				}
			}
			t.Fatal("cancel failure was not recorded as a durable event")
		}
		time.Sleep(time.Millisecond * 5)
	}
	turns, _ := store.SecretaryTurns(ctx, identity.ID)
	t.Fatalf("queued turn did not start after natural completion: %#v", turns)
}

func TestDurableRuntimeCancelTimeoutBlocksQueueUntilSessionRestart(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary-cancel-timeout.db"))
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
	runtime.cancelTimeout = 25 * time.Millisecond
	runtime.AttachMCP("", dataDir)
	runtime.AttachConversation(store, conversation.ID)
	runtime.AttachIdentity(identity)
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	releasePrompt := make(chan struct{})
	releaseCancel := make(chan struct{})
	cancelEntered := make(chan struct{})
	fake.session.onPrompt = func(_ context.Context, text string) error {
		if strings.Contains(text, "User turn:\nactive input") {
			<-releasePrompt
			fake.session.results <- node.Result{Status: "succeeded", Summary: "natural completion"}
			return nil
		}
		if strings.Contains(text, "User turn:\nqueued input") {
			fake.session.results <- node.Result{Status: "succeeded", Summary: "queued completion"}
			return nil
		}
		return errors.New("unexpected prompt")
	}
	fake.session.onCancel = func(context.Context) error {
		close(cancelEntered)
		<-releaseCancel
		return nil
	}
	activeEntry, _, err := store.AppendInbound(ctx, conversation.ID, "web", "active", "active input")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.HandleInbound(ctx, activeEntry); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fake.session.prompts:
	case <-time.After(time.Second):
		t.Fatal("active prompt did not start")
	}
	queuedEntry, _, err := store.AppendInbound(ctx, conversation.ID, "web", "queued", "queued input")
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.HandleInbound(ctx, queuedEntry); err != nil {
		t.Fatal(err)
	}
	select {
	case <-cancelEntered:
	case <-time.After(time.Second):
		t.Fatal("cancel call did not start")
	}
	select {
	case runtimeErr := <-runtime.Errors():
		if !strings.Contains(runtimeErr.Error(), context.DeadlineExceeded.Error()) {
			t.Fatalf("cancel timeout error = %v", runtimeErr)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel timeout was not reported")
	}
	close(releasePrompt)
	select {
	case prompt := <-fake.session.prompts:
		t.Fatalf("queued prompt started while cancellation outcome was unknown: %q", prompt)
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseCancel)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		turns, readErr := store.SecretaryTurns(ctx, identity.ID)
		if readErr == nil && len(turns) == 2 && turns[0].State == core.SecretaryTurnSucceeded && turns[1].State == core.SecretaryTurnQueued {
			fake.session.onCancel = nil
			if err := runtime.Stop(ctx); err != nil {
				t.Fatal(err)
			}
			if err := runtime.Start(ctx); err != nil {
				t.Fatal(err)
			}
			restartDeadline := time.Now().Add(time.Second)
			for time.Now().Before(restartDeadline) {
				turns, readErr = store.SecretaryTurns(ctx, identity.ID)
				if readErr == nil && len(turns) == 2 && turns[1].State == core.SecretaryTurnSucceeded {
					return
				}
				time.Sleep(time.Millisecond * 5)
			}
			t.Fatalf("queued turn did not resume after explicit runtime restart: %#v", turns)
		}
		time.Sleep(time.Millisecond * 5)
	}
	turns, _ := store.SecretaryTurns(ctx, identity.ID)
	t.Fatalf("uncertain session did not preserve queued turn: %#v", turns)
}

func TestDurableRuntimeRestartKeepsUnknownGenerationQueuedUntilRecovery(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary-recovery.db"))
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
	firstNode := &fakeRuntime{}
	first := NewRuntime(node.NewLocal(firstNode), "cap")
	first.cancelTimeout = 50 * time.Millisecond
	first.AttachMCP("", dataDir)
	first.AttachConversation(store, conversation.ID)
	first.AttachIdentity(identity)
	if err := first.Start(ctx); err != nil {
		t.Fatal(err)
	}
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	firstNode.session.onPrompt = func(_ context.Context, text string) error {
		if strings.Contains(text, "active input") {
			close(firstStarted)
			<-firstRelease
			firstNode.session.results <- node.Result{Status: "canceled", Summary: "stopped"}
			return nil
		}
		return nil
	}
	firstNode.session.onCancel = func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}
	if err := first.HandleMessage(ctx, "active input"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("active prompt did not start")
	}
	if err := first.HandleMessage(ctx, "replacement input"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-first.Errors():
	case <-time.After(time.Second):
		t.Fatal("cancel timeout was not reported")
	}
	secondNode := &fakeRuntime{}
	second := NewRuntime(node.NewLocal(secondNode), "cap")
	second.AttachMCP("", dataDir)
	second.AttachConversation(store, conversation.ID)
	second.AttachIdentity(identity)
	if err := second.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer second.Stop(context.Background())
	if required, err := store.SecretaryRecoveryRequired(ctx, identity.ID); err != nil || !required {
		t.Fatalf("restart recovery required=%v err=%v", required, err)
	}
	if err := second.HandleMessage(ctx, "later input"); err != nil {
		t.Fatal(err)
	}
	select {
	case prompt := <-secondNode.session.prompts:
		t.Fatalf("prompt started before recovery confirmation: %q", prompt)
	case <-time.After(50 * time.Millisecond):
	}
	close(firstRelease)
	if err := first.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	firstRecoverySession := secondNode.session
	firstRecoverySession.closeErr = errors.New("close failed")
	if err := second.HandleMessage(ctx, "/recover"); err == nil {
		t.Fatal("recovery succeeded after session close failed")
	}
	if required, err := store.SecretaryRecoveryRequired(ctx, identity.ID); err != nil || !required {
		t.Fatalf("failed close cleared recovery barrier: required=%v err=%v", required, err)
	}
	firstRecoverySession.closeErr = nil
	secondNode.startErr = errors.New("dispatch unavailable")
	if err := second.HandleMessage(ctx, "/recover"); err == nil {
		t.Fatal("recovery succeeded despite replacement session start failure")
	}
	if required, err := store.SecretaryRecoveryRequired(ctx, identity.ID); err != nil || required {
		t.Fatalf("confirmed recovery barrier remained: required=%v err=%v", required, err)
	}
	secondNode.startErr = nil
	if err := second.HandleMessage(ctx, "/recover"); err != nil {
		t.Fatalf("recovery retry after dispatch start failure: %v", err)
	}
	recoveredSession := secondNode.session
	select {
	case prompt := <-secondNode.session.prompts:
		if !strings.Contains(prompt, "replacement input") {
			t.Fatalf("recovery started out of order with prompt %q", prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("queued prompt did not start after recovery confirmation")
	}
	if err := second.HandleMessage(ctx, "/recover"); err != nil {
		t.Fatal(err)
	}
	if recoveredSession.closed {
		t.Fatal("replayed recovery command stopped the healthy replacement session")
	}
}

func TestDurableRuntimeRecoverWithoutBarrierDoesNotStopActivePrompt(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary-recover-idempotent.db"))
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
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	fake.session.onPrompt = func(context.Context, string) error {
		close(started)
		<-release
		fake.session.results <- node.Result{Status: "succeeded", Summary: "completed"}
		return nil
	}
	if err := runtime.HandleMessage(ctx, "active input"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("active prompt did not start")
	}
	if err := runtime.HandleMessage(ctx, "/recover"); err != nil {
		t.Fatal(err)
	}
	if fake.session.closed {
		t.Fatal("recovery command without a barrier stopped the active session")
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		turns, readErr := store.SecretaryTurns(ctx, identity.ID)
		if readErr == nil && len(turns) == 1 && turns[0].State == core.SecretaryTurnSucceeded {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := runtime.Stop(ctx); err != nil {
		t.Fatal(err)
	}
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
