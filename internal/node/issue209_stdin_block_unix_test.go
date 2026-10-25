//go:build !windows

package node

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

type issue209BlockedClaudeStdin struct {
	started          chan struct{}
	closed           chan struct{}
	closeOnce        sync.Once
	successfulWrites atomic.Int32
}

func (w *issue209BlockedClaudeStdin) Write([]byte) (int, error) {
	select {
	case w.started <- struct{}{}:
	default:
	}
	<-w.closed
	return 0, io.ErrClosedPipe
}

func (w *issue209BlockedClaudeStdin) Close() error {
	w.closeOnce.Do(func() { close(w.closed) })
	return nil
}

func TestIssue209CancelBlockedFirstClaudeStdinWriteStopsCLIAndPublishesTerminal(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
	defer stop()
	cmd := exec.Command("sh", "-c", "exec sleep 180")
	configureClaudeProcess(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	writer := &issue209BlockedClaudeStdin{started: make(chan struct{}, 1), closed: make(chan struct{})}
	session := &claudeSession{
		id:                   "4b22ac0e-109c-4aac-8a17-3e78cba13809",
		cmd:                  cmd,
		stdin:                writer,
		stdout:               stdout,
		stderr:               &bytes.Buffer{},
		activity:             make(chan Activity, 4),
		result:               make(chan Result, 4),
		ready:                make(chan error, 1),
		writeGate:            make(chan struct{}, 1),
		done:                 make(chan struct{}),
		initialPromptPending: true,
	}
	server, err := core.Open(ctx, filepath.Join(t.TempDir(), "server.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	_, conversation, err := server.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	instance := daemonInventoryFixture("macbook").Instances[0]
	instance.ID = "macbook/claude"
	instance.Kind = core.HarnessClaudeCode
	worker, _, previousAttempt, err := server.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef: "issue209-blocked-stdin", Intent: "initial", NodeID: "macbook", HarnessInstanceID: string(instance.ID),
	}, core.TurnSpec{Input: "initial"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := server.RecordAttemptOutcome(ctx, previousAttempt.ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "prior completed",
	}); err != nil {
		t.Fatal(err)
	}
	turn, attempt, err := server.CreateTurn(ctx, worker.ID, core.TurnSpec{Input: "continue", CommandKind: "resume"})
	if err != nil {
		t.Fatal(err)
	}
	claim, _, err := server.ClaimWorkerCommand(ctx, "resume", "attempt", worker.ID, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	meta := core.CommandMetadata{CommandID: claim.ID, Node: "macbook", HarnessInstanceID: instance.ID,
		WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID, IssuedAt: time.Now().UTC()}
	envelope := WorkerEnvelope{WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID,
		PreviousAttemptID: previousAttempt.ID, OriginalUserIntent: "continue",
		HarnessInstance: instance, Profile: workerTemplateFixture(instance, "", "")}
	command := Command{Kind: CommandResume, Resume: &ResumeCommand{Metadata: meta, Envelope: envelope}}
	local, err := OpenLocalStore(filepath.Join(t.TempDir(), "node.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	if _, _, err := local.ClaimCommand(command); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	session.claudeRoot = claudeChild{pid: cmd.Process.Pid}
	go session.run()
	execution := NewExecutionNode("macbook", nil, local)
	execution.watchSession(session, envelope, command)
	defer session.Close()
	promptDone := make(chan error, 1)
	go func() { promptDone <- session.Prompt(ctx, "run a long task") }()
	select {
	case <-writer.started:
	case <-ctx.Done():
		t.Fatal("initial Claude stdin write never became blocked")
	}
	if len(session.writeGate) != 1 {
		t.Fatal("blocked Claude stdin did not hold writeGate")
	}
	cancelEntered := make(chan struct{})
	session.beforeInitialCancelClaim = func() { close(cancelEntered) }
	cancelDone := make(chan error, 1)
	go func() { cancelDone <- session.Cancel(ctx) }()
	select {
	case <-cancelEntered:
	case <-ctx.Done():
		t.Fatal("Cancel never reached held stdin writeGate")
	}
	select {
	case err := <-cancelDone:
		if err != nil {
			t.Fatalf("Cancel failed to stop blocked Claude CLI: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Cancel waited for blocked stdin writeGate instead of stopping CLI")
	}
	select {
	case err := <-promptDone:
		if err == nil {
			t.Fatal("native initial Prompt completed after Cancel")
		}
	case <-ctx.Done():
		t.Fatal("blocked native Prompt never returned after CLI shutdown")
	}
	waitFor(t, ctx, "blocked CLI durable terminal", func() bool {
		record, err := local.Command(meta.CommandID)
		return err == nil && record.TerminalOutcomeQueued
	})
	events, err := local.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	terminals := 0
	for _, pending := range events {
		if pending.EventID != "attempt-outcome-"+attempt.ID {
			continue
		}
		var event NodeEvent
		if err := json.Unmarshal(pending.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Outcome == nil || event.Outcome.Status != core.OutcomeCanceled {
			t.Fatalf("blocked Claude stdin produced wrong terminal: %+v", event)
		}
		terminals++
		apply := NewStoreEventSink(server)
		if err := apply(ctx, event); err != nil {
			t.Fatal(err)
		}
		if err := apply(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if terminals != 1 {
		t.Fatalf("blocked stdin durable terminal count=%d, want one", terminals)
	}
	details, err := server.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil || len(details.Results) != 2 || details.Results[1].Status != core.ResultCanceled {
		t.Fatalf("blocked stdin missing canceled Server Result: %+v err=%v", details, err)
	}
	entries, err := server.EntriesAfter(ctx, conversation.ID, 0)
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
		t.Fatalf("blocked stdin produced %d canonical Server Results", count)
	}
	select {
	case <-session.done:
	case <-ctx.Done():
		t.Fatal("Claude CLI was not stopped and reaped")
	}
	if cmd.ProcessState == nil {
		t.Fatal("Claude CLI was not reaped")
	}
	if writer.successfulWrites.Load() != 0 {
		t.Fatal("native Prompt was injected after accepted Cancel")
	}

}
