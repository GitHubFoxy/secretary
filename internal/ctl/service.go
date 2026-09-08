package ctl

import (
	"context"
	"errors"
	"fmt"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

var ErrUnauthorized = errors.New("secretaryctl: unauthorized")

// Dispatcher is deliberately narrow. secretaryctl can ask the server to start
// a Worker, but it cannot obtain generic Node or Channel control.
type Dispatcher interface {
	Dispatch(context.Context, core.Task, string) (core.WorkerBinding, core.Attempt, error)
}

type Service struct {
	Store      *core.Store
	PersonID   string
	Capability string
	Dispatcher Dispatcher
	Node       *node.LocalNode
	WorkerRef  func(core.Task) string
}

func (s Service) authorize(ctx context.Context) error {
	if s.Store == nil || s.PersonID == "" || s.Capability == "" {
		return ErrUnauthorized
	}
	allowed, err := s.Store.AuthorizeSecretaryCapability(ctx, s.PersonID, s.Capability)
	if err != nil {
		return fmt.Errorf("authorize capability: %w", err)
	}
	if !allowed {
		return ErrUnauthorized
	}
	return nil
}

func (s Service) conversation(ctx context.Context) (core.Conversation, error) {
	if err := s.authorize(ctx); err != nil {
		return core.Conversation{}, err
	}
	return s.Store.ConversationForPerson(ctx, s.PersonID)
}

func (s Service) Create(ctx context.Context, text string) (core.Task, error) {
	conversation, err := s.conversation(ctx)
	if err != nil {
		return core.Task{}, err
	}
	if text == "" {
		return core.Task{}, errors.New("secretaryctl: task text is required")
	}
	task, err := s.Store.CreateTask(ctx, conversation.ID, text)
	if err != nil {
		return core.Task{}, fmt.Errorf("create task: %w", err)
	}
	if s.Dispatcher == nil {
		return task, nil
	}
	workerRef := task.ID
	if s.WorkerRef != nil {
		workerRef = s.WorkerRef(task)
	}
	if _, _, err := s.Dispatcher.Dispatch(ctx, task, workerRef); err != nil {
		// The task is intentionally retained as dispatch_failed by Dispatcher.
		failed, readErr := s.Store.Task(ctx, task.ID)
		if readErr == nil {
			return failed, err
		}
		return task, err
	}
	return s.Store.Task(ctx, task.ID)
}

func (s Service) Retry(ctx context.Context, taskID string) (core.Task, error) {
	if err := s.authorize(ctx); err != nil {
		return core.Task{}, err
	}
	task, err := s.Store.Task(ctx, taskID)
	if err != nil {
		return core.Task{}, err
	}
	if err := s.assertOwner(ctx, task); err != nil {
		return core.Task{}, err
	}
	task, err = s.Store.RetryDispatch(ctx, taskID)
	if err != nil {
		return core.Task{}, err
	}
	if s.Dispatcher == nil {
		return task, nil
	}
	workerRef := task.ID
	if s.WorkerRef != nil {
		workerRef = s.WorkerRef(task)
	}
	if _, _, err := s.Dispatcher.Dispatch(ctx, task, workerRef); err != nil {
		failed, readErr := s.Store.Task(ctx, task.ID)
		if readErr == nil {
			return failed, err
		}
		return task, err
	}
	return s.Store.Task(ctx, task.ID)
}

func (s Service) Close(ctx context.Context, taskID string) (core.CloseOutcome, error) {
	if err := s.authorize(ctx); err != nil {
		return core.CloseOutcome{}, err
	}
	task, err := s.Store.Task(ctx, taskID)
	if err != nil {
		return core.CloseOutcome{}, err
	}
	if err := s.assertOwner(ctx, task); err != nil {
		return core.CloseOutcome{}, err
	}
	outcome, err := s.Store.CloseTask(ctx, taskID)
	if err != nil || outcome.CancelAttempt == nil || s.Node == nil {
		return outcome, err
	}
	details, detailsErr := s.Store.TaskDetails(ctx, taskID)
	if detailsErr != nil {
		return outcome, detailsErr
	}
	if details.Binding == nil {
		return outcome, errors.New("secretaryctl: active task has no worker binding")
	}
	session, found := s.Node.Session(details.Binding.WorkerRef)
	if !found {
		return outcome, errors.New("secretaryctl: worker session unavailable")
	}
	if cancelErr := session.Cancel(ctx); cancelErr != nil {
		return outcome, fmt.Errorf("cancel active attempt: %w", cancelErr)
	}
	return outcome, nil
}

func (s Service) List(ctx context.Context) ([]core.Task, error) {
	conversation, err := s.conversation(ctx)
	if err != nil {
		return nil, err
	}
	return s.Store.TasksForConversation(ctx, conversation.ID)
}

func (s Service) Show(ctx context.Context, taskID string) (core.TaskDetails, error) {
	if err := s.authorize(ctx); err != nil {
		return core.TaskDetails{}, err
	}
	details, err := s.Store.TaskDetails(ctx, taskID)
	if err != nil {
		return core.TaskDetails{}, err
	}
	if err := s.assertOwner(ctx, details.Task); err != nil {
		return core.TaskDetails{}, err
	}
	return details, nil
}

func (s Service) assertOwner(ctx context.Context, task core.Task) error {
	conversation, err := s.Store.ConversationForPerson(ctx, s.PersonID)
	if err != nil {
		return err
	}
	if task.ConversationID != conversation.ID {
		return ErrUnauthorized
	}
	return nil
}
