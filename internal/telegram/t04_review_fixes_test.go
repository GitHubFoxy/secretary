package telegram

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestT04UpgradeLegacyForumPreservesState(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.json")
	transport := &fakeTransport{}
	legacy, err := New(Config{StatePath: path, OwnerChatID: -200}, transport, &fakeServer{})
	if err != nil {
		t.Fatal(err)
	}
	// Old forum-only deployments need neither a DM nor the new group option.
	if err := legacy.HandleDurableEvent(ctx, Event{Sequence: 12, Kind: "worker.created", WorkerRef: "legacy-worker", Title: "Legacy"}); err != nil {
		t.Fatal(err)
	}
	legacy.mu.Lock()
	legacy.state.OwnerUser = 100
	legacy.state.Outbox = append(legacy.state.Outbox, OutgoingMessage{ChatID: -200, Text: "queued", Identity: "queued-legacy"})
	if err := legacy.saveLocked(); err != nil {
		t.Fatal(err)
	}
	legacy.mu.Unlock()

	group := int64(0)
	upgraded, err := New(Config{StatePath: path, OwnerChatID: 100, GroupChatID: &group}, transport, &fakeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.state.OwnerChat != 100 || upgraded.state.GroupChat != -200 || upgraded.state.OwnerUser != 100 {
		t.Fatalf("migration identity: %#v", upgraded.state)
	}
	if got := upgraded.state.Topics["legacy-worker"]; got.ChatID != -200 {
		t.Fatalf("legacy topic lost: %#v", got)
	}
	if upgraded.state.LastEventSeq != 12 || len(upgraded.state.Outbox) != 1 {
		t.Fatalf("migration reset durable cursor/outbox: cursor=%d outbox=%#v", upgraded.state.LastEventSeq, upgraded.state.Outbox)
	}
	again, err := New(Config{StatePath: path, OwnerChatID: 100, GroupChatID: &group}, transport, &fakeServer{})
	if err != nil || again.state.OwnerChat != 100 || again.state.GroupChat != -200 {
		t.Fatalf("migration was not persistent/idempotent: state=%#v err=%v", again.state, err)
	}
}

func TestT04LegacyForumWithoutExplicitPrivateOwnerStillWorks(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.json")
	group := int64(0)
	transport := &fakeTransport{}
	old, err := New(Config{StatePath: path, OwnerChatID: -200}, transport, &fakeServer{})
	if err != nil {
		t.Fatal(err)
	}
	_ = old
	restarted, err := New(Config{StatePath: path, GroupChatID: &group}, transport, &fakeServer{})
	if err != nil {
		t.Fatal(err)
	}
	if restarted.config.GroupChatID != nil {
		t.Fatal("existing negative OwnerChat should remain in legacy mode until explicitly upgraded")
	}
	if err := restarted.HandleEvent(ctx, Event{Kind: "worker.created", WorkerRef: "w1", Title: "legacy forum"}); err != nil {
		t.Fatal(err)
	}
	if len(transport.topics) != 1 || transport.topics[0].ChatID != -200 {
		t.Fatalf("existing forum routing was lost: %#v", transport.topics)
	}
}

func TestT04UpgradeRefusesDifferentPairedTelegramOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	legacy, err := New(Config{StatePath: path, OwnerChatID: -200}, &fakeTransport{}, &fakeServer{})
	if err != nil {
		t.Fatal(err)
	}
	legacy.mu.Lock()
	legacy.state.OwnerUser = 555
	if err := legacy.saveLocked(); err != nil {
		t.Fatal(err)
	}
	legacy.mu.Unlock()
	group := int64(0)
	if _, err := New(Config{StatePath: path, OwnerChatID: 100, GroupChatID: &group}, &fakeTransport{}, &fakeServer{}); err == nil {
		t.Fatal("migration must not grant a different user control of the paired bot")
	}
	stillLegacy, err := New(Config{StatePath: path, OwnerChatID: -200}, &fakeTransport{}, &fakeServer{})
	if err != nil || stillLegacy.state.OwnerChat != -200 || stillLegacy.state.OwnerUser != 555 {
		t.Fatalf("failed migration mutated legacy state: state=%#v err=%v", stillLegacy.state, err)
	}
}

func TestT04ReceiptsSurviveFailedMirrorAndCompactAfterCursor(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.json")
	transport := &t04FlakyTransport{failAt: 2}
	server := &fakeServer{}
	group := int64(-200)
	adapter := newT04Adapter(t, path, transport, server, group)
	first := Event{Kind: "message.saved", EventID: "web-1", Sequence: 1, Source: "web", Text: "first"}
	if err := adapter.HandleDurableEvent(ctx, first); err == nil {
		t.Fatal("expected interrupted group delivery")
	}
	if adapter.LastEventSeq() != 0 || len(adapter.state.Delivered) != 1 || len(adapter.state.Outbox) != 1 {
		t.Fatalf("partial send lost dedupe safety: cursor=%d delivered=%#v outbox=%#v", adapter.LastEventSeq(), adapter.state.Delivered, adapter.state.Outbox)
	}
	transport.failAt = 0
	adapter = newT04Adapter(t, path, transport, server, group)
	if err := adapter.HandleDurableEvent(ctx, first); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 2 || transport.sent[0].ChatID != 100 || transport.sent[1].ChatID != -200 {
		t.Fatalf("duplicate or missing mirror on reconnect: %#v", transport.sent)
	}
	if adapter.LastEventSeq() != 1 || len(adapter.state.Delivered) != 0 || len(adapter.state.DeliveredSequence) != 0 {
		t.Fatalf("completed durable receipts were not compacted: %#v %#v", adapter.state.Delivered, adapter.state.DeliveredSequence)
	}
	for n := int64(2); n <= 120; n++ {
		event := Event{Kind: "message.saved", EventID: fmt.Sprintf("web-%d", n), Sequence: n, Source: "web", Text: fmt.Sprintf("message-%d", n)}
		if err := adapter.HandleDurableEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
		if len(adapter.state.Delivered) != 0 || len(adapter.state.DeliveredSequence) != 0 {
			t.Fatalf("receipt growth at cursor %d: count=%d", n, len(adapter.state.Delivered))
		}
	}
	adapter = newT04Adapter(t, path, transport, server, group)
	if err := adapter.HandleDurableEvent(ctx, first); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 240 {
		t.Fatalf("old durable event replayed after compaction: %d sends", len(transport.sent))
	}
}

func TestT04SecretaryBatchReceiptProtectedUntilBothChatsAndPendingClear(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.json")
	group := int64(-200)
	transport := &t04FlakyTransport{failAt: 2}
	adapter := newT04Adapter(t, path, transport, &fakeServer{}, group)
	for _, event := range []Event{
		{Sequence: 1, Kind: "secretary.turn.started"},
		{Sequence: 2, Kind: "secretary.text_delta", Text: "reply"},
		{Sequence: 3, Kind: "secretary.turn.finished"},
	} {
		if err := adapter.HandleDurableEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if err := adapter.Flush(ctx); err == nil {
		t.Fatal("expected interrupted Secretary group mirror")
	}
	if len(adapter.state.Delivered) != 1 || len(adapter.pending.EventSeqs) == 0 || adapter.LastEventSeq() != 3 {
		t.Fatalf("premature receipt compaction while batch pending: delivered=%#v pending=%#v", adapter.state.Delivered, adapter.pending.EventSeqs)
	}
	transport.failAt = 0
	adapter = newT04Adapter(t, path, transport, &fakeServer{}, group)
	if err := adapter.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if len(transport.sent) != 2 || transport.sent[0].ChatID != 100 || transport.sent[1].ChatID != -200 {
		t.Fatalf("Secretary batch resent delivered DM after restart: %#v", transport.sent)
	}
	if len(adapter.state.Delivered) != 0 || len(adapter.pending.EventSeqs) != 0 {
		t.Fatalf("completed batch receipts/pending were not compacted: %#v", adapter.state.Delivered)
	}
}

func TestT04ReceiptCompactionRespectsPendingOutbox(t *testing.T) {
	adapter := newT04Adapter(t, filepath.Join(t.TempDir(), "state.json"), &fakeTransport{}, &fakeServer{}, -200)
	msg := OutgoingMessage{ChatID: -200, Identity: "event-1", Text: "text", DurableSequence: 4}
	key := outgoingMessageIdentity(msg)
	adapter.mu.Lock()
	adapter.state.LastEventSeq = 4
	adapter.state.Delivered[key] = time.Now().UTC()
	adapter.state.DeliveredSequence[key] = 4
	adapter.state.Outbox = append(adapter.state.Outbox, msg)
	adapter.compactDeliveredLocked()
	if _, exists := adapter.state.Delivered[key]; !exists {
		t.Fatal("receipt for pending outbox was prematurely dropped")
	}
	adapter.state.Outbox = nil
	adapter.compactDeliveredLocked()
	if _, exists := adapter.state.Delivered[key]; exists {
		t.Fatal("receipt was retained after outbox was cleared and cursor advanced")
	}
	adapter.mu.Unlock()
}

func TestT04ReceiptCompactionDoesNotDiscardLegacyUnattributedReceipt(t *testing.T) {
	adapter := newT04Adapter(t, filepath.Join(t.TempDir(), "state.json"), &fakeTransport{}, &fakeServer{}, -200)
	adapter.mu.Lock()
	adapter.state.Delivered["legacy"] = time.Now().UTC()
	adapter.state.LastEventSeq = 100
	adapter.compactDeliveredLocked()
	_, kept := adapter.state.Delivered["legacy"]
	adapter.mu.Unlock()
	if !kept {
		t.Fatal("unattributed pre-upgrade receipt cannot be safely tied to a cursor")
	}
}

var _ = errors.New
