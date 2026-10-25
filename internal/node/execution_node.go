package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/beruseruko/secretary/internal/acp"
	"github.com/beruseruko/secretary/internal/core"
)

var ErrRuntimeSessionUnavailable = errors.New("runtime_session_unavailable")

type Responder interface {
	Respond(context.Context, string, string) error
}

type RequestRebinder interface {
	RebindRequests([]string)
}

type PendingRequestRebinder interface {
	RebindPendingRequests([]PendingRequest)
}

type ExecutionNode struct {
	node                     core.NodeReference
	runtime                  Runtime
	store                    *LocalStore
	mu                       sync.Mutex
	sessions                 map[string]Session
	activitySequences        map[string]uint64
	inventory                core.HarnessInventorySnapshot
	workspaces               map[string]string
	mcpServers               []MCPServer
	artifactFetcher          ArtifactFetcher
	publishBroker            *workerPublishBroker
	artifactPublisherCommand string
	// Native Resume may block after durable handoff. Its identity and
	// cancellation survive WebSocket disconnects, unlike commandCtx.
	nativeResumes map[string]*nativeResumeAttempt
	// nativeLifetime is the Node process lifetime, not an individual
	// WebSocket connection or an individual Dispatch command.
	nativeLifetime context.Context
}

type nativeResumeAttempt struct {
	commandID string
	workerRef string
	cancel    context.CancelFunc
	canceled  bool
}

func NewExecutionNode(node core.NodeReference, runtime Runtime, store *LocalStore) *ExecutionNode {
	return &ExecutionNode{node: node, runtime: runtime, store: store, sessions: map[string]Session{}, nativeResumes: map[string]*nativeResumeAttempt{}, activitySequences: map[string]uint64{}, workspaces: map[string]string{}}
}

// SetNativeLifetime binds accepted native sessions to the Node daemon
// lifetime. Before durable handoff Artifact preparation remains cancellable
// by the connection/command context; after handoff a socket reconnect must
// not cancel nativeexec.CommandContext (Claude/Codex/OpenCode).
func (n *ExecutionNode) SetNativeLifetime(ctx context.Context) {
	n.mu.Lock()
	n.nativeLifetime = ctx
	n.mu.Unlock()
}
func (n *ExecutionNode) nativeStartContext(commandCtx context.Context) context.Context {
	n.mu.Lock()
	lifetime := n.nativeLifetime
	n.mu.Unlock()
	if lifetime != nil {
		return lifetime
	}
	// Preserve existing direct-ExecutionNode callers that own their context.
	return commandCtx
}

func (n *ExecutionNode) SetMCPServers(servers []MCPServer) error {
	if err := validateWorkerMCPServers(servers); err != nil {
		return err
	}
	n.mu.Lock()
	n.mcpServers = cloneMCPServers(servers)
	n.mu.Unlock()
	return nil
}
func (n *ExecutionNode) workerMCPServers() []MCPServer {
	n.mu.Lock()
	defer n.mu.Unlock()
	return cloneMCPServers(n.mcpServers)
}

func (n *ExecutionNode) SetWorkspaces(workspaces []Workspace) error {
	mapped := make(map[string]string, len(workspaces))
	for _, workspace := range workspaces {
		if err := workspace.Validate(); err != nil {
			return err
		}
		if _, exists := mapped[workspace.ProjectID]; exists {
			return fmt.Errorf("node: duplicate workspace mapping for Project %q", workspace.ProjectID)
		}
		mapped[workspace.ProjectID] = filepath.Clean(workspace.Path)
	}
	n.mu.Lock()
	n.workspaces = mapped
	n.mu.Unlock()
	return nil
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
		if code, message, ok := managedProfileFailure(err); ok {
			return failedOutcome(command, code, message), nil
		}
		return CommandOutcome{}, err
	}
	pendingContinuation := command.Kind == CommandDispatch && command.Dispatch.Envelope.PreviousAttemptID != ""
	if command.Kind == CommandResume && command.Resume.Envelope.PreviousAttemptID != "" {
		_, mapped := n.store.SessionMapping(command.Metadata().AttemptID)
		pendingContinuation = !mapped
	}
	record, duplicate, err := n.store.ClaimCommand(command)
	if err != nil {
		return CommandOutcome{}, err
	}
	if duplicate {
		if record.State == CommandProcessing {
			// A re-delivered Resume cannot determine native success. Preserve
			// the original claim until its provider goroutine commits the
			// actual outcome; even the handoff-to-completion transition must
			// not manufacture a terminal receipt.
			if command.Kind == CommandResume && record.Outcome.State == CommandProcessing && !record.Replayable && !record.SafePrestart {
				return CommandOutcome{CommandID: command.Metadata().CommandID, Kind: command.Kind, TurnID: command.Metadata().TurnID, AttemptID: command.Metadata().AttemptID, State: CommandProcessing}, nil
			}
			if (command.Kind == CommandResume || command.Kind == CommandDispatch) && n.nativeResumeRunning(command.Metadata().AttemptID, command.Metadata().WorkerRef, command.Metadata().CommandID) {
				// Same durable command is still running at the provider.
				// This is NOT terminal acceptance: the original execution
				// owns the sole final receipt after Resume returns.
				return CommandOutcome{CommandID: command.Metadata().CommandID, Kind: command.Kind, TurnID: command.Metadata().TurnID, AttemptID: command.Metadata().AttemptID, State: CommandProcessing}, nil
			}
			if record.Outcome.State == CommandAccepted {
				if n.liveContinuationReadiness(command, record) {
					return record.Outcome, nil
				}
				if _, same := sameCommandEnvelope(command, record); same {
					completed, err := n.store.Command(command.Metadata().CommandID)
					if err == nil && completed.State == CommandAccepted && completed.Outcome == record.Outcome {
						return completed.Outcome, nil
					}
				}
				return failedOutcome(command, "runtime_session_unavailable", ErrRuntimeSessionUnavailable.Error()), nil
			}
			return failedOutcome(command, "execution_state_unknown", "command execution is already in progress"), nil
		}
		if record.State == CommandAccepted && !record.TerminalOutcomeQueued && (command.Kind == CommandDispatch || command.Kind == CommandResume) {
			if n.liveContinuationReadiness(command, record) {
				return record.Outcome, nil
			}
			return failedOutcome(command, "runtime_session_unavailable", ErrRuntimeSessionUnavailable.Error()), nil
		}
		return bindCommandOutcomeIdentity(record.Outcome, command), nil
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
	if outcome.State == CommandProcessing && (command.Kind == CommandDispatch || command.Kind == CommandResume) {
		if e := n.store.YieldPrestart(command.Metadata().CommandID); e != nil {
			return CommandOutcome{}, e
		}
		return CommandOutcome{}, ctx.Err()
	}
	if pendingContinuation && outcome.State == CommandAccepted {
		return outcome, nil
	}
	stored, err := n.store.CompleteCommand(command.Metadata().CommandID, outcome)
	if err != nil {
		return CommandOutcome{}, err
	}
	return stored.Outcome, nil
}

// beforeNativeHandoff distinguishes a terminal owner Cancel from a transport
// interruption. The latter is yielded as replayable, not artifact_unavailable.
func (n *ExecutionNode) beforeNativeHandoff(ctx context.Context, command Command) (CommandOutcome, bool) {
	if errors.Is(context.Cause(ctx), errWorkerDispatchCanceled) {
		return failedOutcome(command, "canceled_before_handoff", "Worker canceled before native session start"), true
	}
	if ctx.Err() != nil {
		return CommandOutcome{CommandID: command.Metadata().CommandID, Kind: command.Kind, State: CommandProcessing}, true
	}
	return CommandOutcome{}, false
}

func (n *ExecutionNode) dispatch(ctx context.Context, command *DispatchCommand) CommandOutcome {
	if command.Envelope.ProjectID == "" && command.Envelope.Workspace != "" {
		return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "workspace_forbidden", "projectless Workspace must be Node-managed")
	}
	if _, exists := n.store.SessionMapping(command.Envelope.AttemptID); exists {
		return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "runtime_session_unavailable", ErrRuntimeSessionUnavailable.Error())
	}
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
		if err := n.validateReconciledWorkspace(command.Envelope); err != nil {
			return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "workspace_mapping_mismatch", err.Error())
		}
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
	} else if workspace == "" && command.Envelope.PreviousAttemptID == "" {
		var err error
		// Session mappings survive daemon restarts and OS temp-directory cleanup.
		// Allocate projectless workspaces alongside the durable Node state.
		workspace, err = n.newProjectlessWorkspace()
		if err != nil {
			return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "workspace_failed", err.Error())
		}
	}
	continuing := command.Envelope.PreviousAttemptID != ""
	if continuing && command.Envelope.ProjectID == "" && workspace == "" {
		mapping, err := n.continuationMapping(command.Envelope, command.Envelope.PreviousAttemptID)
		if err != nil {
			return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "runtime_session_unavailable", ErrRuntimeSessionUnavailable.Error())
		}
		workspace = mapping.Workspace
	}
	promptText := command.Envelope.OriginalUserIntent
	if command.Envelope.ContinuationPrompt != "" {
		promptText = command.Envelope.ContinuationPrompt
	}
	task := command.Envelope.OriginalUserIntent
	if continuing {
		task = promptText
	}
	prepared, images, artifactErr := n.prepareArtifactInput(ctx, workspace, task)
	if artifactErr != nil {
		if outcome, ok := n.beforeNativeHandoff(ctx, Command{Kind: CommandDispatch, Dispatch: command}); ok {
			return outcome
		}
		return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "artifact_unavailable", artifactErr.Error())
	}
	if len(images) > 0 && !supportsImageHarness(command.Envelope.HarnessInstance) {
		return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "image_input_unsupported", "selected Worker harness does not advertise image input capability")
	}
	task = prepared
	if continuing {
		promptText = prepared
	}
	servers, publishErr := n.workerArtifactMCPServers(command.Envelope.WorkerRef, command.Envelope.AttemptID, workspace)
	if publishErr != nil {
		return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "artifact_publish_unavailable", publishErr.Error())
	}
	activatedPublisher := false
	defer func() {
		if !activatedPublisher {
			n.unregisterArtifactPublisher(command.Envelope.AttemptID)
		}
	}()
	request := StartRequest{MCPServers: servers, WorkerRef: command.Envelope.WorkerRef, Task: task, Images: images, Workspace: workspace, Profile: command.Envelope.Profile, HarnessInstance: command.Envelope.HarnessInstance, Model: command.Envelope.Model, Reasoning: command.Envelope.Reasoning, ApprovalPolicy: command.Envelope.ApprovalPolicy}
	profile, err := request.effectiveProfile()
	if err != nil {
		return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "binding_conflict", err.Error())
	}
	request.Profile = profile
	if outcome, ok := n.beforeNativeHandoff(ctx, Command{Kind: CommandDispatch, Dispatch: command}); ok {
		return outcome
	}
	if e := n.store.MarkNativeHandoff(ctx, command.Metadata.CommandID); e != nil {
		if outcome, ok := n.beforeNativeHandoff(ctx, Command{Kind: CommandDispatch, Dispatch: command}); ok {
			return outcome
		}
		return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "native_handoff_record_failed", e.Error())
	}
	notifyNativeHandoff(ctx)
	// Irreversible boundary has been persisted. Native session and initial
	// prompt now live across WebSocket reconnections.
	nativeCtx := n.nativeStartContext(ctx)
	var session Session
	if continuing {
		mapping, mappingErr := n.continuationMapping(command.Envelope, command.Envelope.PreviousAttemptID)
		resumer, ok := n.runtime.(Resumer)
		if mappingErr != nil || !ok || workspace != mapping.Workspace {
			return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "runtime_session_unavailable", ErrRuntimeSessionUnavailable.Error())
		}
		request.DeferInitialPrompt = true
		session, err = resumeSameIdentity(nativeCtx, resumer, request, mapping.RuntimeSessionID)
	} else {
		if n.store.hasWorkerMapping(command.Envelope.WorkerRef) {
			return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "runtime_session_unavailable", ErrRuntimeSessionUnavailable.Error())
		}
		session, err = n.runtime.Start(nativeCtx, request)
	}
	if err != nil {
		if code, message, ok := managedProfileFailure(err); ok {
			return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, code, message)
		}
		if continuing {
			return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "runtime_session_unavailable", ErrRuntimeSessionUnavailable.Error())
		}
		return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "dispatch_failed", err.Error())
	}
	mapping := sessionMapping{WorkerRef: command.Envelope.WorkerRef, TurnID: command.Envelope.TurnID, AttemptID: command.Envelope.AttemptID, HarnessInstanceID: command.Envelope.HarnessInstance.ID, Workspace: workspace, RuntimeSessionID: session.ID()}
	if err := n.store.SaveSessionMapping(mapping); err != nil {
		_ = session.Close()
		return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "mapping_persist_failed", err.Error())
	}
	if continuing {
		ready, err := n.store.SaveCommandReadiness(command.Metadata.CommandID, acceptedOutcome(Command{Kind: CommandDispatch, Dispatch: command}))
		if err != nil || ready.State != CommandProcessing || ready.Outcome.State != CommandAccepted {
			_ = session.Close()
			return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "runtime_session_unavailable", ErrRuntimeSessionUnavailable.Error())
		}
	}
	n.registerSession(mapping.AttemptID, session)
	activatedPublisher = true
	if continuing {
		n.watchSession(session, command.Envelope, Command{Kind: CommandDispatch, Dispatch: command})
		go func() { _ = promptWithImages(context.Background(), session, promptText, images) }()
	} else {
		n.watchSession(session, command.Envelope, Command{Kind: CommandDispatch, Dispatch: command})
	}
	return acceptedOutcome(Command{Kind: CommandDispatch, Dispatch: command})
}

// Workspaces are intentionally durable: only a separate explicit retention
// mechanism may remove them after all native session references have expired.
func (n *ExecutionNode) newProjectlessWorkspace() (string, error) {
	if n.store == nil || strings.TrimSpace(n.store.path) == "" {
		return "", errors.New("node: durable state path is required for projectless Worker")
	}
	root, err := filepath.Abs(filepath.Join(filepath.Dir(n.store.path), "workspaces"))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(root, "secretary-worker-")
}

func (n *ExecutionNode) validateReconciledWorkspace(envelope WorkerEnvelope) error {
	if envelope.ProjectID == "" {
		return nil
	}
	n.mu.Lock()
	root, configured := n.workspaces[envelope.ProjectID]
	configuredCount := len(n.workspaces)
	n.mu.Unlock()
	if configuredCount == 0 {
		return nil
	}
	if !configured {
		return fmt.Errorf("%w: Project %q is not configured on Node %s", core.ErrProjectMappingMissing, envelope.ProjectID, n.node)
	}
	mapping, ok := envelope.ProjectSnapshotPathMapping()
	if !ok || filepath.Clean(mapping.Path) != root {
		return fmt.Errorf("%w: Project %q mapping differs from Node configuration", core.ErrNodeWorkspaceMismatch, envelope.ProjectID)
	}
	return nil
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

func managedProfileFailure(err error) (string, string, bool) {
	switch {
	case errors.Is(err, ErrManagedProfileRequired):
		return "profile_missing", "managed Worker Profile is unavailable", true
	case errors.Is(err, ErrManagedProfileInvalid):
		return "profile_invalid", "managed Worker Profile is invalid or inconsistent with its binding", true
	default:
		return "", "", false
	}
}

func (w WorkerEnvelope) ProjectSnapshotPathMapping() (core.ProjectPathMapping, bool) {
	for _, mapping := range w.ProjectSnapshot.Mappings {
		if mapping.Node == w.ProjectSnapshot.Node || mapping.NodeID == string(w.ProjectSnapshot.Node) {
			return mapping, true
		}
	}
	return core.ProjectPathMapping{}, false
}

func (n *ExecutionNode) nativeResumeRunning(attemptID, workerRef, commandID string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	pending := n.nativeResumes[attemptID]
	return pending != nil && pending.workerRef == workerRef && pending.commandID == commandID
}

func (n *ExecutionNode) cancel(ctx context.Context, command *CancelCommand) CommandOutcome {
	// A durable native Resume can be in progress without a Session mapping.
	// Match the exact Attempt and Worker, never a historical native ID.
	n.mu.Lock()
	pending := n.nativeResumes[command.Metadata.AttemptID]
	if pending != nil && pending.workerRef == command.Metadata.WorkerRef {
		if err := n.store.RecordNativeResumeCancel(pending.commandID, command.Metadata); err != nil {
			n.mu.Unlock()
			return failedOutcome(Command{Kind: CommandCancel, Cancel: command}, "cancel_checkpoint_failed", err.Error())
		}
		pending.canceled = true
		pending.cancel()
		n.mu.Unlock()
		return acceptedOutcome(Command{Kind: CommandCancel, Cancel: command})
	}
	n.mu.Unlock()
	session, err := n.sessionForCommand(ctx, command.Metadata)
	if err != nil {
		prestart := n.store.PrestartCanceled(command.Metadata.AttemptID)
		if prestart {
			return acceptedOutcome(Command{Kind: CommandCancel, Cancel: command})
		}
		return failedOutcome(Command{Kind: CommandCancel, Cancel: command}, "runtime_session_unavailable", err.Error())
	}
	if err := session.Cancel(ctx); err != nil {
		code := "cancel_failed"
		if errors.Is(err, os.ErrClosed) || errors.Is(err, io.ErrClosedPipe) {
			// The native process exited between lookup and Cancel. Report its
			// lost session explicitly; do not pretend the interrupt was delivered.
			code = "runtime_session_unavailable"
		}
		return failedOutcome(Command{Kind: CommandCancel, Cancel: command}, code, err.Error())
	}
	return acceptedOutcome(Command{Kind: CommandCancel, Cancel: command})
}

func (n *ExecutionNode) steer(ctx context.Context, command *SteeringCommand) CommandOutcome {
	session, err := n.sessionForCommand(ctx, command.Metadata)
	if err != nil {
		return failedOutcome(Command{Kind: CommandSteering, Steering: command}, "runtime_session_unavailable", err.Error())
	}
	mapping, ok := n.store.SessionMapping(command.Metadata.AttemptID)
	if !ok {
		return failedOutcome(Command{Kind: CommandSteering, Steering: command}, "runtime_session_unavailable", ErrRuntimeSessionUnavailable.Error())
	}
	text, images, err := n.prepareArtifactInput(ctx, mapping.Workspace, command.Text)
	if err != nil {
		// Interrupting media preparation before native steering is definitive:
		// no text or image was injected, so acceptance is NOT unknown.
		if errors.Is(context.Cause(ctx), errWorkerDispatchCanceled) {
			return failedOutcome(Command{Kind: CommandSteering, Steering: command}, "steering_not_delivered", "Owner canceled pending media steering before native injection")
		}
		return failedOutcome(Command{Kind: CommandSteering, Steering: command}, "artifact_unavailable", err.Error())
	}
	if errors.Is(context.Cause(ctx), errWorkerDispatchCanceled) {
		return failedOutcome(Command{Kind: CommandSteering, Steering: command}, "steering_not_delivered", "Owner canceled pending media steering before native injection")
	}
	var injected bool
	if len(images) > 0 {
		if multimodal, ok := session.(ImageSteeringSession); ok {
			injected, err = multimodal.SteerImages(ctx, text, images)
		} else {
			return failedOutcome(Command{Kind: CommandSteering, Steering: command}, "image_input_unsupported", "selected Worker session cannot steer image content")
		}
	} else {
		injected, err = session.Steer(ctx, text)
	}
	if err != nil {
		code := "steering_failed"
		if errors.Is(err, ErrClaudeCodeSteeringUnsupported) {
			code = "runtime_not_steerable"
		}
		outcome := failedOutcome(Command{Kind: CommandSteering, Steering: command}, code, err.Error())
		outcome.AcceptanceUnknown = errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		var steerErr *SteeringError
		if errors.As(err, &steerErr) {
			outcome.FailureCategory = steerErr.Failure.Category
			outcome.ProviderCode = steerErr.Failure.ProviderCode
			outcome.Technical = steerErr.Failure.Technical
			if steerErr.Failure.Message != "" {
				outcome.ErrorMessage = steerErr.Failure.Message
			}
		} else {
			var rpcErr *acp.RPCError
			if errors.As(err, &rpcErr) {
				failure := classifyACPError(rpcErr, "")
				outcome.FailureCategory = failure.Category
				outcome.ProviderCode = failure.ProviderCode
				outcome.Technical = failure.Technical
				if failure.Message != "" {
					outcome.ErrorMessage = failure.Message
				}
			}
		}
		return outcome
	}
	if !injected {
		queue, ok := session.(Queueer)
		if !ok {
			return failedOutcome(Command{Kind: CommandSteering, Steering: command}, "runtime_not_steerable", "runtime did not accept steering")
		}
		var queueErr error
		if len(images) > 0 {
			multimodal, ok := session.(ImageQueueSession)
			if !ok {
				return failedOutcome(Command{Kind: CommandSteering, Steering: command}, "image_input_unsupported", "selected Worker session cannot queue image content")
			}
			queueErr = multimodal.QueueImages(ctx, text, images)
		} else {
			queueErr = queue.Queue(ctx, text)
		}
		if err := queueErr; err != nil {
			outcome := failedOutcome(Command{Kind: CommandSteering, Steering: command}, "queue_failed", err.Error())
			outcome.AcceptanceUnknown = errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
			return outcome
		}
	}
	return acceptedOutcome(Command{Kind: CommandSteering, Steering: command})
}

func (n *ExecutionNode) resume(ctx context.Context, command *ResumeCommand) CommandOutcome {
	checkpoint := command.Metadata.AttemptID
	_, mapped := n.store.SessionMapping(checkpoint)
	continuing := !mapped && command.Envelope.PreviousAttemptID != ""
	if continuing {
		checkpoint = command.Envelope.PreviousAttemptID
	}
	mapping, mappingErr := n.continuationMapping(command.Envelope, checkpoint)
	if mappingErr != nil {
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
		if err := n.validateReconciledWorkspace(command.Envelope); err != nil {
			return failedOutcome(Command{Kind: CommandResume, Resume: command}, "workspace_mapping_mismatch", err.Error())
		}
		canonicalWorkspace, err := validateProjectWorkspaceOnNode(command.Envelope)
		if err != nil {
			return failedOutcome(Command{Kind: CommandResume, Resume: command}, "workspace_forbidden", err.Error())
		}
		workspace = canonicalWorkspace
	}
	promptText := command.Envelope.OriginalUserIntent
	if command.Envelope.ContinuationPrompt != "" {
		promptText = command.Envelope.ContinuationPrompt
	}
	task := command.Envelope.OriginalUserIntent
	if continuing {
		task = promptText
	}
	prepared, images, artifactErr := n.prepareArtifactInput(ctx, workspace, task)
	if artifactErr != nil {
		if outcome, ok := n.beforeNativeHandoff(ctx, Command{Kind: CommandResume, Resume: command}); ok {
			return outcome
		}
		return failedOutcome(Command{Kind: CommandResume, Resume: command}, "artifact_unavailable", artifactErr.Error())
	}
	if len(images) > 0 && !supportsImageHarness(command.Envelope.HarnessInstance) {
		return failedOutcome(Command{Kind: CommandResume, Resume: command}, "image_input_unsupported", "selected Worker harness does not advertise image input capability")
	}
	task = prepared
	if continuing {
		promptText = prepared
	}
	servers, publishErr := n.workerArtifactMCPServers(command.Envelope.WorkerRef, command.Envelope.AttemptID, workspace)
	if publishErr != nil {
		return failedOutcome(Command{Kind: CommandResume, Resume: command}, "artifact_publish_unavailable", publishErr.Error())
	}
	activatedPublisher := false
	defer func() {
		if !activatedPublisher {
			n.unregisterArtifactPublisher(command.Envelope.AttemptID)
		}
	}()
	request := StartRequest{MCPServers: servers, WorkerRef: command.Envelope.WorkerRef, Task: task, Images: images, Workspace: workspace, Profile: command.Envelope.Profile, HarnessInstance: command.Envelope.HarnessInstance, Model: command.Envelope.Model, Reasoning: command.Envelope.Reasoning, ApprovalPolicy: command.Envelope.ApprovalPolicy, PendingRequests: n.store.PendingRequests(command.Metadata.AttemptID), PendingRequestKinds: pendingRequestKinds(n.store.PendingRequests(command.Metadata.AttemptID)), PendingRequestIDs: n.store.PendingRequestIDs(command.Metadata.AttemptID), DeferInitialPrompt: continuing}
	profile, err := request.effectiveProfile()
	if err != nil {
		return failedOutcome(Command{Kind: CommandResume, Resume: command}, "binding_conflict", err.Error())
	}
	request.Profile = profile
	if outcome, ok := n.beforeNativeHandoff(ctx, Command{Kind: CommandResume, Resume: command}); ok {
		return outcome
	}
	if e := n.store.MarkNativeHandoff(ctx, command.Metadata.CommandID); e != nil {
		if outcome, ok := n.beforeNativeHandoff(ctx, Command{Kind: CommandResume, Resume: command}); ok {
			return outcome
		}
		return failedOutcome(Command{Kind: CommandResume, Resume: command}, "native_handoff_record_failed", e.Error())
	}
	// Mark the Attempt as in-flight before releasing socket ownership.
	// Cancel on the reconnected WebSocket can now interrupt exactly this
	// native Resume, without waiting for Session registration.
	nativeCtx, nativeCancel := context.WithCancel(n.nativeStartContext(ctx))
	pending := &nativeResumeAttempt{commandID: command.Metadata.CommandID, workerRef: command.Metadata.WorkerRef, cancel: nativeCancel}
	n.mu.Lock()
	if n.nativeResumes[command.Metadata.AttemptID] != nil {
		n.mu.Unlock()
		nativeCancel()
		return failedOutcome(Command{Kind: CommandResume, Resume: command}, "execution_state_unknown", "native Resume already in flight")
	}
	n.nativeResumes[command.Metadata.AttemptID] = pending
	n.mu.Unlock()
	notifyNativeHandoff(ctx)
	session, err := resumeSameIdentity(nativeCtx, resumer, request, mapping.RuntimeSessionID)
	n.mu.Lock()
	ownerCanceled := pending.canceled
	wasCanceled := ownerCanceled || nativeCtx.Err() != nil
	if err != nil || wasCanceled {
		delete(n.nativeResumes, command.Metadata.AttemptID)
	}
	n.mu.Unlock()
	if err != nil || wasCanceled {
		nativeCancel()
		if ownerCanceled && command.Envelope.HarnessInstance.Kind == core.HarnessClaudeCode {
			return n.completeCanceledClaudeResume(command, session)
		}
		if session != nil {
			_ = session.Close()
		}
		if wasCanceled {
			return failedOutcome(Command{Kind: CommandResume, Resume: command}, "native_resume_canceled", "Native Resume canceled before Session registration")
		}
		return failedOutcome(Command{Kind: CommandResume, Resume: command}, "runtime_session_unavailable", ErrRuntimeSessionUnavailable.Error())
	}
	// Cancellation and final durable mapping/readiness + Session registration
	// share one lock: a Cancel accepted before registration cannot leave a
	// late mapping or native session, even when it races with persistence.
	n.mu.Lock()
	if pending.canceled || nativeCtx.Err() != nil {
		delete(n.nativeResumes, command.Metadata.AttemptID)
		n.mu.Unlock()
		nativeCancel()
		if pending.canceled && command.Envelope.HarnessInstance.Kind == core.HarnessClaudeCode {
			return n.completeCanceledClaudeResume(command, session)
		}
		_ = session.Close()
		return failedOutcome(Command{Kind: CommandResume, Resume: command}, "native_resume_canceled", "Native Resume canceled before Session registration")
	}
	if continuing {
		mapping.TurnID, mapping.AttemptID = command.Envelope.TurnID, command.Envelope.AttemptID
		if err := n.store.SaveSessionMapping(mapping); err != nil {
			delete(n.nativeResumes, command.Metadata.AttemptID)
			n.mu.Unlock()
			nativeCancel()
			_ = session.Close()
			return failedOutcome(Command{Kind: CommandResume, Resume: command}, "mapping_persist_failed", "continuation mapping could not be persisted")
		}
		ready, err := n.store.SaveCommandReadiness(command.Metadata.CommandID, acceptedOutcome(Command{Kind: CommandResume, Resume: command}))
		if err != nil || ready.State != CommandProcessing || ready.Outcome.State != CommandAccepted {
			delete(n.nativeResumes, command.Metadata.AttemptID)
			n.mu.Unlock()
			nativeCancel()
			_ = session.Close()
			return failedOutcome(Command{Kind: CommandResume, Resume: command}, "runtime_session_unavailable", ErrRuntimeSessionUnavailable.Error())
		}
	}
	n.sessions[command.Metadata.AttemptID] = session
	delete(n.nativeResumes, command.Metadata.AttemptID)
	n.mu.Unlock()
	n.rebindPendingRequests(command.Metadata.AttemptID, session)
	activatedPublisher = true
	if continuing {
		n.watchSession(session, command.Envelope, Command{Kind: CommandResume, Resume: command})
		go func() { _ = promptWithImages(context.Background(), session, promptText, images) }()
	} else {
		n.watchSession(session, command.Envelope, Command{Kind: CommandResume, Resume: command})
	}
	return acceptedOutcome(Command{Kind: CommandResume, Resume: command})
}

func (n *ExecutionNode) completeCanceledClaudeResume(command *ResumeCommand, session Session) CommandOutcome {
	cmd := Command{Kind: CommandResume, Resume: command}
	if session != nil {
		if err := session.Close(); err != nil {
			return failedOutcome(cmd, "native_resume_close_failed", err.Error())
		}
	}
	metadata := command.Metadata
	terminal := core.AttemptOutcomeEnvelope{
		EventID: "attempt-outcome-" + metadata.AttemptID,
		Node: metadata.Node,
		HarnessInstanceID: metadata.HarnessInstanceID,
		WorkerRef: metadata.WorkerRef,
		TurnID: metadata.TurnID,
		AttemptID: metadata.AttemptID,
		Status: core.OutcomeCanceled,
		Classification: core.OutcomeFinal,
		Summary: "Claude Code attempt canceled",
		ErrorCode: "canceled",
		OccurredAt: time.Now().UTC(),
	}
	outcome := acceptedOutcome(cmd)
	delay := 25 * time.Millisecond
	for {
		if _, err := n.store.CompleteCommandWithOutcome(metadata.CommandID, outcome, terminal); err == nil {
			return outcome
		}
		time.Sleep(delay)
		if delay < time.Second {
			delay *= 2
		}
	}
}

func (n *ExecutionNode) respond(ctx context.Context, command *RespondWorkerCommand) CommandOutcome {
	// Validate and stage image attachments before claiming the request. If
	// unsupported, the owner can still provide an ordinary text response.
	var preparedText string
	var preparedImages []ImageInput
	if len(ArtifactIDs(command.Response)) > 0 {
		mapping, ok := n.store.SessionMapping(command.Metadata.AttemptID)
		if !ok {
			return failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "runtime_session_unavailable", "no Worker session mapping")
		}
		var prepareErr error
		preparedText, preparedImages, prepareErr = n.prepareArtifactInput(ctx, mapping.Workspace, command.Response)
		if prepareErr != nil {
			return failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "artifact_unavailable", prepareErr.Error())
		}
		if len(preparedImages) > 0 {
			session, sessionErr := n.sessionForCommand(ctx, command.Metadata)
			if sessionErr != nil {
				return failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "runtime_session_unavailable", sessionErr.Error())
			}
			if _, ok := session.(ImageResponseSession); !ok {
				return failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "image_input_unsupported", "selected native Worker does not support image needs_input responses")
			}
			if capability, ok := session.(ImageResponseCapability); ok && !capability.SupportsImageResponse() {
				return failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "image_input_unsupported", ErrImageResponseUnsupported.Error())
			}
		}
	}
	storedResponse, duplicate, err := n.store.ClaimWorkerResponse(command.RequestID)
	if err != nil {
		return failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "response_claim_failed", err.Error())
	}
	if duplicate {
		if storedResponse.State == CommandProcessing {
			return failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "execution_state_unknown", "worker response execution state is unknown")
		}
		metadata := command.Metadata
		if (storedResponse.TurnID != "" && storedResponse.TurnID != metadata.TurnID) || (storedResponse.AttemptID != "" && storedResponse.AttemptID != metadata.AttemptID) {
			return failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "request_turn_mismatch", "worker request belongs to another Turn or Attempt")
		}
		storedResponse.CommandID = metadata.CommandID
		storedResponse.Kind = CommandRespondWorker
		storedResponse.TurnID = metadata.TurnID
		storedResponse.AttemptID = metadata.AttemptID
		return storedResponse
	}
	session, err := n.sessionForCommand(ctx, command.Metadata)
	if err != nil {
		outcome := failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "runtime_session_unavailable", err.Error())
		_ = n.store.CompleteWorkerResponse(command.RequestID, outcome)
		return outcome
	}
	var respondErr error
	if len(preparedImages) > 0 {
		respondErr = DeliverImageResponse(ctx, n.store, session, command.Metadata, command.RequestID, preparedText, preparedImages)
	} else {
		responder, ok := session.(Responder)
		if !ok {
			outcome := failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "runtime_does_not_accept_response", "runtime does not accept worker responses")
			_ = n.store.CompleteWorkerResponse(command.RequestID, outcome)
			return outcome
		}
		response := command.Response
		if preparedText != "" {
			response = preparedText
		}
		respondErr = responder.Respond(ctx, command.RequestID, response)
	}
	if err := respondErr; err != nil {
		code := "execution_state_unknown"
		if errors.Is(err, ErrImageResponseUnsupported) {
			code = "image_input_unsupported"
		}
		outcome := failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, code, err.Error())
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
	if n.store == nil || metadata.Node != n.node {
		return nil, ErrRuntimeSessionUnavailable
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
	if envelope.AttemptID != metadata.AttemptID || envelope.WorkerRef != metadata.WorkerRef || envelope.TurnID != metadata.TurnID || (metadata.HarnessInstanceID != "" && envelope.HarnessInstance.ID != metadata.HarnessInstanceID) {
		return nil, ErrRuntimeSessionUnavailable
	}
	mapping, err = n.continuationMapping(envelope, metadata.AttemptID)
	if err != nil {
		return nil, ErrRuntimeSessionUnavailable
	}
	if session, ok := n.session(metadata.AttemptID); ok {
		if session.ID() != mapping.RuntimeSessionID {
			return nil, ErrRuntimeSessionUnavailable
		}
		return session, nil
	}
	resumer, ok := n.runtime.(Resumer)
	if !ok {
		return nil, ErrRuntimeSessionUnavailable
	}
	if err := n.validateReconciledWorkspace(envelope); err != nil {
		return nil, ErrRuntimeSessionUnavailable
	}
	servers, publishErr := n.workerArtifactMCPServers(envelope.WorkerRef, envelope.AttemptID, mapping.Workspace)
	if publishErr != nil {
		return nil, ErrRuntimeSessionUnavailable
	}
	activatedPublisher := false
	defer func() {
		if !activatedPublisher {
			n.unregisterArtifactPublisher(envelope.AttemptID)
		}
	}()
	request := StartRequest{MCPServers: servers, WorkerRef: envelope.WorkerRef, Task: envelope.OriginalUserIntent, Workspace: mapping.Workspace, Profile: envelope.Profile, HarnessInstance: envelope.HarnessInstance, Model: envelope.Model, Reasoning: envelope.Reasoning, ApprovalPolicy: envelope.ApprovalPolicy, PendingRequests: n.store.PendingRequests(metadata.AttemptID), PendingRequestKinds: pendingRequestKinds(n.store.PendingRequests(metadata.AttemptID)), PendingRequestIDs: n.store.PendingRequestIDs(metadata.AttemptID)}
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, ErrRuntimeSessionUnavailable
	}
	request.Profile = profile
	session, err := resumeSameIdentity(n.nativeStartContext(ctx), resumer, request, mapping.RuntimeSessionID)
	if err != nil {
		return nil, ErrRuntimeSessionUnavailable
	}
	n.rebindPendingRequests(metadata.AttemptID, session)
	n.registerSession(metadata.AttemptID, session)
	activatedPublisher = true
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
	if command.Kind == CommandRespondWorker {
		return false, nil
	}
	if command.Kind == CommandDispatch || command.Kind == CommandResume {
		return false, nil
	}
	if command.Kind == CommandSteering {
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
	n.unregisterArtifactPublisher(attemptID)
}

func (n *ExecutionNode) watchSession(session Session, envelope WorkerEnvelope, terminalCommand ...Command) {
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
				if result.Status == "failed" {
					if result.FailureCode != "" {
						outcome.FailureCode = result.FailureCode
						outcome.ErrorCode = result.FailureCode
					}
					if result.FailureCategory != "" {
						outcome.FailureCategory = result.FailureCategory
					}
					if result.Technical != "" {
						outcome.Diagnostics = result.Technical
					}
				}
				if envelope.HarnessInstance.Kind == core.HarnessOpenCode || envelope.HarnessInstance.Kind == core.HarnessClaudeCode || envelope.HarnessInstance.Kind == core.HarnessCodex || envelope.HarnessInstance.Kind == core.HarnessAntigravity {
					if err := session.Close(); err != nil {
						outcome.Status = core.OutcomeFailed
						if result.Status == "succeeded" {
							outcome.ErrorCode = "native_session_close_failed"
							outcome.Summary = "Native session did not close cleanly"
						}
						outcome.Diagnostics = appendFailureDiagnostic(outcome.Diagnostics, "Native session close failed: "+SanitizeFailureMessage(err.Error()))
					}
				}
				n.removeSession(envelope.AttemptID)
				if outcome.Status == core.OutcomeFailed {
					sanitizeTerminalFailureOutcome(&outcome)
					if outcome.FailureCategory == core.FailureCategoryUsageLimit && envelope.HarnessInstance.Kind == core.HarnessCodex {
						commandBin := "codex"
						var env []string
						if n.runtime != nil {
							if cr, ok := n.runtime.(CodexRuntime); ok {
								if configured, err := cr.configured(StartRequest{Profile: envelope.Profile, Workspace: envelope.Workspace}); err == nil {
									commandBin = configured.Command
									env = configured.Environment
								}
							}
						}
						if resp, err := readCodexRateLimits(context.Background(), commandBin, env, envelope.Workspace); err == nil {
							obs := evaluateCodexRateLimits(resp, envelope.Model, outcome.FailureCode, time.Now().UTC(), nil)
							if obs.ResetsAt != nil {
								outcome.ResetsAt = obs.ResetsAt
								outcome.QuotaScope = obs.Scope
							}
						}
					}
				}
				if len(terminalCommand) != 0 {
					command := terminalCommand[0]
					n.completeTerminalCommandUntilPersisted(command, acceptedOutcome(command), outcome)
				} else {
					n.queueTerminalOutcome(outcome)
				}
				return
			}
		}
	}()
}

func (n *ExecutionNode) completeTerminalCommandUntilPersisted(command Command, commandOutcome CommandOutcome, terminal core.AttemptOutcomeEnvelope) {
	delay := 100 * time.Millisecond
	loggedFailure := false
	for {
		if _, err := n.store.CompleteCommandWithOutcome(command.Metadata().CommandID, commandOutcome, terminal); err == nil {
			return
		} else if !loggedFailure {
			log.Printf("node: persist terminal command receipt and outcome for attempt %s failed; retrying: %s", command.Metadata().AttemptID, SanitizeFailureMessage(err.Error()))
			loggedFailure = true
		}
		time.Sleep(delay)
		if delay < 5*time.Second {
			delay *= 2
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
		}
	}
}

func appendFailureDiagnostic(existing, secondary string) string {
	existing = SanitizeFailureMessage(existing)
	secondary = SanitizeFailureMessage(secondary)
	if secondary == "" {
		return existing
	}
	if existing == "" {
		return secondary
	}
	secondaryRunes := []rune(secondary)
	if len(secondaryRunes) > 300 {
		secondary = string(secondaryRunes[:297]) + "..."
		secondaryRunes = []rune(secondary)
	}
	const separator = "; "
	current := []rune(existing)
	maxCurrent := 1000 - len([]rune(separator)) - len(secondaryRunes)
	if maxCurrent < 0 {
		return string(secondaryRunes[:1000])
	}
	if len(current) > maxCurrent {
		if maxCurrent > 3 {
			existing = string(current[:maxCurrent-3]) + "..."
		} else {
			existing = string(current[:maxCurrent])
		}
	}
	return SanitizeFailureMessage(existing + separator + secondary)
}

func (n *ExecutionNode) queueTerminalOutcome(outcome core.AttemptOutcomeEnvelope) {
	if err := outcome.Validate(); err != nil {
		log.Printf("node: invalid terminal outcome for attempt %s: %s", outcome.AttemptID, SanitizeFailureMessage(err.Error()))
		return
	}
	delay := 100 * time.Millisecond
	loggedFailure := false
	for {
		if _, err := n.store.QueueOutcome(outcome); err == nil {
			return
		} else if !loggedFailure {
			log.Printf("node: queue terminal outcome for attempt %s failed; retrying: %s", outcome.AttemptID, SanitizeFailureMessage(err.Error()))
			loggedFailure = true
		}
		time.Sleep(delay)
		if delay < 5*time.Second {
			delay *= 2
			if delay > 5*time.Second {
				delay = 5 * time.Second
			}
		}
	}
}

func sanitizeTerminalFailureOutcome(outcome *core.AttemptOutcomeEnvelope) {
	outcome.Summary = SanitizeFailureMessage(outcome.Summary)
	outcome.ErrorCode = SanitizeFailureMessage(outcome.ErrorCode)
	outcome.ErrorMessage = SanitizeFailureMessage(outcome.ErrorMessage)
	outcome.Diagnostics = SanitizeFailureMessage(outcome.Diagnostics)
	outcome.FailureCategory = core.SanitizeFailureCategory(outcome.FailureCategory)
	outcome.FailureCode = core.SanitizeFailureProviderCode(outcome.FailureCode)
	if strings.TrimSpace(outcome.Summary) == "" {
		outcome.Summary = string(outcome.Status)
	}
}

func (n *ExecutionNode) publishRuntimeActivity(envelope WorkerEnvelope, item Activity) {
	capabilities := capabilitiesForObservedActivity(envelope.HarnessInstance, item.Kind)
	activity, ok := normalizeRuntimeActivity(item, n.nextMetadata(envelope), capabilities, envelope.Workspace)
	if !ok {
		return
	}
	validatedInstance := envelope.HarnessInstance
	validatedInstance.Capabilities = capabilities
	if err := activity.ValidateFor(validatedInstance); err != nil {
		return
	}
	if activity.Kind == core.ActivityPermissionRequest || activity.Kind == core.ActivityUserInputRequest {
		if activity.Request == nil || n.store.SavePendingRequest(activity.Request.RequestID, envelope.AttemptID, item.Kind, envelope.HarnessInstance.ID) != nil {
			return
		}
	}
	_, _ = n.store.QueueActivity(activity)
}

func capabilitiesForObservedActivity(instance core.HarnessInstance, kind ActivityKind) core.HarnessCapabilities {
	capabilities := instance.Capabilities
	if instance.Kind != core.HarnessFX && instance.Kind != core.HarnessCodex {
		return capabilities
	}
	var required core.ActivityCapability
	switch kind {
	case ActivityPermission:
		required = core.ActivityPermissionRequest
	case ActivityUserInput:
		required = core.ActivityUserInputRequest
	default:
		return capabilities
	}
	if capabilities.SupportsActivity(required) {
		return capabilities
	}
	capabilities.Activity = append(append([]core.ActivityCapability(nil), capabilities.Activity...), required)
	return capabilities
}

func pendingRequestKinds(requests []PendingRequest) map[string]ActivityKind {
	kinds := make(map[string]ActivityKind, len(requests))
	for _, request := range requests {
		if request.RequestID != "" && request.Kind != "" {
			kinds[request.RequestID] = request.Kind
		}
	}
	return kinds
}

func (n *ExecutionNode) rebindPendingRequests(attemptID string, session Session) {
	if rebinder, ok := session.(PendingRequestRebinder); ok {
		rebinder.RebindPendingRequests(n.store.PendingRequests(attemptID))
		return
	}
	if rebinder, ok := session.(RequestRebinder); ok {
		rebinder.RebindRequests(n.store.PendingRequestIDs(attemptID))
	}
}

func NormalizeRuntimeActivity(item Activity, metadata core.ActivityMetadata, capabilities core.HarnessCapabilities, workspace ...string) (core.Activity, bool) {
	return normalizeRuntimeActivity(item, metadata, capabilities, workspace...)
}

func normalizeRuntimeActivity(item Activity, metadata core.ActivityMetadata, capabilities core.HarnessCapabilities, workspace ...string) (core.Activity, bool) {
	workspacePath := ""
	if len(workspace) > 0 {
		workspacePath = workspace[0]
	}
	var activity core.Activity
	switch item.Kind {
	case ActivityText:
		if !capabilities.SupportsActivity(core.ActivityAssistantTextDelta) || item.Text == "" {
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
		if !capabilities.SupportsActivity(core.ActivityToolCall) || tool == "" {
			return core.Activity{}, false
		}
		arguments, safe := SanitizeToolArguments(item.Arguments)
		if !safe {
			return core.Activity{}, false
		}
		activity = core.Activity{Metadata: metadata, Kind: core.ActivityToolCall, ToolCall: &core.ToolCall{CallID: strings.TrimSpace(item.CallID), Name: tool, Arguments: arguments, Preview: ToolArgumentPreview(tool, arguments, workspacePath)}}
	case ActivityToolResult:
		tool := strings.TrimSpace(item.Tool)
		if !capabilities.SupportsActivity(core.ActivityToolResult) || tool == "" {
			return core.Activity{}, false
		}
		result := ""
		var failure *core.ToolFailureMetadata
		if strings.EqualFold(strings.TrimSpace(item.Status), "failed") {
			if item.Failure != nil && item.Failure.Validate() == nil {
				copy := *item.Failure
				failure = &copy
			}
		} else {
			var safe bool
			result, safe = SanitizeToolResult(item.Result)
			if !safe {
				return core.Activity{}, false
			}
		}
		if result == "" && strings.TrimSpace(item.Status) == "" && failure == nil {
			return core.Activity{}, false
		}
		outputPresent := item.OutputPresent || semanticOutputPresent(result)
		activity = core.Activity{Metadata: metadata, Kind: core.ActivityToolResult, ToolResult: &core.ToolResult{CallID: strings.TrimSpace(item.CallID), Name: tool, Output: result, OutputPresent: outputPresent, Status: item.Status, Preview: ToolArgumentPreview(tool, item.Arguments, workspacePath), Failure: failure}}
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
		activity = core.Activity{Metadata: metadata, Kind: kind, Request: &core.ActivityRequest{RequestID: item.RequestID, Summary: item.Summary, Schema: append(json.RawMessage(nil), item.RequestSchema...)}}
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
	return bindCommandOutcomeIdentity(CommandOutcome{State: CommandAccepted}, command)
}
func failedOutcome(command Command, code, message string) CommandOutcome {
	return bindCommandOutcomeIdentity(CommandOutcome{State: CommandFailed, ErrorCode: code, ErrorMessage: message}, command)
}
func bindCommandOutcomeIdentity(outcome CommandOutcome, command Command) CommandOutcome {
	metadata := command.Metadata()
	if outcome.CommandID == "" {
		outcome.CommandID = metadata.CommandID
	}
	if outcome.Kind == "" {
		outcome.Kind = command.Kind
	}
	if outcome.TurnID == "" {
		outcome.TurnID = metadata.TurnID
	}
	if outcome.AttemptID == "" {
		outcome.AttemptID = metadata.AttemptID
	}
	return outcome
}
func nodeEventID(attempt string) string {
	return fmt.Sprintf("node-event-%s-%d", attempt, time.Now().UnixNano())
}

func semanticOutputPresent(result string) bool {
	trimmed := strings.TrimSpace(result)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, `"`) && strings.HasSuffix(trimmed, `"`) {
		var decoded string
		if json.Unmarshal([]byte(trimmed), &decoded) == nil {
			return strings.TrimSpace(decoded) != ""
		}
	}
	if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
		var parsed any
		if json.Unmarshal([]byte(trimmed), &parsed) == nil {
			return core.HasSemanticOutput(parsed)
		}
	}
	return true
}

