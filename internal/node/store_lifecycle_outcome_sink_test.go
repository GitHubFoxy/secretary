package node

import (
	"context"
	"errors"
	"github.com/beruseruko/secretary/internal/core"
	"path/filepath"
	"testing"
)

func TestAuthenticatedLifecycleReceiptsExposeAcceptanceAndFailure(t *testing.T) {
	for _, kind := range []CommandKind{CommandDispatch, CommandResume} {
		for _, state := range []CommandState{CommandAccepted, CommandFailed, CommandInterrupted} {
			t.Run(string(kind)+"/"+string(state), func(t *testing.T) {
				ctx := context.Background()
				store, err := core.Open(ctx, filepath.Join(t.TempDir(), "outcomes.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer store.Close()
				_, conversation, err := store.CreatePersonWithConversation(ctx)
				if err != nil {
					t.Fatal(err)
				}
				worker, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{Intent: "continue", ProjectID: "project", NodeID: "node-a", HarnessInstanceID: "node-a/codex"}, core.TurnSpec{Input: "continue", CommandKind: string(kind)})
				if err != nil {
					t.Fatal(err)
				}
				command, _, err := store.ClaimWorkerCommand(ctx, string(kind), "attempt", worker.ID, attempt.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := store.MarkWorkerCommandDelivered(ctx, command.ID); err != nil {
					t.Fatal(err)
				}
				sink := NewStoreCommandOutcomeSink(store)
				receipt := CommandOutcome{CommandID: command.ID, Kind: kind, State: state, TurnID: turn.ID, AttemptID: attempt.ID, ErrorCode: "runtime_session_unavailable", ErrorMessage: "Native session unavailable"}
				if err := sink(ctx, "another-node", receipt); !errors.Is(err, core.ErrInvalidTransition) {
					t.Fatalf("wrong Node accepted: %v", err)
				}
				for i := 0; i < 2; i++ {
					if err := sink(ctx, "node-a", receipt); err != nil {
						t.Fatal(err)
					}
				}
				details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
				if err != nil {
					t.Fatal(err)
				}
				switch state {
				case CommandAccepted:
					if details.Worker.Status != core.WorkerWorking || details.CurrentAttempt().State != core.AttemptActive {
						t.Fatalf("accepted lifecycle stays queued: %#v", details)
					}
				default:
					finalCommand, err := store.MarkWorkerCommandDelivered(ctx, command.ID)
					if err != nil || finalCommand.State == core.WorkerCommandDelivered {
						t.Fatalf("late transport success overwrote native failure: %#v %v", finalCommand, err)
					}
					if len(details.Results) != 1 || details.CurrentAttempt().State == core.AttemptStarting {
						t.Fatalf("failure stays invisible: %#v", details)
					}
					receipt.State = CommandAccepted
					if err := sink(ctx, "node-a", receipt); err != nil {
						t.Fatal(err)
					}
					current, err := store.Phase4Attempt(ctx, attempt.ID)
					if err != nil || !current.State.Terminal() {
						t.Fatalf("late receipt revived terminal: %#v %v", current, err)
					}
				}
			})
		}
	}
}
