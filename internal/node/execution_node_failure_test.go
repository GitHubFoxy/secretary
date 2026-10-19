package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/beruseruko/secretary/internal/acp"
	"github.com/beruseruko/secretary/internal/core"
)

type failedCloseSession struct {
	result     chan Result
	closeErr   error
	closeDone  chan struct{}
	steerErr   error
	steerCalls int
}

func (*failedCloseSession) ID() string                           { return "native-session" }
func (*failedCloseSession) Prompt(context.Context, string) error { return nil }
func (s *failedCloseSession) Steer(context.Context, string) (bool, error) {
	s.steerCalls++
	return false, s.steerErr
}
func (*failedCloseSession) Cancel(context.Context) error { return nil }
func (*failedCloseSession) Activity() <-chan Activity    { return nil }
func (s *failedCloseSession) Result() <-chan Result      { return s.result }
func (s *failedCloseSession) Close() error {
	if s.closeDone != nil {
		close(s.closeDone)
	}
	return s.closeErr
}

func TestWatchSessionSanitizesCloseErrorAndPersistsProviderFailure(t *testing.T) {
	store, err := OpenLocalStore(t.TempDir() + "/node.json")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	session := &failedCloseSession{
		result:    make(chan Result, 1),
		closeErr:  errors.New("close failed Bearer private-close-token sk-private-secret " + strings.Repeat("界", 1100)),
		closeDone: make(chan struct{}),
	}
	session.result <- Result{
		Status: "failed", Summary: "Лимит использования исчерпан; повторите после 4:37 PM",
		FailureCode: "usage_limit_exceeded", FailureCategory: core.FailureCategoryUsageLimit,
		Technical: "acp rpc -32603: Internal error",
	}
	envelope := WorkerEnvelope{
		WorkerRef: "worker", TurnID: "turn", AttemptID: "attempt",
		HarnessInstance: core.HarnessInstance{ID: "local/codex", Node: "local", Kind: core.HarnessCodex},
	}
	NewExecutionNode("local", nil, store).watchSession(session, envelope)

	deadline := time.Now().Add(2 * time.Second)
	var events []PendingEvent
	for time.Now().Before(deadline) {
		events, err = store.PendingEvents()
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(events) != 1 {
		t.Fatalf("terminal event count=%d, want 1", len(events))
	}
	var event NodeEvent
	if err := json.Unmarshal(events[0].Payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.Outcome == nil || event.Outcome.Status != core.OutcomeFailed || event.Outcome.ErrorCode != "usage_limit_exceeded" {
		t.Fatalf("terminal outcome=%#v", event.Outcome)
	}
	if event.Outcome.Summary != "Лимит использования исчерпан; повторите после 4:37 PM" {
		t.Fatalf("provider failure summary was replaced by CloseError: %q", event.Outcome.Summary)
	}
	if event.Outcome.FailureCategory != core.FailureCategoryUsageLimit || event.Outcome.FailureCode != "usage_limit_exceeded" {
		t.Fatalf("provider failure metadata was lost: %#v", event.Outcome)
	}
	if utf8.RuneCountInString(event.Outcome.Diagnostics) > 1000 || !strings.Contains(event.Outcome.Diagnostics, "Native session close failed") || strings.Contains(event.Outcome.Diagnostics, "private-close-token") || strings.Contains(event.Outcome.Diagnostics, "private-secret") {
		t.Fatalf("unsafe secondary close diagnostic persisted: runes=%d diagnostics=%q", utf8.RuneCountInString(event.Outcome.Diagnostics), event.Outcome.Diagnostics)
	}
	select {
	case <-session.closeDone:
	default:
		t.Fatal("native session was not closed before terminal event publication")
	}
}

func TestWatchSessionRetriesTerminalOutboxPersistenceFailure(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "node.json")
	store, err := OpenLocalStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := os.WriteFile(storePath, []byte("{}"), 0o600); err != nil { t.Fatal(err) }
	backup := storePath + ".fault-backup"
	if err := os.Rename(storePath, backup); err != nil { t.Fatal(err) }
	if err := os.Mkdir(storePath, 0o700); err != nil { t.Fatal(err) }
	session := &failedCloseSession{result: make(chan Result, 1), closeDone: make(chan struct{})}
	session.result <- Result{Status: "failed", Summary: "provider failure"}
	envelope := WorkerEnvelope{
		WorkerRef: "worker", TurnID: "turn", AttemptID: "attempt-outbox-retry",
		HarnessInstance: core.HarnessInstance{ID: "local/codex", Node: "local", Kind: core.HarnessCodex},
	}
	releaseDone := make(chan error, 1)
	go func() {
		<-session.closeDone
		time.Sleep(250 * time.Millisecond)
		releaseDone <- func() error {
		if err := os.Remove(storePath); err != nil { return err }
		return os.Rename(backup, storePath)
		}()
	}()
	defer func() {
		if err := <-releaseDone; err != nil { t.Errorf("restore temporary outbox blocker: %v", err) }
	}()
	NewExecutionNode("local", nil, store).watchSession(session, envelope)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		events, err := store.PendingEvents()
		if err != nil {
			t.Fatal(err)
		}
		if len(events) == 1 {
			var event NodeEvent
			if err := json.Unmarshal(events[0].Payload, &event); err != nil {
				t.Fatal(err)
			}
			if event.Outcome == nil || event.Outcome.AttemptID != envelope.AttemptID || event.Outcome.Status != core.OutcomeFailed {
				t.Fatalf("retried terminal outcome=%#v", event.Outcome)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("terminal outcome was lost after transient outbox persistence failure")
}

func TestExecutionNodeSteeringCancellationIsDurablyUncertain(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(cause.Error(), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "node.json")
			store, err := OpenLocalStore(path)
			if err != nil {
				t.Fatal(err)
			}
			session := &failedCloseSession{steerErr: cause}
			execution := NewExecutionNode("macbook", &approvalRuntime{session: session}, store)
			dispatch := dispatchFixture("dispatch-canceled-steer")
			if outcome, err := execution.HandleCommand(context.Background(), dispatch); err != nil || outcome.State != CommandAccepted {
				t.Fatalf("dispatch outcome=%#v err=%v", outcome, err)
			}

			metadata := dispatch.Dispatch.Metadata
			metadata.CommandID = "steer-canceled"
			steering := Command{Kind: CommandSteering, Steering: &SteeringCommand{Metadata: metadata, Text: "continue"}}
			outcome, err := execution.HandleCommand(context.Background(), steering)
			if err != nil || outcome.State != CommandFailed || !outcome.AcceptanceUnknown || !uncertainCommandOutcome(outcome) {
				t.Fatalf("steering outcome=%#v err=%v, want durable uncertainty", outcome, err)
			}

			record, err := store.Command(metadata.CommandID)
			if err != nil || record.State != CommandFailed || !record.Outcome.AcceptanceUnknown {
				t.Fatalf("command record=%#v err=%v, want unknown acceptance persisted", record, err)
			}
			pending, err := store.PendingEvents()
			if err != nil {
				t.Fatal(err)
			}
			var queued CommandOutcome
			found := false
			for _, event := range pending {
				if event.Type == MessageCommandOutcome && event.EventID == "command-outcome-"+metadata.CommandID {
					if err := json.Unmarshal(event.Payload, &queued); err != nil {
						t.Fatal(err)
					}
					found = true
				}
			}
			if !found || !queued.AcceptanceUnknown || !uncertainCommandOutcome(queued) {
				t.Fatalf("durable command receipt=%#v found=%v, want unknown acceptance", queued, found)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenLocalStore(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			recovered, err := reopened.PendingEvents()
			if err != nil {
				t.Fatal(err)
			}
			found = false
			for _, event := range recovered {
				if event.Type == MessageCommandOutcome && event.EventID == "command-outcome-"+metadata.CommandID {
					var replay CommandOutcome
					if err := json.Unmarshal(event.Payload, &replay); err != nil {
						t.Fatal(err)
					}
					found = replay.AcceptanceUnknown && uncertainCommandOutcome(replay)
				}
			}
			if !found {
				t.Fatal("recovered command outbox lost unknown-acceptance marker")
			}
		})
	}
}

func TestExecutionNodeSteeringProviderFailureSanitizesJSONStringBeforeOutbox(t *testing.T) {
	store, err := OpenLocalStore(filepath.Join(t.TempDir(), "node.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	providerMessage := `provider failed: "{\"access_token\":\"review-private-credential\",\"safe\":\"https://untrusted.example/path\"}"`
	data, err := json.Marshal(map[string]string{"code": "usage_limit_exceeded", "message": providerMessage})
	if err != nil {
		t.Fatal(err)
	}
	session := &failedCloseSession{steerErr: &acp.RPCError{Code: -32603, Message: "Internal error", Data: data}}
	execution := NewExecutionNode("macbook", &approvalRuntime{session: session}, store)
	dispatch := dispatchFixture("dispatch-steer-provider-url")
	if outcome, err := execution.HandleCommand(context.Background(), dispatch); err != nil || outcome.State != CommandAccepted {
		t.Fatalf("dispatch outcome=%#v err=%v", outcome, err)
	}
	metadata := dispatch.Dispatch.Metadata
	metadata.CommandID = "steer-provider-url"
	steering := Command{Kind: CommandSteering, Steering: &SteeringCommand{Metadata: metadata, Text: "continue"}}
	if outcome, err := execution.HandleCommand(context.Background(), steering); err != nil || outcome.State != CommandFailed || outcome.FailureCategory != core.FailureCategoryUsageLimit {
		t.Fatalf("steering outcome=%#v err=%v", outcome, err)
	}
	events, err := store.PendingEvents()
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Type != MessageCommandOutcome || event.EventID != "command-outcome-"+metadata.CommandID {
			continue
		}
		var receipt CommandOutcome
		if err := json.Unmarshal(event.Payload, &receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.FailureCategory != core.FailureCategoryUsageLimit || strings.Contains(receipt.ErrorMessage, "review-private-credential") || strings.Contains(receipt.ErrorMessage, "untrusted.example") {
			t.Fatalf("unsafe steering receipt persisted: %#v", receipt)
		}
		return
	}
	t.Fatalf("durable steering command outcome missing: %#v", events)
}

func TestExecutionNodeSteeringProviderFailureRedactsCLISecretsBeforeOutbox(t *testing.T) {
	cases := []struct {
		name    string
		message string
		secrets []string
		status  bool
	}{
		{
			name:    "api key with status suffix",
			message: "provider failed: --api-key review-private-credential [429]",
			secrets: []string{"review-private-credential"},
			status:  true,
		},
		{
			name:    "api key containing comma",
			message: "provider failed: --api-key prefix,review-private-credential [429]",
			secrets: []string{"review-private-credential"},
			status:  true,
		},
		{
			name:    "api key containing spaced quoted fragment",
			message: `provider failed: --api-key prefix" review-private-credential" [429]`,
			secrets: []string{"review-private-credential"},
			status:  true,
		},
		{
			name:    "api key containing JSON-like array",
			message: `provider failed: --api-key "prefix[429]review-private-credential"`,
			secrets: []string{"review-private-credential"},
		},
		{
			name:    "api key containing escaped quote and JSON-like array",
			message: `provider failed: --api-key "prefix\"[429]review-private-credential"`,
			secrets: []string{"review-private-credential"},
		},
		{
			name:    "curl user",
			message: "curl --user alice:review-private-password https://attacker.invalid/path [429]",
			secrets: []string{"alice:", "review-private-password", "attacker.invalid"},
			status:  true,
		},
		{
			name:    "curl user containing comma",
			message: "curl --user alice:prefix,review-private-password [429]",
			secrets: []string{"alice:", "review-private-password"},
			status:  true,
		},
		{
			name:    "curl user containing spaced quoted fragment",
			message: `curl --user alice:prefix" review-private-password" [429]`,
			secrets: []string{"alice:", "review-private-password"},
			status:  true,
		},
		{
			name:    "curl user containing JSON-like array",
			message: `curl --user "alice:prefix[429]review-private-password"`,
			secrets: []string{"alice:", "review-private-password"},
		},
		{
			name:    "curl user containing escaped quote and JSON-like array",
			message: `curl --user "alice:prefix\"[429]review-private-password"`,
			secrets: []string{"alice:", "review-private-password"},
		},
		{
			name:    "CLI secret inside JSON message",
			message: `{"message":"provider failed: --password nested-review-private-value [429]"}`,
			secrets: []string{"nested-review-private-value"},
			status:  true,
		},
	}
	for i, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			store, err := OpenLocalStore(filepath.Join(t.TempDir(), "node.json"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()

			data, err := json.Marshal(map[string]string{"code": "usage_limit_exceeded", "message": test.message})
			if err != nil {
				t.Fatal(err)
			}
			session := &failedCloseSession{steerErr: &acp.RPCError{Code: -32603, Message: "Internal error", Data: data}}
			execution := NewExecutionNode("macbook", &approvalRuntime{session: session}, store)
			dispatch := dispatchFixture(fmt.Sprintf("dispatch-cli-secret-%d", i))
			if outcome, err := execution.HandleCommand(context.Background(), dispatch); err != nil || outcome.State != CommandAccepted {
				t.Fatalf("dispatch outcome=%#v err=%v", outcome, err)
			}
			metadata := dispatch.Dispatch.Metadata
			metadata.CommandID = fmt.Sprintf("steer-cli-secret-%d", i)
			steering := Command{Kind: CommandSteering, Steering: &SteeringCommand{Metadata: metadata, Text: "continue"}}
			if outcome, err := execution.HandleCommand(context.Background(), steering); err != nil || outcome.State != CommandFailed || outcome.FailureCategory != core.FailureCategoryUsageLimit {
				t.Fatalf("steering outcome=%#v err=%v", outcome, err)
			}

			events, err := store.PendingEvents()
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range events {
				if event.Type != MessageCommandOutcome || event.EventID != "command-outcome-"+metadata.CommandID {
					continue
				}
				var receipt CommandOutcome
				if err := json.Unmarshal(event.Payload, &receipt); err != nil {
					t.Fatal(err)
				}
				for _, secret := range test.secrets {
					if strings.Contains(receipt.ErrorMessage, secret) {
						t.Errorf("CLI secret %q persisted in durable outcome: %#v", secret, receipt)
					}
				}
				if !strings.Contains(receipt.ErrorMessage, "[redacted]") {
					t.Errorf("safe redaction marker missing from durable outcome: %#v", receipt)
				}
				if test.status && !strings.Contains(receipt.ErrorMessage, "429") {
					t.Errorf("safe status missing from durable outcome: %#v", receipt)
				}
				return
			}
			t.Fatalf("durable steering command outcome missing: %#v", events)
		})
	}
}

func TestWatchSessionRetriesTerminalCommandReceiptAndUnregisters(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "node.json")
	store, err := OpenLocalStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	command := dispatchFixture("continuation-terminal-receipt")
	command.Dispatch.Envelope.PreviousAttemptID = "prior-attempt"
	if _, _, err := store.ClaimCommand(command); err != nil {
		t.Fatal(err)
	}
	backup := storePath + ".fault-backup"
	if err := os.Rename(storePath, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(storePath, 0o700); err != nil {
		_ = os.Rename(backup, storePath)
		t.Fatal(err)
	}
	session := &failedCloseSession{result: make(chan Result, 1), closeDone: make(chan struct{})}
	session.result <- Result{
		Status: "failed", Summary: "Лимит использования исчерпан; попробуйте снова после 4:37 PM",
		FailureCode: "usage_limit_exceeded", FailureCategory: core.FailureCategoryUsageLimit,
		Technical: "acp rpc -32603: Internal error",
	}
	envelope := command.Dispatch.Envelope
	execution := NewExecutionNode("macbook", nil, store)
	execution.registerSession(envelope.AttemptID, session)
	releaseDone := make(chan error, 1)
	go func() {
		<-session.closeDone
		time.Sleep(250 * time.Millisecond)
		releaseDone <- func() error {
		if err := os.Remove(storePath); err != nil { return err }
		return os.Rename(backup, storePath)
		}()
	}()
	defer func() {
		if err := <-releaseDone; err != nil { t.Errorf("restore temporary receipt blocker: %v", err) }
	}()
	execution.watchSession(session, envelope, command)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		record, recordErr := store.Command(command.Metadata().CommandID)
		if recordErr == nil && record.State == CommandAccepted {
			events, eventErr := store.PendingEvents()
			if eventErr != nil {
				t.Fatal(eventErr)
			}
			if len(events) == 1 {
				var event NodeEvent
				if err := json.Unmarshal(events[0].Payload, &event); err != nil {
					t.Fatal(err)
				}
				if event.Outcome == nil || event.Outcome.Status != core.OutcomeFailed || event.Outcome.Summary != "Лимит использования исчерпан; попробуйте снова после 4:37 PM" || event.Outcome.FailureCode != "usage_limit_exceeded" || event.Outcome.FailureCategory != core.FailureCategoryUsageLimit || event.Outcome.Diagnostics != "acp rpc -32603: Internal error" {
					t.Fatalf("terminal outcome=%#v", event.Outcome)
				}
				if _, registered := execution.session(envelope.AttemptID); registered {
					t.Fatal("terminal Session remained registered after receipt recovery")
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	record, _ := store.Command(command.Metadata().CommandID)
	t.Fatalf("terminal receipt/outcome did not recover: command=%#v", record)
}

func TestLocalStoreTerminalCommandAndTypedOutcomeCommitAtomically(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.json")
	store, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	command := dispatchFixture("atomic-terminal-completion")
	if _, duplicate, err := store.ClaimCommand(command); err != nil || duplicate {
		t.Fatalf("claim duplicate=%v err=%v", duplicate, err)
	}
	accepted := acceptedOutcome(command)
	if _, err := store.SaveCommandReadiness(command.Metadata().CommandID, accepted); err != nil {
		t.Fatal(err)
	}
	terminal := core.AttemptOutcomeEnvelope{
		EventID: "attempt-outcome-" + command.Metadata().AttemptID, Node: command.Metadata().Node,
		HarnessInstanceID: command.Metadata().HarnessInstanceID, WorkerRef: command.Metadata().WorkerRef,
		TurnID: command.Metadata().TurnID, AttemptID: command.Metadata().AttemptID,
		Status: core.OutcomeFailed, Classification: core.OutcomeFinal,
		Summary: "Usage limit reached; try again at 4:37 PM", ErrorCode: "usage_limit_exceeded",
		FailureCode: "usage_limit_exceeded", FailureCategory: core.FailureCategoryUsageLimit,
		Diagnostics: "provider retry context: try again at 4:37 PM", OccurredAt: time.Date(2026, 10, 12, 5, 0, 0, 0, time.UTC),
	}
	backup := path + ".fault-backup"
	if err := os.Rename(path, backup); err != nil { t.Fatal(err) }
	if err := os.Mkdir(path, 0o700); err != nil { t.Fatal(err) }
	if _, err := store.CompleteCommandWithOutcome(command.Metadata().CommandID, accepted, terminal); err == nil {
		t.Fatal("expected injected local-store persistence failure")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil { t.Fatal(err) }
	if err := os.Rename(backup, path); err != nil { t.Fatal(err) }
	reopened, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	record, err := reopened.Command(command.Metadata().CommandID)
	if err != nil {
		t.Fatal(err)
	}
	if record.State != CommandProcessing || record.Outcome.State != CommandAccepted {
		t.Fatalf("failed atomic write left accepted completion: %#v", record)
	}
	if events, err := reopened.PendingEvents(); err != nil || len(events) != 0 {
		t.Fatalf("failed atomic write left a terminal outbox event: events=%#v err=%v", events, err)
	}
	if err := reopened.RecoverRunning(ctx, nil); err != nil {
		t.Fatal(err)
	}
	recovered, err := reopened.Command(command.Metadata().CommandID)
	if err != nil || recovered.State != CommandInterrupted || recovered.State == CommandAccepted {
		t.Fatalf("recovery left an accepted command without its terminal payload: command=%#v err=%v", recovered, err)
	}
	if events, err := reopened.PendingEvents(); err != nil || len(events) != 1 {
		t.Fatalf("recovery terminal outbox=%#v err=%v", events, err)
	}
}

func TestLocalStoreReopenPreservesAtomicTypedTerminalOutcome(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.json")
	store, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	command := dispatchFixture("atomic-terminal-reopen")
	if _, _, err := store.ClaimCommand(command); err != nil {
		t.Fatal(err)
	}
	accepted := acceptedOutcome(command)
	if _, err := store.SaveCommandReadiness(command.Metadata().CommandID, accepted); err != nil {
		t.Fatal(err)
	}
	resetMessage := "Лимит использования исчерпан; попробуйте снова после 4:37 PM"
	occurred := time.Date(2026, 10, 12, 5, 4, 0, 0, time.UTC)
	terminal := core.AttemptOutcomeEnvelope{
		EventID: "attempt-outcome-" + command.Metadata().AttemptID, Node: command.Metadata().Node,
		HarnessInstanceID: command.Metadata().HarnessInstanceID, WorkerRef: command.Metadata().WorkerRef,
		TurnID: command.Metadata().TurnID, AttemptID: command.Metadata().AttemptID,
		Status: core.OutcomeFailed, Classification: core.OutcomeFinal, Summary: resetMessage,
		ErrorCode: "usage_limit_exceeded", FailureCode: "usage_limit_exceeded",
		FailureCategory: core.FailureCategoryUsageLimit, Diagnostics: "retry at 4:37 PM", OccurredAt: occurred,
	}
	if _, err := store.CompleteCommandWithOutcome(command.Metadata().CommandID, accepted, terminal); err != nil {
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
	if err := reopened.RecoverRunning(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	record, err := reopened.Command(command.Metadata().CommandID)
	if err != nil || record.State != CommandAccepted || !record.TerminalOutcomeQueued {
		t.Fatalf("reopened command=%#v err=%v", record, err)
	}
	events, err := reopened.PendingEvents()
	if err != nil || len(events) != 1 {
		t.Fatalf("reopened outbox=%#v err=%v", events, err)
	}
	var event NodeEvent
	if err := json.Unmarshal(events[0].Payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.Outcome == nil || event.Outcome.Summary != resetMessage || event.Outcome.FailureCode != "usage_limit_exceeded" || event.Outcome.FailureCategory != core.FailureCategoryUsageLimit || event.Outcome.Diagnostics != "retry at 4:37 PM" || !event.Outcome.OccurredAt.Equal(occurred) {
		t.Fatalf("atomic terminal payload changed after reopen: %#v", event.Outcome)
	}
}

func TestRecoverLegacyAcceptedCommandKeepsPersistedTypedTerminalOutcome(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.json")
	store, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	command := dispatchFixture("legacy-accepted-terminal")
	if _, _, err := store.ClaimCommand(command); err != nil {
		t.Fatal(err)
	}
	accepted := acceptedOutcome(command)
	if _, err := store.SaveCommandReadiness(command.Metadata().CommandID, accepted); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteCommand(command.Metadata().CommandID, accepted); err != nil {
		t.Fatal(err)
	}
	resetMessage := "Лимит использования исчерпан; попробуйте снова после 4:37 PM"
	occurred := time.Date(2026, 10, 12, 5, 9, 0, 0, time.UTC)
	terminal := core.AttemptOutcomeEnvelope{
		EventID: "attempt-outcome-" + command.Metadata().AttemptID, Node: command.Metadata().Node,
		HarnessInstanceID: command.Metadata().HarnessInstanceID, WorkerRef: command.Metadata().WorkerRef,
		TurnID: command.Metadata().TurnID, AttemptID: command.Metadata().AttemptID,
		Status: core.OutcomeFailed, Classification: core.OutcomeFinal, Summary: resetMessage,
		ErrorCode: "usage_limit_exceeded", FailureCode: "usage_limit_exceeded",
		FailureCategory: core.FailureCategoryUsageLimit, Diagnostics: "retry at 4:37 PM", OccurredAt: occurred,
	}
	if _, err := store.QueueOutcome(terminal); err != nil {
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
	if err := reopened.RecoverRunning(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	record, err := reopened.Command(command.Metadata().CommandID)
	if err != nil || record.State != CommandAccepted || !record.TerminalOutcomeQueued {
		t.Fatalf("legacy recovery changed accepted receipt: record=%#v err=%v", record, err)
	}
	events, err := reopened.PendingEvents()
	if err != nil || len(events) != 1 {
		t.Fatalf("legacy recovery outbox=%#v err=%v", events, err)
	}
	var event NodeEvent
	if err := json.Unmarshal(events[0].Payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.Outcome == nil || event.Outcome.Summary != resetMessage || event.Outcome.FailureCode != "usage_limit_exceeded" || event.Outcome.FailureCategory != core.FailureCategoryUsageLimit || event.Outcome.Diagnostics != "retry at 4:37 PM" || !event.Outcome.OccurredAt.Equal(occurred) {
		t.Fatalf("legacy recovery replaced original typed terminal payload: %#v", event.Outcome)
	}
}

func TestAckLegacyTerminalOutcomePreventsSyntheticRecoveryDuplicate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.json")
	store, err := OpenLocalStore(path)
	if err != nil {
		t.Fatal(err)
	}
	command := dispatchFixture("legacy-accepted-terminal-acked")
	if _, _, err := store.ClaimCommand(command); err != nil {
		t.Fatal(err)
	}
	accepted := acceptedOutcome(command)
	if _, err := store.SaveCommandReadiness(command.Metadata().CommandID, accepted); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteCommand(command.Metadata().CommandID, accepted); err != nil {
		t.Fatal(err)
	}
	terminal := core.AttemptOutcomeEnvelope{
		EventID: "attempt-outcome-" + command.Metadata().AttemptID, Node: command.Metadata().Node,
		HarnessInstanceID: command.Metadata().HarnessInstanceID, WorkerRef: command.Metadata().WorkerRef,
		TurnID: command.Metadata().TurnID, AttemptID: command.Metadata().AttemptID,
		Status: core.OutcomeFailed, Classification: core.OutcomeFinal, Summary: "provider failure",
		FailureCode: "usage_limit_exceeded", FailureCategory: core.FailureCategoryUsageLimit,
		OccurredAt: time.Date(2026, 10, 12, 5, 12, 0, 0, time.UTC),
	}
	if _, err := store.QueueOutcome(terminal); err != nil {
		t.Fatal(err)
	}
	events, err := store.PendingEvents()
	if err != nil || len(events) != 1 {
		t.Fatalf("legacy terminal outbox=%#v err=%v", events, err)
	}
	if err := store.AckThrough(events[0].Sequence); err != nil {
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
	if err := reopened.RecoverRunning(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	record, err := reopened.Command(command.Metadata().CommandID)
	if err != nil || record.State != CommandAccepted || !record.TerminalOutcomeQueued {
		t.Fatalf("acknowledged legacy result was not recognized: record=%#v err=%v", record, err)
	}
	if events, err := reopened.PendingEvents(); err != nil || len(events) != 0 {
		t.Fatalf("recovery synthesized a duplicate terminal event: events=%#v err=%v", events, err)
	}
}

func TestWatchSessionPersistsFailureWhenSanitizedSummaryBecomesEmpty(t *testing.T) {
	store, err := OpenLocalStore(t.TempDir() + "/node.json")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	session := &failedCloseSession{result: make(chan Result, 1), closeDone: make(chan struct{})}
	session.result <- Result{Status: "failed", Summary: "https://attacker.invalid/private?token=hidden"}
	envelope := WorkerEnvelope{
		WorkerRef: "worker", TurnID: "turn", AttemptID: "attempt-url-summary",
		HarnessInstance: core.HarnessInstance{ID: "local/claude", Node: "local", Kind: core.HarnessClaudeCode},
	}
	NewExecutionNode("local", nil, store).watchSession(session, envelope)

	deadline := time.Now().Add(2 * time.Second)
	var events []PendingEvent
	for time.Now().Before(deadline) {
		events, err = store.PendingEvents()
		if err != nil {
			t.Fatal(err)
		}
		if len(events) != 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(events) != 1 {
		t.Fatalf("terminal event count=%d, want 1", len(events))
	}
	var event NodeEvent
	if err := json.Unmarshal(events[0].Payload, &event); err != nil {
		t.Fatal(err)
	}
	if event.Outcome == nil || event.Outcome.Status != core.OutcomeFailed || event.Outcome.Summary != "failed" {
		t.Fatalf("sanitized terminal outcome=%#v, want a durable non-empty summary", event.Outcome)
	}
	if strings.Contains(string(events[0].Payload), "attacker.invalid") || strings.Contains(string(events[0].Payload), "hidden") {
		t.Fatalf("unsafe summary reached the durable outbox: %s", events[0].Payload)
	}
	select {
	case <-session.closeDone:
	default:
		t.Fatal("native session was not closed before terminal event publication")
	}
}
