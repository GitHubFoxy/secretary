package ctl

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

// The provider is an external executable, not a mock of a Secretary module.
// Native identity and nonce stay private to its synthetic workspace.
func TestIdleFollowUpACPProcess(t *testing.T) {
	if !testProcessHasArgument("-test.run=^TestIdleFollowUpACPProcess$") {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	id, workspace := "", ""
	var heldPromptID json.RawMessage
	for scanner.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			os.Exit(2)
		}
		if req.Method == "session/cancel" {
			return
		}
		if len(req.ID) == 0 {
			continue
		}
		result := any(map[string]any{})
		switch req.Method {
		case "initialize":
			result = map[string]any{"protocolVersion": 1}
		case "session/new", "session/load":
			var p struct {
				Cwd       string `json:"cwd"`
				SessionID string `json:"sessionId"`
			}
			if json.Unmarshal(req.Params, &p) != nil {
				os.Exit(2)
			}
			workspace = p.Cwd
			operation := "L"
			if req.Method == "session/new" {
				operation = "N"
			}
			file, err := os.OpenFile(filepath.Join(workspace, "private-session-operations"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				os.Exit(2)
			}
			if _, err = file.WriteString(operation); err != nil {
				os.Exit(2)
			}
			file.Close()
			if req.Method == "session/new" {
				f, err := os.CreateTemp(workspace, "private-native-*")
				if err != nil {
					os.Exit(2)
				}
				id = filepath.Base(f.Name())
				f.Close()
			} else {
				id = p.SessionID
				if _, err := os.Stat(filepath.Join(workspace, id)); err != nil {
					os.Exit(2)
				}
			}
			result = map[string]any{"sessionId": id}
		case "_session/steering":
			var p struct {
				Prompt []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			}
			if json.Unmarshal(req.Params, &p) != nil {
				os.Exit(2)
			}
			if len(p.Prompt) > 0 && p.Prompt[0].Text == "finish-held" && len(heldPromptID) > 0 {
				encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": id, "update": map[string]any{"sessionUpdate": "agent_message_chunk", "messageId": "private-final", "content": map[string]string{"type": "text", "text": "held complete"}}}})
				encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": heldPromptID, "result": map[string]any{"stopReason": "end_turn"}})
				heldPromptID = nil
			}
			result = map[string]any{"outcome": "injected"}
		case "session/prompt":
			var p struct {
				Prompt []struct {
					Text string `json:"text"`
				} `json:"prompt"`
			}
			if json.Unmarshal(req.Params, &p) != nil || len(p.Prompt) == 0 {
				os.Exit(2)
			}
			if p.Prompt[0].Text == "hold" {
				heldPromptID = append(json.RawMessage(nil), req.ID...)
				file, err := os.OpenFile(filepath.Join(workspace, "private-held-prompts"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					os.Exit(2)
				}
				if _, err = file.WriteString("H"); err != nil {
					os.Exit(2)
				}
				file.Close()
				if os.WriteFile(filepath.Join(workspace, "private-hold-ready"), []byte("ready"), 0600) != nil {
					os.Exit(2)
				}
				continue // a real active ACP prompt, interrupted by the public Node Cancel below
			}
			if p.Prompt[0].Text == "recall" {
				if os.WriteFile(filepath.Join(workspace, "private-recall-sent"), []byte("sent"), 0600) != nil {
					os.Exit(2)
				}
			}
			summary := "remembered=false"
			file := filepath.Join(workspace, id)
			if strings.HasPrefix(p.Prompt[0].Text, "remember ") {
				os.WriteFile(file, []byte(strings.TrimPrefix(p.Prompt[0].Text, "remember ")), 0600)
				summary = "initial complete"
			} else {
				saved, _ := os.ReadFile(file)
				expected, _ := os.ReadFile(filepath.Join(workspace, "private-nonce"))
				if string(saved) == string(expected) && len(saved) > 0 {
					summary = "remembered=true"
				}
			}
			encoder.Encode(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"sessionId": id, "update": map[string]any{"sessionUpdate": "agent_message_chunk", "messageId": "private-final", "content": map[string]string{"type": "text", "text": summary}}}})
			result = map[string]any{"stopReason": "end_turn"}
		}
		if encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result}) != nil {
			return
		}
	}
}

type idleInventory struct{ snapshot core.HarnessInventorySnapshot }

func (i idleInventory) Discover(context.Context) (core.HarnessInventorySnapshot, error) {
	return i.snapshot, nil
}

func TestPublicIdleFollowUpRetainsNativeHistory(t *testing.T) {
	runPublicIdleFollowUp(t, false)
}

func TestOpenCodeNativePublicIdleFollowUp(t *testing.T) {
	if os.Getenv("SECRETARY_OPENCODE_ACP_E2E") != "1" {
		t.Skip("private native fixture is opt-in")
	}
	runPublicIdleFollowUp(t, true)
}

func runPublicIdleFollowUp(t *testing.T, native bool) {
	t.Helper()
	fixtureTimeout := 20 * time.Second
	if native {
		fixtureTimeout = 60 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), fixtureTimeout)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	person, _, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, person.ID)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := node.NewServerManagerWithConfig(ctx, store, node.ServerConfig{PairingTokens: []string{"synthetic-pair"}, AdminToken: "synthetic-admin"})
	if err != nil {
		t.Fatal(err)
	}
	manager.SetEventSink(node.NewStoreEventSink(store))
	manager.SetCommandOutcomeSink(func(_ context.Context, _ core.NodeReference, outcome node.CommandOutcome) error {
		t.Logf("safe receipt: kind=%s state=%s code=%s", outcome.Kind, outcome.State, outcome.ErrorCode)
		return nil
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	server := httptest.NewServer(mux)
	defer server.Close()
	identity, err := node.EnrollNode(ctx, server.Client(), server.URL, "synthetic-pair", "synthetic-node")
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	nonce := "synthetic-private-nonce"
	if os.WriteFile(filepath.Join(workspace, "private-nonce"), []byte(nonce), 0600) != nil {
		t.Fatal("nonce unavailable")
	}
	instance := core.HarnessInstance{ID: "synthetic-node/opencode", Node: identity.Node, Kind: core.HarnessOpenCode, Version: "2.0.22", Status: core.HarnessReady, Authentication: core.HarnessAuthentication{Authenticated: true}, Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityCancel}}, ModelIDs: []core.ObservedModelID{"fixture/model"}}
	inventory := core.HarnessInventorySnapshot{Node: identity.Node, ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{instance}}
	statePath := filepath.Join(t.TempDir(), "node-state.json")
	local, err := node.OpenLocalStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { local.Close() }()
	// ACPRuntime is used at the external wire boundary; no internal runtime mocks.
	var runtime node.Runtime = node.ACPRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestIdleFollowUpACPProcess$"}, DrainPromptEvents: true, TerminalMessageGrouping: true}
	var nativeCalls func() int32
	if native {
		runtime, nativeCalls = nativeIdleRuntime(t, nonce, &instance)
		inventory.Instances = []core.HarnessInstance{instance}
	}
	var stop context.CancelFunc
	var done chan error
	start := func() {
		daemonCtx, c := context.WithCancel(ctx)
		stop = c
		done = make(chan error, 1)
		d := node.Daemon{Identity: identity, Store: local, Runtime: runtime, Inventory: idleInventory{inventory}, // Heartbeat races are a separate transport seam; seed public Core
			// inventory once per connection rather than racing SQLite writes.
			HeartbeatInterval: time.Hour, OutboxPollInterval: 5 * time.Millisecond}
		go func() { done <- d.Run(daemonCtx) }()
		waitIdleFixture(t, ctx, func() bool {
			status, e := manager.Status(ctx, identity.Node)
			if e != nil || !status.Online {
				return false
			}
			return true
		})
		if err := store.UpdateNodeHeartbeat(ctx, identity.Node, inventory, core.NodeHeartbeat{Capacity: 1}); err != nil {
			t.Fatal(err)
		}
	}
	start()
	defer func() { stop(); <-done }()
	project, err := store.CreateProject(ctx, core.ProjectSpec{ID: "idle-project", Name: "Idle", Mappings: []core.ProjectPathMapping{{Node: identity.Node, Path: workspace}}, Policy: core.ProjectPolicy{AllowedHarnessKinds: []core.HarnessKind{core.HarnessOpenCode}}})
	if err != nil {
		t.Fatal(err)
	}
	service := WorkerService{Store: store, PersonID: person.ID, Capability: capability, Runtime: NodeRuntime{Manager: manager}, WorkerPolicy: core.HarnessPolicy{DefaultHarness: core.HarnessOpenCode, ModelID: "fixture/model", Reasoning: func() string {
		if native {
			return "low"
		}
		return ""
	}()}, WorkerProfileSource: func() (node.ManagedProfile, error) {
		return node.ManagedProfile{Version: "synthetic-v1", Name: "worker", Content: "Synthetic instructions", Hash: "synthetic-source", Runtime: "opencode", Model: "fixture/model", Reasoning: func() string {
			if native {
				return "low"
			}
			return ""
		}(), AllowTools: []string{"read"}, Skills: []node.ManagedSkill{}}, nil
	}}
	details, err := service.SpawnWorker(ctx, SpawnWorkerRequest{Intent: "remember " + nonce, ProjectID: project.ID, IdempotencyKey: "initial"})
	if err != nil {
		t.Fatal(err)
	}
	ref := details.Worker.WorkerRef
	await := func(count int) core.WorkerDetails {
		var d core.WorkerDetails
		t.Logf("await Results=%d", count)
		waitIdleFixture(t, ctx, func() bool {
			var e error
			d, e = service.GetWorker(ctx, ref)
			return e == nil && d.Worker.Status == core.WorkerIdle && len(d.Results) == count
		})
		return d
	}
	details = await(1)
	first := details.CurrentAttempt().ID
	initialMapping, ok := local.SessionMapping(first)
	if !ok {
		t.Fatal("initial mapping absent")
	}
	frozen := details.Worker.ProfileSnapshot
	service.WorkerProfileSource = func() (node.ManagedProfile, error) {
		t.Error("Follow-up read current defaults")
		return node.ManagedProfile{}, fmt.Errorf("unavailable")
	}
	for phase := 1; phase <= 2; phase++ {
		if phase == 2 {
			stop()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			waitIdleFixture(t, ctx, func() bool { status, err := manager.Status(ctx, identity.Node); return err == nil && !status.Online })
			if local.Close() != nil {
				t.Fatal("close failed")
			}
			local, err = node.OpenLocalStore(statePath)
			if err != nil {
				t.Fatal(err)
			}
			start()
		}
		request := MessageWorkerRequest{WorkerRef: ref, Text: "recall", IdempotencyKey: fmt.Sprintf("followup-%d", phase)}
		_, err = service.MessageWorker(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		details = await(phase + 1)
		mapping, ok := local.SessionMapping(details.CurrentAttempt().ID)
		if !ok || mapping.RuntimeSessionID != initialMapping.RuntimeSessionID || details.CurrentAttempt().ID == first || details.Worker.ProfileSnapshot != frozen {
			t.Fatal("Follow-up replaced native identity or frozen binding")
		}
		for _, result := range details.Results {
			if result.TurnID == details.Worker.CurrentTurnID && result.Summary != "remembered=true" {
				t.Fatal("idle Follow-up lost remembered nonce")
			}
		}
		// Replay after completion must not create a Turn, Attempt or another Result.
		if _, err := service.MessageWorker(ctx, request); err != nil {
			t.Fatal(err)
		}
		replay, err := service.GetWorker(ctx, ref)
		if err != nil || len(replay.Attempts) != phase+1 || len(replay.Results) != phase+1 {
			t.Fatal("Follow-up replay duplicated lifecycle")
		}
	}
	public, err := json.Marshal(details)
	if err != nil || strings.Contains(string(public), initialMapping.RuntimeSessionID) || strings.Contains(string(public), "previous_attempt_id") {
		t.Fatal("private continuation metadata escaped public state")
	}
	if native {
		// Inject only the durable crash state: native history already exists,
		// but this Follow-up input has not crossed the external ACP boundary.
		beforeCalls := nativeCalls()
		turn, attempt, err := store.CreateTurn(ctx, details.Worker.ID, core.TurnSpec{Input: "recall", IdempotencyKey: "native-crash-window", CommandKind: "dispatch"})
		if err != nil {
			t.Fatal(err)
		}
		binding, err := store.ResolveWorkerBinding(ctx, details.Worker.ID)
		if err != nil {
			t.Fatal(err)
		}
		var profile node.ManagedProfile
		if json.Unmarshal([]byte(frozen), &profile) != nil {
			t.Fatal("native frozen template unavailable")
		}
		intent, found, err := store.FindWorkerCommand(ctx, "dispatch", "attempt", details.Worker.ID, attempt.ID)
		if err != nil || !found {
			t.Fatal("native continuation command intent absent")
		}
		envelope := node.WorkerEnvelope{PreviousAttemptID: turn.PreviousAttemptID, WorkerRef: ref, TurnID: turn.ID, AttemptID: attempt.ID, OriginalUserIntent: turn.Input, ProjectID: details.Worker.ProjectID, ProjectSnapshot: binding.Snapshot, Workspace: binding.Workspace, HarnessInstance: binding.HarnessInstance, Model: binding.Snapshot.Policy.ModelPin(), Reasoning: binding.Snapshot.Policy.Reasoning, Profile: profile}
		command := node.Command{Kind: node.CommandDispatch, Dispatch: &node.DispatchCommand{Metadata: core.CommandMetadata{CommandID: intent.ID, Node: identity.Node, HarnessInstanceID: instance.ID, WorkerRef: ref, TurnID: turn.ID, AttemptID: attempt.ID, IssuedAt: time.Now().UTC()}, Envelope: envelope}}
		stop()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		waitIdleFixture(t, ctx, func() bool { status, err := manager.Status(ctx, identity.Node); return err == nil && !status.Online })
		mapping, ok := local.SessionMapping(turn.PreviousAttemptID)
		if !ok {
			t.Fatal("native predecessor missing")
		}
		if _, _, err := local.ClaimCommand(command); err != nil {
			t.Fatal(err)
		}
		mapping.AttemptID, mapping.TurnID = attempt.ID, turn.ID
		if local.SaveSessionMapping(mapping) != nil {
			t.Fatal("native crash fixture mapping failed")
		}
		if _, err := local.SaveCommandReadiness(intent.ID, node.CommandOutcome{CommandID: intent.ID, Kind: command.Kind, State: node.CommandAccepted, TurnID: turn.ID, AttemptID: attempt.ID}); err != nil {
			t.Fatal(err)
		}
		local, err = node.OpenLocalStore(statePath)
		if err != nil {
			t.Fatal(err)
		}
		start()
		waitIdleFixture(t, ctx, func() bool {
			d, err := service.GetWorker(ctx, ref)
			if err != nil {
				return false
			}
			details = d
			return len(d.Results) == 4 && d.Worker.Status == core.WorkerOffline
		})
		record, err := local.Command(intent.ID)
		if err != nil || record.State != node.CommandInterrupted || nativeCalls() != beforeCalls {
			t.Fatal("native recovery accepted/replayed an unproven input")
		}
		if _, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: ref, Text: "recall", IdempotencyKey: "native-explicit-resume"}); err != nil {
			t.Fatal(err)
		}
		details = await(5)
		resumed, ok := local.SessionMapping(details.CurrentAttempt().ID)
		if !ok || resumed.RuntimeSessionID != initialMapping.RuntimeSessionID || details.Worker.ProfileSnapshot != frozen || nativeCalls() != beforeCalls+1 {
			t.Fatal("native explicit Resume replaced identity/profile or replayed input")
		}
		for _, result := range details.Results {
			if result.TurnID == details.Worker.CurrentTurnID && result.Summary != "remembered=true" {
				t.Fatal("native explicit Resume lost history")
			}
		}
		return
	}
	// A public active message must steer the current Attempt, not allocate
	// a new native session. Core's public recovery operation supplies the
	// interrupted outcome, as after a lost Node process.
	_, err = service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: ref, Text: "hold", IdempotencyKey: "hold"})
	if err != nil {
		t.Fatal(err)
	}
	waitIdleFixture(t, ctx, func() bool { _, err := os.Stat(filepath.Join(workspace, "private-hold-ready")); return err == nil })
	held, err := service.GetWorker(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	holdAttempt := *held.CurrentAttempt()
	intent, found, err := store.FindWorkerCommand(ctx, "dispatch", "attempt", held.Worker.ID, holdAttempt.ID)
	if err != nil || !found {
		t.Fatal("held continuation intent missing")
	}
	claim, err := local.Command(intent.ID)
	if err != nil || claim.State != node.CommandProcessing {
		t.Fatal("readiness persisted accepted before execution evidence")
	}
	if _, err := store.SetPhase4AttemptActive(ctx, holdAttempt.ID); err != nil {
		t.Fatal(err)
	}
	active, err := service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: ref, Text: "active steering", IdempotencyKey: "active"})
	if err != nil || active.CurrentAttempt().ID != holdAttempt.ID || len(active.Attempts) != 4 {
		t.Fatal("active message replaced its Attempt")
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, holdAttempt.ID, core.AttemptOutcomeInput{Status: core.OutcomeInterrupted, Classification: core.OutcomeFinal, Summary: "Synthetic Node interruption"}); err != nil {
		t.Fatal(err)
	}
	terminalSequence := local.NextEventSequence()
	if err := manager.SendCommandAndWait(ctx, identity.Node, node.Command{Kind: node.CommandCancel, Cancel: &node.CancelCommand{Metadata: core.CommandMetadata{CommandID: "synthetic-close-interrupted", Node: identity.Node, HarnessInstanceID: instance.ID, WorkerRef: ref, TurnID: holdAttempt.TurnID, AttemptID: holdAttempt.ID, IssuedAt: time.Now().UTC()}}}); err != nil {
		t.Fatal(err)
	}
	waitIdleFixture(t, ctx, func() bool { return local.LastAcknowledgedSequence() >= terminalSequence })
	stop()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	waitIdleFixture(t, ctx, func() bool { status, err := manager.Status(ctx, identity.Node); return err == nil && !status.Online })
	if local.Close() != nil {
		t.Fatal("interrupted Node close failed")
	}
	local, err = node.OpenLocalStore(statePath)
	if err != nil {
		t.Fatal(err)
	}
	start()
	_, err = service.MessageWorker(ctx, MessageWorkerRequest{WorkerRef: ref, Text: "recall", IdempotencyKey: "explicit-resume"})
	if err != nil {
		t.Fatal(err)
	}
	details = await(5)
	resumed, ok := local.SessionMapping(details.CurrentAttempt().ID)
	if !ok || resumed.RuntimeSessionID != initialMapping.RuntimeSessionID || details.Worker.ProfileSnapshot != frozen {
		t.Fatal("interrupted Resume replaced identity or template")
	}
	for _, result := range details.Results {
		if result.TurnID == details.Worker.CurrentTurnID && result.Summary != "remembered=true" {
			t.Fatal("explicit Resume lost prior history")
		}
	}
	// Enrollment revocation is enforced before historical ACK delivery, even
	// when the Node still has a connection and a saved accepted outcome.
	last, found, err := store.FindWorkerCommand(ctx, "resume", "attempt", details.Worker.ID, details.CurrentAttempt().ID)
	if err != nil || !found {
		t.Fatal("resume intent missing")
	}
	record, err := local.Command(last.ID)
	if err != nil {
		t.Fatal(err)
	}
	var duplicate node.Command
	if json.Unmarshal(record.CommandJSON, &duplicate) != nil {
		t.Fatal("resume command fixture invalid")
	}
	if _, err := store.RevokeNode(ctx, identity.Node); err != nil {
		t.Fatal(err)
	}
	if err := manager.SendCommandAndWait(ctx, identity.Node, duplicate); !errors.Is(err, core.ErrNodeRevoked) {
		t.Fatal("revoked enrollment bypassed duplicate ACK boundary")
	}
}

func waitIdleFixture(t *testing.T, ctx context.Context, ready func() bool) {
	t.Helper()
	for !ready() {
		select {
		case <-ctx.Done():
			t.Fatal("public lifecycle fixture timed out")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
