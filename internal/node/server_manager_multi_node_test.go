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

func TestServerManagerTracksTwoIndependentNodesAndDisconnects(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err := NewServerManagerWithConfig(ctx, store, ServerConfig{PairingTokens: []string{"pair-token-macbook", "pair-token-home"}, AdminToken: "admin-token"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	server := httptest.NewServer(mux)
	defer server.Close()

	macbook, err := EnrollNode(ctx, server.Client(), server.URL, "pair-token-macbook", "macbook")
	if err != nil {
		t.Fatal(err)
	}
	home, err := EnrollNode(ctx, server.Client(), server.URL, "pair-token-home", "home-server")
	if err != nil {
		t.Fatal(err)
	}
	macConnection := dialEnrolledNode(t, ctx, macbook, daemonInventoryFixture("macbook"), "macbook-nonce")
	homeConnection := dialEnrolledNode(t, ctx, home, daemonInventoryFixture("home-server"), "home-nonce")
	defer homeConnection.Close()

	waitFor(t, ctx, "two Nodes online", func() bool {
		macStatus, macErr := manager.Status(context.Background(), "macbook")
		homeStatus, homeErr := manager.Status(context.Background(), "home-server")
		return macErr == nil && homeErr == nil && macStatus.Online && homeStatus.Online && macStatus.Inventory.Node == "macbook" && homeStatus.Inventory.Node == "home-server"
	})

	if err := macConnection.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, ctx, "MacBook Node offline", func() bool {
		macStatus, macErr := manager.Status(context.Background(), "macbook")
		homeStatus, homeErr := manager.Status(context.Background(), "home-server")
		return macErr == nil && homeErr == nil && !macStatus.Online && homeStatus.Online
	})
}

func dialEnrolledNode(t *testing.T, ctx context.Context, identity NodeIdentity, inventory core.HarnessInventorySnapshot, nonce string) *ProtocolConnection {
	t.Helper()
	auth, err := identity.Authenticator()
	if err != nil {
		t.Fatal(err)
	}
	connection, err := DialProtocol(ctx, identity.ConnectURL, identity.Node, auth, Handshake{
		Node: identity.Node, ProtocolVersion: ProtocolVersion, Inventory: inventory, Nonce: nonce,
	})
	if err != nil {
		t.Fatal(err)
	}
	return connection
}
