package node

import (
	"context"
	"errors"

	"github.com/beruseruko/secretary/internal/core"
)

type Phase4RecoveryResolver struct {
	Store        *core.Store
	Remote       *ServerManager
}

func (r Phase4RecoveryResolver) ResolvePhase4Attempt(ctx context.Context, attempt core.Phase4Attempt) (core.Phase4RecoveryDecision, error) {
	if r.Store == nil {
		return core.Phase4RecoveryUnknown, errors.New("node: Phase 4 recovery store is required")
	}
	worker, err := r.Store.Worker(ctx, attempt.WorkerID)
	if err != nil {
		if errors.Is(err, core.ErrNotFound) {
			return core.Phase4RecoveryUnknown, nil
		}
		return core.Phase4RecoveryUnknown, err
	}
	if r.Remote == nil {
		return core.Phase4RecoveryUnknown, nil
	}
	status, err := r.Remote.Status(ctx, core.NodeReference(attempt.NodeID))
	if errors.Is(err, core.ErrNotFound) {
		return core.Phase4RecoveryUnknown, nil
	}
	if err != nil {
		return core.Phase4RecoveryUnknown, err
	}
	if status.Revoked {
		return core.Phase4RecoveryUnknown, nil
	}
	if !status.Online {
		return core.Phase4RecoveryAwaiting, nil
	}
	for _, active := range status.ActiveAttempts {
		if active.AttemptID == attempt.ID && active.TurnID == attempt.TurnID && active.WorkerRef == worker.WorkerRef {
			return core.Phase4RecoveryAlive, nil
		}
	}
	// The Node can still have an accepted command or a terminal event
	// in its durable outbox even when its heartbeat lists no Attempt.
	return core.Phase4RecoveryAwaiting, nil
}
