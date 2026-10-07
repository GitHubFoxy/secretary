package secretary

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/mcp"
	"github.com/beruseruko/secretary/internal/node"
)

func TestAddressedReplyOnlyEndTurnCompletesWithoutAssistantEcho(t *testing.T) {
	for _, tc := range []struct {
		name                string
		scenario            string
		replyMode           string
		contractVersion     string
		wantState           core.SecretaryTurnState
		wantCurrentResponse string
		wantPriorResponse   string
	}{
		{name: "one exact durable reply", scenario: "mcp-only", replyMode: "exact", contractVersion: core.SecretaryReplyContractAddressedV1, wantState: core.SecretaryTurnSucceeded, wantCurrentResponse: "The answer is 42."},
		{name: "no durable reply", scenario: "mcp-only", contractVersion: core.SecretaryReplyContractAddressedV1, wantState: core.SecretaryTurnFailed},
		{name: "revoked MCP capability", scenario: "mcp-only", replyMode: "revoked", contractVersion: core.SecretaryReplyContractAddressedV1, wantState: core.SecretaryTurnFailed},
		{name: "unknown native stop", scenario: "unknown-stop", replyMode: "exact", contractVersion: core.SecretaryReplyContractAddressedV1, wantState: core.SecretaryTurnFailed, wantCurrentResponse: "The answer is 42."},
		{name: "terminal summary alone is not a reply", scenario: "summary-only", contractVersion: core.SecretaryReplyContractAddressedV1, wantState: core.SecretaryTurnFailed},
		{name: "reply belongs to another turn", scenario: "mcp-only", replyMode: "foreign", contractVersion: core.SecretaryReplyContractAddressedV1, wantState: core.SecretaryTurnFailed, wantPriorResponse: "Reply belongs to a different turn."},
		{name: "missing stop reason", scenario: "missing-stop", replyMode: "exact", contractVersion: core.SecretaryReplyContractAddressedV1, wantState: core.SecretaryTurnFailed, wantCurrentResponse: "The answer is 42."},
		{name: "malformed stop reason", scenario: "malformed-stop", replyMode: "exact", contractVersion: core.SecretaryReplyContractAddressedV1, wantState: core.SecretaryTurnFailed, wantCurrentResponse: "The answer is 42."},
		{name: "max tokens", scenario: "max-tokens", replyMode: "exact", contractVersion: core.SecretaryReplyContractAddressedV1, wantState: core.SecretaryTurnFailed, wantCurrentResponse: "The answer is 42."},
		{name: "refusal", scenario: "refusal", replyMode: "exact", contractVersion: core.SecretaryReplyContractAddressedV1, wantState: core.SecretaryTurnFailed, wantCurrentResponse: "The answer is 42."},
		{name: "progress but no final text", scenario: "progress-only", replyMode: "exact", contractVersion: core.SecretaryReplyContractAddressedV1, wantState: core.SecretaryTurnFailed, wantCurrentResponse: "The answer is 42."},
		{name: "native RPC error", scenario: "rpc-error", replyMode: "exact", contractVersion: core.SecretaryReplyContractAddressedV1, wantState: core.SecretaryTurnFailed, wantCurrentResponse: "The answer is 42."},
		{name: "legacy contract remains fail closed", scenario: "mcp-only", wantState: core.SecretaryTurnFailed},
		{name: "assistant final without addressed reply", scenario: "assistant-final", contractVersion: core.SecretaryReplyContractAddressedV1, wantState: core.SecretaryTurnFailed},
		{name: "assistant final with exact reply", scenario: "assistant-final", replyMode: "exact", contractVersion: core.SecretaryReplyContractAddressedV1, wantState: core.SecretaryTurnSucceeded, wantCurrentResponse: "The answer is 42."},
		{name: "legacy assistant final remains compatible", scenario: "assistant-final", wantState: core.SecretaryTurnSucceeded, wantCurrentResponse: "actual assistant final text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runAddressedReplyCompletionScenario(t, tc.scenario, tc.replyMode, tc.contractVersion, tc.wantState, tc.wantCurrentResponse, tc.wantPriorResponse)
		})
	}
}

func runAddressedReplyCompletionScenario(t *testing.T, scenario, replyMode, contractVersion string, wantState core.SecretaryTurnState, wantCurrentResponse, wantPriorResponse string) {
	t.Helper()
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
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	setRuntimeTestPolicy(t, store)
	dataDir := t.TempDir()
	if _, err := store.SaveUserDocument(ctx, filepath.Join(dataDir, "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	handler := mcp.Secretary{
		Workers:              ctl.WorkerService{Store: store, PersonID: person.ID, Capability: capability},
		ReplyContractVersion: core.SecretaryReplyContractAddressedV1,
	}
	priorTurns := 0
	if replyMode == "foreign" {
		foreign, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "prior synthetic input")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.StartSecretaryTurn(ctx, foreign.ID); err != nil {
			t.Fatal(err)
		}
		args, _ := json.Marshal(map[string]string{"secretary_turn_id": foreign.ID, "input_id": foreign.InputID, "text": "Reply belongs to a different turn."})
		if _, err := handler.Call(ctx, "reply_to_user", args); err != nil {
			t.Fatalf("prior addressed reply rejected: %v", err)
		}
		if _, _, err := store.FinishSecretaryTurnWithOutput(ctx, foreign.ID, foreign.InputID, core.SecretaryTurnSucceeded, "", "", nil); err != nil {
			t.Fatalf("prior Secretary turn did not finish: %v", err)
		}
		priorTurns = 1
	}

	ready := make(chan struct{}, 1)
	release := make(chan struct{})
	gate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/hold" {
			http.NotFound(w, r)
			return
		}
		ready <- struct{}{}
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	defer gate.Close()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()

	runtimeProfile := node.ManagedProfile{
		Name: "secretary", Version: "addressed-v1", Hash: "addressed-v1-fixture",
		Content: "synthetic addressed-reply profile", Runtime: "opencode", Model: "fixture-model", Reasoning: "xhigh",
		ReplyContractVersion: contractVersion,
	}
	acpRuntime := node.ACPRuntime{
		Command: os.Args[0], Arguments: []string{"-test.run=^TestAddressedReplyACPFixtureProcess$"},
		Environment: []string{
			"TEST_ADDRESS_REPLY_ACP=1", "TEST_ADDRESS_REPLY_GATE=" + gate.URL,
			"TEST_ADDRESS_REPLY_SCENARIO=" + scenario,
		},
		TerminalMessageGrouping: true, DrainPromptEvents: true,
	}
	runtime := NewRuntime(node.NewLocal(acpRuntime), capability)
	runtime.AttachConversation(store, conversation.ID)
	runtime.AttachIdentity(identity)
	runtime.AttachProfile(func() node.ManagedProfile { return runtimeProfile })
	runtime.AttachMCP("unused-fixture-command", dataDir)
	if err := runtime.Start(ctx); err != nil {
		t.Fatal("Secretary runtime did not start on the executable ACP fixture")
	}
	if err := runtime.HandleMessage(ctx, "Answer through an addressed reply."); err != nil {
		t.Fatal("Secretary runtime did not queue the synthetic user turn")
	}
	select {
	case <-ready:
	case runtimeErr := <-runtime.Errors():
		t.Fatalf("Secretary runtime stopped before the ACP terminal boundary: %s", runtimeErr.Error())
	case <-ctx.Done():
		t.Fatal("ACP fixture did not reach its gated terminal boundary")
	}
	turns, err := store.SecretaryTurns(ctx, identity.ID)
	if err != nil || len(turns) != priorTurns+1 {
		t.Fatalf("durable originating turn count=%d err=%v", len(turns), err)
	}
	turn := turns[len(turns)-1]
	if replyMode == "revoked" {
		if _, err := store.RotateSecretaryCapability(ctx, person.ID); err != nil {
			t.Fatal(err)
		}
		args, _ := json.Marshal(map[string]string{"secretary_turn_id": turn.ID, "input_id": turn.InputID, "text": "must not persist"})
		if _, err := handler.Call(ctx, "reply_to_user", args); err == nil {
			t.Fatal("revoked MCP capability persisted a reply")
		}
	}
	if replyMode == "exact" {
		args, err := json.Marshal(map[string]string{
			"secretary_turn_id": turn.ID, "input_id": turn.InputID, "text": "The answer is 42.",
		})
		if err != nil {
			t.Fatal("could not prepare the exact MCP origin")
		}
		first, err := handler.Call(ctx, "reply_to_user", args)
		if err != nil {
			t.Fatalf("server-owned addressed reply was rejected: %v", err)
		}
		second, err := handler.Call(ctx, "reply_to_user", args)
		if err != nil {
			t.Fatalf("idempotent addressed reply replay was rejected: %v", err)
		}
		firstResult, firstOK := first.(map[string]any)
		secondResult, secondOK := second.(map[string]any)
		if !firstOK || !secondOK || firstResult["duplicate"] != false || secondResult["duplicate"] != true {
			t.Fatal("MCP reply replay did not identify the single durable entry")
		}
	}
	close(release)

	waitForSecretaryTurn(t, ctx, store, turn.ID, wantState)
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	secretaryBodies := make([]string, 0)
	for _, entry := range entries {
		if entry.Kind == core.EntrySecretary {
			secretaryBodies = append(secretaryBodies, entry.Body)
		}
	}
	wantBodies := make([]string, 0, priorTurns+1)
	if wantPriorResponse != "" {
		wantBodies = append(wantBodies, wantPriorResponse)
	}
	if wantCurrentResponse != "" {
		wantBodies = append(wantBodies, wantCurrentResponse)
	}
	if len(secretaryBodies) != len(wantBodies) {
		t.Fatalf("Secretary Conversation entry count=%d, want=%d", len(secretaryBodies), len(wantBodies))
	}
	for _, wantBody := range wantBodies {
		found := false
		for _, body := range secretaryBodies {
			found = found || body == wantBody
		}
		if !found {
			t.Fatalf("expected Conversation entry was missing: exact_body=%t", false)
		}
	}
	turns, err = store.SecretaryTurns(ctx, identity.ID)
	if err != nil || len(turns) != priorTurns+1 {
		t.Fatalf("completion created an automatic Secretary turn: count=%d err=%v", len(turns), err)
	}
}

func waitForSecretaryTurn(t *testing.T, ctx context.Context, store *core.Store, turnID string, want core.SecretaryTurnState) {
	t.Helper()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		turn, err := store.SecretaryTurn(ctx, turnID)
		if err == nil && turn.State.Terminal() {
			if turn.State != want {
				t.Fatalf("Secretary turn terminal state=%s, want=%s", turn.State, want)
			}
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("Secretary turn did not reach a terminal state")
		}
	}
}

// Executable ACP fixture: it pauses session/prompt until the test has called
// the public MCP operation (when applicable), then emits terminal metadata.
func TestAddressedReplyACPFixtureProcess(t *testing.T) {
	if os.Getenv("TEST_ADDRESS_REPLY_ACP") != "1" {
		return
	}
	gateURL := os.Getenv("TEST_ADDRESS_REPLY_GATE")
	scenario := os.Getenv("TEST_ADDRESS_REPLY_SCENARIO")
	encoder := json.NewEncoder(os.Stdout)
	write := func(value any) {
		if err := encoder.Encode(value); err != nil {
			t.Fatal("ACP fixture could not write protocol frame")
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			t.Fatal("ACP fixture received malformed request")
		}
		if len(request.ID) == 0 {
			continue
		}
		switch request.Method {
		case "initialize":
			write(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"protocolVersion": 1}})
		case "session/new":
			write(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"sessionId": "addressed-session"}})
		case "session/prompt":
			update := func(kind, id, text string) {
				write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{
					"sessionId": "addressed-session", "update": map[string]any{"sessionUpdate": kind, "messageId": id, "content": map[string]string{"type": "text", "text": text}},
				}})
			}
			switch scenario {
			case "progress-only":
				update("agent_message_chunk", "progress", "nonterminal progress")
				update("tool_call", "", "")
			case "summary-only":
				// The unverified terminal Summary is the only text-like response.
			case "assistant-final":
				update("agent_message_chunk", "final", "actual assistant final text")
			default:
				update("tool_call", "", "")
			}
			response, err := http.Get(strings.TrimRight(gateURL, "/") + "/hold")
			if err != nil {
				t.Fatal("ACP fixture could not reach local synchronization gate")
			}
			_ = response.Body.Close()
			switch scenario {
			case "missing-stop":
				write(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{}})
			case "malformed-stop":
				write(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"stopReason": []string{"end_turn"}}})
			case "unknown-stop":
				write(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"stopReason": "unknown_native_status"}})
			case "max-tokens":
				write(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"stopReason": "max_tokens"}})
			case "refusal":
				write(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"stopReason": "refusal"}})
			case "rpc-error":
				write(map[string]any{"jsonrpc": "2.0", "id": request.ID, "error": map[string]any{"code": -32000, "message": "synthetic provider failure"}})
			case "summary-only":
				write(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"stopReason": "end_turn", "summary": "unverified terminal response summary"}})
			default:
				write(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"stopReason": "end_turn"}})
			}
		default:
			write(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{}})
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal("ACP fixture input failed")
	}
}
