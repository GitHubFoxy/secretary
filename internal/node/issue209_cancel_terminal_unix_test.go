//go:build !windows

package node

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestIssue209ClaudeCancelWithoutNativeTerminalPersistsAfterServerReopen(t *testing.T) {
	issue209ClaudeCancelDaemonLifecycle(t, false)
}

func TestIssue209ClaudeNativeErrorAfterAcceptedCancelCreatesCanceledServerResult(t *testing.T) {
	issue209ClaudeCancelDaemonLifecycle(t, true)
}

func issue209ClaudeCancelDaemonLifecycle(t *testing.T, nativeError bool) {
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
	rootPIDFile := filepath.Join(dir, "claude.pid")
	nativeErrorRelease := filepath.Join(dir, "native-error.release")
	script := `#!/bin/sh
echo ROOT_PID_TOKEN > "$ISSUE209_ROOT_PID"
read prompt
/bin/sh -c 'sleep 180 & wait' &
echo $! > "$ISSUE209_SHELL_PID"
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}'
read interrupt
while read line; do :; done
`
	script = strings.Replace(script, "ROOT_PID_TOKEN", strings.Repeat("$", 2), 1)
	if nativeError {
		script = strings.Replace(script, "read interrupt\n", "read interrupt\nwhile [ ! -f \"$ISSUE209_NATIVE_ERROR_RELEASE\" ]; do sleep 0.02; done\nprintf '%s\\n' '{\"type\":\"error\",\"error\":\"native cancellation response\"}'\n", 1)
	}
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
		Runtime: ClaudeCodeRuntime{Command: launcher, Environment: []string{"ISSUE209_SHELL_PID=" + shellPIDFile, "ISSUE209_ROOT_PID=" + rootPIDFile, "ISSUE209_NATIVE_ERROR_RELEASE=" + nativeErrorRelease}},
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
	if !nativeError {
		pending, err := serverStore.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
		if err != nil || pending.CurrentAttempt().State != core.AttemptActive || len(pending.Results) != 0 {
			t.Fatalf("cancel receipt became premature terminal: %+v %v", pending, err)
		}
	}
	if nativeError {
		if err := os.WriteFile(nativeErrorRelease, []byte("release"), 0600); err != nil {
			t.Fatal(err)
		}
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
	if nativeError {
		data, err := os.ReadFile(rootPIDFile)
		if err != nil {
			t.Fatal(err)
		}
		rootPID, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil {
			t.Fatal(err)
		}
		waitFor(t, ctx, "Claude CLI exit after native error", func() bool {
			processes, inspectErr := claudeProcessSnapshot()
			if inspectErr != nil {
				return false
			}
			process, exists := processes[rootPID]
			return !exists || strings.HasPrefix(process.state, "Z")
		})
	}
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

func TestIssue209NodeRestartDoesNotInferCancelFromReceipt(t *testing.T) {
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
		if pending.EventID != "interrupted-"+dispatch.Metadata().CommandID {
			continue
		}
		count++
		var event NodeEvent
		if err := json.Unmarshal(pending.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Outcome == nil || event.Outcome.Status != core.OutcomeInterrupted ||
			event.Outcome.WorkerRef != dispatch.Metadata().WorkerRef || event.Outcome.AttemptID != dispatch.Metadata().AttemptID {
			t.Fatalf("recovered terminal identity: %+v", event)
		}
	}
	if count != 1 {
		t.Fatalf("recovered terminal events=%d, want one", count)
	}
}

func TestIssue209NodeRestartDoesNotInferCanceledInFlightResume(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.json")
	store, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	instance := daemonInventoryFixture("macbook").Instances[0]
	instance.Kind = core.HarnessClaudeCode
	instance.ID = "macbook/claude"
	source := daemonDispatchFixture("macbook", "issue209-resume")
	source.Dispatch.Metadata.HarnessInstanceID = instance.ID
	source.Dispatch.Envelope.HarnessInstance = instance
	source.Dispatch.Envelope.Profile = workerTemplateFixture(instance, "", "")
	source.Dispatch.Envelope.PreviousAttemptID = "previous-attempt"
	resume := Command{Kind: CommandResume, Resume: &ResumeCommand{Metadata: source.Dispatch.Metadata, Envelope: source.Dispatch.Envelope}}
	if _, _, err := store.ClaimCommand(resume); err != nil {
		t.Fatal(err)
	}
	cancelMeta := resume.Metadata()
	cancelMeta.CommandID = "issue209-cancel-resume"
	cancelMeta.IssuedAt = time.Now().UTC()
	if err := store.RecordNativeResumeCancel(resume.Metadata().CommandID, cancelMeta); err != nil {
		t.Fatal(err)
	}
	cancel := Command{Kind: CommandCancel, Cancel: &CancelCommand{Metadata: cancelMeta, Reason: "owner"}}
	if _, _, err := store.ClaimCommand(cancel); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteCommand(cancelMeta.CommandID, acceptedOutcome(cancel)); err != nil {
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
	record, err := reopened.Command(resume.Metadata().CommandID)
	if err != nil || record.State != CommandInterrupted || !record.TerminalOutcomeQueued {
		t.Fatalf("native Resume cancellation not recovered: %+v %v", record, err)
	}
	events, err := reopened.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, pending := range events {
		if pending.EventID != "interrupted-"+resume.Metadata().CommandID {
			continue
		}
		count++
		var event NodeEvent
		if err := json.Unmarshal(pending.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Outcome == nil || event.Outcome.Status != core.OutcomeInterrupted ||
			event.Outcome.AttemptID != resume.Metadata().AttemptID {
			t.Fatalf("recovered native Resume terminal: %+v", event)
		}
	}
	if count != 1 {
		t.Fatalf("native Resume terminal events=%d, want one", count)
	}
}

func TestIssue209NodeRestartDoesNotInferOtherProviderCancellation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.json")
	store, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	dispatch := daemonDispatchFixture("macbook", "issue209-fx-dispatch")
	if _, _, err := store.ClaimCommand(dispatch); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteCommand(dispatch.Metadata().CommandID, acceptedOutcome(dispatch)); err != nil {
		t.Fatal(err)
	}
	meta := dispatch.Metadata()
	meta.CommandID = "issue209-fx-cancel"
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
	if err := reopened.RecoverRunning(ctx, nil); err != nil {
		t.Fatal(err)
	}
	events, err := reopened.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	for _, pending := range events {
		if pending.EventID != "interrupted-"+dispatch.Metadata().CommandID {
			continue
		}
		var event NodeEvent
		if err := json.Unmarshal(pending.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Outcome == nil || event.Outcome.Status != core.OutcomeInterrupted {
			t.Fatalf("other provider recovery changed: %+v", event)
		}
		return
	}
	t.Fatal("other provider lost interrupted recovery event")
}

func TestIssue209NodeCrashWithLiveClaudeAfterAcceptedCancelIsInterrupted(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	dir := t.TempDir()
	rootFile := filepath.Join(dir, "claude.pid")
	sleepFile := filepath.Join(dir, "sleep.pid")
	script := filepath.Join(dir, "claude")
	content := `#!/bin/sh
echo $$ > "$ISSUE209_CLAUDE_ROOT"
read prompt
/bin/sh -c 'sleep 180 & echo $! > "$ISSUE209_SLEEP_PID"; wait' &
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}'
read interrupt
while read line; do :; done
`
	if err := os.WriteFile(script, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	serverStore, err := core.Open(ctx, filepath.Join(dir, "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer serverStore.Close()
	_, conversation, err := serverStore.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, turn, attempt, err := serverStore.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef: "issue209-crash-worker", Intent: "run sleep", NodeID: "macbook", HarnessInstanceID: "macbook/claude",
	}, core.TurnSpec{Input: "run sleep"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "node.json")
	store, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	execution := NewExecutionNode("macbook", ClaudeCodeRuntime{
		Command: script, Environment: []string{"ISSUE209_CLAUDE_ROOT=" + rootFile, "ISSUE209_SLEEP_PID=" + sleepFile},
	}, store)
	instance := daemonInventoryFixture("macbook").Instances[0]
	instance.Kind = core.HarnessClaudeCode
	instance.ID = "macbook/claude"
	dispatch := daemonDispatchFixture("macbook", "issue209-live-crash-dispatch")
	dispatch.Dispatch.Metadata.HarnessInstanceID = instance.ID
	dispatch.Dispatch.Metadata.WorkerRef = worker.WorkerRef
	dispatch.Dispatch.Metadata.TurnID = turn.ID
	dispatch.Dispatch.Metadata.AttemptID = attempt.ID
	dispatch.Dispatch.Envelope.WorkerRef = worker.WorkerRef
	dispatch.Dispatch.Envelope.TurnID = turn.ID
	dispatch.Dispatch.Envelope.AttemptID = attempt.ID
	dispatch.Dispatch.Envelope.HarnessInstance = instance
	dispatch.Dispatch.Envelope.Profile = workerTemplateFixture(instance, "", "")
	first, err := execution.HandleCommand(ctx, dispatch)
	if err != nil || first.State != CommandAccepted {
		t.Fatalf("Claude dispatch=%+v err=%v", first, err)
	}
	session, ok := execution.session(dispatch.Metadata().AttemptID)
	if !ok {
		t.Fatal("native Claude session not registered")
	}
	defer session.Close()
	var pid int
	waitFor(t, ctx, "native Claude root PID", func() bool {
		data, readErr := os.ReadFile(rootFile)
		if readErr != nil {
			return false
		}
		pid, readErr = strconv.Atoi(strings.TrimSpace(string(data)))
		return readErr == nil && pid > 1
	})
	var sleepPID int
	waitFor(t, ctx, "real descendant sleep PID", func() bool {
		data, readErr := os.ReadFile(sleepFile)
		if readErr != nil {
			return false
		}
		sleepPID, readErr = strconv.Atoi(strings.TrimSpace(string(data)))
		return readErr == nil && sleepPID > 1
	})
	meta := dispatch.Metadata()
	meta.CommandID = "issue209-live-crash-cancel"
	meta.IssuedAt = time.Now().UTC()
	cancel := Command{Kind: CommandCancel, Cancel: &CancelCommand{Metadata: meta, Reason: "owner"}}
	outcome, err := execution.HandleCommand(ctx, cancel)
	if err != nil || outcome.State != CommandAccepted {
		t.Fatalf("native cancel=%+v err=%v", outcome, err)
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("native CLI exited before crash-window recovery: %v", err)
	}
	processes, err := claudeProcessSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if child, ok := processes[sleepPID]; ok && !strings.HasPrefix(child.state, "Z") {
		t.Fatalf("native descendant survived accepted cancel: %+v", child)
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
	record, err := reopened.Command(dispatch.Metadata().CommandID)
	if err != nil || record.State != CommandInterrupted || !record.TerminalOutcomeQueued {
		t.Fatalf("crash-window recovery incorrectly finalized cancel: %+v err=%v", record, err)
	}
	events, err := reopened.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	sink := NewStoreEventSink(serverStore)
	for _, pending := range events {
		if pending.EventID == "attempt-outcome-"+dispatch.Metadata().AttemptID {
			t.Fatal("accepted Cancel produced a fabricated terminal event")
		}
		if pending.EventID != "interrupted-"+dispatch.Metadata().CommandID {
			continue
		}
		count++
		var event NodeEvent
		if err := json.Unmarshal(pending.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Outcome == nil || event.Outcome.Status != core.OutcomeInterrupted {
			t.Fatalf("live native process inferred as canceled: %+v", event)
		}
		for i := 0; i < 2; i++ {
			if err := sink(ctx, event); err != nil {
				t.Fatalf("replay %d interrupted terminal to Server: %v", i, err)
			}
		}
	}
	if count != 1 {
		t.Fatalf("crash-window recovery emitted %d interrupted outcomes, want one", count)
	}
	details, err := serverStore.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil || details.CurrentAttempt() == nil || string(details.CurrentAttempt().State) != "interrupted" ||
		len(details.Results) != 1 || string(details.Results[0].Status) != "interrupted" {
		t.Fatalf("crash-window Server published wrong Result: %+v err=%v", details, err)
	}
	entries, err := serverStore.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	resultCount := 0
	for _, entry := range entries {
		if entry.Kind == core.EntryWorkerResult {
			resultCount++
			if entry.ResultID != details.Results[0].ID {
				t.Fatalf("crash-window Result identity changed: %+v", entry)
			}
		}
	}
	if resultCount != 1 {
		t.Fatalf("crash-window canonical Server Result count=%d, want one", resultCount)
	}
}

func TestIssue209DaemonSameSocketCancelWhileClaudeResumeBlocked(t *testing.T) {
	issue209SameSocketClaudeResumeCancel(t, false, false)
}

func TestIssue209DaemonCancelDuringHandoffRegistrationWindow(t *testing.T) {
	issue209SameSocketClaudeResumeCancel(t, true, false)
}

func TestIssue209DaemonCancelRegisteredClaudeResumeSession(t *testing.T) {
	issue209SameSocketClaudeResumeCancel(t, false, true)
}

type issue209ReadyResumeSession struct {
	*t12HoldSession
	cancels atomic.Int32
}

func (s *issue209ReadyResumeSession) Cancel(context.Context) error {
	s.cancels.Add(1)
	s.result <- Result{Status: "canceled", Summary: "Claude native attempt canceled after cleanup"}
	return nil
}

type issue209ReadyResumeRuntime struct {
	*t12RemoteStalledResumeRuntime
	active *issue209ReadyResumeSession
}

func (r *issue209ReadyResumeRuntime) Resume(ctx context.Context, request StartRequest, nativeID string) (Session, error) {
	session, err := r.t12RemoteStalledResumeRuntime.Resume(ctx, request, nativeID)
	if err != nil {
		return nil, err
	}
	r.active = &issue209ReadyResumeSession{t12HoldSession: session.(*t12HoldSession)}
	return r.active, nil
}

func issue209SameSocketClaudeResumeCancel(t *testing.T, pauseRegistration, readySession bool) {
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	serverStore, err := core.Open(ctx, filepath.Join(t.TempDir(), "server.db"))
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
	manager.SetCommandOutcomeSink(NewStoreCommandOutcomeSink(serverStore))
	manager.SetEventSink(NewStoreEventSink(serverStore))
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	server := httptest.NewServer(mux)
	defer server.Close()
	identity, err := EnrollNode(ctx, server.Client(), server.URL, "pair-token", "macbook")
	if err != nil {
		t.Fatal(err)
	}
	instance := daemonInventoryFixture(identity.Node).Instances[0]
	instance.Kind = core.HarnessClaudeCode
	instance.ID = core.HarnessInstanceID(string(identity.Node) + "/claude")
	inventory := core.HarnessInventorySnapshot{Node: identity.Node, ObservedAt: time.Now().UTC(), Instances: []core.HarnessInstance{instance}}
	runtime := &t12RemoteStalledResumeRuntime{entered: make(chan struct{}, 1), release: make(chan struct{})}
	var provider Runtime = runtime
	var activeRuntime *issue209ReadyResumeRuntime
	if readySession {
		activeRuntime = &issue209ReadyResumeRuntime{t12RemoteStalledResumeRuntime: runtime}
		provider = activeRuntime
	}
	defer func() {
		select {
		case <-runtime.release:
		default:
			close(runtime.release)
		}
		if runtime.session != nil {
			_ = runtime.session.Close()
		}
	}()
	local, err := OpenLocalStore(filepath.Join(t.TempDir(), "node.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	daemon := &Daemon{
		Identity: identity, Store: local, Runtime: provider, Inventory: daemonStaticInventory{snapshot: inventory},
		Capacity: 2, HeartbeatInterval: 20 * time.Millisecond, OutboxPollInterval: 10 * time.Millisecond,
		ReconnectMin: 10 * time.Millisecond, ReconnectMax: 40 * time.Millisecond,
	}
	handoffEntered := make(chan struct{})
	handoffReleased := make(chan struct{})
	if pauseRegistration {
		daemon.nativeResumeRegistrationBarrier = func() {
			close(handoffEntered)
			<-handoffReleased
		}
	}
	daemonCtx, stopDaemon := context.WithCancel(ctx)
	daemonDone := make(chan error, 1)
	go func() { daemonDone <- daemon.Run(daemonCtx) }()
	defer func() {
		stopDaemon()
		select {
		case err := <-daemonDone:
			if err != nil {
				t.Errorf("daemon exited: %v", err)
			}
		case <-time.After(time.Second):
			t.Error("daemon did not stop")
		}
	}()
	if pauseRegistration {
		defer func() {
			select {
			case <-handoffReleased:
			default:
				close(handoffReleased)
			}
		}()
	}
	waitFor(t, ctx, "authenticated same-socket Claude Node", func() bool {
		manager.mu.Lock()
		connected := manager.connections[identity.Node] != nil
		manager.mu.Unlock()
		return connected
	})
	worker, firstTurn, firstAttempt, err := serverStore.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef: "issue209-socket-worker", Intent: "initial work", NodeID: string(identity.Node), HarnessInstanceID: string(instance.ID),
	}, core.TurnSpec{Input: "initial work"})
	if err != nil {
		t.Fatal(err)
	}
	firstClaim, _, err := serverStore.ClaimWorkerCommand(ctx, "dispatch", "attempt", worker.ID, firstAttempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	metadata := core.CommandMetadata{
		CommandID: firstClaim.ID, Node: identity.Node, HarnessInstanceID: instance.ID,
		WorkerRef: worker.WorkerRef, TurnID: firstTurn.ID, AttemptID: firstAttempt.ID, IssuedAt: time.Now().UTC(),
	}
	envelope := WorkerEnvelope{
		WorkerRef: worker.WorkerRef, TurnID: firstTurn.ID, AttemptID: firstAttempt.ID,
		OriginalUserIntent: "initial work", HarnessInstance: instance, Profile: workerTemplateFixture(instance, "", ""),
	}
	first := Command{Kind: CommandDispatch, Dispatch: &DispatchCommand{Metadata: metadata, Envelope: envelope}}
	if err := manager.SendCommandAndWait(ctx, identity.Node, first); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := serverStore.RecordAttemptOutcome(ctx, firstAttempt.ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "prior completed",
	}); err != nil {
		t.Fatal(err)
	}
	turn, attempt, err := serverStore.CreateTurn(ctx, worker.ID, core.TurnSpec{Input: "continue", CommandKind: "resume"})
	if err != nil {
		t.Fatal(err)
	}
	resumeClaim, _, err := serverStore.ClaimWorkerCommand(ctx, "resume", "attempt", worker.ID, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	resumeMetadata := metadata
	resumeMetadata.CommandID = resumeClaim.ID
	resumeMetadata.TurnID = turn.ID
	resumeMetadata.AttemptID = attempt.ID
	resumeMetadata.IssuedAt = time.Now().UTC()
	resumeEnvelope := envelope
	resumeEnvelope.PreviousAttemptID = firstAttempt.ID
	resumeEnvelope.TurnID = turn.ID
	resumeEnvelope.AttemptID = attempt.ID
	resumeEnvelope.OriginalUserIntent = "continue"
	resume := Command{Kind: CommandResume, Resume: &ResumeCommand{Metadata: resumeMetadata, Envelope: resumeEnvelope}}
	if err := manager.SendCommand(ctx, identity.Node, resume); err != nil {
		t.Fatal(err)
	}
	if readySession {
		select {
		case <-runtime.entered:
		case <-ctx.Done():
			t.Fatal("Claude native Resume never entered")
		}
		close(runtime.release)
		waitFor(t, ctx, "registered Claude Resume readiness", func() bool {
			record, err := local.Command(resumeMetadata.CommandID)
			_, mapped := local.SessionMapping(resumeMetadata.AttemptID)
			return err == nil && record.Outcome.State == CommandAccepted && mapped
		})
	} else if pauseRegistration {
		select {
		case <-handoffEntered:
		case <-ctx.Done():
			t.Fatal("Claude Resume did not reach handoff-registration window")
		}
	} else {
		select {
		case <-runtime.entered:
		case <-ctx.Done():
			t.Fatal("Claude native Resume never entered")
		}
	}
	manager.mu.Lock()
	socketBefore := manager.connections[identity.Node]
	manager.mu.Unlock()
	if socketBefore == nil {
		t.Fatal("initial native Resume WebSocket absent")
	}
	before, err := local.Command(resumeMetadata.CommandID)
	if err != nil || before.State != CommandProcessing || before.SafePrestart {
		t.Fatalf("native Resume was not blocked beyond handoff: %+v err=%v", before, err)
	}
	cancelClaim, _, err := serverStore.ClaimWorkerCommand(ctx, "cancel", "attempt", worker.ID, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	cancelMetadata := resumeMetadata
	cancelMetadata.CommandID = cancelClaim.ID
	cancelMetadata.IssuedAt = time.Now().UTC()
	cancel := Command{Kind: CommandCancel, Cancel: &CancelCommand{Metadata: cancelMetadata, Reason: "owner"}}
	cancelCtx, cancelStop := context.WithTimeout(ctx, 2*time.Second)
	defer cancelStop()
	if pauseRegistration {
		cancelOutcome := make(chan error, 1)
		go func() { cancelOutcome <- manager.SendCommandAndWait(cancelCtx, identity.Node, cancel) }()
		select {
		case <-cancelOutcome:
			t.Fatal("Cancel replied before handoff-registration barrier released")
		case <-time.After(100 * time.Millisecond):
		}
		close(handoffReleased)
		select {
		case <-runtime.entered:
		case <-ctx.Done():
			t.Fatal("Claude native Resume failed to enter after releasing handoff barrier")
		}
		select {
		case err := <-cancelOutcome:
			if err != nil {
				t.Fatalf("Cancel blocked in native Resume registration window: %v", err)
			}
		case <-cancelCtx.Done():
			t.Fatal("Cancel blocked in native Resume registration window")
		}
	} else if err := manager.SendCommandAndWait(cancelCtx, identity.Node, cancel); err != nil {
		t.Fatalf("Cancel blocked behind native Resume on one socket: %v", err)
	}
	manager.mu.Lock()
	socketAfter := manager.connections[identity.Node]
	manager.mu.Unlock()
	if socketAfter == nil || socketAfter != socketBefore {
		t.Fatal("Cancel required a WebSocket reconnect")
	}
	after, err := local.Command(resumeMetadata.CommandID)
	if readySession {
		if err != nil || after.NativeResumeCancelRequested {
			t.Fatalf("registered Claude Session fell back to native Resume checkpoint: %+v err=%v", after, err)
		}
		if activeRuntime.active == nil || activeRuntime.active.cancels.Load() != 1 {
			t.Fatal("active Claude Session did not receive exactly one native Cancel")
		}
	} else {
		if err != nil || !after.NativeResumeCancelRequested || after.State != CommandProcessing {
			t.Fatalf("Cancel missed in-flight native Resume: %+v err=%v", after, err)
		}
		if runtime.Context() == nil || runtime.Context().Err() == nil {
			t.Fatal("Cancel did not cancel daemon-owned native Resume")
		}
	}
	details, err := serverStore.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil || details.CurrentAttempt() == nil || details.CurrentAttempt().State.Terminal() {
		t.Fatalf("Cancel receipt fabricated terminal before native returned: %+v err=%v", details, err)
	}
	if !readySession {
		close(runtime.release)
	}
	waitFor(t, ctx, "same-socket canceled Server Result", func() bool {
		details, err := serverStore.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
		return err == nil && details.CurrentAttempt() != nil && details.CurrentAttempt().State == core.AttemptCanceled &&
			len(details.Results) == 2 && details.Results[1].Status == core.ResultCanceled
	})
	record, err := local.Command(resumeMetadata.CommandID)
	if readySession {
		if err != nil || record.Outcome.State != CommandAccepted {
			t.Fatalf("active Resume readiness not durable: %+v err=%v", record, err)
		}
	} else if err != nil || record.State != CommandAccepted || !record.TerminalOutcomeQueued {
		t.Fatalf("same-socket terminal not durable: %+v err=%v", record, err)
	}
	entries, err := serverStore.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.Kind == core.EntryWorkerResult && entry.TurnID == turn.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("same-socket canceled Result entries=%d, want one", count)
	}
}

func TestIssue209ClaudeNativeResumeCancelProducesCanonicalCanceledResult(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 12*time.Second)
	defer stop()
	serverPath := filepath.Join(t.TempDir(), "server.db")
	serverStore, err := core.Open(ctx, serverPath)
	if err != nil {
		t.Fatal(err)
	}
	defer serverStore.Close()
	_, conversation, err := serverStore.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	worker, initialTurn, initialAttempt, err := serverStore.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef: "issue209-resume-worker", Intent: "do work", NodeID: "macbook", HarnessInstanceID: "macbook/claude",
	}, core.TurnSpec{Input: "initial work"})
	if err != nil {
		t.Fatal(err)
	}
	instance := daemonInventoryFixture("macbook").Instances[0]
	instance.Kind = core.HarnessClaudeCode
	instance.ID = "macbook/claude"
	runtime := &t12RemoteStalledResumeRuntime{entered: make(chan struct{}, 1), release: make(chan struct{})}
	defer func() {
		select {
		case <-runtime.release:
		default:
			close(runtime.release)
		}
		if runtime.session != nil {
			_ = runtime.session.Close()
		}
	}()
	local, err := OpenLocalStore(filepath.Join(t.TempDir(), "node.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	execution := NewExecutionNode("macbook", runtime, local)
	dispatchClaim, _, err := serverStore.ClaimWorkerCommand(ctx, "dispatch", "attempt", worker.ID, initialAttempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	metadata := core.CommandMetadata{
		CommandID: dispatchClaim.ID, Node: "macbook", HarnessInstanceID: instance.ID,
		WorkerRef: worker.WorkerRef, TurnID: initialTurn.ID, AttemptID: initialAttempt.ID, IssuedAt: time.Now().UTC(),
	}
	initial := Command{Kind: CommandDispatch, Dispatch: &DispatchCommand{
		Metadata: metadata,
		Envelope: WorkerEnvelope{
			WorkerRef: worker.WorkerRef, TurnID: initialTurn.ID, AttemptID: initialAttempt.ID,
			OriginalUserIntent: "initial work", HarnessInstance: instance, Profile: workerTemplateFixture(instance, "", ""),
		},
	}}
	first, err := execution.HandleCommand(ctx, initial)
	if err != nil || first.State != CommandAccepted {
		t.Fatalf("initial Dispatch=%+v err=%v", first, err)
	}
	receiptSink := NewStoreCommandOutcomeSink(serverStore)
	if err := receiptSink(ctx, "macbook", first); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := serverStore.RecordAttemptOutcome(ctx, initialAttempt.ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "prior completed",
	}); err != nil {
		t.Fatal(err)
	}
	turn, attempt, err := serverStore.CreateTurn(ctx, worker.ID, core.TurnSpec{Input: "continue", CommandKind: "resume"})
	if err != nil {
		t.Fatal(err)
	}
	resumeClaim, _, err := serverStore.ClaimWorkerCommand(ctx, "resume", "attempt", worker.ID, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	resumeMetadata := metadata
	resumeMetadata.CommandID = resumeClaim.ID
	resumeMetadata.TurnID = turn.ID
	resumeMetadata.AttemptID = attempt.ID
	resumeMetadata.IssuedAt = time.Now().UTC()
	envelope := initial.Dispatch.Envelope
	envelope.PreviousAttemptID = initialAttempt.ID
	envelope.TurnID = turn.ID
	envelope.AttemptID = attempt.ID
	envelope.OriginalUserIntent = "continue"
	resume := Command{Kind: CommandResume, Resume: &ResumeCommand{Metadata: resumeMetadata, Envelope: envelope}}
	type finishedResume struct {
		outcome CommandOutcome
		err error
	}
	finished := make(chan finishedResume, 1)
	go func() {
		outcome, err := execution.HandleCommand(ctx, resume)
		finished <- finishedResume{outcome, err}
	}()
	select {
	case <-runtime.entered:
	case <-ctx.Done():
		t.Fatal("native Claude Resume did not enter")
	}
	cancelClaim, _, err := serverStore.ClaimWorkerCommand(ctx, "cancel", "attempt", worker.ID, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	cancelMetadata := resumeMetadata
	cancelMetadata.CommandID = cancelClaim.ID
	cancelMetadata.IssuedAt = time.Now().UTC()
	cancel := Command{Kind: CommandCancel, Cancel: &CancelCommand{Metadata: cancelMetadata, Reason: "owner"}}
	accepted, err := execution.HandleCommand(ctx, cancel)
	if err != nil || accepted.State != CommandAccepted {
		t.Fatalf("native cancel=%+v err=%v", accepted, err)
	}
	if _, err := serverStore.MarkWorkerCommandDelivered(ctx, cancelClaim.ID); err != nil {
		t.Fatal(err)
	}
	current, err := serverStore.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil || current.CurrentAttempt().ID != attempt.ID || current.CurrentAttempt().State.Terminal() {
		t.Fatalf("Cancel receipt fabricated a Result: %+v err=%v", current, err)
	}
	local.mu.Lock()
	healthyPath := local.path
	local.path = filepath.Join(t.TempDir(), "missing-directory", "node.json")
	local.mu.Unlock()
	defer func() {
		local.mu.Lock()
		local.path = healthyPath
		local.mu.Unlock()
	}()
	close(runtime.release)
	waitFor(t, ctx, "closed late native Session before terminal persistence", func() bool {
		runtime.mu.Lock()
		late := runtime.lateSession
		runtime.mu.Unlock()
		if late == nil {
			return false
		}
		select {
		case <-late.activity:
			return true
		default:
			return false
		}
	})
	select {
	case result := <-finished:
		t.Fatalf("terminal persistence error completed Resume prematurely: %+v", result)
	case <-time.After(175 * time.Millisecond):
	}
	uncommitted, err := local.Command(resumeMetadata.CommandID)
	if err != nil || uncommitted.State != CommandProcessing || uncommitted.TerminalOutcomeQueued {
		t.Fatalf("terminal outcome committed during persistence failure: %+v err=%v", uncommitted, err)
	}
	beforeRepair, err := local.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range beforeRepair {
		if event.EventID == "attempt-outcome-"+attempt.ID || event.EventID == "command-outcome-"+resumeMetadata.CommandID {
			t.Fatalf("failed terminal persistence published a receipt: %+v", event)
		}
	}
	local.mu.Lock()
	local.path = healthyPath
	local.mu.Unlock()
	var terminalCommand CommandOutcome
	select {
	case result := <-finished:
		if result.err != nil || result.outcome.State != CommandAccepted {
			t.Fatalf("canceled native Resume receipt=%+v err=%v", result.outcome, result.err)
		}
		terminalCommand = result.outcome
	case <-ctx.Done():
		t.Fatal("canceled native Resume did not finish")
	}
	runtime.mu.Lock()
	late := runtime.lateSession
	runtime.mu.Unlock()
	if late == nil {
		t.Fatal("native Resume did not return a late Session")
	}
	select {
	case <-late.activity:
	default:
		t.Fatal("late native session was not closed")
	}
	record, err := local.Command(resumeMetadata.CommandID)
	if err != nil || record.State != CommandAccepted || !record.TerminalOutcomeQueued {
		t.Fatalf("terminal native Resume not persisted: %+v err=%v", record, err)
	}
	if _, err := local.QueueLifecycleCommandOutcome(terminalCommand); err != nil {
		t.Fatal(err)
	}
	sink := NewStoreEventSink(serverStore)
	events, err := local.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, pending := range events {
		if pending.Type == MessageCommandOutcome {
			var receipt CommandOutcome
			if err := json.Unmarshal(pending.Payload, &receipt); err != nil {
				t.Fatal(err)
			}
			if err := receiptSink(ctx, "macbook", receipt); err != nil {
				t.Fatal(err)
			}
			continue
		}
		var event NodeEvent
		if err := json.Unmarshal(pending.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Outcome == nil {
			continue
		}
		if event.Outcome.AttemptID != attempt.ID || event.Outcome.Status != core.OutcomeCanceled {
			t.Fatalf("incorrect native terminal: %+v", event.Outcome)
		}
		count++
		for i := 0; i < 2; i++ {
			if err := sink(ctx, event); err != nil {
				t.Fatal(err)
			}
		}
	}
	if count != 1 {
		t.Fatalf("terminal canceled outcomes=%d, want one", count)
	}
	reopened, err := core.Open(ctx, serverPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	details, err := reopened.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil || details.CurrentAttempt().State != core.AttemptCanceled || len(details.Results) != 2 ||
		details.Results[len(details.Results)-1].Status != core.ResultCanceled {
		t.Fatalf("native Resume did not create durable canceled Result: %+v err=%v", details, err)
	}
	entries, err := reopened.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	count = 0
	for _, entry := range entries {
		if entry.Kind == core.EntryWorkerResult && entry.TurnID == turn.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("native Resume produced %d canonical Results", count)
	}
}

type issue209RejectedCleanupSession struct {
	Session
}

func (s *issue209RejectedCleanupSession) Close() error {
	return context.DeadlineExceeded
}

type issue209UnverifiedNativeResumeRuntime struct {
	t12HoldRuntime
	entered chan struct{}
	release chan struct{}
}

func (r *issue209UnverifiedNativeResumeRuntime) Resume(_ context.Context, _ StartRequest, _ string) (Session, error) {
	select {
	case r.entered <- struct{}{}:
	default:
	}
	<-r.release
	session := &issue209RejectedCleanupSession{
		Session: &t12HoldSession{runtime: &r.t12HoldRuntime, activity: make(chan Activity), result: make(chan Result)},
	}
	return nil, claudeResumeInitializationFailure(session, context.Canceled)
}

func TestIssue209ClaudeNativeResumeUnverifiedCleanupIsInterrupted(t *testing.T) {
	issue209NativeResumeUnverifiedCleanup(t, false)
}

func TestIssue209ClaudeNativeResumeErrorCancelInterleaving(t *testing.T) {
	issue209NativeResumeUnverifiedCleanup(t, true)
}

func issue209NativeResumeUnverifiedCleanup(t *testing.T, afterNativeError bool) {
	ctx, cancelContext := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancelContext()
	serverStore, err := core.Open(ctx, filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer serverStore.Close()
	_, conversation, err := serverStore.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	instance := daemonInventoryFixture("macbook").Instances[0]
	instance.ID = "macbook/claude"
	instance.Kind = core.HarnessClaudeCode
	worker, turn, attempt, err := serverStore.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef: "issue209-uncertain-cleanup", Intent: "initial work", NodeID: "macbook", HarnessInstanceID: string(instance.ID),
	}, core.TurnSpec{Input: "initial work"})
	if err != nil {
		t.Fatal(err)
	}
	local, err := OpenLocalStore(filepath.Join(t.TempDir(), "node.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	runtime := &issue209UnverifiedNativeResumeRuntime{entered: make(chan struct{}, 1), release: make(chan struct{})}
	defer func() {
		select {
		case <-runtime.release:
		default:
			close(runtime.release)
		}
	}()
	execution := NewExecutionNode("macbook", runtime, local)
	errorReached := make(chan struct{})
	finishError := make(chan struct{})
	if afterNativeError {
		execution.nativeResumeErrorBeforeFinalization = func() {
			close(errorReached)
			<-finishError
		}
		defer func() {
			select {
			case <-finishError:
			default:
				close(finishError)
			}
		}()
	}
	firstMeta := core.CommandMetadata{
		CommandID: "issue209-uncertain-first", Node: "macbook", HarnessInstanceID: instance.ID,
		WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, IssuedAt: time.Now().UTC(),
	}
	envelope := WorkerEnvelope{
		WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID,
		OriginalUserIntent: "initial work", HarnessInstance: instance, Profile: workerTemplateFixture(instance, "", ""),
	}
	first := Command{Kind: CommandDispatch, Dispatch: &DispatchCommand{Metadata: firstMeta, Envelope: envelope}}
	if accepted, err := execution.HandleCommand(ctx, first); err != nil || accepted.State != CommandAccepted {
		t.Fatalf("first Dispatch=%+v err=%v", accepted, err)
	}
	if _, _, _, err := serverStore.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "completed",
	}); err != nil {
		t.Fatal(err)
	}
	continued, nextAttempt, err := serverStore.CreateTurn(ctx, worker.ID, core.TurnSpec{Input: "continue", CommandKind: "resume"})
	if err != nil {
		t.Fatal(err)
	}
	resumeClaim, _, err := serverStore.ClaimWorkerCommand(ctx, "resume", "attempt", worker.ID, nextAttempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	resumeMeta := firstMeta
	resumeMeta.CommandID = resumeClaim.ID
	resumeMeta.TurnID = continued.ID
	resumeMeta.AttemptID = nextAttempt.ID
	resumeMeta.IssuedAt = time.Now().UTC()
	resumeEnvelope := envelope
	resumeEnvelope.PreviousAttemptID = attempt.ID
	resumeEnvelope.TurnID = continued.ID
	resumeEnvelope.AttemptID = nextAttempt.ID
	resumeEnvelope.OriginalUserIntent = "continue"
	resume := Command{Kind: CommandResume, Resume: &ResumeCommand{Metadata: resumeMeta, Envelope: resumeEnvelope}}
	type completed struct {
		outcome CommandOutcome
		err error
	}
	finished := make(chan completed, 1)
	go func() {
		outcome, err := execution.HandleCommand(ctx, resume)
		finished <- completed{outcome: outcome, err: err}
	}()
	select {
	case <-runtime.entered:
	case <-ctx.Done():
		t.Fatal("native Resume never entered")
	}
	if afterNativeError {
		close(runtime.release)
		select {
		case <-errorReached:
		case <-ctx.Done():
			t.Fatal("native Resume error did not reach terminal decision window")
		}
		if !execution.nativeResumeRunning(resumeMeta.AttemptID, worker.WorkerRef, resumeMeta.CommandID) {
			t.Fatal("native Resume error released cancellation identity prematurely")
		}
		record, err := local.Command(resumeMeta.CommandID)
		if err != nil || record.State != CommandProcessing || record.TerminalOutcomeQueued {
			t.Fatalf("native Resume error finalized before Cancel: %+v err=%v", record, err)
		}
	}
	cancelMeta := resumeMeta
	cancelMeta.CommandID = "issue209-uncertain-cancel"
	cancelMeta.IssuedAt = time.Now().UTC()
	cancelCommand := Command{Kind: CommandCancel, Cancel: &CancelCommand{Metadata: cancelMeta, Reason: "owner"}}
	cancelOutcome, err := execution.HandleCommand(ctx, cancelCommand)
	if err != nil || cancelOutcome.State != CommandAccepted {
		t.Fatalf("Cancel=%+v err=%v", cancelOutcome, err)
	}
	if afterNativeError {
		record, err := local.Command(resumeMeta.CommandID)
		if err != nil || !record.NativeResumeCancelRequested || record.State != CommandProcessing {
			t.Fatalf("Cancel checkpoint did not win native error window: %+v err=%v", record, err)
		}
		close(finishError)
	} else {
		close(runtime.release)
	}
	var terminalReceipt CommandOutcome
	select {
	case result := <-finished:
		if result.err != nil || result.outcome.State != CommandInterrupted {
			t.Fatalf("unverified native cleanup must stay interrupted: %+v err=%v", result.outcome, result.err)
		}
		terminalReceipt = result.outcome
	case <-ctx.Done():
		t.Fatal("native cleanup did not finish")
	}
	record, err := local.Command(resumeMeta.CommandID)
	if err != nil || record.State != CommandInterrupted || !record.TerminalOutcomeQueued {
		t.Fatalf("unverified cleanup not durably interrupted: %+v err=%v", record, err)
	}
	events, err := local.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, pending := range events {
		if pending.EventID != "attempt-outcome-"+nextAttempt.ID {
			continue
		}
		count++
		var event NodeEvent
		if err := json.Unmarshal(pending.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Outcome == nil || event.Outcome.Status != core.OutcomeInterrupted {
			t.Fatalf("unverified cleanup published canceled: %+v", event)
		}
		if err := NewStoreEventSink(serverStore)(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if count != 1 {
		t.Fatalf("unverified cleanup outcomes=%d, want one", count)
	}
	if err := NewStoreCommandOutcomeSink(serverStore)(ctx, "macbook", terminalReceipt); err != nil {
		t.Fatalf("interrupted native Resume receipt reconciled as failure: %v", err)
	}
	details, err := serverStore.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil || len(details.Results) != 2 || string(details.Results[1].Status) != "interrupted" {
		t.Fatalf("unverified native cleanup produced wrong Server Result: %+v err=%v", details, err)
	}
}

func TestIssue209ClaudeInitializationCloseFailureIsNotProof(t *testing.T) {
	original := context.Canceled
	session := &issue209RejectedCleanupSession{Session: &t12HoldSession{
		runtime: &t12HoldRuntime{}, activity: make(chan Activity), result: make(chan Result),
	}}
	err := claudeResumeInitializationFailure(session, original)
	var confirmed *claudeNativeCleanupConfirmedError
	if errors.As(err, &confirmed) || !errors.Is(err, ErrClaudeCodeUnavailable) {
		t.Fatalf("failed native Close incorrectly attested cleanup: %v", err)
	}
}

func TestIssue209ClaudeResumeCancelDuringPreNativePreparation(t *testing.T) {
	for _, stage := range []string{"artifact", "workspace"} {
		t.Run(stage, func(t *testing.T) {
			ctx, stop := context.WithTimeout(context.Background(), 12*time.Second)
			defer stop()
			store, err := OpenLocalStore(filepath.Join(t.TempDir(), "node.json"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			runtime := &t12HoldRuntime{}
			execution := NewExecutionNode("macbook", runtime, store)
			initial := daemonDispatchFixture("macbook", "issue209-preparation-initial")
			instance := initial.Dispatch.Envelope.HarnessInstance
			instance.ID = "macbook/claude"
			instance.Kind = core.HarnessClaudeCode
			initial.Dispatch.Envelope.HarnessInstance = instance
			initial.Dispatch.Envelope.Profile = workerTemplateFixture(instance, "", "")
			initial.Dispatch.Metadata.HarnessInstanceID = instance.ID
			initial.Dispatch.Envelope.OriginalUserIntent = "initial request"
			first, err := execution.HandleCommand(ctx, initial)
			if err != nil || first.State != CommandAccepted {
				t.Fatalf("initial native Dispatch=%+v err=%v", first, err)
			}
			metadata := initial.Metadata()
			metadata.CommandID = "issue209-preparation-resume-" + stage
			metadata.AttemptID = "attempt-issue209-preparation-" + stage
			metadata.TurnID = "turn-issue209-preparation-" + stage
			metadata.IssuedAt = time.Now().UTC()
			envelope := initial.Dispatch.Envelope
			envelope.PreviousAttemptID = initial.Metadata().AttemptID
			envelope.AttemptID = metadata.AttemptID
			envelope.TurnID = metadata.TurnID
			envelope.OriginalUserIntent = "continue"
			if stage == "artifact" {
				envelope.OriginalUserIntent = "Read artifact://art_" + strings.Repeat("a", 32)
			} else {
				envelope.PreviousAttemptID = "attempt-without-workspace-mapping"
			}
			resume := Command{Kind: CommandResume, Resume: &ResumeCommand{Metadata: metadata, Envelope: envelope}}
			entered := make(chan struct{})
			release := make(chan struct{})
			execution.nativeResumePreparationBarrier = func() {
				close(entered)
				<-release
			}
			type finishedResume struct {
				outcome CommandOutcome
				err error
			}
			finished := make(chan finishedResume, 1)
			go func() {
				outcome, err := execution.HandleCommand(ctx, resume)
				finished <- finishedResume{outcome: outcome, err: err}
			}()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("Claude Resume never reached pre-native preparation")
			}
			cancelMeta := metadata
			cancelMeta.CommandID = "issue209-preparation-cancel-" + stage
			cancelMeta.IssuedAt = time.Now().UTC()
			cancel := Command{Kind: CommandCancel, Cancel: &CancelCommand{Metadata: cancelMeta, Reason: "owner"}}
			accepted, err := execution.HandleCommand(ctx, cancel)
			if err != nil || accepted.State != CommandAccepted {
				t.Fatalf("Cancel in %s preparation=%+v err=%v", stage, accepted, err)
			}
			record, err := store.Command(metadata.CommandID)
			if err != nil || !record.NativeResumeCancelRequested || record.State != CommandProcessing {
				t.Fatalf("pre-native checkpoint missing: %+v err=%v", record, err)
			}
			close(release)
			select {
			case result := <-finished:
				if result.err != nil || result.outcome.State != CommandAccepted {
					t.Fatalf("accepted Cancel became pre-native failed Resume: %+v err=%v", result.outcome, result.err)
				}
			case <-ctx.Done():
				t.Fatal("pre-native preparation did not finish")
			}
			record, err = store.Command(metadata.CommandID)
			if err != nil || record.State != CommandAccepted || !record.TerminalOutcomeQueued {
				t.Fatalf("pre-native cancellation did not persist terminal: %+v err=%v", record, err)
			}
			if runtime.resumes.Load() != 0 {
				t.Fatal("pre-native Cancel launched native Resume")
			}
			events, err := store.PendingEvents()
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, item := range events {
				if item.EventID != "attempt-outcome-"+metadata.AttemptID {
					continue
				}
				count++
				var event NodeEvent
				if err := json.Unmarshal(item.Payload, &event); err != nil {
					t.Fatal(err)
				}
				if event.Outcome == nil || event.Outcome.Status != core.OutcomeCanceled {
					t.Fatalf("pre-native Cancel published non-canceled terminal: %+v", event)
				}
			}
			if count != 1 {
				t.Fatalf("pre-native terminal count=%d, want one", count)
			}
		})
	}
}
