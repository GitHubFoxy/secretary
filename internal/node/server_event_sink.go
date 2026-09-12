package node

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

// TrustedLocalApprovalHandler is called only after a permission activity is
// durable. The handler owns the explicit local policy and Node handoff.
type TrustedLocalApprovalHandler func(context.Context, string, core.NodeReference) error

const trustedLocalApprovalHandoffTimeout = 30 * time.Second

type trustedLocalApprovalHandoffs struct {
	mu     sync.Mutex
	active map[string]struct{}
}

func (h *trustedLocalApprovalHandoffs) start(ctx context.Context, apply TrustedLocalApprovalHandler, requestID string, nodeRef core.NodeReference) {
	h.mu.Lock()
	if h.active == nil {
		h.active = make(map[string]struct{})
	}
	if _, exists := h.active[requestID]; exists {
		h.mu.Unlock()
		return
	}
	h.active[requestID] = struct{}{}
	h.mu.Unlock()

	go func() {
		defer func() {
			h.mu.Lock()
			delete(h.active, requestID)
			h.mu.Unlock()
		}()
		applyCtx, cancel := context.WithTimeout(ctx, trustedLocalApprovalHandoffTimeout)
		defer cancel()
		// Errors remain represented by the pending Approval and failed Worker
		// command. The durable auto-approval audit is written only by the
		// handler after the Node returns an accepted typed outcome.
		_ = apply(applyCtx, requestID, nodeRef)
	}()
}

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
	handoffs := &trustedLocalApprovalHandoffs{}
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
			handoffs.start(ctx, apply, event.Activity.Request.RequestID, event.Activity.Metadata.Node)
		}
		return nil
	}
}
