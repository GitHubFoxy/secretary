package core

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestNodeRegistryLifecycleAndTransportSecretPersist(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	secret1, err := store.EnsureNodeTransportSecret(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(secret1) < 32 {
		t.Fatalf("transport secret too short: %d", len(secret1))
	}

	record, err := store.EnrollNode(ctx, "macbook")
	if err != nil {
		t.Fatal(err)
	}
	if record.Node != "macbook" || record.Online || record.Draining || record.Revoked {
		t.Fatalf("unexpected enrolled Node: %#v", record)
	}
	if _, err := store.EnrollNode(ctx, "macbook"); !errors.Is(err, ErrNodeAlreadyEnrolled) {
		t.Fatalf("duplicate enrollment error=%v", err)
	}

	inventory := registryInventoryFixture("macbook", time.Now().UTC())
	if err := store.MarkNodeConnected(ctx, "macbook", inventory); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateNodeHeartbeat(ctx, "macbook", inventory); err != nil {
		t.Fatal(err)
	}
	record, err = store.NodeRecord(ctx, "macbook")
	if err != nil {
		t.Fatal(err)
	}
	if !record.Online || record.LastHeartbeatAt.IsZero() || record.Inventory.Node != "macbook" || len(record.Inventory.Instances) != 1 {
		t.Fatalf("heartbeat was not persisted: %#v", record)
	}

	record, err = store.SetNodeDraining(ctx, "macbook", true)
	if err != nil {
		t.Fatal(err)
	}
	if !record.Draining || !record.Online {
		t.Fatalf("drain state=%#v", record)
	}
	record, err = store.RevokeNode(ctx, "macbook")
	if err != nil {
		t.Fatal(err)
	}
	if !record.Revoked || !record.Draining || record.Online {
		t.Fatalf("revoke state=%#v", record)
	}
	if err := store.MarkNodeConnected(ctx, "macbook", inventory); !errors.Is(err, ErrNodeRevoked) {
		t.Fatalf("revoked Node reconnected: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	secret2, err := reopened.EnsureNodeTransportSecret(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(secret1, secret2) {
		t.Fatal("Node transport secret changed across server restart")
	}
	record, err = reopened.NodeRecord(ctx, "macbook")
	if err != nil {
		t.Fatal(err)
	}
	if !record.Revoked || record.Online {
		t.Fatalf("durable Node state after reopen=%#v", record)
	}
}

func TestNodeRegistryMarksConnectedNodesOfflineOnServerRestart(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.EnrollNode(ctx, "home-server"); err != nil {
		t.Fatal(err)
	}
	inventory := registryInventoryFixture("home-server", time.Now().UTC())
	if err := store.MarkNodeConnected(ctx, "home-server", inventory); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkAllNodesOffline(ctx); err != nil {
		t.Fatal(err)
	}
	record, err := store.NodeRecord(ctx, "home-server")
	if err != nil {
		t.Fatal(err)
	}
	if record.Online {
		t.Fatalf("Node remained online after server restart: %#v", record)
	}
}

func registryInventoryFixture(node NodeReference, observedAt time.Time) HarnessInventorySnapshot {
	return HarnessInventorySnapshot{
		Node: node,
		ObservedAt: observedAt,
		Instances: []HarnessInstance{{
			ID: HarnessInstanceID(string(node) + "/fx"), Node: node, Kind: HarnessFX, Version: "1.2.3",
			Authentication: HarnessAuthentication{Authenticated: true, Method: "local"}, Status: HarnessReady,
			Capabilities: HarnessCapabilities{Execution: []ExecutionCapability{CapabilityCancel}, Activity: []ActivityCapability{ActivityStatus}},
		}},
	}
}
