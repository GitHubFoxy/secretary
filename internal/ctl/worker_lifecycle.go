package ctl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
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
	Store               *core.Store
	PersonID            string
	Capability          string
	WorkerPolicy        core.HarnessPolicy
	WorkerPolicySource  func() core.HarnessPolicy
	WorkerProfileSource func() (node.ManagedProfile, error)
	Runtime             WorkerRuntime
	commandNow          func() time.Time
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
	Intent          string            `json:"intent"`
	Preferences     WorkerPreferences `json:"preferences"`
	SecretaryTurnID string            `json:"-"`
	InputID         string            `json:"-"`
	IdempotencyKey  string            `json:"idempotency_key,omitempty"`
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
	WorkerRef       string `json:"worker_ref"`
	Text            string `json:"text"`
	SecretaryTurnID string `json:"-"`
	InputID         string `json:"-"`
	RequestID       string `json:"request_id,omitempty"`
	ClientID        string `json:"client_id,omitempty"`
	IdempotencyKey  string `json:"idempotency_key,omitempty"`
}

type SecretaryReplyRequest struct {
	SecretaryTurnID string `json:"secretary_turn_id"`
	InputID         string `json:"input_id"`
	Text            string `json:"text"`
}

// RespondWorker is the single server-side entry point for permission and input
// responses. It intentionally delegates to the same generic Node command path
// as Worker messages.
func (s WorkerService) RespondWorker(ctx context.Context, request MessageWorkerRequest) (core.WorkerDetails, error) {
	return s.MessageWorker(ctx, request)
}

// RetryApprovalResolution is an explicit owner action. It never accepts a
// replacement response or command identity from the caller; it reuses the
// saved decision and exact respond command for the original immutable Attempt.
func (s WorkerService) RetryApprovalResolution(ctx context.Context, requestID, clientID string) (core.WorkerDetails, error) {
	conversation, err := s.authorize(ctx)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	approval, err := s.Store.Approval(ctx, requestID)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	worker, err := s.Store.Worker(ctx, approval.WorkerID)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	details, err := s.Store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	listed := false
	for _, item := range details.Approvals {
		if item.ID == approval.ID {
			listed = true
			break
		}
	}
	if !listed {
		return core.WorkerDetails{}, core.ErrNotFound
	}
	if approval.State != core.ApprovalResolving {
		if approval.State != core.ApprovalPending {
			return details, nil
		}
		return core.WorkerDetails{}, core.ErrInvalidTransition
	}
	if approval.ResolutionCommandID == "" || (approval.ResolutionState != core.ApprovalApproved && approval.ResolutionState != core.ApprovalDenied) {
		return core.WorkerDetails{}, core.ErrInvalidTransition
	}
	attempt, err := s.Store.Phase4Attempt(ctx, approval.AttemptID)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	if attempt.WorkerID != approval.WorkerID || attempt.TurnID != approval.TurnID {
		return core.WorkerDetails{}, core.ErrInvalidTransition
	}
	command, found, err := s.Store.FindWorkerCommand(ctx, "respond", "request:"+approval.RequestID, approval.WorkerID, approval.AttemptID)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	if !found || command.ID != approval.ResolutionCommandID || command.Kind != "respond" || command.AttemptID != approval.AttemptID {
		return core.WorkerDetails{}, core.ErrInvalidTransition
	}
	clientID = strings.TrimSpace(clientID)
	if clientID == "" {
		clientID = "client"
	}
	request := MessageWorkerRequest{
		WorkerRef: worker.WorkerRef, RequestID: approval.RequestID,
		Text: approval.ResolutionResponse, ClientID: clientID,
	}
	return s.respondApproval(ctx, conversation.ID, request, details, attempt, approval, approval.ResolutionState, clientID)
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

func (s WorkerService) secretaryOrigin(secretaryTurnID, inputID string) *core.SecretaryOriginIdentity {
	secretaryTurnID, inputID = strings.TrimSpace(secretaryTurnID), strings.TrimSpace(inputID)
	if secretaryTurnID == "" && inputID == "" {
		return nil
	}
	return &core.SecretaryOriginIdentity{PersonID: s.PersonID, Capability: s.Capability, SecretaryTurnID: secretaryTurnID, InputID: inputID}
}

func (s WorkerService) ValidateSecretaryOrigin(ctx context.Context, secretaryTurnID, inputID string) error {
	if _, err := s.authorize(ctx); err != nil {
		return err
	}
	return s.Store.ValidateSecretaryOrigin(ctx, s.PersonID, s.Capability, secretaryTurnID, inputID)
}

func (s WorkerService) LinkSecretaryWorkerTurn(ctx context.Context, secretaryTurnID, inputID, workerTurnID string) (bool, error) {
	if _, err := s.authorize(ctx); err != nil {
		return false, err
	}
	return s.Store.LinkSecretaryWorkerTurn(ctx, s.PersonID, s.Capability, secretaryTurnID, inputID, workerTurnID)
}

func (s WorkerService) ReplyToUser(ctx context.Context, request SecretaryReplyRequest) (core.ConversationEntry, bool, error) {
	if _, err := s.authorize(ctx); err != nil {
		return core.ConversationEntry{}, false, err
	}
	return s.Store.RecordSecretaryReply(ctx, s.PersonID, s.Capability, request.SecretaryTurnID, request.InputID, request.Text)
}

// claimCommand commits the server-side command identity before handoff. The
// same Worker, Attempt and command kind always reuse one ID. A duplicate
// pending command is resent only after core atomically reclaims its expired
// lease. Node command dedupe then protects the side effect of that handoff.
func (s WorkerService) claimCommand(ctx context.Context, kind, dedupeKey string, worker core.Worker, attempt core.Phase4Attempt, origin *core.SecretaryOriginIdentity) (core.WorkerCommand, bool, error) {
	origins := []core.SecretaryOriginIdentity(nil)
	if origin != nil {
		origins = append(origins, *origin)
	}
	command, duplicate, err := s.Store.ClaimWorkerCommand(ctx, kind, dedupeKey, worker.ID, attempt.ID, origins...)
	if err != nil {
		return core.WorkerCommand{}, false, err
	}
	if duplicate && (command.State == core.WorkerCommandPending || command.State == core.WorkerCommandUncertain) {
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
		uncertain := (command.Kind == "respond" || command.Kind == "steer" || command.Kind == "cancel") && (errors.Is(err, node.ErrCommandOutcomeUnknown) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded))
		if uncertain {
			if !errors.Is(err, node.ErrCommandOutcomeUnknown) {
				err = fmt.Errorf("%w: %w", node.ErrCommandOutcomeUnknown, err)
			}
			_, _ = s.Store.MarkWorkerCommandUncertain(context.Background(), command.ID, err.Error())
		} else {
			_, _ = s.Store.MarkWorkerCommandFailed(context.Background(), command.ID, err.Error())
		}
		return err
	}
	_, err := s.Store.MarkWorkerCommandDelivered(ctx, command.ID)
	return err
}

// recoverLifecycleCommand repairs pre-intent committed Attempts, then claims
// the stable command ID. The runtime is required only when a handoff is due.
func (s WorkerService) recoverLifecycleCommand(ctx context.Context, kind string, worker core.Worker, attempt core.Phase4Attempt, send func(string) error, origin *core.SecretaryOriginIdentity) error {
	if _, err := s.Store.EnsureLifecycleCommandIntent(ctx, kind, worker.ID, attempt.ID); err != nil {
		return err
	}
	command, handoff, err := s.claimCommand(ctx, kind, "attempt", worker, attempt, origin)
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
	origin := s.secretaryOrigin(request.SecretaryTurnID, request.InputID)
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
		// Queued spawn acceptance is committed with its original input identity;
		// retries must not reassign that durable action to a later user turn.
		if origin != nil {
			if _, err := s.Store.ValidateSecretaryWorkerCreationOriginIfPresent(ctx, *origin, turn.ID); err != nil {
				return core.WorkerDetails{}, err
			}
		}
		// A new Worker is stored queued until the Node acknowledges the initial
		// dispatch. Its replay snapshot therefore cannot decide whether the
		// durable pending handoff still needs delivery.
		if err := s.recoverLifecycleCommand(ctx, "dispatch", worker, attempt, func(commandID string) error {
			return s.Runtime.Dispatch(ctx, commandID, worker, turn, attempt, resolution)
		}, origin); err != nil {
			return core.WorkerDetails{}, fmt.Errorf("recover dispatch Worker: %w", err)
		}
		details, err := s.Store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
		if err != nil {
			return core.WorkerDetails{}, err
		}
		details.ActionTurnID = turn.ID
		return details, nil
	}
	if s.WorkerPolicySource != nil {
		resolutionRequest.WorkerPolicy = s.WorkerPolicySource()
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
	if s.WorkerProfileSource == nil {
		return core.WorkerDetails{}, ErrWorkerProfileUnavailable
	}
	profile, profileErr := s.WorkerProfileSource()
	if profileErr != nil {
		return core.WorkerDetails{}, ErrWorkerProfileUnavailable
	}
	boundProfile, profileErr := bindManagedWorkerProfile(profile, preview)
	if profileErr != nil {
		return core.WorkerDetails{}, profileErr
	}
	encoded, encodeErr := json.Marshal(boundProfile)
	if encodeErr != nil {
		return core.WorkerDetails{}, ErrWorkerProfileInvalid
	}
	profileSnapshot := string(encoded)
	origins := []core.SecretaryOriginIdentity(nil)
	if origin != nil {
		origins = append(origins, *origin)
	}
	worker, turn, attempt, resolution, err := s.Store.ResolveAndCreateWorkerWithProfileSnapshot(ctx, conversation.ID, request.Intent, resolutionRequest, request.IdempotencyKey, profileSnapshot, origins...)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	if !resolution.Queued {
		if err := s.recoverLifecycleCommand(ctx, "dispatch", worker, attempt, func(commandID string) error {
			return s.Runtime.Dispatch(ctx, commandID, worker, turn, attempt, resolution)
		}, origin); err != nil {
			return core.WorkerDetails{}, fmt.Errorf("dispatch Worker: %w", err)
		}
	}
	details, err := s.Store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	details.ActionTurnID = turn.ID
	return details, nil
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

// ApplyTrustedLocalApproval is the explicit policy-side local handoff. The
// server commits approval only after the typed response reaches the Node
// runtime, so local policy cannot leave the harness waiting forever.
func (s WorkerService) ApplyTrustedLocalApproval(ctx context.Context, requestID string, policy core.TrustedLocalApprovalPolicy) (core.Approval, error) {
	approval, err := s.Store.Approval(ctx, requestID)
	if err != nil {
		return core.Approval{}, err
	}
	if (approval.State != core.ApprovalPending && approval.State != core.ApprovalResolving) || !policy.Enabled || !policy.Explicit {
		return approval, nil
	}
	if approval.State == core.ApprovalResolving && (approval.ResolutionState != core.ApprovalApproved || approval.ResolutionResponse != "auto_approved") {
		return approval, core.ErrApprovalResolutionConflict
	}
	if !policy.LocalNode || strings.TrimSpace(string(policy.Node)) == "" || string(policy.Node) != approval.NodeID {
		return approval, core.ErrTrustedLocalApprovalDenied
	}
	if approval.Kind != core.ApprovalPermission {
		return approval, errors.New("worker: trusted-local policy only resolves permission requests")
	}
	worker, err := s.Store.Worker(ctx, approval.WorkerID)
	if err != nil {
		return core.Approval{}, err
	}
	attempt, err := s.Store.Phase4Attempt(ctx, approval.AttemptID)
	if err != nil {
		return core.Approval{}, err
	}
	command, send, err := s.claimCommand(ctx, "respond", "request:"+approval.RequestID, worker, attempt, nil)
	if err != nil {
		return core.Approval{}, err
	}
	intent, _, err := s.Store.BeginApprovalResolution(ctx, approval.RequestID, command.ID, core.ApprovalApproved, "trusted-local-policy", "auto_approved")
	if err != nil {
		return core.Approval{}, err
	}
	if intent.State != core.ApprovalResolving {
		return intent, nil
	}
	if send {
		if err := s.requireRuntime(); err != nil {
			return core.Approval{}, err
		}
		if err := s.deliverCommand(ctx, command, func(commandID string) error {
			return s.Runtime.Respond(ctx, commandID, worker, attempt, approval.RequestID, "approved")
		}); err != nil {
			return core.Approval{}, err
		}
		command.State = core.WorkerCommandDelivered
	}
	if command.State != core.WorkerCommandDelivered {
		return core.Approval{}, ErrWorkerCommandPending
	}
	resolved, _, err := s.Store.CommitApprovalResolution(ctx, approval.RequestID, core.ApprovalApproved, "trusted-local-policy", "auto_approved")
	if err != nil {
		return core.Approval{}, err
	}
	_, err = s.Store.RecordEventWithMetadata(ctx, core.EventInput{Kind: "approval.auto_approved", AggregateType: "approval", AggregateID: resolved.ID, Source: "policy", CorrelationID: resolved.TurnID, AttemptID: resolved.AttemptID, Payload: map[string]any{"request_id": resolved.RequestID, "node_id": resolved.NodeID, "policy": "trusted_local_explicit"}})
	return resolved, err
}

func (s WorkerService) respondApproval(ctx context.Context, conversationID string, request MessageWorkerRequest, details core.WorkerDetails, attempt core.Phase4Attempt, approval core.Approval, state core.ApprovalState, clientID string) (core.WorkerDetails, error) {
	var origin *core.SecretaryOriginIdentity
	if approval.State == core.ApprovalPending {
		origin = s.secretaryOrigin(request.SecretaryTurnID, request.InputID)
	}
	command, send, err := s.claimCommand(ctx, "respond", "request:"+request.RequestID, details.Worker, attempt, origin)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	intent, _, err := s.Store.BeginApprovalResolution(ctx, approval.RequestID, command.ID, state, clientID, request.Text)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	if intent.State != core.ApprovalResolving {
		return s.Store.WorkerDetailsForConversation(ctx, conversationID, request.WorkerRef)
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
	if request.Text == "/q" || strings.HasPrefix(request.Text, "/q ") || strings.HasPrefix(request.Text, "/q\n") || strings.HasPrefix(request.Text, "/q\t") {
		text := strings.TrimSpace(strings.TrimPrefix(request.Text, "/q"))
		if text == "" {
			return core.WorkerDetails{}, errors.New("worker: /q requires message text")
		}
		key := strings.TrimSpace(request.IdempotencyKey)
		if key == "" {
			key = request.InputID
		}
		if key == "" {
			key, err = steeringDedupeKey("")
			if err != nil {
				return core.WorkerDetails{}, err
			}
		}
		origins := []core.SecretaryOriginIdentity(nil)
		if origin := s.secretaryOrigin(request.SecretaryTurnID, request.InputID); origin != nil {
			origins = append(origins, *origin)
		}
		message, err := s.Store.EnqueueWorkerMessage(ctx, details.Worker.ID, text, key, origins...)
		if err != nil {
			return core.WorkerDetails{}, err
		}
		details, err = s.Store.WorkerDetailsForConversation(ctx, conversation.ID, request.WorkerRef)
		details.ActionMode = "queued"
		details.ActionMessageID = message.ID
		return details, err
	}
	attempt := details.CurrentAttempt()
	var actionTurnID string
	if strings.TrimSpace(request.RequestID) != "" {
		approval, approvalErr := s.Store.Approval(ctx, request.RequestID)
		if approvalErr == nil {
			if approval.WorkerID != details.Worker.ID {
				return core.WorkerDetails{}, core.ErrInvalidTransition
			}
			if approval.State == core.ApprovalPending || approval.State == core.ApprovalResolving {
				if approval.State == core.ApprovalPending && approval.ExpiresAt != nil && !s.workerCommandNow().Before(*approval.ExpiresAt) {
					if _, _, err := s.Store.ExpireApproval(ctx, request.RequestID, s.workerCommandNow()); err != nil {
						return core.WorkerDetails{}, err
					}
					return s.Store.WorkerDetailsForConversation(ctx, conversation.ID, request.WorkerRef)
				}
				state, stateErr := approvalResponseState(approval.Kind, request.Text)
				if stateErr != nil {
					return core.WorkerDetails{}, stateErr
				}
				if approval.State == core.ApprovalResolving && (approval.ResolutionState != state || approval.ResolutionResponse != request.Text) {
					return core.WorkerDetails{}, core.ErrApprovalResolutionConflict
				}
				approvalAttempt, attemptErr := s.Store.Phase4Attempt(ctx, approval.AttemptID)
				if attemptErr != nil || approvalAttempt.WorkerID != details.Worker.ID || approvalAttempt.TurnID != approval.TurnID {
					if attemptErr != nil {
						return core.WorkerDetails{}, attemptErr
					}
					return core.WorkerDetails{}, core.ErrInvalidTransition
				}
				clientID := strings.TrimSpace(request.ClientID)
				if clientID == "" {
					clientID = "client"
				}
				responded, err := s.respondApproval(ctx, conversation.ID, request, details, approvalAttempt, approval, state, clientID)
				if err == nil {
					responded.ActionTurnID = approval.TurnID
				}
				return responded, err
			}
			// A terminal Approval is authoritative. In particular, denied,
			// expired and revoked requests never trigger another machine action.
			return s.Store.WorkerDetailsForConversation(ctx, conversation.ID, request.WorkerRef)
		}
		if !errors.Is(approvalErr, core.ErrNotFound) {
			return core.WorkerDetails{}, approvalErr
		}
		if attempt != nil {
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
				updated, err := s.Store.WorkerDetailsForConversation(ctx, conversation.ID, request.WorkerRef)
				if err != nil {
					return core.WorkerDetails{}, err
				}
				updated.ActionTurnID = attempt.TurnID
				return updated, nil
			}
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
		actionTurnID = attempt.TurnID
		dedupeKey, err := steeringDedupeKey(request.IdempotencyKey)
		if err != nil {
			return core.WorkerDetails{}, err
		}
		command, send, err := s.claimCommand(ctx, "steering", dedupeKey, details.Worker, *attempt, s.secretaryOrigin(request.SecretaryTurnID, request.InputID))
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
		actionTurnID = attempt.TurnID
		command, send, claimErr := s.claimCommand(ctx, "respond", commandDedupeKey(request.IdempotencyKey, "request:"+request.RequestID), details.Worker, *attempt, s.secretaryOrigin(request.SecretaryTurnID, request.InputID))
		if claimErr != nil {
			return core.WorkerDetails{}, claimErr
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
		actionTurnID = turn.ID
		binding, bindingErr := s.Store.ResolveWorkerBinding(ctx, details.Worker.ID)
		if bindingErr != nil {
			err = bindingErr
			break
		}
		command, send, claimErr := s.claimCommand(ctx, "dispatch", "attempt", details.Worker, next, s.secretaryOrigin(request.SecretaryTurnID, request.InputID))
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
		actionTurnID = turn.ID
		kind := "dispatch"
		if _, found, findErr := s.Store.FindWorkerCommand(ctx, "resume", "attempt", details.Worker.ID, next.ID); findErr != nil {
			err = findErr
			break
		} else if found {
			kind = "resume"
		}
		command, send, claimErr := s.claimCommand(ctx, kind, "attempt", details.Worker, next, s.secretaryOrigin(request.SecretaryTurnID, request.InputID))
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
		actionTurnID = turn.ID
		command, send, claimErr := s.claimCommand(ctx, "resume", "attempt", details.Worker, next, s.secretaryOrigin(request.SecretaryTurnID, request.InputID))
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
	updated, err := s.Store.WorkerDetailsForConversation(ctx, conversation.ID, request.WorkerRef)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	updated.ActionTurnID = actionTurnID
	return updated, nil
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
		canceledBeforeHandoff := false
		for _, kind := range []string{"dispatch", "resume"} {
			command, found, err := s.Store.FindWorkerCommand(ctx, kind, "attempt", details.Worker.ID, attempt.ID)
			if err != nil {
				return core.WorkerDetails{}, err
			}
			canceledBeforeHandoff = canceledBeforeHandoff || (found && command.State == core.WorkerCommandFailed && command.LastError == "canceled_before_handoff" && command.LeaseUntil.IsZero())
		}
		if !canceledBeforeHandoff {
			if err := s.requireRuntime(); err != nil {
				return core.WorkerDetails{}, err
			}
			command, send, err := s.claimCommand(ctx, "cancel", "attempt", details.Worker, *attempt, nil)
			if err != nil {
				return core.WorkerDetails{}, err
			}
			if send {
				if err := s.deliverCommand(ctx, command, func(commandID string) error { return s.Runtime.Cancel(ctx, commandID, details.Worker, *attempt) }); err != nil {
					return core.WorkerDetails{}, err
				}
			}
		}
		if canceledBeforeHandoff {
			if _, _, _, err := s.Store.RecordAttemptOutcome(ctx, attempt.ID, core.AttemptOutcomeInput{Status: core.OutcomeCanceled, Classification: core.OutcomeFinal, ErrorCode: "canceled_before_handoff", FailureCode: "canceled_before_handoff", Summary: "Canceled before runtime handoff"}); err != nil {
				return core.WorkerDetails{}, err
			}
		}
	}
	return s.Store.WorkerDetailsForConversation(ctx, conversation.ID, workerRef)
}

func (s WorkerService) CloseWorker(ctx context.Context, workerRef string) (core.WorkerDetails, error) {
	conversation, err := s.authorize(ctx)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	closing, err := s.Store.WorkerDetailsForConversation(ctx, conversation.ID, workerRef)
	if err != nil {
		return core.WorkerDetails{}, err
	}
	var result core.WorkerDetails
	err = s.Store.WithWorkerLifecycle(ctx, closing.Worker.ID, func(ctx context.Context) error {
		var err error
		if err := s.Store.CancelQueuedWorkerMessages(ctx, closing.Worker.ID); err != nil {
			return err
		}
		if _, err := s.CancelWorker(ctx, workerRef); err != nil {
			return err
		}
		for {
			waiting, err := s.Store.WorkerDetailsForConversation(ctx, conversation.ID, workerRef)
			if err != nil {
				return err
			}
			if attempt := waiting.CurrentAttempt(); attempt == nil || attempt.State.Terminal() {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(10 * time.Millisecond):
			}
		}
		details, err := s.Store.WorkerDetailsForConversation(ctx, conversation.ID, workerRef)
		if err != nil {
			return err
		}
		if _, err := s.Store.CloseWorker(ctx, details.Worker.ID); err != nil {
			return err
		}
		result, err = s.Store.WorkerDetailsForConversation(ctx, conversation.ID, workerRef)
		return err
	})
	return result, err
}
