package node

import (
	"context"
	"errors"
	"fmt"

	"github.com/beruseruko/secretary/internal/core"
)

// TrustedLocalApprovalHandler is called only after a permission activity is
// durable. The handler owns the explicit local policy and Node handoff.
type TrustedLocalApprovalHandler func(context.Context, string, core.NodeReference) error

// NewStoreEventSink is the production acceptance boundary for Node events.
// It writes activity to the durable event log and terminal outcomes through
// the lifecycle transaction before the protocol ACK is emitted.
func NewStoreEventSink(store *core.Store) func(context.Context, NodeEvent) error {
	return NewStoreEventSinkWithTrustedLocalApproval(store, nil)
}

// NewStoreEventSinkWithTrustedLocalApproval keeps trusted-local approval on the
// real Node activity path. A nil handler leaves remote and ordinary approvals
// pending. The handler must reject every non-local Node itself.
func NewStoreEventSinkWithTrustedLocalApproval(store *core.Store, apply TrustedLocalApprovalHandler) func(context.Context, NodeEvent) error {
	return func(ctx context.Context, event NodeEvent) error {
		if store == nil {
			return errors.New("node server: durable event store is required")
		}
		if err := event.Validate(); err != nil {
			return err
		}
		if event.Outcome != nil {
			if event.Kind != "attempt.outcome" {
				return fmt.Errorf("node server: invalid terminal event kind %q", event.Kind)
			}
			_, _, _, err := store.RecordNodeAttemptOutcome(ctx, *event.Outcome)
			return err
		}
		if event.Kind != "activity" {
			return fmt.Errorf("node server: invalid activity event kind %q", event.Kind)
		}
		_, err := store.RecordNodeActivityReplay(ctx, *event.Activity)
		if err != nil {
			return err
		}
		if apply != nil && event.Activity.Kind == core.ActivityPermissionRequest && event.Activity.Request != nil {
			if err := apply(ctx, event.Activity.Request.RequestID, event.Activity.Metadata.Node); err != nil {
				if errors.Is(err, core.ErrTrustedLocalApprovalDenied) {
					return nil
				}
				return err
			}
		}
		return nil
	}
}
