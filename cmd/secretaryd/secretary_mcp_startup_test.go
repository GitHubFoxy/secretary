package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/mcp"
	"github.com/beruseruko/secretary/internal/webapi"
)

type protocolBoundaryWorkers struct {
	store          *core.Store
	conversationID string
}

func (w *protocolBoundaryWorkers) ListNodes(context.Context) ([]core.NodeRecord, error) {
	return nil, nil
}
func (w *protocolBoundaryWorkers) ListProjects(context.Context) ([]core.Project, error) {
	return nil, nil
}
func (w *protocolBoundaryWorkers) ListWorkers(context.Context) ([]core.Worker, error) {
	return nil, nil
}
func (w *protocolBoundaryWorkers) GetWorker(context.Context, string) (core.WorkerDetails, error) {
	return core.WorkerDetails{}, core.ErrNotFound
}
func (w *protocolBoundaryWorkers) MessageWorker(context.Context, ctl.MessageWorkerRequest) (core.WorkerDetails, error) {
	return core.WorkerDetails{}, core.ErrNotFound
}
func (w *protocolBoundaryWorkers) CancelWorker(context.Context, string) (core.WorkerDetails, error) {
	return core.WorkerDetails{}, core.ErrNotFound
}
func (w *protocolBoundaryWorkers) CloseWorker(context.Context, string) (core.WorkerDetails, error) {
	return core.WorkerDetails{}, core.ErrNotFound
}

func (w *protocolBoundaryWorkers) SpawnWorker(ctx context.Context, request ctl.SpawnWorkerRequest) (core.WorkerDetails, error) {
	worker, turn, attempt, err := w.store.CreateWorker(ctx, w.conversationID, core.WorkerSpec{
		WorkerRef: "protocol-worker", Intent: request.Intent, NodeID: "synthetic-node",
		HarnessInstanceID: "synthetic-node/codex", PolicySnapshot: "synthetic",
	}, core.TurnSpec{Input: request.Intent})
	if err != nil {
		return core.WorkerDetails{}, err
	}
	if _, err := w.store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		return core.WorkerDetails{}, err
	}
	if _, result, _, err := w.store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "Worker Result from accepted action",
	}); err != nil || result == nil {
		return core.WorkerDetails{}, fmt.Errorf("record Worker result: %w", err)
	}
	return core.WorkerDetails{Worker: worker, ActionTurnID: turn.ID}, nil
}

type protocolMCPResponse struct {
	ID     int            `json:"id"`
	Error  map[string]any `json:"error"`
	Result map[string]any `json:"result"`
}

func TestAddressedReplyMCPProtocolAcrossSecretaryStartup(t *testing.T) {
	for _, scenario := range []string{"answer", "spawn_worker", "missing_reply"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			api, err := webapi.New(ctx, store, "bootstrap")
			if err != nil {
				t.Fatal(err)
			}
			personID := api.OwnerID()
			conversation, err := store.ConversationForPerson(ctx, personID)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := store.EnsureSecretaryIdentity(ctx, personID, conversation.ID)
			if err != nil {
				t.Fatal(err)
			}
			capability, err := store.RotateSecretaryCapability(ctx, personID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "owner"); err != nil {
				t.Fatal(err)
			}
			if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{
				Version: "test", Harness: "codex", Model: "default", Reasoning: "default",
				ProfileVersion: "test", ProfileName: "secretary", ProfileHash: "hash", ProfileContent: "reply through MCP",
			}); err != nil {
				t.Fatal(err)
			}
			launch, err := store.BeginSecretaryRuntime(ctx, identity.ID, capability, "codex", "default", "default")
			if err != nil {
				t.Fatal(err)
			}
			defer store.EndSecretaryRuntime(context.Background(), launch)
			identity.RuntimeGeneration = launch.Generation
			turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "Answer or create a Worker")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.StartSecretaryTurn(ctx, turn.ID); err != nil {
				t.Fatal(err)
			}
			if err := store.BindSecretaryRuntimeTurn(ctx, launch, turn.ID, turn.InputID); err != nil {
				t.Fatal(err)
			}

			api.AttachSecretaryWorkerTools(&protocolBoundaryWorkers{store: store, conversationID: conversation.ID})
			api.AttachSecretaryReplyContract(func() string { return core.SecretaryReplyContractAddressedV1 })
			var ready atomic.Bool
			server := &http.Server{Handler: secretaryStartupHandler(&ready, rootHandler(api.Handler(), api.ControlHandler(), http.NotFoundHandler(), http.NotFoundHandler(), nil, false))}
			defer server.Close()
			var address string
			inputReader, inputWriter := io.Pipe()
			outputReader, outputWriter := io.Pipe()
			defer inputReader.Close()
			defer outputReader.Close()
			defer outputWriter.Close()
			defer inputWriter.Close()
			done := make(chan error, 1)
			encoder := json.NewEncoder(inputWriter)
			decoder := json.NewDecoder(outputReader)
			call := func(id int, method string, params any) protocolMCPResponse {
				t.Helper()
				if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
					t.Fatal(err)
				}
				var response protocolMCPResponse
				if err := decoder.Decode(&response); err != nil {
					t.Fatal(err)
				}
				if response.ID != id || response.Error != nil {
					t.Fatalf("MCP %s protocol error: %+v", method, response)
				}
				return response
			}
			toolCall := func(id int, name string, args any) (map[string]any, bool) {
				t.Helper()
				response := call(id, "tools/call", map[string]any{"name": name, "arguments": args})
				if response.Result["isError"] == true {
					return nil, true
				}
				content, ok := response.Result["content"].([]any)
				if !ok || len(content) != 1 {
					t.Fatalf("MCP tool %s omitted content: %+v", name, response)
				}
				part, ok := content[0].(map[string]any)
				if !ok {
					t.Fatalf("MCP tool %s returned malformed content", name)
				}
				var value map[string]any
				if err := json.Unmarshal([]byte(fmt.Sprint(part["text"])), &value); err != nil {
					t.Fatal(err)
				}
				return value, false
			}
			listener, err := listenWhenSecretaryReady(func() error {
				go func() {
					done <- (mcp.Server{
						Handler: mcp.RemoteSecretary{
							BaseURL: "http://" + address, Capability: capability,
							ReplyContractVersion: core.SecretaryReplyContractAddressedV1,
						},
						Observe: (mcp.RemoteMCPObserver{
							BaseURL: "http://" + address, Capability: launch.ObserverCapability,
						}).Observe,
					}).Serve(ctx, inputReader, outputWriter)
				}()
				call(1, "initialize", map[string]any{"protocolVersion": "2025-06-18"})
				listed := call(2, "tools/list", map[string]any{})
				tools, ok := listed.Result["tools"].([]any)
				if !ok {
					return fmt.Errorf("MCP tools/list did not return a catalogue")
				}
				foundReply, foundSpawn := false, false
				for _, item := range tools {
					tool, _ := item.(map[string]any)
					foundReply = foundReply || tool["name"] == "reply_to_user"
					foundSpawn = foundSpawn || tool["name"] == "spawn_worker"
				}
				if !foundReply || !foundSpawn {
					return fmt.Errorf("MCP catalogue omitted addressed reply or spawn")
				}
				return waitForSecretaryMCPReadiness(ctx, store, identity)
			}, nil, func() (net.Listener, error) {
				return net.Listen("tcp", "127.0.0.1:0")
			}, func(l net.Listener) {
				address = l.Addr().String()
				go func() { _ = server.Serve(l) }()
			})
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			discovery, err := store.SecretaryRuntimeMCPDiscovery(ctx, identity.ID, launch.Generation)
			if err != nil || !discovery.HasReplyToUser || !discovery.HasSpawnWorker || !discovery.ToolsListed {
				t.Fatalf("native launch MCP readiness unconfirmed: %+v err=%v", discovery, err)
			}
			prepared, err := http.Get("http://" + address + "/v1/health")
			if err != nil {
				t.Fatal(err)
			}
			_ = prepared.Body.Close()
			if prepared.StatusCode != http.StatusServiceUnavailable {
				t.Fatalf("public HTTP accepted request before readiness: %d", prepared.StatusCode)
			}
			ready.Store(true)
			after, err := http.Get("http://" + address + "/v1/health")
			if err != nil {
				t.Fatal(err)
			}
			_ = after.Body.Close()
			if after.StatusCode == http.StatusServiceUnavailable {
				t.Fatal("public HTTP remained startup-gated after confirmed MCP readiness")
			}

			reply := map[string]any{
				"secretary_turn_id": turn.ID, "input_id": turn.InputID,
				"text": "The answer is 42.", "reply_kind": "answer",
			}
			if scenario == "spawn_worker" {
				invalid := map[string]any{
					"secretary_turn_id": turn.ID, "input_id": turn.InputID,
					"text": "Worker created", "reply_kind": "spawn_worker",
					"action_kind": "spawn_worker", "worker_turn_id": "unaccepted-turn",
				}
				if _, rejected := toolCall(3, "reply_to_user", invalid); !rejected {
					t.Fatal("unaccepted Worker origin was confirmed")
				}
				details, rejected := toolCall(4, "spawn_worker", map[string]any{
					"secretary_turn_id": turn.ID, "input_id": turn.InputID,
					"intent": "Create a list", "idempotency_key": "one-worker",
				})
				if rejected || fmt.Sprint(details["action_turn_id"]) == "" {
					t.Fatalf("Worker action was not accepted: %+v", details)
				}
				workerTurnID, _ := details["action_turn_id"].(string)
				if workerTurnID == "" {
					t.Fatal("spawn_worker did not return a canonical action_turn_id")
				}
				reply["reply_kind"], reply["action_kind"], reply["worker_turn_id"] = "spawn_worker", "spawn_worker", workerTurnID
				reply["text"] = "Worker created"
				related, err := store.SecretaryOriginHasResult(ctx, turn.ID, turn.InputID)
				if err != nil || !related {
					t.Fatalf("durable Worker Result missing exact origin: related=%v err=%v", related, err)
				}
			}
			if scenario != "missing_reply" {
				receipt, rejected := toolCall(5, "reply_to_user", reply)
				if rejected || receipt["duplicate"] != false || receipt["entry_id"] == "" {
					t.Fatalf("addressed reply was not accepted: %+v rejected=%v", receipt, rejected)
				}
				replayed, rejected := toolCall(6, "reply_to_user", reply)
				if rejected || replayed["duplicate"] != true || replayed["entry_id"] != receipt["entry_id"] {
					t.Fatalf("MCP reply replay was not idempotent: %+v rejected=%v", replayed, rejected)
				}
			}

			evidence := core.SecretaryCompletionEvidence{
				Branch: "addressed_reply_only", TerminalClass: "end_turn",
				TerminalValid: true, RPCSucceeded: true, DrainCompleted: true,
			}
			completed, err := store.FinishSecretaryTurnWithRequiredReply(ctx, turn.ID, turn.InputID, core.SecretaryTurnSucceeded, evidence)
			if err != nil {
				t.Fatal(err)
			}
			expected := core.SecretaryTurnSucceeded
			if scenario == "missing_reply" {
				expected = core.SecretaryTurnFailed
			}
			if completed.State != expected {
				t.Fatalf("Secretary completion=%s want=%s", completed.State, expected)
			}
			entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			replyCount, resultCount, failureCount := 0, 0, 0
			for _, entry := range entries {
				if entry.Kind == core.EntrySecretary {
					if entry.Body == "The answer is 42." || entry.Body == "Worker создан. Действие подтверждено сервером." {
						replyCount++
					}
					if strings.Contains(entry.Body, "Secretary не получил подтверждённый адресованный ответ") {
						failureCount++
					}
				}
				if entry.Kind == core.EntryWorkerResult && entry.Body == "Worker Result from accepted action" {
					resultCount++
				}
			}
			if scenario == "missing_reply" {
				if replyCount != 0 || failureCount != 1 {
					t.Fatalf("missing reply claimed success: replies=%d failures=%d", replyCount, failureCount)
				}
			} else if replyCount != 1 || failureCount != 0 {
				t.Fatalf("addressed reply duplicated or failed: replies=%d failures=%d", replyCount, failureCount)
			}
			if scenario == "spawn_worker" && resultCount != 1 {
				t.Fatalf("accepted Worker Result count=%d", resultCount)
			}
			if scenario != "spawn_worker" && resultCount != 0 {
				t.Fatal("unrequested Worker Result entered Conversation")
			}
			if err := inputWriter.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("live MCP process exited with error: %v", err)
				}
			case <-ctx.Done():
				t.Fatal("MCP process did not remain available after public readiness")
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
