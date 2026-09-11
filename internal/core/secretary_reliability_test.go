package core

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSecretaryQueuePositionRemainsUniqueAfterPromotion(t *testing.T) {
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
	if _, err := store.StartSecretaryTurn(ctx, first.ID); err != nil {
		t.Fatal(err)
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
		t.Fatalf("initial queued positions=%d,%d", second.QueuePosition, third.QueuePosition)
	}
	if _, err := store.FinishSecretaryTurn(ctx, first.ID, SecretaryTurnSucceeded, ""); err != nil {
		t.Fatal(err)
	}
	startedSecond, err := store.StartNextSecretaryTurn(ctx, identity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if startedSecond.ID != second.ID {
		t.Fatalf("started=%s want=%s", startedSecond.ID, second.ID)
	}

	fourth, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "fourth")
	if err != nil {
		t.Fatalf("enqueue after queue promotion failed: %v", err)
	}
	if fourth.QueuePosition != 3 {
		t.Fatalf("fourth position=%d want=3", fourth.QueuePosition)
	}
}

func TestFinishSecretaryTurnWithResponsePersistsCanonicalConversationEntry(t *testing.T) {
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

	finished, entry, err := store.FinishSecretaryTurnWithResponse(ctx, turn.ID, SecretaryTurnSucceeded, "", "final answer")
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != SecretaryTurnSucceeded || entry.Kind != EntrySecretary || entry.Body != "final answer" || entry.ConversationID != conversation.ID {
		t.Fatalf("finished=%#v entry=%#v", finished, entry)
	}

	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ID != entry.ID {
		t.Fatalf("conversation entries=%#v", entries)
	}
	deliveries, err := store.DeliveriesForEntry(ctx, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deliveries) != 1 {
		t.Fatalf("response deliveries=%#v", deliveries)
	}

	events, err := store.SecretaryEvents(ctx, turn.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var linked bool
	for _, event := range events {
		if event.Kind != SecretaryTurnFinishedEvent {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		linked = payload["conversation_entry_id"] == entry.ID
	}
	if !linked {
		t.Fatal("Secretary terminal event was not linked to the canonical conversation entry")
	}
}
