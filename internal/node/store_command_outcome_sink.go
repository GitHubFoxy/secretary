package node

import (
	"context"
	"errors"
	"fmt"

	"github.com/beruseruko/secretary/internal/core"
)

// NewStoreCommandOutcomeSink is the production bridge from authenticated Node
// receipts to Core's durable command/Worker-Turn reconciliation. It only
// changes respond_worker lifecycle state; other command kinds keep their
// existing delivery behavior.
func NewStoreCommandOutcomeSink(store *core.Store) func(context.Context, core.NodeReference, CommandOutcome) error {
	return func(ctx context.Context, nodeRef core.NodeReference, outcome CommandOutcome) error {
		if store == nil {
			return errors.New("node: Core command outcome store is required")
		}
		if outcome.Kind != CommandRespondWorker {
			return nil
		}
		state := core.WorkerCommandReceiptUncertain
		switch outcome.State {
		case CommandAccepted:
			state = core.WorkerCommandReceiptAccepted
		case CommandFailed:
			if !uncertainRespondOutcome(outcome) {
				state = core.WorkerCommandReceiptDenied
			}
		case CommandInterrupted:
			state = core.WorkerCommandReceiptUncertain
		default:
			return nil
		}
		_, err := store.ReconcileWorkerCommandReceipt(ctx, nodeRef, core.WorkerCommandReceipt{
			CommandID: outcome.CommandID, Kind: "respond", TurnID: outcome.TurnID, AttemptID: outcome.AttemptID,
			State: state, ErrorCode: outcome.ErrorCode, ErrorMessage: outcome.ErrorMessage,
		})
		if err != nil {
			return fmt.Errorf("node: reconcile authenticated respond_worker outcome: %w", err)
		}
		return nil
	}
}
