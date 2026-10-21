package ctl

import (
	"context"
	"errors"
	"fmt"

	"github.com/beruseruko/secretary/internal/core"
)

var ErrUnauthorized = errors.New("secretaryctl: unauthorized")

type Service struct {
	Store      *core.Store
	PersonID   string
	Capability string
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
	if err := s.authorize(ctx); err != nil {
		return core.Task{}, err
	}
	return core.Task{}, core.ErrLegacyTaskReadOnly
}

func (s Service) Retry(ctx context.Context, taskID string) (core.Task, error) {
	if err := s.authorize(ctx); err != nil {
		return core.Task{}, err
	}
	return core.Task{}, core.ErrLegacyTaskReadOnly
}

func (s Service) Close(ctx context.Context, taskID string) (core.CloseOutcome, error) {
	if err := s.authorize(ctx); err != nil {
		return core.CloseOutcome{}, err
	}
	return core.CloseOutcome{}, core.ErrLegacyTaskReadOnly
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
