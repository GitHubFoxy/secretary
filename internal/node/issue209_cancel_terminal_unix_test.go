//go:build !windows

package node

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestIssue209ClaudeCancelWithoutNativeTerminalPersistsAfterServerReopen(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	dir := t.TempDir()
	serverPath := filepath.Join(dir, "server.db")
	serverStore, err := core.Open(ctx, serverPath)
	if err != nil {
		t.Fatal(err)
	}
	defer serverStore.Close()
	_, conversation, err := serverStore.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewServerManager(ctx, serverStore, "pair-token", "admin-token")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	httpServer := httptest.NewServer(mux)
	defer httpServer.Close()
	identity, err := EnrollNode(ctx, httpServer.Client(), httpServer.URL, "pair-token", "macbook")
	if err != nil {
		t.Fatal(err)
	}
	instance := core.HarnessInstance{
		ID: core.HarnessInstanceID(string(identity.Node) + "/claude"),
		Node: identity.Node,
		Kind: core.HarnessClaudeCode,
		Version: "1.2.3",
		Authentication: core.HarnessAuthentication{Authenticated: true, Method: "local"},
		Status: core.HarnessReady,
		Capabilities: core.HarnessCapabilities{Execution: []core.ExecutionCapability{core.CapabilityCancel}, Activity: []core.ActivityCapability{core.ActivityStatus}},
	}
	inventory := core.HarnessInventorySnapshot{Node: identity.Node, ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{instance}}
	worker, turn, attempt, err := serverStore.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef: "issue209-worker", Intent: "run sleep", NodeID: string(identity.Node), HarnessInstanceID: string(instance.ID),
	}, core.TurnSpec{Input: "run sleep"})
	if err != nil {
		t.Fatal(err)
	}
	dispatchClaim, _, err := serverStore.ClaimWorkerCommand(ctx, "dispatch", "attempt", worker.ID, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	manager.SetCommandOutcomeSink(NewStoreCommandOutcomeSink(serverStore))
	sink := NewStoreEventSink(serverStore)
	var terminalMu sync.Mutex
	var terminal *core.AttemptOutcomeEnvelope
	manager.SetEventSink(func(ctx context.Context, event NodeEvent) error {
		if err := sink(ctx, event); err != nil {
			return err
		}
		if event.Outcome != nil {
			copy := *event.Outcome
			terminalMu.Lock()
			terminal = &copy
			terminalMu.Unlock()
		}
		return nil
	})
	launcher := filepath.Join(dir, "claude")
	shellPIDFile := filepath.Join(dir, "shell.pid")
	script := `#!/bin/sh
read prompt
/bin/sh -c 'sleep 180 & wait' &
echo $! > "$ISSUE209_SHELL_PID"
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}'
read interrupt
while read line; do :; done
`
	if err := os.WriteFile(launcher, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	nodeStore, err := OpenLocalStore(filepath.Join(dir, "node.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer nodeStore.Close()
	daemon := &Daemon{
		Identity: identity, Store: nodeStore,
		Runtime: ClaudeCodeRuntime{Command: launcher, Environment: []string{"ISSUE209_SHELL_PID=" + shellPIDFile}},
		Inventory: daemonStaticInventory{snapshot: inventory},
		Capacity: 2, HeartbeatInterval: 20 * time.Millisecond, OutboxPollInterval: 10 * time.Millisecond,
		ReconnectMin: 10 * time.Millisecond, ReconnectMax: 40 * time.Millisecond,
	}
	nodeCtx, stopNode := context.WithCancel(ctx)
	nodeDone := make(chan error, 1)
	go func() { nodeDone <- daemon.Run(nodeCtx) }()
	defer func() {
		stopNode()
		select {
		case err := <-nodeDone:
			if err != nil {
				t.Errorf("Node exit: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("Node did not exit")
		}
	}()
	waitFor(t, ctx, "Claude Node connection", func() bool {
		status, err := manager.Status(ctx, identity.Node)
		return err == nil && status.Online && len(status.Inventory.Instances) == 1
	})
	metadata := core.CommandMetadata{
		CommandID: dispatchClaim.ID, Node: identity.Node, HarnessInstanceID: instance.ID,
		WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, IssuedAt: time.Now().UTC(),
	}
	dispatch := Command{Kind: CommandDispatch, Dispatch: &DispatchCommand{
		Metadata: metadata,
		Envelope: WorkerEnvelope{
			WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID,
			OriginalUserIntent: "run sleep", HarnessInstance: instance, Profile: workerTemplateFixture(instance, "", ""),
		},
	}}
	if err := manager.SendCommand(ctx, identity.Node, dispatch); err != nil {
		t.Fatal(err)
	}
	waitFor(t, ctx, "active native Claude Attempt", func() bool {
		details, err := serverStore.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
		_, pidErr := os.ReadFile(shellPIDFile)
		return err == nil && details.CurrentAttempt() != nil && details.CurrentAttempt().State == core.AttemptActive && pidErr == nil
	})
	cancelClaim, _, err := serverStore.ClaimWorkerCommand(ctx, "cancel", "attempt", worker.ID, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	cancelMeta := metadata
	cancelMeta.CommandID = cancelClaim.ID
	cancelMeta.IssuedAt = time.Now().UTC()
	cancelCommand := Command{Kind: CommandCancel, Cancel: &CancelCommand{Metadata: cancelMeta, Reason: "owner requested cancel"}}
	if err := manager.SendCommandAndWait(ctx, identity.Node, cancelCommand); err != nil {
		t.Fatal(err)
	}
	if _, err := serverStore.MarkWorkerCommandDelivered(ctx, cancelClaim.ID); err != nil {
		t.Fatal(err)
	}
	pending, err := serverStore.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil || pending.CurrentAttempt().State != core.AttemptActive || len(pending.Results) != 0 {
		t.Fatalf("cancel receipt became premature terminal: %+v %v", pending, err)
	}
	manager.mu.Lock()
	firstConnection := manager.connections[identity.Node]
	manager.mu.Unlock()
	if firstConnection == nil {
		t.Fatal("Node connection disappeared before replay")
	}
	manager.closeConnection(identity.Node)
	waitFor(t, ctx, "Node reconnect after accepted cancellation", func() bool {
		manager.mu.Lock()
		current := manager.connections[identity.Node]
		manager.mu.Unlock()
		return current != nil && current != firstConnection
	})
	waitFor(t, ctx, "durable canceled Result", func() bool {
		details, err := serverStore.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
		return err == nil && details.CurrentAttempt().State == core.AttemptCanceled && len(details.Results) == 1 && details.Results[0].Status == core.ResultCanceled
	})
	if err := manager.SendCommandAndWait(ctx, identity.Node, cancelCommand); err != nil {
		t.Fatal(err)
	}
	terminalMu.Lock()
	saved := terminal
	terminalMu.Unlock()
	if saved == nil || saved.Status != core.OutcomeCanceled || saved.AttemptID != attempt.ID {
		t.Fatalf("missing canonical terminal outcome: %+v", saved)
	}
	reopened, err := core.Open(ctx, serverPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.RecoverPhase4Attempts(ctx, core.Phase4AttemptRecoveryFunc(func(context.Context, core.Phase4Attempt) (core.Phase4RecoveryDecision, error) {
		return core.Phase4RecoveryAwaiting, nil
	})); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, _, duplicate, err := reopened.RecordNodeAttemptOutcome(ctx, *saved); err != nil || !duplicate {
			t.Fatalf("terminal replay %d: duplicate=%t err=%v", i, duplicate, err)
		}
	}
	details, err := reopened.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		t.Fatal(err)
	}
	if details.Worker.WorkerRef != worker.WorkerRef || details.CurrentAttempt().ID != attempt.ID || details.CurrentAttempt().State != core.AttemptCanceled || len(details.Results) != 1 {
		t.Fatalf("restarted server lost terminal binding: %+v", details)
	}
	entries, err := reopened.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.Kind == core.EntryWorkerResult {
			count++
			if entry.ResultID != details.Results[0].ID {
				t.Fatalf("terminal Result identity changed: %+v", entry)
			}
		}
	}
	if count != 1 {
		t.Fatalf("canonical Result entries=%d, want one", count)
	}
}

func TestIssue209NodeRestartRecoversAcceptedClaudeCancelOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.json")
	store, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	instance := daemonInventoryFixture("macbook").Instances[0]
	instance.Kind = core.HarnessClaudeCode
	instance.ID = "macbook/claude"
	dispatch := daemonDispatchFixture("macbook", "issue209-dispatch")
	dispatch.Dispatch.Metadata.HarnessInstanceID = instance.ID
	dispatch.Dispatch.Envelope.HarnessInstance = instance
	dispatch.Dispatch.Envelope.Profile = workerTemplateFixture(instance, "", "")
	if _, _, err := store.ClaimCommand(dispatch); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteCommand(dispatch.Metadata().CommandID, acceptedOutcome(dispatch)); err != nil {
		t.Fatal(err)
	}
	meta := dispatch.Metadata()
	meta.CommandID = "issue209-cancel"
	meta.IssuedAt = time.Now().UTC()
	cancel := Command{Kind: CommandCancel, Cancel: &CancelCommand{Metadata: meta, Reason: "owner"}}
	if _, _, err := store.ClaimCommand(cancel); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteCommand(meta.CommandID, acceptedOutcome(cancel)); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for i := 0; i < 2; i++ {
		if err := reopened.RecoverRunning(ctx, nil); err != nil {
			t.Fatal(err)
		}
	}
	events, err := reopened.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, pending := range events {
		if pending.EventID != "attempt-outcome-"+dispatch.Metadata().AttemptID {
			continue
		}
		count++
		var event NodeEvent
		if err := json.Unmarshal(pending.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Outcome == nil || event.Outcome.Status != core.OutcomeCanceled ||
			event.Outcome.WorkerRef != dispatch.Metadata().WorkerRef || event.Outcome.AttemptID != dispatch.Metadata().AttemptID {
			t.Fatalf("recovered terminal identity: %+v", event)
		}
	}
	if count != 1 {
		t.Fatalf("recovered terminal events=%d, want one", count)
	}
}
