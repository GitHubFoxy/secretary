package node

import (
	"context"
	"errors"

	"github.com/beruseruko/secretary/internal/core"
)

// Phase4RecoveryResolver probes the server-owned local and remote runtime
// wiring. It never starts, resumes, or substitutes a runtime during recovery.
type Phase4RecoveryResolver struct {
	Store        *core.Store
	Local        *LocalNode
	LocalNodeRef core.NodeReference
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
	if r.Local != nil && core.NodeReference(attempt.NodeID) == r.LocalNodeRef {
		if _, ok := r.Local.Session(worker.WorkerRef); ok {
			return core.Phase4RecoveryAlive, nil
		}
		return core.Phase4RecoveryUnknown, nil
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
	if !status.Online || status.Revoked {
		return core.Phase4RecoveryUnknown, nil
	}
	for _, active := range status.ActiveAttempts {
		if active.AttemptID == attempt.ID && active.TurnID == attempt.TurnID && active.WorkerRef == worker.WorkerRef {
			return core.Phase4RecoveryAlive, nil
		}
	}
	return core.Phase4RecoveryUnknown, nil
}
