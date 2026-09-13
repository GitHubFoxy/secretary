package node

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestTicket13bHandshakePersistsPerNodeWorkspacesAndRejectsMismatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	project, err := store.CreateProject(ctx, core.ProjectSpec{
		ID: "frontend", Name: "Frontend", Mappings: []core.ProjectPathMapping{
			{Node: "macbook", Path: "/Users/alice/src/frontend"},
			{Node: "home-server", Path: "/srv/src/frontend"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewServerManagerWithConfig(ctx, store, ServerConfig{PairingTokens: []string{"mac-pair", "home-pair"}, AdminToken: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	server := httptest.NewServer(mux)
	defer server.Close()

	mac, err := EnrollNode(ctx, server.Client(), server.URL, "mac-pair", "macbook")
	if err != nil {
		t.Fatal(err)
	}
	home, err := EnrollNode(ctx, server.Client(), server.URL, "home-pair", "home-server")
	if err != nil {
		t.Fatal(err)
	}
	macConn := dialEnrolledNodeWithWorkspaces(t, ctx, mac, daemonInventoryFixture("macbook"), []Workspace{{ProjectID: project.ID, Path: "/Users/alice/src/frontend"}}, "mac-nonce")
	defer macConn.Close()
	homeConn := dialEnrolledNodeWithWorkspaces(t, ctx, home, daemonInventoryFixture("home-server"), []Workspace{{ProjectID: project.ID, Path: "/srv/src/frontend"}}, "home-nonce")
	defer homeConn.Close()

	macRecord, err := store.NodeRecord(ctx, "macbook")
	if err != nil {
		t.Fatal(err)
	}
	if !nodeRecordHasWorkspace(macRecord, project.ID, "/Users/alice/src/frontend") {
		t.Fatalf("macbook workspace registry=%#v", macRecord)
	}
	homeRecord, err := store.NodeRecord(ctx, "home-server")
	if err != nil {
		t.Fatal(err)
	}
	if !nodeRecordHasWorkspace(homeRecord, project.ID, "/srv/src/frontend") {
		t.Fatalf("home workspace registry=%#v", homeRecord)
	}
	resolved, err := store.ResolveProjectDispatch(ctx, core.ProjectDispatchRequest{ProjectID: project.ID, NodeID: "macbook", HarnessInstanceID: "macbook/fx"})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Workspace != "/Users/alice/src/frontend" {
		t.Fatalf("dispatch workspace=%q", resolved.Workspace)
	}

	if err := macConn.Close(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, ctx, "macbook disconnect", func() bool {
		record, recordErr := store.NodeRecord(context.Background(), "macbook")
		return recordErr == nil && !record.Online
	})
	macConn = dialEnrolledNodeWithWorkspaces(t, ctx, mac, daemonInventoryFixture("macbook"), []Workspace{{ProjectID: project.ID, Path: "/Users/alice/src/frontend"}}, "mac-reconnect")
	defer macConn.Close()
	macRecord, err = store.NodeRecord(ctx, "macbook")
	if err != nil {
		t.Fatal(err)
	}
	if !nodeRecordHasWorkspace(macRecord, project.ID, "/Users/alice/src/frontend") {
		t.Fatalf("workspace mapping was not durable across reconnect: %#v", macRecord)
	}

	if _, err := dialEnrolledNodeWithWorkspacesResult(ctx, home, daemonInventoryFixture("home-server"), []Workspace{{ProjectID: project.ID, Path: "/Users/alice/src/frontend"}}, "home-mismatch"); err == nil {
		t.Fatal("handshake accepted a workspace path belonging to another Node")
	}
}

func dialEnrolledNodeWithWorkspaces(t *testing.T, ctx context.Context, identity NodeIdentity, inventory core.HarnessInventorySnapshot, workspaces []Workspace, nonce string) *ProtocolConnection {
	t.Helper()
	connection, err := dialEnrolledNodeWithWorkspacesResult(ctx, identity, inventory, workspaces, nonce)
	if err != nil {
		t.Fatal(err)
	}
	return connection
}

func dialEnrolledNodeWithWorkspacesResult(ctx context.Context, identity NodeIdentity, inventory core.HarnessInventorySnapshot, workspaces []Workspace, nonce string) (*ProtocolConnection, error) {
	auth, err := identity.Authenticator()
	if err != nil {
		return nil, err
	}
	return DialProtocol(ctx, identity.ConnectURL, identity.Node, auth, Handshake{Node: identity.Node, ProtocolVersion: ProtocolVersion, Inventory: inventory, Workspaces: workspaces, Nonce: nonce})
}

func TestTicket13bRevokeRefusesActiveNodeUnlessForce(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err := NewServerManagerWithConfig(ctx, store, ServerConfig{PairingTokens: []string{"pair"}, AdminToken: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnrollNodeWithPairing(ctx, "pair", "draining-node", bytesOf(32, 'x')); err != nil {
		t.Fatal(err)
	}
	inventory := daemonInventoryFixture("draining-node")
	if err := store.UpdateNodeHeartbeat(ctx, "draining-node", inventory, core.NodeHeartbeat{Capacity: 1, ActiveAttempts: []core.NodeActiveAttempt{{AttemptID: "active"}}}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/nodes/draining-node/revoke", nil)
	request.Header.Set("Authorization", "Bearer admin")
	response := httptest.NewRecorder()
	manager.ServeHTTP(response, request)
	if response.Code == http.StatusOK {
		t.Fatal("revoke accepted active Node without force")
	}
	record, err := store.NodeRecord(ctx, "draining-node")
	if err != nil {
		t.Fatal(err)
	}
	if record.Revoked {
		t.Fatal("active Node was revoked after refusal")
	}

	request = httptest.NewRequest(http.MethodPost, "/v1/nodes/draining-node/revoke", stringsReader(`{"force":true}`))
	request.Header.Set("Authorization", "Bearer admin")
	response = httptest.NewRecorder()
	manager.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("forced revoke status=%d body=%s", response.Code, response.Body.String())
	}
	record, err = store.NodeRecord(ctx, "draining-node")
	if err != nil {
		t.Fatal(err)
	}
	if !record.Revoked {
		t.Fatal("forced revoke did not revoke Node")
	}
}

func stringsReader(value string) *strings.Reader { return strings.NewReader(value) }

func nodeRecordHasWorkspace(record core.NodeRecord, projectID, path string) bool {
	encoded, err := json.Marshal(record)
	if err != nil {
		return false
	}
	return strings.Contains(string(encoded), `"project_id":"`+projectID+`"`) && strings.Contains(string(encoded), `"path":"`+path+`"`)
}
