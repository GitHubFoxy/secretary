package node

import (
	"context"
	"errors"
	"fmt"

	"github.com/beruseruko/secretary/internal/core"
)

func NewStoreCommandOutcomeSink(store *core.Store) func(context.Context, core.NodeReference, CommandOutcome) error {
	return func(ctx context.Context, nodeRef core.NodeReference, outcome CommandOutcome) error {
		if store == nil {
			return errors.New("node: Core command outcome store is required")
		}
		kind := ""
		switch outcome.Kind {
		case CommandRespondWorker:
			kind = "respond"
		case CommandDispatch:
			kind = "dispatch"
		case CommandResume:
			kind = "resume"
		default:
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
			CommandID: outcome.CommandID, Kind: kind, TurnID: outcome.TurnID, AttemptID: outcome.AttemptID,
			State: state, ErrorCode: outcome.ErrorCode, ErrorMessage: outcome.ErrorMessage,
		})
		if err != nil {
			return fmt.Errorf("node: reconcile authenticated Worker command outcome: %w", err)
		}
		return nil
	}
}
