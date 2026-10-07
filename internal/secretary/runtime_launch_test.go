package secretary

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

func TestRuntimeLaunchGenerationAndReloadRequiresExplicitRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, person.ID, conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	cap, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	if _, err := store.SaveUserDocument(ctx, filepath.Join(dataDir, "user.md"), "synthetic"); err != nil {
		t.Fatal(err)
	}
	gate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer gate.Close()
	var mu sync.Mutex
	profile := node.ManagedProfile{Name: "secretary", Version: "v1", Hash: "h1", Content: "delivered profile one", Runtime: "opencode", Model: "fixture/model", Reasoning: "xhigh", ReplyContractVersion: core.SecretaryReplyContractAddressedV1}
	runtime := NewRuntime(node.NewLocal(node.ACPRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestAddressedReplyACPFixtureProcess$"}, Environment: []string{"TEST_ADDRESS_REPLY_ACP=1", "TEST_ADDRESS_REPLY_GATE=" + gate.URL, "TEST_ADDRESS_REPLY_SCENARIO=assistant-final"}, TerminalMessageGrouping: true, DrainPromptEvents: true}), cap)
	runtime.AttachIdentity(identity)
	runtime.AttachConversation(store, conversation.ID)
	runtime.AttachProfile(func() node.ManagedProfile { mu.Lock(); defer mu.Unlock(); return profile })
	runtime.AttachMCP("fixture-mcp", dataDir)
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	first := runtime.Identity().RuntimeGeneration
	if first < 1 {
		t.Fatal("actual launch was not registered")
	}
	if err := runtime.Start(ctx); err != nil || runtime.Identity().RuntimeGeneration != first {
		t.Fatal("idempotent Start invented a launch")
	}
	if err := runtime.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if runtime.Identity().RuntimeGeneration != first+1 {
		t.Fatal("same-pins restart reused generation")
	}
	mu.Lock()
	profile.Content = "undelivered profile two"
	profile.Hash = "h2"
	profile.Version = "v2"
	mu.Unlock()
	if err := runtime.HandleMessage(ctx, "synthetic queued input"); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-runtime.Errors():
		if !strings.Contains(err.Error(), "restart") {
			t.Fatal("reload had no explicit restart requirement")
		}
	case <-ctx.Done():
		t.Fatal("reload silently reused native session")
	}
	current, err := store.SecretaryPolicySnapshot(ctx)
	if err != nil || current.ProfileContent != "delivered profile one" {
		t.Fatal("reload claimed undelivered profile")
	}
	if err := runtime.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer runtime.Stop(context.Background())
	current, err = store.SecretaryPolicySnapshot(ctx)
	if err != nil || current.ProfileContent != "undelivered profile two" {
		t.Fatal("restart did not deliver selected profile")
	}
	turns, err := store.SecretaryTurns(ctx, identity.ID)
	if err != nil || len(turns) != 1 {
		t.Fatal("queued reload turn was lost")
	}
	waitForSecretaryTurn(t, ctx, store, turns[0].ID, core.SecretaryTurnFailed)
	evidence, err := store.SecretaryMCPDiscovery(ctx, turns[0].ID)
	if err != nil || !evidence.GenerationMatches || evidence.ToolsListed {
		t.Fatal("turn was not privately linked to current launch or invented discovery")
	}
}
