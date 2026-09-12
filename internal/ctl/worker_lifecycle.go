package ctl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

var (
	ErrWorkerRuntimeUnavailable = errors.New("worker: runtime command delivery is unavailable")
	ErrWorkerCommandPending     = errors.New("worker: runtime command delivery is pending recovery")
)

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
	commandNow   func() time.Time
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
	ClientID       string `json:"client_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

// RespondWorker is the single server-side entry point for permission and input
// responses. It intentionally delegates to the same generic Node command path
// as Worker messages.
func (s WorkerService) RespondWorker(ctx context.Context, request MessageWorkerRequest) (core.WorkerDetails, error) {
	return s.MessageWorker(ctx, request)
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
// same Worker, Attempt and command kind always reuse one ID. A duplicate
// pending command is resent only after core atomically reclaims its expired
// lease. Node command dedupe then protects the side effect of that handoff.
func (s WorkerService) claimCommand(ctx context.Context, kind, dedupeKey string, worker core.Worker, attempt core.Phase4Attempt) (core.WorkerCommand, bool, error) {
	command, duplicate, err := s.Store.ClaimWorkerCommand(ctx, kind, dedupeKey, worker.ID, attempt.ID)
	if err != nil {
		return core.WorkerCommand{}, false, err
	}
	if duplicate && command.State == core.WorkerCommandPending {
		reclaimed, send, err := s.Store.ReclaimWorkerCommand(ctx, command.ID, s.workerCommandNow())
		if err != nil {
			return core.WorkerCommand{}, false, err
		}
		if !send {
			return core.WorkerCommand{}, false, fmt.Errorf("%w: %s", ErrWorkerCommandPending, kind)
		}
		return reclaimed, true, nil
	}
	if duplicate && command.State == core.WorkerCommandFailed {
		if kind != "respond" {
			return core.WorkerCommand{}, false, fmt.Errorf("worker: prior %s command failed: %s", kind, command.LastError)
		}
		retried, retry, err := s.Store.RetryWorkerCommand(ctx, command.ID, s.workerCommandNow())
		if err != nil {
			return core.WorkerCommand{}, false, err
		}
		if !retry {
			return core.WorkerCommand{}, false, fmt.Errorf("%w: %s", ErrWorkerCommandPending, kind)
		}
		return retried, true, nil
	}
	return command, !duplicate, nil
}

func (s WorkerService) workerCommandNow() time.Time {
	if s.commandNow != nil {
		return s.commandNow().UTC()
	}
	return time.Now().UTC()
}

func commandDedupeKey(idempotencyKey, fallback string) string {
	if key := strings.TrimSpace(idempotencyKey); key != "" {
		return "key:" + key
	}
	return fallback
}

func steeringDedupeKey(idempotencyKey string) (string, error) {
	if key := strings.TrimSpace(idempotencyKey); key != "" {
		return "key:" + key, nil
	}
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("worker: generate steering command key: %w", err)
	}
	return "call:" + hex.EncodeToString(bytes), nil
}

func (s WorkerService) deliverCommand(ctx context.Context, command core.WorkerCommand, send func(string) error) error {
	if err := send(command.ID); err != nil {
		_, _ = s.Store.MarkWorkerCommandFailed(context.Background(), command.ID, err.Error())
		return err
	}
	_, err := s.Store.MarkWorkerCommandDelivered(ctx, command.ID)
	return err
}

// recoverLifecycleCommand repairs pre-intent committed Attempts, then claims
// the stable command ID. The runtime is required only when a handoff is due.
func (s WorkerService) recoverLifecycleCommand(ctx context.Context, kind string, worker core.Worker, attempt core.Phase4Attempt, send func(string) error) error {
	if _, err := s.Store.EnsureLifecycleCommandIntent(ctx, kind, worker.ID, attempt.ID); err != nil {
		return err
	}
	command, handoff, err := s.claimCommand(ctx, kind, "attempt", worker, attempt)
	if err != nil || !handoff {
		return err
	}
	if err := s.requireRuntime(); err != nil {
		return err
	}
	return s.deliverCommand(ctx, command, send)
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
	resolutionRequest := core.DispatchResolutionRequest{ProjectID: preferences.ProjectID, NodeID: preferences.NodeID, HarnessInstanceID: preferences.HarnessInstance,
		HarnessKind: preferences.HarnessKind, Workspace: preferences.Workspace, ModelID: preferences.ModelID,
		Reasoning: preferences.Reasoning, WorkerPolicy: s.WorkerPolicy}
	// Replay precedes the preview because the durable outcome is valid even if
	// its Project, Node inventory, or dispatch preferences have since changed.
	if worker, turn, attempt, resolution, found, err := s.Store.ReplayWorkerCreation(ctx, request.IdempotencyKey); err != nil {
		return core.WorkerDetails{}, err
	} else if found {
		// A new Worker is stored queued until the Node acknowledges the initial
		// dispatch. Its replay snapshot therefore cannot decide whether the
		// durable pending handoff still needs delivery.
		if err := s.recoverLifecycleCommand(ctx, "dispatch", worker, attempt, func(commandID string) error {
			return s.Runtime.Dispatch(ctx, commandID, worker, turn, attempt, resolution)
		}); err != nil {
			return core.WorkerDetails{}, fmt.Errorf("recover dispatch Worker: %w", err)
		}
		return s.Store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	}
	// Resolve before creation so the production MCP wiring with Runtime=nil
	// cannot leave an online Worker/Turn/Attempt behind on a rejected spawn.
	preview, err := s.Store.ResolveDispatch(ctx, resolutionRequest)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	if !preview.Queued {
		if err := s.requireRuntime(); err != nil {
			return core.WorkerDetails{}, err
		}
	}
	worker, turn, attempt, resolution, err := s.Store.ResolveAndCreateWorker(ctx, conversation.ID, request.Intent, resolutionRequest, request.IdempotencyKey)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	if !resolution.Queued {
		if err := s.recoverLifecycleCommand(ctx, "dispatch", worker, attempt, func(commandID string) error {
			return s.Runtime.Dispatch(ctx, commandID, worker, turn, attempt, resolution)
		}); err != nil {
			return core.WorkerDetails{}, fmt.Errorf("dispatch Worker: %w", err)
		}
	}
	return s.Store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
}

func approvalResponseState(kind core.ApprovalKind, response string) (core.ApprovalState, error) {
	value := strings.ToLower(strings.TrimSpace(response))
	if kind == core.ApprovalInput {
		if value == "" {
			return "", errors.New("worker: input response is required")
		}
		return core.ApprovalApproved, nil
	}
	switch value {
	case "approve", "approved", "allow", "yes", `{"approved":true}`:
		return core.ApprovalApproved, nil
	case "deny", "denied", "reject", "rejected", "no", `{"approved":false}`:
		return core.ApprovalDenied, nil
	default:
		return "", errors.New("worker: approval response must approve or deny")
	}
}

func (s WorkerService) respondApproval(ctx context.Context, conversationID string, request MessageWorkerRequest, details core.WorkerDetails, attempt core.Phase4Attempt, approval core.Approval, state core.ApprovalState, clientID string) (core.WorkerDetails, error) {
	command, send, err := s.claimCommand(ctx, "respond", "request:"+request.RequestID, details.Worker, attempt)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	if send {
		if err := s.requireRuntime(); err != nil {
			return core.WorkerDetails{}, err
		}
		if err := s.deliverCommand(ctx, command, func(commandID string) error {
			return s.Runtime.Respond(ctx, commandID, details.Worker, attempt, request.RequestID, request.Text)
		}); err != nil {
			return core.WorkerDetails{}, err
		}
		command.State = core.WorkerCommandDelivered
	}
	if command.State != core.WorkerCommandDelivered {
		return core.WorkerDetails{}, ErrWorkerCommandPending
	}
	if _, _, err := s.Store.CommitApprovalResolution(ctx, approval.RequestID, state, clientID, request.Text); err != nil {
		return core.WorkerDetails{}, err
	}
	return s.Store.WorkerDetailsForConversation(ctx, conversationID, request.WorkerRef)
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
	if attempt != nil && strings.TrimSpace(request.RequestID) != "" {
		approval, approvalErr := s.Store.Approval(ctx, request.RequestID)
		if approvalErr == nil {
			if approval.WorkerID != details.Worker.ID || approval.AttemptID != attempt.ID {
				return core.WorkerDetails{}, core.ErrInvalidTransition
			}
			if approval.State == core.ApprovalPending {
				if approval.ExpiresAt != nil && !s.workerCommandNow().Before(*approval.ExpiresAt) {
					if _, _, err := s.Store.ExpireApproval(ctx, request.RequestID, s.workerCommandNow()); err != nil {
						return core.WorkerDetails{}, err
					}
					return s.Store.WorkerDetailsForConversation(ctx, conversation.ID, request.WorkerRef)
				}
				state, stateErr := approvalResponseState(approval.Kind, request.Text)
				if stateErr != nil {
					return core.WorkerDetails{}, stateErr
				}
				clientID := strings.TrimSpace(request.ClientID)
				if clientID == "" {
					clientID = "client"
				}
				return s.respondApproval(ctx, conversation.ID, request, details, *attempt, approval, state, clientID)
			}
			// A terminal Approval is authoritative. In particular, denied,
			// expired and revoked requests never trigger another machine action.
			return s.Store.WorkerDetailsForConversation(ctx, conversation.ID, request.WorkerRef)
		}
		if !errors.Is(approvalErr, core.ErrNotFound) {
			return core.WorkerDetails{}, approvalErr
		}
		command, found, commandErr := s.Store.FindWorkerCommand(ctx, "respond", commandDedupeKey(request.IdempotencyKey, "request:"+request.RequestID), details.Worker.ID, attempt.ID)
		if commandErr != nil {
			return core.WorkerDetails{}, commandErr
		}
		if found && command.State == core.WorkerCommandDelivered {
			if details.Worker.Status == core.WorkerNeedsInput {
				if _, commandErr := s.Store.ResumePhase4Attempt(ctx, attempt.ID); commandErr != nil {
					return core.WorkerDetails{}, commandErr
				}
			}
			return s.Store.WorkerDetailsForConversation(ctx, conversation.ID, request.WorkerRef)
		}
	}
	switch details.Worker.Status {
	case core.WorkerWaitingApproval:
		return core.WorkerDetails{}, errors.New("worker: approval request is required")
	case core.WorkerWorking:
		if attempt == nil {
			return core.WorkerDetails{}, core.ErrInvalidTransition
		}
		if err := s.requireRuntime(); err != nil {
			return core.WorkerDetails{}, err
		}
		dedupeKey, err := steeringDedupeKey(request.IdempotencyKey)
		if err != nil {
			return core.WorkerDetails{}, err
		}
		command, send, err := s.claimCommand(ctx, "steering", dedupeKey, details.Worker, *attempt)
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
		command, send, err := s.claimCommand(ctx, "respond", commandDedupeKey(request.IdempotencyKey, "request:"+request.RequestID), details.Worker, *attempt)
		if err != nil {
			return core.WorkerDetails{}, err
		}
		if send {
			err = s.deliverCommand(ctx, command, func(commandID string) error {
				return s.Runtime.Respond(ctx, commandID, details.Worker, *attempt, request.RequestID, request.Text)
			})
		}
		if err == nil && send {
			_, err = s.Store.ResumePhase4Attempt(ctx, attempt.ID)
		}
	case core.WorkerIdle:
		if err := s.requireRuntime(); err != nil {
			return core.WorkerDetails{}, err
		}
		turn, next, createErr := s.Store.CreateTurn(ctx, details.Worker.ID, core.TurnSpec{Input: request.Text, IdempotencyKey: request.IdempotencyKey, CommandKind: "dispatch"})
		if createErr != nil {
			err = createErr
			break
		}
		binding, bindingErr := s.Store.ResolveWorkerBinding(ctx, details.Worker.ID)
		if bindingErr != nil {
			err = bindingErr
			break
		}
		command, send, claimErr := s.claimCommand(ctx, "dispatch", "attempt", details.Worker, next)
		if claimErr != nil {
			err = claimErr
			break
		}
		if send {
			err = s.deliverCommand(ctx, command, func(commandID string) error {
				return s.Runtime.Dispatch(ctx, commandID, details.Worker, turn, next, core.DispatchResolution{ProjectDispatch: binding})
			})
		}
	case core.WorkerQueued:
		if attempt == nil || attempt.State != core.AttemptStarting || strings.TrimSpace(request.IdempotencyKey) == "" {
			return core.WorkerDetails{}, core.ErrInvalidTransition
		}
		turn, next, createErr := s.Store.CreateTurn(ctx, details.Worker.ID, core.TurnSpec{Input: request.Text, IdempotencyKey: request.IdempotencyKey})
		if createErr != nil {
			err = createErr
			break
		}
		kind := "dispatch"
		if _, found, findErr := s.Store.FindWorkerCommand(ctx, "resume", "attempt", details.Worker.ID, next.ID); findErr != nil {
			err = findErr
			break
		} else if found {
			kind = "resume"
		}
		command, send, claimErr := s.claimCommand(ctx, kind, "attempt", details.Worker, next)
		if claimErr != nil {
			err = claimErr
			break
		}
		if send {
			if runtimeErr := s.requireRuntime(); runtimeErr != nil {
				err = runtimeErr
				break
			}
			if kind == "resume" {
				err = s.deliverCommand(ctx, command, func(commandID string) error {
					return s.Runtime.Resume(ctx, commandID, details.Worker, turn, next, turn.Input)
				})
			} else {
				binding, bindingErr := s.Store.ResolveWorkerBinding(ctx, details.Worker.ID)
				if bindingErr != nil {
					err = bindingErr
					break
				}
				err = s.deliverCommand(ctx, command, func(commandID string) error {
					return s.Runtime.Dispatch(ctx, commandID, details.Worker, turn, next, core.DispatchResolution{ProjectDispatch: binding})
				})
			}
		}
	case core.WorkerOffline:
		if attempt == nil {
			return core.WorkerDetails{}, core.ErrInvalidTransition
		}
		if err := s.requireRuntime(); err != nil {
			return core.WorkerDetails{}, err
		}
		turn, next, createErr := s.Store.CreateTurn(ctx, details.Worker.ID, core.TurnSpec{Input: request.Text, IdempotencyKey: request.IdempotencyKey, CommandKind: "resume"})
		if createErr != nil {
			err = createErr
			break
		}
		command, send, claimErr := s.claimCommand(ctx, "resume", "attempt", details.Worker, next)
		if claimErr != nil {
			err = claimErr
			break
		}
		if send {
			err = s.deliverCommand(ctx, command, func(commandID string) error {
				return s.Runtime.Resume(ctx, commandID, details.Worker, turn, next, turn.Input)
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
		command, send, err := s.claimCommand(ctx, "cancel", "attempt", details.Worker, *attempt)
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
