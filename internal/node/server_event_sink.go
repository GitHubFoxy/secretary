package node

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

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
		_ = apply(applyCtx, requestID, nodeRef)
	}()
}

func NewStoreEventSink(store *core.Store) func(context.Context, NodeEvent) error {
	return NewStoreEventSinkWithTrustedLocalApproval(store, nil)
}

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
		recorded, err := store.RecordNodeActivityReplay(ctx, *event.Activity)
		if err != nil {
			return err
		}
		if recorded.ID == "" {
			return nil
		}
		if apply != nil && event.Activity.Kind == core.ActivityPermissionRequest && event.Activity.Request != nil {
			handoffs.start(ctx, apply, event.Activity.Request.RequestID, event.Activity.Metadata.Node)
		}
		return nil
	}
}
