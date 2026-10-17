package ctl

import (
	"context"
	"errors"

	"github.com/beruseruko/secretary/internal/core"
)

const ownerAbandonedUnknownExecution = "owner_abandoned_execution_unknown"

func (s WorkerService) AbandonAttempt(ctx context.Context, workerRef, attemptID string) (core.WorkerDetails, error) {
	conversation, err := s.authorize(ctx)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	details, err := s.Store.WorkerDetailsForConversation(ctx, conversation.ID, workerRef)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	if attemptID == "" {
		return core.WorkerDetails{}, core.ErrInvalidTransition
	}
	err = s.Store.WithWorkerLifecycle(ctx, details.Worker.ID, func(ctx context.Context) error {
		current, err := s.Store.WorkerDetailsForConversation(ctx, conversation.ID, workerRef)
		if err != nil {
			return err
		}
		attempt := current.CurrentAttempt()
		if attempt == nil || attempt.ID != attemptID {
			return core.ErrInvalidTransition
		}
		if attempt.State.Terminal() {
			for _, result := range current.Results {
				if result.AttemptID == attemptID && result.FailureCode == ownerAbandonedUnknownExecution {
					return nil
				}
			}
			return core.ErrInvalidTransition
		}
		if current.Worker.Status != core.WorkerOffline ||
			(attempt.State != core.AttemptStarting && attempt.State != core.AttemptActive) {
			return core.ErrInvalidTransition
		}
		_, _, _, err = s.Store.InterruptPhase4Attempt(ctx, attempt.ID, ownerAbandonedUnknownExecution,
			"Owner abandoned an unresolved Attempt. External side effects may already have completed. The native execution was not canceled.")
		if errors.Is(err, core.ErrInvalidTransition) {
			return core.ErrInvalidTransition
		}
		return err
	})
	if err != nil {
		return core.WorkerDetails{}, err
	}
	return s.Store.WorkerDetailsForConversation(ctx, conversation.ID, workerRef)
}
