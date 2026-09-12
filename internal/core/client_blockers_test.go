package core

import (
	"context"
	"errors"
	"testing"
)

func TestRedeemClientDoesNotReplayAcrossPairingGenerations(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	person, _, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}

	firstPairing, err := store.PairClientWithToken(ctx, person.ID, "cross-generation", "First", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApproveClient(ctx, firstPairing.ID); err != nil {
		t.Fatal(err)
	}
	firstClient, firstCredential, err := store.RedeemClient(ctx, firstPairing.ID, firstPairing.PendingToken, "redeem-once")
	if err != nil {
		t.Fatal(err)
	}
	if firstCredential == "" {
		t.Fatal("first redeem returned empty credential")
	}

	if _, err := store.RevokeClient(ctx, firstPairing.ID); err != nil {
		t.Fatal(err)
	}
	secondPairing, err := store.PairClientWithToken(ctx, person.ID, "cross-generation", "Second", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if secondPairing.ID != firstPairing.ID || secondPairing.PendingToken == firstPairing.PendingToken {
		t.Fatal("same-device re-pair did not create a new pairing generation")
	}
	if _, _, err := store.ApproveClient(ctx, secondPairing.ID); err != nil {
		t.Fatal(err)
	}
	_, secondCredential, err := store.RedeemClient(ctx, secondPairing.ID, secondPairing.PendingToken)
	if err != nil {
		t.Fatal(err)
	}
	if secondCredential == "" || secondCredential == firstCredential {
		t.Fatal("second generation did not receive a distinct credential")
	}

	// A duplicate of the first generation must replay its durable outcome,
	// never the credential currently stored on the re-paired Client.
	duplicateClient, duplicateCredential, err := store.RedeemClient(ctx, firstPairing.ID, firstPairing.PendingToken, "redeem-once")
	if err != nil {
		t.Fatal(err)
	}
	if duplicateClient.ID != firstClient.ID || duplicateCredential != firstCredential {
		t.Fatalf("old generation replay=%#v credential=%q want=%q", duplicateClient, duplicateCredential, firstCredential)
	}
	if duplicateCredential == secondCredential {
		t.Fatal("old generation replay returned current generation credential")
	}

	if _, _, err := store.RedeemClient(ctx, secondPairing.ID, "garbage-token", "redeem-once"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("garbage token reused old idempotency key: %v", err)
	}
	if _, _, err := store.RedeemClient(ctx, secondPairing.ID, firstPairing.PendingToken, "new-redeem"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old pending token remained valid after re-pair: %v", err)
	}
}
