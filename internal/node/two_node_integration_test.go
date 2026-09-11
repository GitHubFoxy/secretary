package node

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestTwoDaemonsPairWithOneServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverStore, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer serverStore.Close()
	manager, err := NewServerManager(ctx, serverStore, "pair-token", "admin-token")
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	server := httptest.NewServer(mux)
	defer server.Close()

	macbookIdentity, err := EnrollNode(ctx, server.Client(), server.URL, "pair-token", "macbook")
	if err != nil {
		t.Fatal(err)
	}
	homeIdentity, err := EnrollNode(ctx, server.Client(), server.URL, "pair-token", "home-server")
	if err != nil {
		t.Fatal(err)
	}
	if macbookIdentity.Credential == homeIdentity.Credential {
		t.Fatal("different Nodes received the same transport credential")
	}

	macbookStore, err := OpenLocalStore(filepath.Join(t.TempDir(), "macbook-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer macbookStore.Close()
	homeStore, err := OpenLocalStore(filepath.Join(t.TempDir(), "home-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer homeStore.Close()

	newDaemon := func(identity NodeIdentity, local *LocalStore) *Daemon {
		return &Daemon{
			Identity: identity,
			Store: local,
			Runtime: &daemonCountingRuntime{},
			Inventory: daemonStaticInventory{snapshot: daemonInventoryFixture(identity.Node)},
			HeartbeatInterval: 20 * time.Millisecond,
			InventoryInterval: time.Hour,
			OutboxPollInterval: 10 * time.Millisecond,
			ReconnectMin: 10 * time.Millisecond,
			ReconnectMax: 40 * time.Millisecond,
		}
	}

	daemonCtx, stopDaemons := context.WithCancel(ctx)
	defer stopDaemons()
	done := make(chan error, 2)
	go func() { done <- newDaemon(macbookIdentity, macbookStore).Run(daemonCtx) }()
	go func() { done <- newDaemon(homeIdentity, homeStore).Run(daemonCtx) }()

	waitFor(t, ctx, "both Nodes online", func() bool {
		macbook, macErr := manager.Status(context.Background(), "macbook")
		home, homeErr := manager.Status(context.Background(), "home-server")
		return macErr == nil && homeErr == nil &&
			macbook.Online && home.Online &&
			macbook.Inventory.Node == "macbook" && home.Inventory.Node == "home-server" &&
			len(macbook.Inventory.Instances) == 1 && len(home.Inventory.Instances) == 1
	})

	statuses, err := manager.Statuses(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(statuses) != 2 {
		t.Fatalf("server sees %d Nodes, want 2: %#v", len(statuses), statuses)
	}

	stopDaemons()
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("daemon exit: %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("daemon did not stop")
		}
	}
}
