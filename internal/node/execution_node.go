package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

var ErrRuntimeSessionUnavailable = errors.New("runtime_session_unavailable")

type Responder interface {
	Respond(context.Context, string, string) error
}

// RequestRebinder lets a resumed Node-local session reuse durable request IDs
// that were created before the transport or process reconnect.
type RequestRebinder interface {
	RebindRequests([]string)
}

type ExecutionNode struct {
	node              core.NodeReference
	runtime           Runtime
	store             *LocalStore
	mu                sync.Mutex
	sessions          map[string]Session
	activitySequences map[string]uint64
	inventory         core.HarnessInventorySnapshot
}

func NewExecutionNode(node core.NodeReference, runtime Runtime, store *LocalStore) *ExecutionNode {
	return &ExecutionNode{node: node, runtime: runtime, store: store, sessions: map[string]Session{}, activitySequences: map[string]uint64{}}
}

func (n *ExecutionNode) SetInventory(inventory core.HarnessInventorySnapshot) error {
	if inventory.Node != n.node {
		return errors.New("node: inventory belongs to another Node")
	}
	if err := inventory.Validate(); err != nil {
		return err
	}
	n.mu.Lock()
	n.inventory = inventory
	n.mu.Unlock()
	return nil
}

func (n *ExecutionNode) HandleCommand(ctx context.Context, command Command) (CommandOutcome, error) {
	if err := command.Validate(n.node); err != nil {
		return CommandOutcome{}, err
	}
	record, duplicate, err := n.store.ClaimCommand(command)
	if err != nil {
		return CommandOutcome{}, err
	}
	if duplicate {
		if record.State == CommandProcessing {
			return failedOutcome(command, "execution_state_unknown", "command execution is already in progress"), nil
		}
		return record.Outcome, nil
	}
	var outcome CommandOutcome
	switch command.Kind {
	case CommandDispatch:
		outcome = n.dispatch(ctx, command.Dispatch)
	case CommandCancel:
		outcome = n.cancel(ctx, command.Cancel)
	case CommandSteering:
		outcome = n.steer(ctx, command.Steering)
	case CommandResume:
		outcome = n.resume(ctx, command.Resume)
	case CommandRespondWorker:
		outcome = n.respond(ctx, command.RespondWorker)
	default:
		outcome = failedOutcome(command, "unknown_command", "unknown command")
	}
	stored, err := n.store.CompleteCommand(command.Metadata().CommandID, outcome)
	if err != nil {
		return CommandOutcome{}, err
	}
	return stored.Outcome, nil
}

func (n *ExecutionNode) dispatch(ctx context.Context, command *DispatchCommand) CommandOutcome {
	n.mu.Lock()
	inventory := n.inventory
	n.mu.Unlock()
	if inventory.Node != "" {
		if err := command.Envelope.ValidateAgainstInventory(n.node, inventory); err != nil {
			return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "harness_unavailable", err.Error())
		}
	}
	workspace := command.Envelope.Workspace
	if command.Envelope.ProjectID != "" {
		canonicalWorkspace, err := validateProjectWorkspaceOnNode(command.Envelope)
		if err != nil {
			code := "workspace_forbidden"
			if errors.Is(err, core.ErrWorkspaceMissing) {
				code = "workspace_missing"
			} else if errors.Is(err, core.ErrProjectPolicyDenied) {
				code = "project_policy_denied"
			} else if errors.Is(err, core.ErrProjectMappingMissing) {
				code = "project_mapping_missing"
			}
			return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, code, err.Error())
		}
		workspace = canonicalWorkspace
	} else if workspace == "" {

		var err error
		workspace, err = os.MkdirTemp("", "secretary-worker-")
		if err != nil {
			return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "workspace_failed", err.Error())
		}
	}
	request := StartRequest{WorkerRef: command.Envelope.WorkerRef, Task: command.Envelope.OriginalUserIntent, Workspace: workspace, Profile: command.Envelope.Profile, HarnessInstance: command.Envelope.HarnessInstance, Model: command.Envelope.Model, Reasoning: command.Envelope.Reasoning, ApprovalPolicy: command.Envelope.ApprovalPolicy}
	profile, err := request.effectiveProfile()
	if err != nil {
		return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "binding_conflict", err.Error())
	}
	request.Profile = profile
	session, err := n.runtime.Start(ctx, request)
	if err != nil {
		return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "dispatch_failed", err.Error())
	}
	mapping := sessionMapping{WorkerRef: command.Envelope.WorkerRef, TurnID: command.Envelope.TurnID, AttemptID: command.Envelope.AttemptID, HarnessInstanceID: command.Envelope.HarnessInstance.ID, Workspace: workspace, RuntimeSessionID: session.ID()}
	if err := n.store.SaveSessionMapping(mapping); err != nil {
		_ = session.Close()
		return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "mapping_persist_failed", err.Error())
	}
	n.registerSession(mapping.AttemptID, session)
	n.watchSession(session, command.Envelope)
	return acceptedOutcome(Command{Kind: CommandDispatch, Dispatch: command})
}

func validateProjectWorkspaceOnNode(envelope WorkerEnvelope) (string, error) {
	if err := envelope.ProjectSnapshot.Validate(); err != nil {
		return "", err
	}
	workspace := envelope.Workspace
	if workspace != envelope.ProjectSnapshot.Workspace {
		return "", fmt.Errorf("%w: envelope workspace differs from Project snapshot", core.ErrWorkspaceOutsideRoot)
	}
	if strings.TrimSpace(workspace) == "" {
		return "", core.ErrWorkspaceMissing
	}
	info, err := os.Stat(workspace)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w: %s", core.ErrWorkspaceMissing, workspace)
	}
	if err != nil {
		return "", fmt.Errorf("workspace stat: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%w: workspace is not a directory", core.ErrWorkspaceOutsideRoot)
	}
	mapping, ok := envelope.ProjectSnapshotPathMapping()
	if !ok {
		return "", core.ErrProjectMappingMissing
	}
	rootInfo, err := os.Stat(mapping.Path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w: Project root %s", core.ErrWorkspaceMissing, mapping.Path)
	}
	if err != nil {
		return "", fmt.Errorf("Project root stat: %w", err)
	}
	if !rootInfo.IsDir() {
		return "", fmt.Errorf("%w: Project root is not a directory", core.ErrWorkspaceOutsideRoot)
	}
	resolvedRoot, err := filepath.EvalSymlinks(mapping.Path)
	if err != nil {
		return "", fmt.Errorf("Project root: %w", err)
	}
	resolvedPolicyRoot := resolvedRoot
	if policyRoot := envelope.ProjectSnapshot.Policy.EffectiveExecution().WorkspaceRoot; policyRoot != "" {
		resolvedPolicyRoot, err = filepath.EvalSymlinks(policyRoot)
		if err != nil {
			return "", fmt.Errorf("Project policy root: %w", err)
		}
		if !pathWithin(resolvedRoot, resolvedPolicyRoot) {
			return "", core.ErrWorkspaceOutsideRoot
		}
	}
	resolvedWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", fmt.Errorf("workspace: %w", err)
	}
	if !pathWithin(resolvedPolicyRoot, resolvedWorkspace) {
		return "", core.ErrWorkspaceOutsideRoot
	}
	return filepath.Clean(resolvedWorkspace), nil
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)))
}

func (w WorkerEnvelope) ProjectSnapshotPathMapping() (core.ProjectPathMapping, bool) {
	for _, mapping := range w.ProjectSnapshot.Mappings {
		if mapping.Node == w.ProjectSnapshot.Node || mapping.NodeID == string(w.ProjectSnapshot.Node) {
			return mapping, true
		}
	}
	return core.ProjectPathMapping{}, false
}

func (n *ExecutionNode) cancel(ctx context.Context, command *CancelCommand) CommandOutcome {
	session, err := n.sessionForCommand(ctx, command.Metadata)
	if err != nil {
		return failedOutcome(Command{Kind: CommandCancel, Cancel: command}, "runtime_session_unavailable", err.Error())
	}
	if err := session.Cancel(ctx); err != nil {
		return failedOutcome(Command{Kind: CommandCancel, Cancel: command}, "cancel_failed", err.Error())
	}
	return acceptedOutcome(Command{Kind: CommandCancel, Cancel: command})
}

func (n *ExecutionNode) steer(ctx context.Context, command *SteeringCommand) CommandOutcome {
	session, err := n.sessionForCommand(ctx, command.Metadata)
	if err != nil {
		return failedOutcome(Command{Kind: CommandSteering, Steering: command}, "runtime_session_unavailable", err.Error())
	}
	injected, err := session.Steer(ctx, command.Text)
	if err != nil {
		code := "steering_failed"
		if errors.Is(err, ErrClaudeCodeSteeringUnsupported) {
			code = "runtime_not_steerable"
		}
		return failedOutcome(Command{Kind: CommandSteering, Steering: command}, code, err.Error())
	}
	if !injected {
		queue, ok := session.(Queueer)
		if !ok {
			return failedOutcome(Command{Kind: CommandSteering, Steering: command}, "runtime_not_steerable", "runtime did not accept steering")
		}
		if err := queue.Queue(ctx, command.Text); err != nil {
			return failedOutcome(Command{Kind: CommandSteering, Steering: command}, "queue_failed", err.Error())
		}
	}
	return acceptedOutcome(Command{Kind: CommandSteering, Steering: command})
}

func (n *ExecutionNode) resume(ctx context.Context, command *ResumeCommand) CommandOutcome {
	mapping, ok := n.store.SessionMapping(command.Metadata.AttemptID)
	if !ok || strings.TrimSpace(mapping.RuntimeSessionID) == "" {
		return failedOutcome(Command{Kind: CommandResume, Resume: command}, "runtime_session_unavailable", ErrRuntimeSessionUnavailable.Error())
	}
	if _, ok := n.session(command.Metadata.AttemptID); ok {
		return acceptedOutcome(Command{Kind: CommandResume, Resume: command})
	}
	resumer, ok := n.runtime.(Resumer)
	if !ok {
		return failedOutcome(Command{Kind: CommandResume, Resume: command}, "runtime_session_unavailable", ErrRuntimeSessionUnavailable.Error())
	}
	workspace := mapping.Workspace
	if command.Envelope.ProjectID != "" {
		canonicalWorkspace, err := validateProjectWorkspaceOnNode(command.Envelope)
		if err != nil {
			return failedOutcome(Command{Kind: CommandResume, Resume: command}, "workspace_forbidden", err.Error())
		}
		workspace = canonicalWorkspace
	}
	request := StartRequest{WorkerRef: command.Envelope.WorkerRef, Task: command.Envelope.OriginalUserIntent, Workspace: workspace, Profile: command.Envelope.Profile, HarnessInstance: command.Envelope.HarnessInstance, Model: command.Envelope.Model, Reasoning: command.Envelope.Reasoning, ApprovalPolicy: command.Envelope.ApprovalPolicy, PendingRequestIDs: n.store.PendingRequestIDs(command.Metadata.AttemptID)}
	profile, err := request.effectiveProfile()
	if err != nil {
		return failedOutcome(Command{Kind: CommandResume, Resume: command}, "binding_conflict", err.Error())
	}
	request.Profile = profile
	session, err := resumer.Resume(ctx, request, mapping.RuntimeSessionID)
	if err != nil {
		return failedOutcome(Command{Kind: CommandResume, Resume: command}, "runtime_session_unavailable", err.Error())
	}
	n.rebindPendingRequests(command.Metadata.AttemptID, session)
	n.registerSession(command.Metadata.AttemptID, session)
	n.watchSession(session, command.Envelope)
	return acceptedOutcome(Command{Kind: CommandResume, Resume: command})
}

func (n *ExecutionNode) respond(ctx context.Context, command *RespondWorkerCommand) CommandOutcome {
	storedResponse, duplicate, err := n.store.ClaimWorkerResponse(command.RequestID)
	if err != nil {
		return failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "response_claim_failed", err.Error())
	}
	if duplicate {
		if storedResponse.State == CommandProcessing {
			return failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "execution_state_unknown", "worker response execution state is unknown")
		}
		storedResponse.CommandID = command.Metadata.CommandID
		storedResponse.Kind = CommandRespondWorker
		return storedResponse
	}
	session, err := n.sessionForCommand(ctx, command.Metadata)
	if err != nil {
		outcome := failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "runtime_session_unavailable", err.Error())
		_ = n.store.CompleteWorkerResponse(command.RequestID, outcome)
		return outcome
	}
	responder, ok := session.(Responder)
	if !ok {
		outcome := failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "runtime_does_not_accept_response", "runtime does not accept worker responses")
		_ = n.store.CompleteWorkerResponse(command.RequestID, outcome)
		return outcome
	}
	if err := responder.Respond(ctx, command.RequestID, command.Response); err != nil {
		outcome := failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "response_failed", err.Error())
		_ = n.store.CompleteWorkerResponse(command.RequestID, outcome)
		return outcome
	}
	outcome := acceptedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command})
	if err := n.store.CompleteWorkerResponse(command.RequestID, outcome); err != nil {
		return failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "response_record_failed", err.Error())
	}
	if err := n.store.ClearPendingRequest(command.RequestID); err != nil {
		return failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "response_record_failed", err.Error())
	}
	return outcome
}

func (n *ExecutionNode) sessionForCommand(ctx context.Context, metadata core.CommandMetadata) (Session, error) {
	if session, ok := n.session(metadata.AttemptID); ok {
		return session, nil
	}
	mapping, ok := n.store.SessionMapping(metadata.AttemptID)
	if !ok || mapping.RuntimeSessionID == "" {
		return nil, ErrRuntimeSessionUnavailable
	}
	record, err := n.store.CommandForAttempt(metadata.AttemptID)
	if err != nil {
		return nil, ErrRuntimeSessionUnavailable
	}
	command, err := commandFromJSON(record.CommandJSON)
	if err != nil {
		return nil, ErrRuntimeSessionUnavailable
	}
	var envelope WorkerEnvelope
	switch command.Kind {
	case CommandDispatch:
		if command.Dispatch == nil {
			return nil, ErrRuntimeSessionUnavailable
		}
		envelope = command.Dispatch.Envelope
	case CommandResume:
		if command.Resume == nil {
			return nil, ErrRuntimeSessionUnavailable
		}
		envelope = command.Resume.Envelope
	default:
		return nil, ErrRuntimeSessionUnavailable
	}
	if envelope.AttemptID != metadata.AttemptID || envelope.WorkerRef != mapping.WorkerRef || envelope.TurnID != mapping.TurnID || envelope.HarnessInstance.ID != mapping.HarnessInstanceID {
		return nil, ErrRuntimeSessionUnavailable
	}
	resumer, ok := n.runtime.(Resumer)
	if !ok {
		return nil, ErrRuntimeSessionUnavailable
	}
	request := StartRequest{WorkerRef: envelope.WorkerRef, Task: envelope.OriginalUserIntent, Workspace: mapping.Workspace, Profile: envelope.Profile, HarnessInstance: envelope.HarnessInstance, Model: envelope.Model, Reasoning: envelope.Reasoning, ApprovalPolicy: envelope.ApprovalPolicy, PendingRequestIDs: n.store.PendingRequestIDs(metadata.AttemptID)}
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, ErrRuntimeSessionUnavailable
	}
	request.Profile = profile
	session, err := resumer.Resume(ctx, request, mapping.RuntimeSessionID)
	if err != nil {
		return nil, ErrRuntimeSessionUnavailable
	}
	n.rebindPendingRequests(metadata.AttemptID, session)
	n.registerSession(metadata.AttemptID, session)
	n.watchSession(session, envelope)
	return session, nil
}

func (n *ExecutionNode) Restore(ctx context.Context) error {
	return n.store.RecoverRunning(ctx, n)
}

func (n *ExecutionNode) Inspect(ctx context.Context, record CommandRecord) (bool, error) {
	command, err := commandFromJSON(record.CommandJSON)
	if err != nil {
		return false, nil
	}
	// Session resume is not evidence that a native Respond completed. The
	// command must go through normal retry/re-dispatch with the same IDs.
	if command.Kind == CommandRespondWorker {
		return false, nil
	}
	metadata := command.Metadata()
	mapping, ok := n.store.SessionMapping(metadata.AttemptID)
	if !ok || mapping.RuntimeSessionID == "" {
		return false, nil
	}
	if _, err := n.sessionForCommand(ctx, metadata); err != nil {
		return false, nil
	}
	if _, err := n.store.CompleteCommand(record.CommandID, acceptedOutcome(command)); err != nil {
		return false, err
	}
	return true, nil
}

func (n *ExecutionNode) registerSession(attemptID string, session Session) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sessions[attemptID] = session
}
func (n *ExecutionNode) session(attemptID string) (Session, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	s, ok := n.sessions[attemptID]
	return s, ok
}

func (n *ExecutionNode) ActiveAttempts() []ActiveAttempt {
	n.mu.Lock()
	defer n.mu.Unlock()
	attempts := make([]ActiveAttempt, 0, len(n.sessions))
	for attemptID := range n.sessions {
		mapping, ok := n.store.SessionMapping(attemptID)
		if !ok {
			continue
		}
		attempts = append(attempts, ActiveAttempt{WorkerRef: mapping.WorkerRef, TurnID: mapping.TurnID, AttemptID: mapping.AttemptID})
	}
	return attempts
}

func (n *ExecutionNode) removeSession(attemptID string) {
	n.mu.Lock()
	delete(n.sessions, attemptID)
	n.mu.Unlock()
}

func (n *ExecutionNode) watchSession(session Session, envelope WorkerEnvelope) {
	go func() {
		activity := session.Activity()
		results := session.Result()
		for activity != nil || results != nil {
			select {
			case item, ok := <-activity:
				if !ok {
					activity = nil
					continue
				}
				n.publishRuntimeActivity(envelope, item)
			case result, ok := <-results:
				if !ok {
					results = nil
					continue
				}
				n.removeSession(envelope.AttemptID)
				status := core.OutcomeFailed
				if result.Status == "succeeded" {
					status = core.OutcomeSucceeded
				} else if result.Status == "canceled" || result.Status == "cancelled" {
					status = core.OutcomeCanceled
				}
				classification := core.OutcomeFinal
				outcome := core.AttemptOutcomeEnvelope{EventID: "attempt-outcome-" + envelope.AttemptID, Node: envelope.HarnessInstance.Node, HarnessInstanceID: envelope.HarnessInstance.ID, WorkerRef: envelope.WorkerRef, TurnID: envelope.TurnID, AttemptID: envelope.AttemptID, Status: status, Classification: classification, Summary: result.Summary, ErrorCode: result.Status, OccurredAt: time.Now().UTC()}
				if strings.TrimSpace(outcome.Summary) == "" {
					outcome.Summary = result.Status
				}
				_, _ = n.store.QueueOutcome(outcome)
			}
		}
	}()
}

func (n *ExecutionNode) publishRuntimeActivity(envelope WorkerEnvelope, item Activity) {
	activity, ok := normalizeRuntimeActivity(item, n.nextMetadata(envelope), envelope.HarnessInstance.Capabilities)
	if !ok {
		return
	}
	if err := activity.ValidateFor(envelope.HarnessInstance); err != nil {
		return
	}
	if activity.Kind == core.ActivityPermissionRequest || activity.Kind == core.ActivityUserInputRequest {
		if activity.Request == nil || n.store.SavePendingRequest(activity.Request.RequestID, envelope.AttemptID, item.Kind, envelope.HarnessInstance.ID) != nil {
			return
		}
	}
	_, _ = n.store.QueueActivity(activity)
}

func (n *ExecutionNode) rebindPendingRequests(attemptID string, session Session) {
	rebinder, ok := session.(RequestRebinder)
	if !ok {
		return
	}
	rebinder.RebindRequests(n.store.PendingRequestIDs(attemptID))
}

// NormalizeRuntimeActivity is the adapter boundary for normalized activity.
// Unsupported or unknown runtime observations are never synthesized.
func NormalizeRuntimeActivity(item Activity, metadata core.ActivityMetadata, capabilities core.HarnessCapabilities) (core.Activity, bool) {
	return normalizeRuntimeActivity(item, metadata, capabilities)
}

// normalizeRuntimeActivity is intentionally allow-list based. An adapter event
// with no normalized representation is dropped rather than turned into fake
// status or progress.
func normalizeRuntimeActivity(item Activity, metadata core.ActivityMetadata, capabilities core.HarnessCapabilities) (core.Activity, bool) {
	var activity core.Activity
	switch item.Kind {
	case ActivityText:
		if !capabilities.SupportsActivity(core.ActivityAssistantTextDelta) || strings.TrimSpace(item.Text) == "" {
			return core.Activity{}, false
		}
		activity = core.Activity{Metadata: metadata, Kind: core.ActivityAssistantTextDelta, Text: item.Text}
	case ActivityTool:
		if !capabilities.SupportsActivity(core.ActivityToolCall) || strings.TrimSpace(item.Text) == "" {
			return core.Activity{}, false
		}
		activity = core.Activity{Metadata: metadata, Kind: core.ActivityToolCall, ToolCall: &core.ToolCall{Name: item.Text}}
	case ActivityThinkingSummary:
		summary := strings.TrimSpace(item.Summary)
		if summary == "" {
			summary = strings.TrimSpace(item.Text)
		}
		if !capabilities.SupportsActivity(core.ActivityThinkingSummary) || !safeRuntimeSummary(summary) {
			return core.Activity{}, false
		}
		activity = core.Activity{Metadata: metadata, Kind: core.ActivityThinkingSummary, Text: summary}
	case ActivityToolCall:
		tool := strings.TrimSpace(item.Tool)
		if tool == "" {
			tool = strings.TrimSpace(item.Text)
		}
		if !capabilities.SupportsActivity(core.ActivityToolCall) || tool == "" {
			return core.Activity{}, false
		}
		arguments, safe := SanitizeToolArguments(item.Arguments)
		if !safe {
			return core.Activity{}, false
		}
		activity = core.Activity{Metadata: metadata, Kind: core.ActivityToolCall, ToolCall: &core.ToolCall{Name: tool, Arguments: arguments}}
	case ActivityToolResult:
		tool := strings.TrimSpace(item.Tool)
		if tool == "" {
			tool = strings.TrimSpace(item.Text)
		}
		if !capabilities.SupportsActivity(core.ActivityToolResult) || tool == "" || strings.TrimSpace(item.Result) == "" {
			return core.Activity{}, false
		}
		result, safe := SanitizeToolResult(item.Result)
		if !safe {
			return core.Activity{}, false
		}
		errorText, safe := SanitizeToolResult(item.Error)
		if !safe {
			return core.Activity{}, false
		}
		activity = core.Activity{Metadata: metadata, Kind: core.ActivityToolResult, ToolResult: &core.ToolResult{Name: tool, Output: result, Error: errorText}}
	case ActivityStatus:
		if !capabilities.SupportsActivity(core.ActivityStatus) || strings.TrimSpace(item.Text) == "" {
			return core.Activity{}, false
		}
		activity = core.Activity{Metadata: metadata, Kind: core.ActivityStatus, Status: item.Text}
	case ActivityPermission, ActivityUserInput:
		if strings.TrimSpace(item.RequestID) == "" {
			return core.Activity{}, false
		}
		kind := core.ActivityPermissionRequest
		if item.Kind == ActivityUserInput {
			kind = core.ActivityUserInputRequest
		}
		if !capabilities.SupportsActivity(kind) {
			return core.Activity{}, false
		}
		activity = core.Activity{Metadata: metadata, Kind: kind, Request: &core.ActivityRequest{RequestID: item.RequestID, Summary: item.Summary}}
	default:
		return core.Activity{}, false
	}
	return activity, true
}

func safeRuntimeSummary(summary string) bool {
	if summary == "" || len(summary) > 1000 {
		return false
	}
	lower := strings.ToLower(summary)
	for _, marker := range []string{"chain-of-thought", "chain of thought", "raw thought", "internal reasoning", "thought process", "analysis:", "reasoning:", "thought:", "<think>", "</think>"} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func (n *ExecutionNode) nextMetadata(envelope WorkerEnvelope) core.ActivityMetadata {
	n.mu.Lock()
	defer n.mu.Unlock()
	next := n.store.NextEventSequence()
	if localNext := n.activitySequences[envelope.AttemptID] + 1; localNext > next {
		next = localNext
	}
	n.activitySequences[envelope.AttemptID] = next
	return core.ActivityMetadata{EventID: nodeEventID(envelope.AttemptID), Node: envelope.HarnessInstance.Node, HarnessInstanceID: envelope.HarnessInstance.ID, WorkerRef: envelope.WorkerRef, TurnID: envelope.TurnID, AttemptID: envelope.AttemptID, Sequence: next, ObservedAt: time.Now().UTC(), CorrelationID: envelope.TurnID}
}

func acceptedOutcome(command Command) CommandOutcome {
	return CommandOutcome{CommandID: command.Metadata().CommandID, Kind: command.Kind, State: CommandAccepted}
}
func failedOutcome(command Command, code, message string) CommandOutcome {
	return CommandOutcome{CommandID: command.Metadata().CommandID, Kind: command.Kind, State: CommandFailed, ErrorCode: code, ErrorMessage: message}
}
func nodeEventID(attempt string) string {
	return fmt.Sprintf("node-event-%s-%d", attempt, time.Now().UnixNano())
}
