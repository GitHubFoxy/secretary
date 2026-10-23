package secretary

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"regexp"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/config"
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

func TestProductionCodexProfileAddressedReplyACPCompletion(t *testing.T) {
	for _, scenario := range []string{"acp-mcp-answer", "acp-mcp-spawn", "acp-mcp-no-reply"} {
		t.Run(scenario, func(t *testing.T) {
			want := core.SecretaryTurnSucceeded
			body := "The answer is 42."
			if scenario == "acp-mcp-spawn" { body = "Worker создан. Действие подтверждено сервером." }
			if scenario == "acp-mcp-no-reply" { want = core.SecretaryTurnFailed; body = "" }
			runAddressedReplyCompletionScenario(t, scenario, "", core.SecretaryReplyContractAddressedV1, want, body, "", true)
		})
	}
}

func runAddressedReplyCompletionScenario(t *testing.T, scenario, replyMode, contractVersion string, wantState core.SecretaryTurnState, wantCurrentResponse, wantPriorResponse string, productionCodex ...bool) {
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
	production := len(productionCodex) > 0 && productionCodex[0]
	if production {
		if _, err := store.EnrollNode(ctx, "local"); err != nil { t.Fatal(err) }
		instance := core.HarnessInstance{ID: "local/codex", Node: "local", Kind: core.HarnessCodex, Version: "fixture", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}, Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityShell}}}
		if err := store.UpdateNodeHeartbeat(ctx, "local", core.HarnessInventorySnapshot{Node: "local", ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{instance}}, core.NodeHeartbeat{Capacity: 2}); err != nil { t.Fatal(err) }
		handler.Workers.Runtime = &mac12DispatchRuntime{}
		handler.Workers.WorkerPolicy = core.HarnessPolicy{DefaultHarness: core.HarnessCodex}
		handler.Workers.WorkerProfileSource = func() (node.ManagedProfile, error) {
			return node.ManagedProfile{Version: "fixture", Name: "worker", Content: "Synthetic worker.", Runtime: "codex", Hash: "fixture"}, nil
		}
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
		args, _ := json.Marshal(map[string]string{"secretary_turn_id": foreign.ID, "input_id": foreign.InputID, "text": "Reply belongs to a different turn.", "reply_kind": "answer"})
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
		if production && r.URL.Path == "/mcp" {
		defer r.Body.Close()
		payload, err := io.ReadAll(r.Body)
		if err != nil { http.Error(w, err.Error(), 400); return }
		var output bytes.Buffer
		if err := (mcp.Server{Handler: handler}).Serve(ctx, bytes.NewReader(append(payload, '\\n')), &output); err != nil { http.Error(w, err.Error(), 500); return }
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(output.Bytes())
		return
	}
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
	if len(productionCodex) > 0 && productionCodex[0] {
		manager, err := config.Open(filepath.Join(t.TempDir(), "config.toml"))
		if err != nil { t.Fatal(err) }
		generated := manager.Snapshot().Profiles["secretary"]
		if generated.Runtime != "codex" || generated.ReplyContractVersion != core.SecretaryReplyContractAddressedV1 || !strings.Contains(generated.Content, "reply_to_user") {
			t.Fatal("official profile lacks Codex addressed reply contract")
		}
		runtimeProfile = node.ManagedProfile{
			Name: generated.Name, Version: manager.Snapshot().Version, Content: generated.Content, Hash: generated.Hash,
			Runtime: generated.Runtime, Model: generated.Model, Reasoning: generated.Reasoning,
			ReplyContractVersion: generated.ReplyContractVersion,
		}
	}
	acpRuntime := node.ACPRuntime{
		Command: os.Args[0], Arguments: []string{"-test.run=^TestAddressedReplyACPFixtureProcess$"},
		Environment: []string{
			"TEST_ADDRESS_REPLY_ACP=1", "TEST_ADDRESS_REPLY_GATE=" + gate.URL,
			"TEST_ADDRESS_REPLY_SCENARIO=" + scenario,
			"TEST_ADDRESS_REPLY_PRODUCTION=" + fmt.Sprint(production),
		},
		TerminalMessageGrouping: true, DrainPromptEvents: true,
	}
	local := node.NewLocal(acpRuntime)
	if len(productionCodex) > 0 && productionCodex[0] {
		local = node.NewLocal(node.CodexRuntime{ACPRuntime: acpRuntime, CodexCommand: os.Args[0]})
	}
	runtime := NewRuntime(local, capability)
	runtime.AttachConversation(store, conversation.ID)
	runtime.AttachIdentity(identity)
	runtime.AttachProfile(func() node.ManagedProfile { return runtimeProfile })
	runtime.AttachMCP(os.Args[0], dataDir)
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
		args, _ := json.Marshal(map[string]string{"secretary_turn_id": turn.ID, "input_id": turn.InputID, "text": "must not persist", "reply_kind": "answer"})
		if _, err := handler.Call(ctx, "reply_to_user", args); err == nil {
			t.Fatal("revoked MCP capability persisted a reply")
		}
	}
	if replyMode == "exact" {
		args, err := json.Marshal(map[string]string{
			"secretary_turn_id": turn.ID, "input_id": turn.InputID, "text": "The answer is 42.", "reply_kind": "answer",
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
	if production {
		workers, err := store.WorkersForConversation(ctx, conversation.ID)
		if err != nil { t.Fatal(err) }
		wantWorkers := 0
		if scenario == "acp-mcp-spawn" { wantWorkers = 1 }
		if len(workers) != wantWorkers { t.Fatalf("projectless spawn count=%d, want=%d", len(workers), wantWorkers) }
		if wantWorkers == 1 && workers[0].ProjectID != "" { t.Fatal("spawn unexpectedly acquired a Project") }
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
			Params json.RawMessage `json:"params"`
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
			if strings.HasPrefix(scenario, "acp-mcp-") {
				var params struct { Prompt []struct { Text string `json:"text"` } `json:"prompt"` }
				if err := json.Unmarshal(request.Params, &params); err != nil || len(params.Prompt) == 0 { t.Fatal("ACP prompt did not supply origin") }
				prompt := params.Prompt[0].Text
				turnID := regexp.MustCompile(`secretary_turn_id=([^\\s]+)`).FindStringSubmatch(prompt)
				inputID := regexp.MustCompile(`input_id=([^\\s]+)`).FindStringSubmatch(prompt)
				if len(turnID) < 2 || len(inputID) < 2 { t.Fatal("ACP prompt lacked canonical origin") }
				var config map[string]any
				if err := json.Unmarshal([]byte(os.Getenv("CODEX_CONFIG")), &config); err != nil { t.Fatal(err) }
				instructions, _ := config["developer_instructions"].(string)
				if !strings.Contains(instructions, "reply_kind=spawn_worker") || !strings.Contains(instructions, "action_turn_id") { t.Fatal("generated profile not delivered to Codex process") }
				mcpCall := func(name string, args map[string]any) map[string]any {
					payload, _ := json.Marshal(map[string]any{"jsonrpc":"2.0","id":1,"method":"tools/call","params":map[string]any{"name":name,"arguments":args}})
					response, err := http.Post(strings.TrimRight(gateURL, "/")+"/mcp", "application/json", bytes.NewReader(payload))
					if err != nil { t.Fatal(err) }
					defer response.Body.Close()
					var decoded struct { Error any `json:"error"`; Result struct { IsError bool `json:"isError"`; Content []struct { Text string `json:"text"` } `json:"content"` } `json:"result"` }
					if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil || decoded.Error != nil || decoded.Result.IsError || len(decoded.Result.Content) == 0 { t.Fatalf("MCP tool %s failed: %+v %v", name, decoded, err) }
					var result map[string]any
					if err := json.Unmarshal([]byte(decoded.Result.Content[0].Text), &result); err != nil { t.Fatal(err) }
					return result
				}
				origin := map[string]any{"secretary_turn_id":turnID[1],"input_id":inputID[1]}
				if scenario == "acp-mcp-spawn" {
					spawn := map[string]any{"secretary_turn_id":turnID[1],"input_id":inputID[1],"intent":"Make a list EЛЬ-5731","idempotency_key":"mac12-single-spawn"}
					details := mcpCall("spawn_worker", spawn)
					turn, _ := details["action_turn_id"].(string)
					if turn == "" { t.Fatal("accepted spawn lacked action_turn_id") }
					origin["reply_kind"], origin["action_kind"], origin["worker_turn_id"], origin["text"] = "spawn_worker", "spawn_worker", turn, "Worker created"
				} else {
					origin["reply_kind"], origin["text"] = "answer", "The answer is 42."
				}
				if scenario != "acp-mcp-no-reply" {
					first := mcpCall("reply_to_user", origin)
					second := mcpCall("reply_to_user", origin)
					if first["duplicate"] != false || second["duplicate"] != true { t.Fatal("addressed reply was not idempotent") }
				}
			}
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

type mac12DispatchRuntime struct{}

func (*mac12DispatchRuntime) Dispatch(context.Context, string, core.Worker, core.Turn, core.Phase4Attempt, core.DispatchResolution) error { return nil }
func (*mac12DispatchRuntime) Resume(context.Context, string, core.Worker, core.Turn, core.Phase4Attempt, string) error { return fmt.Errorf("unexpected resume") }
func (*mac12DispatchRuntime) Steer(context.Context, string, core.Worker, core.Phase4Attempt, string) error { return fmt.Errorf("unexpected steering") }
func (*mac12DispatchRuntime) Respond(context.Context, string, core.Worker, core.Phase4Attempt, string, string) error { return fmt.Errorf("unexpected response") }
func (*mac12DispatchRuntime) Cancel(context.Context, string, core.Worker, core.Phase4Attempt) error { return fmt.Errorf("unexpected cancellation") }
