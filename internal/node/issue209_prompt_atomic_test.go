package node

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

type issue209PromptSink struct {
	mu       sync.Mutex
	records  []map[string]any
	failUser bool
}

func (s *issue209PromptSink) Write(data []byte) (int, error) {
	var message map[string]any
	if err := json.Unmarshal(data, &message); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failUser && message["type"] == "user" {
		return 0, errors.New("Claude stdin write rejected")
	}
	s.records = append(s.records, message)
	return len(data), nil
}

func (*issue209PromptSink) Close() error { return nil }

func (s *issue209PromptSink) Count(kind string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, message := range s.records {
		if message["type"] == kind {
			count++
		}
	}
	return count
}

func issue209DeferredPromptSession(sink io.WriteCloser) *claudeSession {
	return &claudeSession{
		id:                   "4b22ac0e-109c-4aac-8a17-3e78cba13809",
		stdin:                sink,
		writeGate:            make(chan struct{}, 1),
		result:               make(chan Result, 3),
		claudeRoot:           claudeChild{pid: 123},
		initialPromptPending: true,
		cleanupAttempt:       func(context.Context, *exec.Cmd, claudeChild) error { return nil },
	}
}

func TestIssue209DeferredClaudePromptNeverWritesAfterAcceptedCancel(t *testing.T) {
	for _, cancelFirst := range []bool{true, false} {
		name := "prompt_is_admitted_first"
		if cancelFirst {
			name = "cancel_is_accepted_first"
		}
		t.Run(name, func(t *testing.T) {
			ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			sink := &issue209PromptSink{}
			session := issue209DeferredPromptSession(sink)
			entered := make(chan struct{})
			release := make(chan struct{})
			if cancelFirst {
				session.beforeInitialPromptAdmission = func() { close(entered); <-release }
			} else {
				session.beforeInitialPromptWrite = func() { close(entered); <-release }
			}
			promptFinished := make(chan error, 1)
			go func() { promptFinished <- session.Prompt(ctx, "start native attempt") }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("first Claude prompt did not reach injection barrier")
			}
			cancelFinished := make(chan error, 1)
			cancelEntered := make(chan struct{})
			session.beforeInitialCancelClaim = func() { close(cancelEntered) }
			if !cancelFirst && len(session.writeGate) != 1 {
				t.Fatal("first Prompt did not hold writeGate at the concurrency barrier")
			}
			go func() { cancelFinished <- session.Cancel(ctx) }()
			select {
			case <-cancelEntered:
			case <-ctx.Done():
				t.Fatal("Cancel did not reach competing initial Prompt state")
			}
			if cancelFirst {
				select {
				case err := <-cancelFinished:
					if err != nil {
						t.Fatalf("Cancel before deferred prompt: %v", err)
					}
				case <-ctx.Done():
					t.Fatal("Cancel did not preempt waiting Claude prompt")
				}
				close(release)
				select {
				case err := <-promptFinished:
					if err == nil {
						t.Fatal("native prompt injected after Cancel acceptance")
					}
				case <-ctx.Done():
					t.Fatal("canceled deferred prompt remained blocked")
				}
				if sink.Count("user") != 0 {
					t.Fatal("native user injection occurred after accepted Cancel")
				}
				if err := session.Prompt(ctx, "unexpected replay"); err == nil {
					t.Fatal("a canceled initial Prompt restarted")
				}
			} else {
				select {
				case err := <-cancelFinished:
					if err == nil {
						t.Fatal("Cancel reported successful native cleanup despite missing process state")
					}
				case <-ctx.Done():
					t.Fatal("Cancel was blocked by the held stdin writeGate")
				}
				close(release)
				select {
				case err := <-promptFinished:
					if err == nil {
						t.Fatal("initial Prompt injected after native process stop")
					}
				case <-ctx.Done():
					t.Fatal("stopped initial Prompt did not unblock")
				}
				if sink.Count("user") != 0 {
					t.Fatal("native Prompt injection occurred after failed process cleanup")
				}
			}
		})
	}
}

func TestIssue209FailedDeferredClaudePromptDeliversTerminalResult(t *testing.T) {
	sink := &issue209PromptSink{failUser: true}
	session := issue209DeferredPromptSession(sink)
	err := session.Prompt(context.Background(), "native attempt")
	if err == nil || !strings.Contains(err.Error(), "stdin write rejected") {
		t.Fatalf("deferred native stdin write error=%v", err)
	}
	select {
	case result := <-session.Result():
		if result.Status != "failed" || result.FailureCode != "native_prompt_write_failed" {
			t.Fatalf("first deferred Prompt error not terminal: %+v", result)
		}
	default:
		t.Fatal("failed initial Prompt left Attempt without terminal")
	}
	select {
	case result := <-session.Result():
		t.Fatalf("duplicate failed Prompt Result: %+v", result)
	default:
	}
}

func TestIssue209DeferredClaudePromptWriteErrorCancelRaceOneDurableServerResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
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
	worker, firstTurn, firstAttempt, err := server.CreateWorker(ctx, conversation.ID, core.WorkerSpec{
		WorkerRef: "issue209-stdin-error", Intent: "initial", NodeID: "macbook", HarnessInstanceID: string(instance.ID),
	}, core.TurnSpec{Input: "initial"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := server.RecordAttemptOutcome(ctx, firstAttempt.ID, core.AttemptOutcomeInput{
		Status: core.OutcomeSucceeded, Classification: core.OutcomeFinal, Summary: "prior completed",
	}); err != nil {
		t.Fatal(err)
	}
	turn, attempt, err := server.CreateTurn(ctx, worker.ID, core.TurnSpec{Input: "continue", CommandKind: "resume"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, _, err := server.ClaimWorkerCommand(ctx, "resume", "attempt", worker.ID, attempt.ID)
	if err != nil {
		t.Fatal(err)
	}
	metadata := core.CommandMetadata{
		CommandID: claimed.ID, Node: "macbook", HarnessInstanceID: instance.ID, WorkerRef: worker.WorkerRef,
		TurnID: turn.ID, AttemptID: attempt.ID, IssuedAt: time.Now().UTC(),
	}
	envelope := WorkerEnvelope{
		WorkerRef: worker.WorkerRef, TurnID: turn.ID, AttemptID: attempt.ID,
		PreviousAttemptID: firstAttempt.ID, OriginalUserIntent: "continue", HarnessInstance: instance,
		Profile: workerTemplateFixture(instance, "", ""),
	}
	resume := Command{Kind: CommandResume, Resume: &ResumeCommand{Metadata: metadata, Envelope: envelope}}
	local, err := OpenLocalStore(filepath.Join(t.TempDir(), "node.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	if _, _, err := local.ClaimCommand(resume); err != nil {
		t.Fatal(err)
	}
	sink := &issue209PromptSink{failUser: true}
	session := issue209DeferredPromptSession(sink)
	session.activity = make(chan Activity)
	execution := NewExecutionNode("macbook", nil, local)
	execution.watchSession(session, envelope, resume)
	entered := make(chan struct{})
	release := make(chan struct{})
	session.beforeInitialPromptWrite = func() { close(entered); <-release }
	promptDone := make(chan error, 1)
	go func() { promptDone <- session.Prompt(ctx, "first deferred prompt") }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("deferred initial Prompt never reached write boundary")
	}
	cancelDone := make(chan error, 1)
	cancelEntered := make(chan struct{})
	session.beforeInitialCancelClaim = func() { close(cancelEntered) }
	if len(session.writeGate) != 1 {
		t.Fatal("failed Prompt did not hold native write gate")
	}
	go func() { cancelDone <- session.Cancel(ctx) }()
	select {
	case <-cancelEntered:
	case <-ctx.Done():
		t.Fatal("Cancel did not reach held native stdin write")
	}
	select {
	case err := <-cancelDone:
		if err == nil {
			t.Fatal("Cancel unexpectedly confirmed cleanup without native process state")
		}
	case <-ctx.Done():
		t.Fatal("Cancel blocked on held native write gate")
	}
	close(release)
	select {
	case err := <-promptDone:
		if err == nil {
			t.Fatal("stdin failure lost during simultaneous Cancel")
		}
	case <-ctx.Done():
		t.Fatal("stdin failure blocked")
	}
	waitFor(t, ctx, "durable failed terminal for initial prompt error", func() bool {
		record, err := local.Command(metadata.CommandID)
		return err == nil && record.TerminalOutcomeQueued
	})
	events, err := local.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	terminalEvents := 0
	for _, pending := range events {
		if pending.EventID != "attempt-outcome-"+attempt.ID {
			continue
		}
		var event NodeEvent
		if err := json.Unmarshal(pending.Payload, &event); err != nil {
			t.Fatal(err)
		}
		if event.Outcome == nil || event.Outcome.Status != core.OutcomeFailed {
			t.Fatalf("stdin error did not persist failed Outcome: %+v", event)
		}
		terminalEvents++
		apply := NewStoreEventSink(server)
		if err := apply(ctx, event); err != nil {
			t.Fatal(err)
		}
		if err := apply(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	if terminalEvents != 1 {
		t.Fatalf("deferred Prompt produced %d terminal events, want one", terminalEvents)
	}
	details, err := server.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil || len(details.Results) != 2 || details.Results[1].Status != core.ResultFailed {
		t.Fatalf("deferred Prompt did not produce one durable failed Server Result: %+v err=%v", details, err)
	}
	entries, err := server.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	resultEntries := 0
	for _, entry := range entries {
		if entry.Kind == core.EntryWorkerResult && entry.TurnID == turn.ID {
			resultEntries++
		}
	}
	if resultEntries != 1 {
		t.Fatalf("deferred Prompt created %d canonical Server Results", resultEntries)
	}
	_ = firstTurn
}

func TestIssue209AcceptedCancelWinsOverLateDeferredPromptError(t *testing.T) {
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	sink := &issue209PromptSink{failUser: true}
	session := issue209DeferredPromptSession(sink)
	entered := make(chan struct{})
	release := make(chan struct{})
	session.beforeInitialPromptAdmission = func() { close(entered); <-release }
	prompt := make(chan error, 1)
	go func() { prompt <- session.Prompt(ctx, "late initial input") }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("deferred Prompt did not reach admission barrier")
	}
	if err := session.Cancel(ctx); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case err := <-prompt:
		if err == nil {
			t.Fatal("deferred Prompt accepted after durable cancellation")
		}
	case <-ctx.Done():
		t.Fatal("deferred Prompt never returned after Cancel")
	}
	if sink.Count("user") != 0 {
		t.Fatal("deferred Prompt wrote stdin after accepted Cancel")
	}
	session.emitResult(Result{Status: "canceled", Summary: "Claude Code attempt canceled"})
	select {
	case result := <-session.Result():
		if result.Status != "canceled" {
			t.Fatalf("accepted Cancel converted into failure: %+v", result)
		}
	default:
		t.Fatal("accepted Cancel lost terminal")
	}
	select {
	case extra := <-session.Result():
		t.Fatalf("late deferred Prompt created duplicate terminal: %+v", extra)
	default:
	}
}
