package ctl

import (
	"context"
	"errors"
	"github.com/beruseruko/secretary/internal/core"
	"time"
)

func (s WorkerService) RunQueuedWorkerMessages(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		_ = s.ProcessQueuedWorkerMessages(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s WorkerService) ProcessQueuedWorkerMessages(ctx context.Context) error {
	conversation, err := s.authorize(ctx)
	if err != nil {
		return err
	}
	now := s.workerCommandNow()
	dueSchedules, err := s.Store.DueAttemptSchedules(ctx, now)
	if err == nil {
		for _, schedule := range dueSchedules {
			_ = s.Store.WithWorkerLifecycle(ctx, schedule.WorkerID, func(ctx context.Context) error {
				return s.processDueSchedule(ctx, schedule)
			})
		}
	}
	workers, err := s.Store.WorkersForConversation(ctx, conversation.ID)
	if err != nil {
		return err
	}
	var failures []error
	for _, worker := range workers {
		err := s.Store.WithWorkerLifecycle(ctx, worker.ID, func(ctx context.Context) error {
			return s.processQueuedWorker(ctx, conversation.ID, worker)
		})
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (s WorkerService) processDueSchedule(ctx context.Context, schedule core.AttemptSchedule) error {
	if !s.Store.EffectiveAutoResumeOnLimit() {
		return nil
	}
	worker, err := s.Store.Worker(ctx, schedule.WorkerID)
	if err != nil {
		return err
	}
	if !s.workerNodeReady(ctx, worker) {
		return nil
	}
	turn, err := s.Store.Turn(ctx, schedule.TurnID)
	if err != nil {
		return err
	}
	attempt, err := s.Store.Phase4Attempt(ctx, schedule.AttemptID)
	if err != nil {
		return err
	}
	now := s.workerCommandNow()
	claimed, err := s.Store.ClaimAttemptSchedule(ctx, schedule.ID, now)
	if err != nil || !claimed {
		return err
	}
	if err := s.Store.ReserveAttemptCapacity(ctx, attempt.ID); err != nil {
		_ = s.Store.ReleaseAttemptSchedule(ctx, schedule.ID, now)
		if errors.Is(err, core.ErrNodeCapacityExhausted) {
			return nil // retry when Node capacity becomes available
		}
		return err
	}
	command, send, err := s.claimCommand(ctx, "resume", "attempt", worker, attempt, nil)
	if err != nil {
		_ = s.Store.ReleaseAttemptSchedule(ctx, schedule.ID, now)
		return err
	}
	if send {
		continuationPrompt := schedule.ContinuationPrompt
		if continuationPrompt == "" {
			continuationPrompt = core.ContinuationPrompt
		}
		err = s.deliverCommand(ctx, command, func(commandID string) error {
			return s.Runtime.Resume(ctx, commandID, worker, turn, attempt, continuationPrompt)
		})
		if err != nil {
			_ = s.Store.ReleaseAttemptSchedule(ctx, schedule.ID, now)
			return err
		}
	}
	return s.Store.CompleteAttemptSchedule(ctx, schedule.ID, now)
}

func (s WorkerService) queuedWorkerRuntimeUnavailable(ctx context.Context, worker core.Worker) bool {
	runtime, ok := s.Runtime.(interface {
		CommandReady(context.Context, core.NodeReference) (bool, error)
	})
	if !ok {
		return false
	}
	ready, err := runtime.CommandReady(ctx, core.NodeReference(worker.NodeID))
	return err == nil && !ready
}

func (s WorkerService) workerNodeReady(ctx context.Context, worker core.Worker) bool {
	if s.requireRuntime() != nil {
		return false
	}
	runtime, ok := s.Runtime.(interface {
		CommandReady(context.Context, core.NodeReference) (bool, error)
	})
	if !ok {
		return true
	}
	ready, err := runtime.CommandReady(ctx, core.NodeReference(worker.NodeID))
	return err == nil && ready
}

func (s WorkerService) processQueuedWorker(ctx context.Context, conversationID string, worker core.Worker) error {
	var failures []error
	details, err := s.Store.WorkerDetailsForConversation(ctx, conversationID, worker.WorkerRef)
	if err != nil {
		return err
	}
	for _, message := range details.QueuedMessages {
		if message.State == "delivered" || message.State == "canceled" {
			continue
		}
		if message.State == "blocked" {
			break
		}
		kind := "dispatch"
		if message.State == "pending" && worker.Status != core.WorkerIdle && worker.Status != core.WorkerOffline {
			break
		}
		if message.State == "pending" && worker.Status == core.WorkerOffline {
			kind = "resume"
		}
		if message.State == "delivering" {
			promoted, err := s.Store.Turn(ctx, message.TurnID)
			if err != nil {
				failures = append(failures, err)
				break
			}
			if _, found, err := s.Store.FindWorkerCommand(ctx, "resume", "attempt", worker.ID, promoted.CurrentAttemptID); err != nil {
				failures = append(failures, err)
				break
			} else if found {
				kind = "resume"
			}
		}
		if message.State == "pending" && s.queuedWorkerRuntimeUnavailable(ctx, worker) {
			break
		}
		turn, attempt, err := s.Store.PromoteQueuedWorkerMessage(ctx, message, kind)
		if errors.Is(err, core.ErrInvalidTransition) {
			break
		}
		if err != nil {
			failures = append(failures, err)
			break
		}
		attempt, err = s.Store.Phase4Attempt(ctx, attempt.ID)
		if err != nil {
			failures = append(failures, err)
			break
		}
		if _, found, findErr := s.Store.FindWorkerCommand(ctx, "resume", "attempt", worker.ID, attempt.ID); findErr != nil {
			failures = append(failures, findErr)
			break
		} else if found {
			kind = "resume"
		}
		command, found, findErr := s.Store.FindWorkerCommand(ctx, kind, "attempt", worker.ID, attempt.ID)
		if findErr != nil {
			failures = append(failures, findErr)
			break
		}
		if found && command.State == core.WorkerCommandDelivered {
			if err := s.Store.CompleteQueuedWorkerMessage(ctx, message.ID, "delivered", ""); err != nil {
				failures = append(failures, err)
			}
			break
		}
		if attempt.State.Terminal() || (found && (command.State == core.WorkerCommandFailed || command.State == core.WorkerCommandUncertain || (!command.LeaseUntil.IsZero() && !s.workerCommandNow().Before(command.LeaseUntil)))) {
			err := s.Store.CompleteQueuedWorkerMessage(ctx, message.ID, "blocked", "worker: prior delivery interrupted or acceptance unknown; automatic replay disabled")
			if err != nil {
				failures = append(failures, err)
			}
			break
		}
		if found && !command.LeaseUntil.IsZero() {
			break
		}
		if s.queuedWorkerRuntimeUnavailable(ctx, worker) {
			break
		}
		err = s.recoverLifecycleCommand(ctx, kind, worker, attempt, func(commandID string) error {
			if err := s.requireRuntime(); err != nil {
				return err
			}
			if kind == "resume" {
				return s.Runtime.Resume(ctx, commandID, worker, turn, attempt, turn.Input)
			}
			binding, err := s.Store.ResolveWorkerBinding(ctx, worker.ID)
			if err != nil {
				return err
			}
			return s.Runtime.Dispatch(ctx, commandID, worker, turn, attempt, core.DispatchResolution{ProjectDispatch: binding})
		}, nil)
		if errors.Is(err, ErrWorkerCommandPending) || errors.Is(err, core.ErrNodeCapacityExhausted) {
			// Preserve a prepared Turn/Attempt while waiting for another
			// Worker to release capacity. No handoff command was claimed.
			break
		}
		state, lastError := "delivered", ""
		if err != nil {
			state, lastError = "blocked", err.Error()
			failures = append(failures, err)
		}
		if completeErr := s.Store.CompleteQueuedWorkerMessage(ctx, message.ID, state, lastError); completeErr != nil {
			failures = append(failures, completeErr)
		}
		break
	}
	return errors.Join(failures...)
}
