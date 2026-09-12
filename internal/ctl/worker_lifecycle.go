package ctl

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
)

var ErrWorkerRuntimeUnavailable = errors.New("worker: runtime command delivery is unavailable")

// WorkerRuntime delivers lifecycle commands to the immutable Worker binding.
// commandID is durable and stable across duplicate Secretary tool delivery.
// It deliberately has no retry operation and never receives a runtime session ID.
type WorkerRuntime interface {
	Dispatch(context.Context, string, core.Worker, core.Turn, core.Phase4Attempt, core.DispatchResolution) error
	Steer(context.Context, string, core.Worker, core.Phase4Attempt, string) error
	Respond(context.Context, string, core.Worker, core.Phase4Attempt, string, string) error
	Resume(context.Context, string, core.Worker, core.Turn, core.Phase4Attempt, string) error
	Cancel(context.Context, string, core.Worker, core.Phase4Attempt) error
}

type WorkerService struct {
	Store        *core.Store
	PersonID     string
	Capability   string
	WorkerPolicy core.HarnessPolicy
	Runtime      WorkerRuntime
}

type WorkerPreferences struct {
	ProjectID       string                 `json:"project_id"`
	NodeID          core.NodeReference     `json:"node_id,omitempty"`
	HarnessInstance core.HarnessInstanceID `json:"harness_instance_id,omitempty"`
	HarnessKind     core.HarnessKind       `json:"harness_kind,omitempty"`
	Workspace       string                 `json:"workspace,omitempty"`
	ModelID         string                 `json:"model_id,omitempty"`
	Reasoning       string                 `json:"reasoning,omitempty"`
}

type SpawnWorkerRequest struct {
	Intent         string            `json:"intent"`
	Preferences    WorkerPreferences `json:"preferences"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
	// Flat fields preserve direct Go callers while MCP uses preferences.
	ProjectID       string                 `json:"project_id,omitempty"`
	NodeID          core.NodeReference     `json:"node_id,omitempty"`
	HarnessInstance core.HarnessInstanceID `json:"harness_instance_id,omitempty"`
	HarnessKind     core.HarnessKind       `json:"harness_kind,omitempty"`
	Workspace       string                 `json:"workspace,omitempty"`
	ModelID         string                 `json:"model_id,omitempty"`
	Reasoning       string                 `json:"reasoning,omitempty"`
}

type MessageWorkerRequest struct {
	WorkerRef      string `json:"worker_ref"`
	Text           string `json:"text"`
	RequestID      string `json:"request_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

func (s WorkerService) authorize(ctx context.Context) (core.Conversation, error) {
	if s.Store == nil || s.PersonID == "" || s.Capability == "" {
		return core.Conversation{}, ErrUnauthorized
	}
	allowed, err := s.Store.AuthorizeSecretaryCapability(ctx, s.PersonID, s.Capability)
	if err != nil {
		return core.Conversation{}, fmt.Errorf("authorize capability: %w", err)
	}
	if !allowed {
		return core.Conversation{}, ErrUnauthorized
	}
	return s.Store.ConversationForPerson(ctx, s.PersonID)
}

func (s WorkerService) requireRuntime() error {
	if s.Runtime == nil {
		return ErrWorkerRuntimeUnavailable
	}
	return nil
}

// claimCommand commits the server-side command identity before handoff. The
// same Worker, Attempt and command kind always reuse one ID. Pending,
// delivered and failed commands are never sent twice by this service. A failed
// handoff remains visible and requires explicit recovery rather than a retry.
func (s WorkerService) claimCommand(ctx context.Context, kind string, worker core.Worker, attempt core.Phase4Attempt) (core.WorkerCommand, bool, error) {
	command, duplicate, err := s.Store.ClaimWorkerCommand(ctx, kind, worker.ID, attempt.ID)
	if err != nil {
		return core.WorkerCommand{}, false, err
	}
	if duplicate && command.State == core.WorkerCommandFailed {
		return core.WorkerCommand{}, false, fmt.Errorf("worker: prior %s command failed: %s", kind, command.LastError)
	}
	return command, !duplicate, nil
}

func (s WorkerService) deliverCommand(ctx context.Context, command core.WorkerCommand, send func(string) error) error {
	if err := send(command.ID); err != nil {
		_, _ = s.Store.MarkWorkerCommandFailed(context.Background(), command.ID, err.Error())
		return err
	}
	_, err := s.Store.MarkWorkerCommandDelivered(ctx, command.ID)
	return err
}

func (s WorkerService) ListNodes(ctx context.Context) ([]core.NodeRecord, error) {
	if _, err := s.authorize(ctx); err != nil {
		return nil, err
	}
	return s.Store.NodeRecords(ctx)
}

func (s WorkerService) ListProjects(ctx context.Context) ([]core.Project, error) {
	if _, err := s.authorize(ctx); err != nil {
		return nil, err
	}
	return s.Store.Projects(ctx)
}

func (s WorkerService) ListWorkers(ctx context.Context) ([]core.Worker, error) {
	conversation, err := s.authorize(ctx)
	if err != nil {
		return nil, err
	}
	return s.Store.WorkersForConversation(ctx, conversation.ID)
}

func (s WorkerService) GetWorker(ctx context.Context, workerRef string) (core.WorkerDetails, error) {
	conversation, err := s.authorize(ctx)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	return s.Store.WorkerDetailsForConversation(ctx, conversation.ID, workerRef)
}

func (s WorkerService) SpawnWorker(ctx context.Context, request SpawnWorkerRequest) (core.WorkerDetails, error) {
	conversation, err := s.authorize(ctx)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	request.Intent = strings.TrimSpace(request.Intent)
	if request.Intent == "" {
		return core.WorkerDetails{}, errors.New("worker: intent is required")
	}
	preferences := request.Preferences
	if preferences.ProjectID == "" {
		preferences = WorkerPreferences{ProjectID: request.ProjectID, NodeID: request.NodeID, HarnessInstance: request.HarnessInstance, HarnessKind: request.HarnessKind, Workspace: request.Workspace, ModelID: request.ModelID, Reasoning: request.Reasoning}
	}
	worker, turn, attempt, resolution, err := s.Store.ResolveAndCreateWorker(ctx, conversation.ID, request.Intent, core.DispatchResolutionRequest{
		ProjectID: preferences.ProjectID, NodeID: preferences.NodeID, HarnessInstanceID: preferences.HarnessInstance,
		HarnessKind: preferences.HarnessKind, Workspace: preferences.Workspace, ModelID: preferences.ModelID,
		Reasoning: preferences.Reasoning, WorkerPolicy: s.WorkerPolicy,
	}, request.IdempotencyKey)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	if !resolution.Queued {
		if err := s.requireRuntime(); err != nil {
			return core.WorkerDetails{}, err
		}
		command, send, err := s.claimCommand(ctx, "dispatch", worker, attempt)
		if err != nil {
			return core.WorkerDetails{}, err
		}
		if send {
			if err := s.deliverCommand(ctx, command, func(commandID string) error {
				return s.Runtime.Dispatch(ctx, commandID, worker, turn, attempt, resolution)
			}); err != nil {
				return core.WorkerDetails{}, fmt.Errorf("dispatch Worker: %w", err)
			}
		}
	}
	return s.Store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
}

func (s WorkerService) MessageWorker(ctx context.Context, request MessageWorkerRequest) (core.WorkerDetails, error) {
	conversation, err := s.authorize(ctx)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	request.Text = strings.TrimSpace(request.Text)
	if request.Text == "" {
		return core.WorkerDetails{}, errors.New("worker: message text is required")
	}
	details, err := s.Store.WorkerDetailsForConversation(ctx, conversation.ID, request.WorkerRef)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	attempt := details.CurrentAttempt()
	switch details.Worker.Status {
	case core.WorkerWorking:
		if attempt == nil {
			return core.WorkerDetails{}, core.ErrInvalidTransition
		}
		if err := s.requireRuntime(); err != nil {
			return core.WorkerDetails{}, err
		}
		command, send, err := s.claimCommand(ctx, "steering", details.Worker, *attempt)
		if err != nil {
			return core.WorkerDetails{}, err
		}
		if send {
			err = s.deliverCommand(ctx, command, func(commandID string) error {
				return s.Runtime.Steer(ctx, commandID, details.Worker, *attempt, request.Text)
			})
		}
	case core.WorkerNeedsInput:
		if attempt == nil {
			return core.WorkerDetails{}, core.ErrInvalidTransition
		}
		if strings.TrimSpace(request.RequestID) == "" {
			return core.WorkerDetails{}, errors.New("worker: needs_input response requires request_id")
		}
		if err := s.requireRuntime(); err != nil {
			return core.WorkerDetails{}, err
		}
		command, send, err := s.claimCommand(ctx, "respond", details.Worker, *attempt)
		if err != nil {
			return core.WorkerDetails{}, err
		}
		if send {
			err = s.deliverCommand(ctx, command, func(commandID string) error {
				return s.Runtime.Respond(ctx, commandID, details.Worker, *attempt, request.RequestID, request.Text)
			})
		}
		if err == nil {
			_, err = s.Store.ResumePhase4Attempt(ctx, attempt.ID)
		}
	case core.WorkerIdle:
		if err := s.requireRuntime(); err != nil {
			return core.WorkerDetails{}, err
		}
		turn, next, createErr := s.Store.CreateTurn(ctx, details.Worker.ID, core.TurnSpec{Input: request.Text, IdempotencyKey: request.IdempotencyKey})
		if createErr != nil {
			err = createErr
			break
		}
		binding, bindingErr := s.Store.ResolveWorkerBinding(ctx, details.Worker.ID)
		if bindingErr != nil {
			err = bindingErr
			break
		}
		command, send, claimErr := s.claimCommand(ctx, "dispatch", details.Worker, next)
		if claimErr != nil {
			err = claimErr
			break
		}
		if send {
			err = s.deliverCommand(ctx, command, func(commandID string) error {
				return s.Runtime.Dispatch(ctx, commandID, details.Worker, turn, next, core.DispatchResolution{ProjectDispatch: binding})
			})
		}
	case core.WorkerOffline:
		if attempt == nil {
			return core.WorkerDetails{}, core.ErrInvalidTransition
		}
		if err := s.requireRuntime(); err != nil {
			return core.WorkerDetails{}, err
		}
		turn, next, createErr := s.Store.CreateTurn(ctx, details.Worker.ID, core.TurnSpec{Input: request.Text, IdempotencyKey: request.IdempotencyKey})
		if createErr != nil {
			err = createErr
			break
		}
		command, send, claimErr := s.claimCommand(ctx, "resume", details.Worker, next)
		if claimErr != nil {
			err = claimErr
			break
		}
		if send {
			err = s.deliverCommand(ctx, command, func(commandID string) error {
				return s.Runtime.Resume(ctx, commandID, details.Worker, turn, next, request.Text)
			})
		}
	default:
		err = core.ErrInvalidTransition
	}
	if err != nil {
		return core.WorkerDetails{}, err
	}
	return s.Store.WorkerDetailsForConversation(ctx, conversation.ID, request.WorkerRef)
}

func (s WorkerService) CancelWorker(ctx context.Context, workerRef string) (core.WorkerDetails, error) {
	conversation, err := s.authorize(ctx)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	details, err := s.Store.WorkerDetailsForConversation(ctx, conversation.ID, workerRef)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	attempt := details.CurrentAttempt()
	if attempt != nil && !attempt.State.Terminal() {
		if err := s.requireRuntime(); err != nil {
			return core.WorkerDetails{}, err
		}
		command, send, err := s.claimCommand(ctx, "cancel", details.Worker, *attempt)
		if err != nil {
			return core.WorkerDetails{}, err
		}
		if send {
			if err := s.deliverCommand(ctx, command, func(commandID string) error { return s.Runtime.Cancel(ctx, commandID, details.Worker, *attempt) }); err != nil {
				return core.WorkerDetails{}, err
			}
		}
		if _, _, _, err := s.Store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{Status: core.OutcomeCanceled, Classification: core.OutcomeFinal, ErrorCode: "canceled", FailureCode: "canceled", Summary: "Canceled by Secretary"}); err != nil {
			return core.WorkerDetails{}, err
		}
	}
	return s.Store.WorkerDetailsForConversation(ctx, conversation.ID, workerRef)
}

func (s WorkerService) CloseWorker(ctx context.Context, workerRef string) (core.WorkerDetails, error) {
	conversation, err := s.authorize(ctx)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	if _, err := s.CancelWorker(ctx, workerRef); err != nil {
		return core.WorkerDetails{}, err
	}
	details, err := s.Store.WorkerDetailsForConversation(ctx, conversation.ID, workerRef)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	if _, err := s.Store.CloseWorker(ctx, details.Worker.ID); err != nil {
		return core.WorkerDetails{}, err
	}
	return s.Store.WorkerDetailsForConversation(ctx, conversation.ID, workerRef)
}
