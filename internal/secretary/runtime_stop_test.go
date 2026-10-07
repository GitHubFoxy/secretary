package secretary

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

func TestSpecReviewStopRetryAllowsExplicitRestart(t *testing.T) {
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
	profile := node.ManagedProfile{Name: "secretary", Version: "v1", Hash: "h", Content: "synthetic", Runtime: "opencode", Model: "fixture/model", Reasoning: "xhigh", ReplyContractVersion: core.SecretaryReplyContractAddressedV1}
	local := node.NewLocal(node.ACPRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestAddressedReplyACPFixtureProcess$"}, Environment: []string{"TEST_ADDRESS_REPLY_ACP=1", "TEST_ADDRESS_REPLY_GATE=" + gate.URL, "TEST_ADDRESS_REPLY_SCENARIO=assistant-final"}, TerminalMessageGrouping: true, DrainPromptEvents: true})
	defer local.Close()
	agent := NewRuntime(local, cap)
	agent.AttachIdentity(identity)
	agent.AttachConversation(store, conversation.ID)
	agent.AttachProfile(func() node.ManagedProfile { return profile })
	agent.AttachMCP("fixture-mcp", dataDir)
	if err := agent.Start(ctx); err != nil {
		t.Fatal(err)
	}
	canceled, stopCancel := context.WithCancel(context.Background())
	stopCancel()
	if err := agent.Stop(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("Stop error category unexpected: %t", err != nil)
	}
	if err := agent.Stop(ctx); err != nil {
		t.Fatal("Stop retry failed")
	}
	_, retained := local.Session("secretary")
	if retained {
		t.Error("successful Stop retry retained native session")
	}
	if err := agent.Start(ctx); err != nil {
		t.Errorf("explicit restart after Stop retry failed: %s", err)
	}
}

// Metadata comes from the executable fixture's actual session/new MCP wiring,
// never from Runtime's private session/launch fields.
type stopLaunchMetadata struct {
	PID        int    `json:"pid"`
	Capability string `json:"capability"`
}

func TestRuntimeStopFailureCleanupAndRevokeRetry(t *testing.T) {
	for _, dbFailed := range []bool{false, true} {
		name := "canceled_context"
		if dbFailed {
			name = "closed_database"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			path := filepath.Join(t.TempDir(), "secretary.db")
			store, err := core.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
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
			launches := make(chan stopLaunchMetadata, 2)
			gate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var metadata stopLaunchMetadata
				if err := json.NewDecoder(r.Body).Decode(&metadata); err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				launches <- metadata
				w.WriteHeader(http.StatusNoContent)
			}))
			defer gate.Close()
			local := node.NewLocal(node.ACPRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestRuntimeStopACPFixtureProcess$"}, Environment: []string{"TEST_RUNTIME_STOP_ACP=1", "TEST_RUNTIME_STOP_GATE=" + gate.URL}, TerminalMessageGrouping: true, DrainPromptEvents: true})
			agent := NewRuntime(local, cap)
			defer func() {
				agent.Stop(context.Background())
				local.Close()
				store.Close()
			}()
			agent.AttachIdentity(identity)
			agent.AttachConversation(store, conversation.ID)
			agent.AttachProfile(func() node.ManagedProfile {
				return node.ManagedProfile{Name: "secretary", Version: "v1", Hash: "h", Content: "synthetic", Runtime: "opencode", Model: "fixture/model", Reasoning: "xhigh", ReplyContractVersion: core.SecretaryReplyContractAddressedV1}
			})
			agent.AttachMCPServer("fixture-mcp", dataDir, gate.URL)
			if err := agent.Start(ctx); err != nil {
				t.Fatal(err)
			}
			old := <-launches
			generation := agent.Identity().RuntimeGeneration
			startup := core.SecretaryMCPObservation{Phase: "startup", Success: true}
			if old.PID <= 0 || old.Capability == "" || store.RecordSecretaryMCPObservation(ctx, person.ID, old.Capability, startup) != nil {
				t.Fatal("fixture did not receive an authorized launch")
			}
			stopCtx := ctx
			if dbFailed {
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				var stopCancel context.CancelFunc
				stopCtx, stopCancel = context.WithCancel(ctx)
				stopCancel()
			}
			err = agent.Stop(stopCtx)
			if dbFailed {
				if err == nil || !strings.Contains(err.Error(), "database is closed") {
					t.Fatal("Stop concealed the database revoke failure")
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatal("Stop concealed caller cancellation")
			}
			if _, retained := local.Session("secretary"); retained {
				t.Error("failed Stop left LocalNode's old session attached")
			}
			if err := syscall.Kill(old.PID, 0); !errors.Is(err, syscall.ESRCH) {
				t.Error("failed Stop left the old subprocess alive")
			}
			if dbFailed {
				store, err = core.Open(ctx, path)
				if err != nil {
					t.Fatal(err)
				}
				agent.AttachConversation(store, conversation.ID)
				if err := agent.Start(ctx); err == nil || !strings.Contains(err.Error(), "cleanup pending") {
					t.Fatal("Start bypassed pending revoke after database recovery")
				}
				current, err := store.SecretaryIdentity(ctx, person.ID)
				if err != nil || current.RuntimeGeneration != generation {
					t.Fatal("pending cleanup advanced generation")
				}
			}
			if err := agent.Stop(ctx); err != nil {
				t.Fatal("valid Stop retry did not complete cleanup")
			}
			if err := store.RecordSecretaryMCPObservation(ctx, person.ID, old.Capability, startup); !errors.Is(err, core.ErrMCPObservationUnauthorized) {
				t.Fatal("successful Stop retry retained old observation authority")
			}
			if err := agent.Stop(ctx); err != nil {
				t.Fatal("completed Stop was not repeatable")
			}
			if err := agent.Start(ctx); err != nil {
				t.Fatal("explicit restart failed after Stop retry")
			}
			next := <-launches
			if agent.Identity().RuntimeGeneration != generation+1 || next.Capability == old.Capability {
				t.Fatal("restart reused old launch authority/generation")
			}
			if err := store.RecordSecretaryMCPObservation(ctx, person.ID, old.Capability, startup); !errors.Is(err, core.ErrMCPObservationUnauthorized) {
				t.Fatal("new launch accepted stale authority")
			}
			if err := store.RecordSecretaryMCPObservation(ctx, person.ID, next.Capability, startup); err != nil {
				t.Fatal("new launch did not have its own authority")
			}
		})
	}
}

func TestRuntimeStopACPFixtureProcess(t *testing.T) {
	if os.Getenv("TEST_RUNTIME_STOP_ACP") != "1" {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				MCPServers []node.MCPServer `json:"mcpServers"`
			} `json:"params"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			t.Fatal("fixture received malformed ACP")
		}
		if len(request.ID) == 0 {
			continue
		}
		result := map[string]any{"protocolVersion": 1}
		if request.Method == "session/new" {
			metadata := stopLaunchMetadata{PID: os.Getpid()}
			for _, server := range request.Params.MCPServers {
				for _, env := range server.Env {
					if env.Name == "SECRETARY_MCP_OBSERVER_CAPABILITY" {
						metadata.Capability = env.Value
					}
				}
			}
			body, _ := json.Marshal(metadata)
			response, err := http.Post(os.Getenv("TEST_RUNTIME_STOP_GATE"), "application/json", bytes.NewReader(body))
			if err != nil {
				t.Fatal("fixture metadata loopback failed")
			}
			response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				t.Fatal("fixture metadata rejected")
			}
			result = map[string]any{"sessionId": "synthetic-stop-session"}
		}
		if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result}); err != nil {
			t.Fatal("fixture could not write ACP")
		}
	}
}
