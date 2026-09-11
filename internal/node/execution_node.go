package node

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

var ErrRuntimeSessionUnavailable = errors.New("runtime_session_unavailable")

type Responder interface {
	Respond(context.Context, string, string) error
}

type ExecutionNode struct {
	node              core.NodeReference
	runtime           Runtime
	store             *LocalStore
	mu                sync.Mutex
	sessions          map[string]Session
	activitySequences map[string]uint64
}

func NewExecutionNode(node core.NodeReference, runtime Runtime, store *LocalStore) *ExecutionNode {
	return &ExecutionNode{node: node, runtime: runtime, store: store, sessions: map[string]Session{}, activitySequences: map[string]uint64{}}
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
	workspace := command.Envelope.Workspace
	if workspace == "" {
		var err error
		workspace, err = os.MkdirTemp("", "secretary-worker-")
		if err != nil {
			return failedOutcome(Command{Kind: CommandDispatch, Dispatch: command}, "workspace_failed", err.Error())
		}
	}
	request := StartRequest{WorkerRef: command.Envelope.WorkerRef, Task: command.Envelope.OriginalUserIntent, Workspace: workspace, Profile: command.Envelope.Profile}
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
		return failedOutcome(Command{Kind: CommandSteering, Steering: command}, "steering_failed", err.Error())
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
	request := StartRequest{WorkerRef: command.Envelope.WorkerRef, Task: command.Envelope.OriginalUserIntent, Workspace: command.Envelope.Workspace, Profile: command.Envelope.Profile}
	session, err := resumer.Resume(ctx, request, mapping.RuntimeSessionID)
	if err != nil {
		return failedOutcome(Command{Kind: CommandResume, Resume: command}, "runtime_session_unavailable", err.Error())
	}
	n.registerSession(command.Metadata.AttemptID, session)
	n.watchSession(session, command.Envelope)
	return acceptedOutcome(Command{Kind: CommandResume, Resume: command})
}

func (n *ExecutionNode) respond(ctx context.Context, command *RespondWorkerCommand) CommandOutcome {
	session, err := n.sessionForCommand(ctx, command.Metadata)
	if err != nil {
		return failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "runtime_session_unavailable", err.Error())
	}
	responder, ok := session.(Responder)
	if !ok {
		return failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "runtime_does_not_accept_response", "runtime does not accept worker responses")
	}
	if err := responder.Respond(ctx, command.RequestID, command.Response); err != nil {
		return failedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command}, "response_failed", err.Error())
	}
	return acceptedOutcome(Command{Kind: CommandRespondWorker, RespondWorker: command})
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
	if command.Dispatch != nil {
		envelope = command.Dispatch.Envelope
	}
	if command.Resume != nil {
		envelope = command.Resume.Envelope
	}
	resumer, ok := n.runtime.(Resumer)
	if !ok {
		return nil, ErrRuntimeSessionUnavailable
	}
	session, err := resumer.Resume(ctx, StartRequest{WorkerRef: envelope.WorkerRef, Task: envelope.OriginalUserIntent, Workspace: mapping.Workspace, Profile: envelope.Profile}, mapping.RuntimeSessionID)
	if err != nil {
		return nil, ErrRuntimeSessionUnavailable
	}
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
	kind := core.ActivityKindStatus
	activity := core.Activity{Metadata: n.nextMetadata(envelope), Kind: kind, Status: item.Text}
	switch item.Kind {
	case ActivityText:
		kind = core.ActivityKindAssistantTextDelta
		activity.Kind = kind
		activity.Text = item.Text
	case ActivityTool:
		kind = core.ActivityKindToolCall
		activity.Kind = kind
		activity.ToolCall = &core.ToolCall{Name: item.Text}
	case ActivityStatus:
		activity.Kind = kind
	}
	if err := activity.ValidateFor(envelope.HarnessInstance); err != nil {
		return
	}
	_, _ = n.store.QueueActivity(activity)
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
