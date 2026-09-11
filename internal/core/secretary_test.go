package core

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSecretaryIdentityPersistsAcrossRestartAndRuntimeReplacement(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReplaceSecretaryRuntime(ctx, first.ID, "fx", "model-a", "low"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	second, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.ConversationID != conversation.ID || second.RuntimeGeneration != 1 || second.RuntimeHarness != "fx" {
		t.Fatalf("identity changed: first=%#v second=%#v", first, second)
	}
	third, err := store.ReplaceSecretaryRuntime(ctx, second.ID, "codex", "model-b", "high")
	if err != nil {
		t.Fatal(err)
	}
	if third.ID != first.ID || third.RuntimeGeneration != 2 || third.RuntimeHarness != "codex" || third.RuntimeModel != "model-b" {
		t.Fatalf("replacement=%#v", third)
	}
}

func TestSecretaryQueueIsDurableOrderedAndHasPosition(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "first")
	if err != nil {
		t.Fatal(err)
	}
	if first.QueuePosition != 1 {
		t.Fatalf("first position=%d", first.QueuePosition)
	}
	started, err := store.StartSecretaryTurn(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if started.State != SecretaryTurnActive {
		t.Fatalf("started=%#v", started)
	}
	second, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "second")
	if err != nil {
		t.Fatal(err)
	}
	third, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "third")
	if err != nil {
		t.Fatal(err)
	}
	if second.QueuePosition != 1 || third.QueuePosition != 2 {
		t.Fatalf("positions=%d,%d", second.QueuePosition, third.QueuePosition)
	}
	if _, err := store.StartSecretaryTurn(ctx, second.ID); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("second started while active: %v", err)
	}
	if _, err := store.FinishSecretaryTurn(ctx, first.ID, SecretaryTurnSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	next, err := store.StartNextSecretaryTurn(ctx, identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if next.ID != second.ID || next.State != SecretaryTurnActive {
		t.Fatalf("next=%#v", next)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	durableSecond, err := store.SecretaryTurn(ctx, second.ID)
	if err != nil || durableSecond.State != SecretaryTurnActive {
		t.Fatalf("durable turn=%#v err=%v", durableSecond, err)
	}
	durableThird, err := store.SecretaryTurn(ctx, third.ID)
	if err != nil || durableThird.QueuePosition != 2 {
		t.Fatalf("durable queued turn=%#v err=%v", durableThird, err)
	}
}

func TestSecretaryStreamOrderReplayAndSafeThinkingSummary(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, turn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendSecretaryEvent(ctx, SecretaryEventInput{TurnID: turn.ID, Kind: SecretaryTextDeltaEvent, Text: "hi"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendSecretaryEvent(ctx, SecretaryEventInput{TurnID: turn.ID, Kind: SecretaryThinkingSummaryEvent, Summary: "Checking the saved conversation."}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendSecretaryEvent(ctx, SecretaryEventInput{TurnID: turn.ID, Kind: SecretaryToolCallEvent, Tool: "list_workers", Arguments: "{}"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendSecretaryEvent(ctx, SecretaryEventInput{TurnID: turn.ID, Kind: SecretaryToolResultEvent, Tool: "list_workers", Result: "[]", Status: "ok"}); err != nil {
		t.Fatal(err)
	}
	firstEvent, err := store.AppendSecretaryEvent(ctx, SecretaryEventInput{TurnID: turn.ID, Kind: SecretaryTextDeltaEvent, Text: "same", IdempotencyKey: "delta-1"})
	if err != nil {
		t.Fatal(err)
	}
	secondEvent, err := store.AppendSecretaryEvent(ctx, SecretaryEventInput{TurnID: turn.ID, Kind: SecretaryTextDeltaEvent, Text: "different", IdempotencyKey: "delta-1"})
	if err != nil || firstEvent.ID != secondEvent.ID {
		t.Fatalf("duplicate events=%#v %#v err=%v", firstEvent, secondEvent, err)
	}
	if _, err := store.FinishSecretaryTurn(ctx, turn.ID, SecretaryTurnSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	events, err := store.SecretaryEvents(ctx, turn.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{SecretaryTurnQueuedEvent, SecretaryTurnStartedEvent, SecretaryTextDeltaEvent, SecretaryThinkingSummaryEvent, SecretaryToolCallEvent, SecretaryToolResultEvent, SecretaryTextDeltaEvent, SecretaryTurnFinishedEvent}
	if len(events) != len(want) {
		t.Fatalf("events=%#v", events)
	}
	for i, event := range events {
		if event.Kind != want[i] || event.Seq <= 0 {
			t.Fatalf("event[%d]=%#v", i, event)
		}
	}
	if _, err := store.AppendSecretaryEvent(ctx, SecretaryEventInput{TurnID: turn.ID, Kind: SecretaryThinkingSummaryEvent, Summary: "raw chain-of-thought: secret reasoning"}); err == nil {
		t.Fatal("raw thinking summary accepted")
	}
	page, err := store.ReplaySecretaryEvents(ctx, turn.ID, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 3 || !page.HasMore {
		t.Fatalf("page=%#v", page)
	}
	page2, err := store.ReplaySecretaryEvents(ctx, turn.ID, page.LastReturnedSeq, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Events) != 5 {
		t.Fatalf("page2=%#v", page2)
	}
}

func TestSecretaryEventSubscriptionReplaysWithoutDuplicates(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	store := newTestStore(t)
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "stream")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := store.SubscribeSecretaryEvents(ctx, turn.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, turn.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordSecretaryTextDelta(ctx, turn.ID, "delta"); err != nil {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	for len(seen) < 3 {
		select {
		case event := <-stream:
			if seen[event.Seq] {
				t.Fatalf("duplicate event=%#v", event)
			}
			seen[event.Seq] = true
		case <-time.After(time.Second):
			t.Fatalf("stream replay incomplete: %#v", seen)
		}
	}
}

func TestConcurrentSecretaryStartsKeepOneOrderedActiveTurn(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "second")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, turn := range []SecretaryTurn{second, first} {
		wg.Add(1)
		go func(turn SecretaryTurn) {
			defer wg.Done()
			_, startErr := store.StartSecretaryTurn(ctx, turn.ID)
			results <- startErr
		}(turn)
	}
	wg.Wait()
	close(results)
	var successes int
	for startErr := range results {
		if startErr == nil {
			successes++
		} else if !errors.Is(startErr, ErrInvalidTransition) {
			t.Fatalf("start error=%v", startErr)
		}
	}
	if successes != 1 {
		t.Fatalf("successful starts=%d", successes)
	}
	active, err := store.StartNextSecretaryTurn(ctx, identity.ID)
	if !errors.Is(err, ErrInvalidTransition) || active.ID != "" {
		t.Fatalf("second active turn was allowed: %#v err=%v", active, err)
	}
}

func TestSecretaryTurnsAllowWorkersToRunConcurrently(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	secretaryTurn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "secretary")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.StartSecretaryTurn(ctx, secretaryTurn.ID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, _, attemptErr := store.CreateWorker(ctx, conversation.ID, phase4WorkerSpec(), TurnSpec{Input: "worker"})
		if attemptErr != nil {
			t.Errorf("worker create: %v", attemptErr)
		}
	}()
	wg.Wait()
	if _, err := store.FinishSecretaryTurn(ctx, secretaryTurn.ID, SecretaryTurnSucceeded, ""); err != nil {
		t.Fatal(err)
	}
}
