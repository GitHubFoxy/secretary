package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/mcp"
)

func TestAddressedReplyMCPProtocolAcrossSecretaryStartup(t *testing.T) {
	for _, scenario := range []struct {
		name          string
		callReply     bool
		rejectStartup bool
	}{
		{name: "confirmed addressed reply", callReply: true},
		{name: "no reply fails closed"},
		{name: "MCP observation rejected", callReply: true, rejectStartup: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := context.Background()
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
			capability, err := store.RotateSecretaryCapability(ctx, person.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{
				Version: "test", Harness: "codex", Model: "default", Reasoning: "default",
				ProfileVersion: "test", ProfileName: "secretary", ProfileHash: "test", ProfileContent: "reply through MCP",
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "owner"); err != nil {
				t.Fatal(err)
			}
			launch, err := store.BeginSecretaryRuntime(ctx, identity.ID, capability, "codex", "default", "default")
			if err != nil {
				t.Fatal(err)
			}
			defer store.EndSecretaryRuntime(ctx, launch)
			turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "What is the answer?")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.StartSecretaryTurn(ctx, turn.ID); err != nil {
				t.Fatal(err)
			}
			if err := store.BindSecretaryRuntimeTurn(ctx, launch, turn.ID, turn.InputID); err != nil {
				t.Fatal(err)
			}

			handler := mcp.Secretary{Workers: ctl.WorkerService{Store: store, PersonID: person.ID, Capability: capability},
				ReplyContractVersion: core.SecretaryReplyContractAddressedV1}
			mux := http.NewServeMux()
			mux.HandleFunc("POST /v1/internal/secretary/mcp/observe", func(w http.ResponseWriter, r *http.Request) {
				var observation core.SecretaryMCPObservation
				if err := json.NewDecoder(r.Body).Decode(&observation); err != nil {
					http.Error(w, "invalid observation", http.StatusBadRequest)
					return
				}
				token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
				if err := store.RecordSecretaryMCPObservation(r.Context(), person.ID, token, observation); err != nil {
					http.Error(w, "observation rejected", http.StatusUnauthorized)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
			mux.HandleFunc("POST /v1/internal/secretary/tools/call", func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+capability {
					http.Error(w, "invalid Secretary capability", http.StatusUnauthorized)
					return
				}
				var request struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					http.Error(w, "invalid tool arguments", http.StatusBadRequest)
					return
				}
				value, callErr := handler.Call(r.Context(), request.Name, request.Arguments)
				response := map[string]any{"value": value}
				if callErr != nil {
					response = map[string]any{"error": callErr.Error()}
				}
				_ = json.NewEncoder(w).Encode(response)
			})
			var ready atomic.Bool
			httpServer := &http.Server{Handler: secretaryStartupHandler(&ready, mux)}
			defer httpServer.Close()
			var address string
			var protocolOutput bytes.Buffer
			start := func() error {
				frames := []map[string]any{
					{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-06-18"}},
					{"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": map[string]any{}},
				}
				if scenario.callReply {
					frames = append(frames, map[string]any{
						"jsonrpc": "2.0", "id": 3, "method": "tools/call",
						"params": map[string]any{"name": "reply_to_user", "arguments": map[string]string{
							"secretary_turn_id": turn.ID, "input_id": turn.InputID, "reply_kind": "answer", "text": "The answer is 42.",
						}},
					})
				}
				var input bytes.Buffer
				for _, frame := range frames {
					encoded, err := json.Marshal(frame)
					if err != nil {
						return err
					}
					input.Write(encoded)
					input.WriteByte('\n')
				}
				observerToken := launch.ObserverCapability
				if scenario.rejectStartup {
					observerToken = "invalid-observer"
				}
				server := mcp.Server{
					Handler: mcp.RemoteSecretary{
						BaseURL: "http://" + address, Capability: capability,
						ReplyContractVersion: core.SecretaryReplyContractAddressedV1,
					},
					Observe: (mcp.RemoteMCPObserver{
						BaseURL: "http://" + address, Capability: observerToken,
					}).Observe,
				}
				return server.Serve(ctx, &input, &protocolOutput)
			}
			listener, err := listenWhenSecretaryReady(start, nil, func() (net.Listener, error) {
				return net.Listen("tcp", "127.0.0.1:0")
			}, func(l net.Listener) {
				address = l.Addr().String()
				go func() { _ = httpServer.Serve(l) }()
			})
			if scenario.rejectStartup {
				if listener != nil || err == nil {
					t.Fatalf("rejected MCP observer permitted startup: listener=%v err=%v", listener, err)
				}
			} else {
				if err != nil {
					t.Fatalf("MCP server could not complete startup protocol: %v", err)
				}
				defer listener.Close()
				ready.Store(true)
				var replies []struct {
					ID     int            `json:"id"`
					Error  map[string]any `json:"error"`
					Result map[string]any `json:"result"`
				}
				for _, line := range bytes.Split(bytes.TrimSpace(protocolOutput.Bytes()), []byte{'\n'}) {
					var reply struct {
						ID     int            `json:"id"`
						Error  map[string]any `json:"error"`
						Result map[string]any `json:"result"`
					}
					if err := json.Unmarshal(line, &reply); err != nil {
						t.Fatalf("invalid MCP response: %v", err)
					}
					replies = append(replies, reply)
					if reply.Error != nil || reply.Result["isError"] == true {
						t.Fatalf("MCP protocol error: %s", line)
					}
				}
				wantFrames := 2
				if scenario.callReply {
					wantFrames = 3
				}
				if len(replies) != wantFrames {
					t.Fatalf("MCP responses=%d want=%d", len(replies), wantFrames)
				}
				tools, ok := replies[1].Result["tools"].([]any)
				if !ok {
					t.Fatal("MCP tools/list omitted tools")
				}
				found := false
				for _, value := range tools {
					tool, _ := value.(map[string]any)
					found = found || tool["name"] == "reply_to_user"
				}
				if !found {
					t.Fatal("MCP tools/list omitted addressed reply contract")
				}
				if scenario.callReply {
					parts, ok := replies[2].Result["content"].([]any)
					if !ok || len(parts) != 1 {
						t.Fatal("MCP reply tool returned no content")
					}
					part, _ := parts[0].(map[string]any)
					var receipt struct {
						EntryID   string `json:"entry_id"`
						Duplicate bool   `json:"duplicate"`
					}
					if err := json.Unmarshal([]byte(fmt.Sprint(part["text"])), &receipt); err != nil || receipt.EntryID == "" || receipt.Duplicate {
						t.Fatalf("MCP reply receipt invalid: %+v err=%v", receipt, err)
					}
				}
			}
			discovery, err := store.SecretaryMCPDiscovery(ctx, turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			if scenario.rejectStartup {
				if discovery.Started || discovery.Initialized || discovery.ToolsListed {
					t.Fatalf("rejected startup claimed MCP discovery: %+v", discovery)
				}
			} else if !discovery.Started || !discovery.Initialized || !discovery.ToolsListed || !discovery.HasReplyToUser {
				t.Fatalf("MCP discovery did not observe protocol handshake: %+v", discovery)
			}
			evidence := core.SecretaryCompletionEvidence{
				Branch: "addressed_reply_only", TerminalClass: "end_turn",
				TerminalValid: true, RPCSucceeded: true, DrainCompleted: true,
			}
			finished, err := store.FinishSecretaryTurnWithRequiredReply(ctx, turn.ID, turn.InputID, core.SecretaryTurnSucceeded, evidence)
			if err != nil {
				t.Fatal(err)
			}
			expectedState := core.SecretaryTurnFailed
			if scenario.callReply && !scenario.rejectStartup {
				expectedState = core.SecretaryTurnSucceeded
			}
			if finished.State != expectedState {
				t.Fatalf("turn completion=%s want=%s", finished.State, expectedState)
			}
			entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			foundReply, foundFailure := false, false
			for _, entry := range entries {
				if entry.Kind != core.EntrySecretary {
					continue
				}
				foundReply = foundReply || entry.Body == "The answer is 42."
				foundFailure = foundFailure || strings.Contains(entry.Body, "Secretary не получил подтверждённый адресованный ответ")
			}
			if foundReply != (expectedState == core.SecretaryTurnSucceeded) || foundFailure != (expectedState == core.SecretaryTurnFailed) {
				t.Fatalf("incorrect durable addressed reply/failure: reply=%v failure=%v", foundReply, foundFailure)
			}
			hasReply, err := store.SecretaryHasAddressedReply(ctx, turn.ID, turn.InputID)
			if err != nil || hasReply != (expectedState == core.SecretaryTurnSucceeded) {
				t.Fatalf("addressed reply proof=%v err=%v", hasReply, err)
			}
		})
	}
}

func TestStartupOnlyAllowsInternalMCPMethodsBeforeReady(t *testing.T) {
	var ready atomic.Bool
	allowed := secretaryStartupHandler(&ready, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct {
		method string
		path   string
		status int
	}{
		{http.MethodPost, "/v1/internal/secretary/mcp/observe", http.StatusNoContent},
		{http.MethodPost, "/v1/internal/secretary/tools/call", http.StatusNoContent},
		{http.MethodGet, "/v1/internal/secretary/mcp/observe", http.StatusServiceUnavailable},
		{http.MethodPost, "/v1/secretary/messages", http.StatusServiceUnavailable},
		{http.MethodGet, "/health", http.StatusServiceUnavailable},
	} {
		req, err := http.NewRequest(tc.method, "http://localhost"+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		writer := &startupStatusWriter{header: make(http.Header)}
		allowed.ServeHTTP(writer, req)
		if writer.status != tc.status {
			t.Fatalf("%s %s: status=%d want=%d", tc.method, tc.path, writer.status, tc.status)
		}
	}
}

type startupStatusWriter struct {
	header http.Header
	status int
}

func (w *startupStatusWriter) Header() http.Header { return w.header }
func (w *startupStatusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return len(data), nil
}
func (w *startupStatusWriter) WriteHeader(status int) { w.status = status }

