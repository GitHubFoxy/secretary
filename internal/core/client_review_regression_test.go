package core

import (
	"context"
	"testing"
)

func TestApproveClientRollsBackOnEventIntegrityFailure(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	person, _, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	client, err := store.PairClient(ctx, person.ID, "device-event", "Event", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER fail_client_event BEFORE INSERT ON events WHEN NEW.kind = 'client.connected' BEGIN SELECT RAISE(ABORT, 'event integrity failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApproveClient(ctx, client.ID); err == nil {
		t.Fatal("expected event integrity failure")
	}
	stored, err := store.Client(ctx, client.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != ClientPending {
		t.Fatalf("client state survived rolled back approval: %#v", stored)
	}
}

func TestPairClientRollsBackClientAndPairingOnPairingIntegrityFailure(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, "file:client-pair-rollback?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, _, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `CREATE TRIGGER fail_client_pairing BEFORE INSERT ON client_pairings BEGIN SELECT RAISE(ABORT, 'pairing integrity failure'); END`); err != nil {
		t.Fatal(err)
	}
	_, err = store.PairClient(ctx, person.ID, "device-rollback", "Rollback", "test", nil)
	if err == nil {
		t.Fatal("expected pairing integrity failure")
	}
	clients, err := store.Clients(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 0 {
		t.Fatalf("client row survived rolled back pairing: %#v", clients)
	}
}
