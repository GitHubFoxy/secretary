package secretary

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

// These synthetic chronology tests distinguish fixed orphan/replay handling
// from the still-blocked ticket29 originating mixed-response contract. They
// do not assert that a model followed a profile or that Telegram accepted it.
func TestResultOriginLifecycleAndRemainingAmbiguity(t *testing.T) {
	for _, order := range []string{"fast-result", "slow-result"} {
		t.Run(order, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "secretary.db")
			store, err := core.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = store.Close() }()
			person, conversation, err := store.CreatePersonWithConversation(ctx)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "synthetic owner"); err != nil {
				t.Fatal(err)
			}
			setRuntimeTestPolicy(t, store)
			origin, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "dispatch two tasks and answer an independent question")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = store.StartSecretaryTurn(ctx, origin.ID); err != nil {
				t.Fatal(err)
			}
			attempts := make([]core.Phase4Attempt, 0, 2)
			for _, ref := range []string{"worker-a", "worker-b"} {
				_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: ref, Intent: "synthetic work", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "synthetic"}, core.TurnSpec{Input: "work"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
					t.Fatal(err)
				}
				attempts = append(attempts, attempt)
			}
			finishWorkers := func() {
				for _, attempt := range attempts {
					input := core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "synthetic Worker Result"}
					_, result, duplicate, err := store.RecordAttemptOutcome(ctx, attempt.ID, input)
					if err != nil || result == nil || duplicate {
						t.Fatal("first Result identity was not accepted")
					}
					_, retry, duplicate, err := store.RecordAttemptOutcome(ctx, attempt.ID, input)
					if err != nil || retry == nil || !duplicate || retry.ID != result.ID {
						t.Fatal("event retry changed durable Result identity")
					}
				}
			}
			if order == "fast-result" {
				finishWorkers()
			}
			runtime := NewRuntime(nil, "synthetic-capability")
			runtime.AttachConversation(store, conversation.ID)
			runtime.AttachIdentity(identity)
			consume := func(turnID, summary string, copies int) {
				session := &fakeSession{results: make(chan node.Result, copies)}
				runtime.session = session
				runtime.activeTurnID = turnID
				runtime.busy = turnID != ""
				for i := 0; i < copies; i++ {
					session.results <- node.Result{Status: "succeeded", Summary: summary}
				}
				close(session.results)
				runtime.consumeResults(session)
			}
			// A legitimate direct answer AFTER dispatch must remain available.
			// The same lifecycle would also accept a narration/echo here: ACP
			// Summary has no reply/task/result scope. This is the pending blocker.
			consume(origin.ID, "independent answer after dispatch", 2)
			if order == "slow-result" {
				finishWorkers()
			}
			entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			workerCount, secretaryCount := 0, 0
			for _, entry := range entries {
				if entry.Kind == core.EntryWorkerResult {
					workerCount++
				}
				if entry.Kind == core.EntrySecretary {
					secretaryCount++
				}
			}
			if workerCount != 2 || secretaryCount != 1 {
				t.Fatal("durable retry/orphan produced a duplicate entry")
			}
			turns, err := store.SecretaryTurns(ctx, identity.ID)
			if err != nil || len(turns) != 1 {
				t.Fatal("Worker completion created an automatic Secretary turn")
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			store, err = core.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			runtime.AttachConversation(store, conversation.ID)
			consume("", "orphan after restart", 2)
			newTurn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "explain the Results")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = store.StartSecretaryTurn(ctx, newTurn.ID); err != nil {
				t.Fatal(err)
			}
			consume(newTurn.ID, "normal distinct user turn response", 1)
			entries, err = store.EntriesAfter(ctx, conversation.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			secretaryCount = 0
			for _, entry := range entries {
				if entry.Kind == core.EntrySecretary {
					secretaryCount++
				}
			}
			if secretaryCount != 2 {
				t.Fatal("orphan replay added a reply or new user reply was suppressed")
			}
		})
	}
}

func TestTicket29OriginatingEchoBlockerIsReproducible(t *testing.T) {
	ctx := context.Background()
	tmp := t.TempDir()
	store, err := core.Open(ctx, filepath.Join(tmp, "secretary.db"))
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
	dataDir := filepath.Join(tmp, "data")
	if _, err := store.SaveUserDocument(ctx, filepath.Join(dataDir, "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	setRuntimeTestPolicy(t, store)
	harness := &fakeRuntime{}
	runtime := NewRuntime(node.NewLocal(harness), "synthetic-capability")
	runtime.AttachConversation(store, conversation.ID)
	runtime.AttachIdentity(identity)
	runtime.AttachMCP("", dataDir)
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	harness.session.promptResult = false
	if err := runtime.HandleMessage(ctx, "dispatch work"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-harness.session.prompts:
	case <-time.After(time.Second):
		t.Fatal("public Runtime did not start the queued user turn")
	}
	turns, err := store.SecretaryTurns(ctx, identity.ID)
	if err != nil || len(turns) != 1 {
		t.Fatalf("origin turn not durable: turns=%#v err=%v", turns, err)
	}
	origin := turns[0]
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		turn, loadErr := store.SecretaryTurn(ctx, origin.ID)
		if loadErr == nil && turn.PromptState == "accepted" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	turn, err := store.SecretaryTurn(ctx, origin.ID)
	if err != nil || turn.PromptState != "accepted" {
		t.Fatalf("origin prompt was not accepted: turn=%#v err=%v", turn, err)
	}
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "worker", Intent: "work", ProjectID: "p", NodeID: "n", HarnessInstanceID: "n/fx", PolicySnapshot: "synthetic"}, core.TurnSpec{Input: "work"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "synthetic result"}); err != nil {
		t.Fatal(err)
	}
	// The harness Result arrives while the original user-owned turn is active.
	harness.session.results <- node.Result{Status: "succeeded", Summary: "synthetic result"}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		entries, readErr := store.EntriesAfter(ctx, conversation.ID, 0)
		if readErr == nil && len(entries) == 2 {
			worker, secretary := 0, 0
			for _, entry := range entries {
				if entry.Kind == core.EntryWorkerResult {
					worker++
				}
				if entry.Kind == core.EntrySecretary && entry.Body == "synthetic result" {
					secretary++
				}
			}
			if worker == 1 && secretary == 1 {
				t.Log("known blocker reproduced through public Runtime: canonical_results=1 originating_untyped_replies=1; not a fixed-echo acceptance")
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	entries, _ := store.EntriesAfter(ctx, conversation.ID, 0)
	t.Fatalf("public Runtime did not reproduce canonical Result plus originating echo: %#v", entries)
}
