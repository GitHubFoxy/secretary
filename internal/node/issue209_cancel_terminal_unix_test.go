//go:build !windows

package node

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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
	script := filepath.Join(dir, "claude")
	content := `#!/bin/sh
echo $$ > "$ISSUE209_CLAUDE_ROOT"
read prompt
printf '%s\n' '{"type":"assistant","message":{"content":[{"type":"text","text":"working"}]}}'
read interrupt
while read line; do :; done
`
	if err := os.WriteFile(script, []byte(content), 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "node.json")
	store, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	execution := NewExecutionNode("macbook", ClaudeCodeRuntime{
		Command: script, Environment: []string{"ISSUE209_CLAUDE_ROOT=" + rootFile},
	}, store)
	instance := daemonInventoryFixture("macbook").Instances[0]
	instance.Kind = core.HarnessClaudeCode
	instance.ID = "macbook/claude"
	dispatch := daemonDispatchFixture("macbook", "issue209-live-crash-dispatch")
	dispatch.Dispatch.Metadata.HarnessInstanceID = instance.ID
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
	reopened, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.RecoverRunning(ctx, nil); err != nil {
		t.Fatal(err)
	}
	record, err := reopened.Command(dispatch.Metadata().CommandID)
	if err != nil || record.State != CommandInterrupted || !record.TerminalOutcomeQueued {
		t.Fatalf("crash-window recovery incorrectly finalized cancel: %+v err=%v", record, err)
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
			t.Fatalf("live native process inferred as canceled: %+v", event)
		}
		return
	}
	t.Fatal("live process crash-window lost interrupted event")
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
	close(runtime.release)
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
