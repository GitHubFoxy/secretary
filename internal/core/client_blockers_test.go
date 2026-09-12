package core

import (
	"context"
	"errors"
	"testing"
)

func TestClientApproveIdempotencyDoesNotReplayCurrentGenerationCredential(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	person, _, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	firstPairing, err := store.PairClientWithToken(ctx, person.ID, "approve-cross-generation", "First", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	firstClient, firstCredential, err := store.ApproveClient(ctx, firstPairing.ID, "approve-once")
	if err != nil || firstCredential == "" {
		t.Fatalf("first approve client=%#v credential=%q err=%v", firstClient, firstCredential, err)
	}
	if _, err := store.RevokeClient(ctx, firstPairing.ID); err != nil {
		t.Fatal(err)
	}
	secondPairing, err := store.PairClientWithToken(ctx, person.ID, "approve-cross-generation", "Second", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, secondCredential, err := store.ApproveClient(ctx, secondPairing.ID, "approve-new-generation")
	if err != nil || secondCredential == "" || secondCredential == firstCredential {
		t.Fatalf("second approve credential=%q err=%v", secondCredential, err)
	}
	replayedClient, replayedCredential, err := store.ApproveClient(ctx, firstPairing.ID, "approve-once")
	if err != nil {
		t.Fatal(err)
	}
	if replayedClient.ID != firstClient.ID || replayedCredential != firstCredential {
		t.Fatalf("old approve replay client=%#v credential=%q want=%q", replayedClient, replayedCredential, firstCredential)
	}
	if replayedCredential == secondCredential {
		t.Fatal("old approve replay returned current generation credential")
	}
}

func TestClientPairIdempotencyDoesNotReplayCurrentGenerationPendingToken(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	person, _, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.PairClientWithToken(ctx, person.ID, "pair-cross-generation", "Same", "test", nil, "pair-once")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RevokeClient(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	second, err := store.PairClientWithToken(ctx, person.ID, "pair-cross-generation", "Changed", "test", nil, "pair-new-generation")
	if err != nil {
		t.Fatal(err)
	}
	if second.PendingToken == "" || second.PendingToken == first.PendingToken {
		t.Fatal("re-pair did not create a distinct pending token")
	}
	replayed, err := store.PairClientWithToken(ctx, person.ID, "pair-cross-generation", "Same", "test", nil, "pair-once")
	if err != nil {
		t.Fatal(err)
	}
	if replayed.ID != first.ID || replayed.PendingToken != first.PendingToken {
		t.Fatalf("old pair replay=%#v want token=%q", replayed, first.PendingToken)
	}
	if replayed.PendingToken == second.PendingToken {
		t.Fatal("old pair replay returned current generation pending token")
	}
}

func TestClientPairConcurrentDuplicateHasOneEffectAndExactOutcome(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	person, _, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const callers = 12
	type result struct {
		pairing ClientPairing
		err     error
	}
	results := make(chan result, callers)
	for i := 0; i < callers; i++ {
		go func() {
			pairing, callErr := store.PairClientWithToken(ctx, person.ID, "pair-concurrent", "Concurrent", "test", nil, "pair-concurrent-key")
			results <- result{pairing: pairing, err: callErr}
		}()
	}
	var expected ClientPairing
	for i := 0; i < callers; i++ {
		current := <-results
		if current.err != nil {
			t.Fatal(current.err)
		}
		if expected.PendingToken == "" {
			expected = current.pairing
		} else if current.pairing.ID != expected.ID || current.pairing.PendingToken != expected.PendingToken {
			t.Fatalf("concurrent pair outcomes differ: %#v != %#v", current.pairing, expected)
		}
	}
	var paired int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE kind = 'client.paired' AND aggregate_id = ?`, expected.ID).Scan(&paired); err != nil {
		t.Fatal(err)
	}
	if paired != 1 {
		t.Fatalf("concurrent pair effects=%d want=1", paired)
	}
}

func TestClientApproveConcurrentDuplicateHasOneEffectAndExactOutcome(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	person, _, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := store.PairClientWithToken(ctx, person.ID, "approve-concurrent", "Concurrent", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	const callers = 12
	credentials := make(chan string, callers)
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() {
			_, credential, callErr := store.ApproveClient(ctx, pairing.ID, "approve-concurrent-key")
			credentials <- credential
			errs <- callErr
		}()
	}
	var expected string
	for i := 0; i < callers; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		credential := <-credentials
		if expected == "" {
			expected = credential
		} else if credential != expected {
			t.Fatalf("concurrent approve outcomes differ: %q != %q", credential, expected)
		}
	}
	var connected int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE kind = 'client.connected' AND aggregate_id = ?`, pairing.ID).Scan(&connected); err != nil {
		t.Fatal(err)
	}
	if connected != 1 {
		t.Fatalf("concurrent approve effects=%d want=1", connected)
	}
}

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
