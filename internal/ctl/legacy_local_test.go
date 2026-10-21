package ctl

import (
	"context"
	"errors"
	"fmt"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type legacyLocalTestRuntime struct {
	Local *node.LocalNode
}

func (r legacyLocalTestRuntime) LocalCommand(ctx context.Context, command node.Command) (node.CommandOutcome, error) {
	if r.Local == nil {
		return node.CommandOutcome{}, ErrWorkerRuntimeUnavailable
	}
	metadata := command.Metadata()
	// Local Dispatch may still be waiting for video frames: there is no
	// Session yet. Cancel by AttemptID before any Session lookup, so the
	// pre-native checkpoint prevents a late Start.
	if command.Kind==node.CommandCancel {
		if command.Cancel==nil{return node.CommandOutcome{},errors.New("worker: local cancel command is missing")}
		handled,err:=r.Local.CancelLocalAttempt(ctx,metadata.WorkerRef,metadata.AttemptID)
		if err!=nil{return node.CommandOutcome{},err}
		if !handled{return node.CommandOutcome{},ErrWorkerRuntimeUnavailable}
		return node.CommandOutcome{CommandID:metadata.CommandID,Kind:command.Kind,State:node.CommandAccepted,TurnID:metadata.TurnID,AttemptID:metadata.AttemptID},nil
	}
	session, ok := r.Local.Session(metadata.WorkerRef)
	if command.Kind == node.CommandDispatch {
		if command.Dispatch == nil {
			return node.CommandOutcome{}, errors.New("worker: local dispatch command is missing")
		}
		var err error
		envelope := command.Dispatch.Envelope
		if envelope.ProjectID == "" && envelope.Workspace != "" {
			return node.CommandOutcome{}, errors.New("worker: projectless Workspace must be Node-managed")
		}
		request := node.StartRequest{AttemptID: envelope.AttemptID, WorkerRef: envelope.WorkerRef, Task: envelope.OriginalUserIntent, Workspace: envelope.Workspace, Profile: envelope.Profile, HarnessInstance: envelope.HarnessInstance, Model: envelope.Model, Reasoning: envelope.Reasoning, ApprovalPolicy: envelope.ApprovalPolicy}
		if envelope.PreviousAttemptID != "" {
			session, err = r.Local.Continue(ctx, request, envelope.PreviousAttemptID)
		} else {
			session, err = r.Local.Dispatch(ctx, request)
		}
		if err != nil {
			return node.CommandOutcome{}, err
		}
		return node.CommandOutcome{CommandID: metadata.CommandID, Kind: command.Kind, State: node.CommandAccepted, TurnID: metadata.TurnID, AttemptID: metadata.AttemptID}, nil
	}
	if !ok {
		return node.CommandOutcome{}, ErrWorkerRuntimeUnavailable
	}
	switch command.Kind {
	case node.CommandRespondWorker:
		if command.RespondWorker == nil {
			return node.CommandOutcome{}, ErrWorkerRuntimeUnavailable
		}
		response := command.RespondWorker
		text, images, prepErr := r.Local.PrepareWorkerSteering(ctx, metadata.WorkerRef, response.Response)
		if prepErr != nil {
			return node.CommandOutcome{}, prepErr
		}
		var err error
		if len(images) > 0 {
			multimodal, ok := session.(node.ImageResponseSession)
			if !ok {
				return node.CommandOutcome{}, node.ErrImageResponseUnsupported
			}
			if capability, ok := session.(node.ImageResponseCapability); ok && !capability.SupportsImageResponse() {
				return node.CommandOutcome{}, node.ErrImageResponseUnsupported
			}
			_ = multimodal // capability is validated above; shared helper performs delivery.
			err = node.DeliverImageResponse(ctx, r.Local.ImageResponseStore(), session, metadata, response.RequestID, text, images)
		} else {
			responder, ok := session.(node.Responder)
			if !ok {
				return node.CommandOutcome{}, ErrWorkerRuntimeUnavailable
			}
			err = responder.Respond(ctx, response.RequestID, text)
		}
		if err != nil {
			if errors.Is(err, node.ErrImageResponseUnsupported) {
				return node.CommandOutcome{}, err
			}
			return node.CommandOutcome{}, fmt.Errorf("%w: %w", node.ErrCommandOutcomeUnknown, err)
		}
	case node.CommandSteering:
		if command.Steering == nil {
			return node.CommandOutcome{}, errors.New("worker: local steering command is missing")
		}
		text, images, prepErr := r.Local.PrepareWorkerSteering(ctx, metadata.WorkerRef, command.Steering.Text)
		if prepErr != nil { return node.CommandOutcome{}, prepErr }
		var err error
		failureCode := "steering_failed"
		if len(images) > 0 {
			multimodal, supports := session.(node.ImageSteeringSession)
			if !supports {
				failureCode = "image_input_unsupported"
				err = errors.New("image: local Worker does not support image steering")
			} else {
				var injected bool
				injected, err = multimodal.SteerImages(ctx, text, images)
				if err == nil && !injected {
					if queued, ok := session.(node.ImageQueueSession); ok {
						failureCode = "queue_failed"
						err = queued.QueueImages(ctx, text, images)
					} else {
						failureCode = "image_input_unsupported"
						err = errors.New("image: native Worker did not inject steering and cannot queue image content")
					}
				}
			}
		} else {
			_, err = session.Steer(ctx, text)
		}
		if err != nil {
			outcome := node.CommandOutcome{
				CommandID:         metadata.CommandID,
				Kind:              command.Kind,
				State:             node.CommandFailed,
				TurnID:            metadata.TurnID,
				AttemptID:         metadata.AttemptID,
				ErrorCode:         failureCode,
				ErrorMessage:      err.Error(),
				AcceptanceUnknown: errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded),
			}
			var steerErr *node.SteeringError
			if errors.As(err, &steerErr) {
				outcome.FailureCategory = steerErr.Failure.Category
				outcome.ProviderCode = steerErr.Failure.ProviderCode
				outcome.Technical = steerErr.Failure.Technical
				if steerErr.Failure.Message != "" {
					outcome.ErrorMessage = steerErr.Failure.Message
				}
			}
			commandErr := &node.NodeCommandError{Outcome: outcome, Err: err}
			if outcome.AcceptanceUnknown {
				return outcome, errors.Join(node.ErrCommandOutcomeUnknown, commandErr)
			}
			return outcome, commandErr
		}

	default:
		return node.CommandOutcome{}, ErrWorkerRuntimeUnavailable
	}
	return node.CommandOutcome{CommandID: metadata.CommandID, Kind: command.Kind, State: node.CommandAccepted, TurnID: metadata.TurnID, AttemptID: metadata.AttemptID}, nil
}


func (r legacyLocalTestRuntime) Dispatch(ctx context.Context, commandID string, worker core.Worker, turn core.Turn, attempt core.Phase4Attempt, resolution core.DispatchResolution) error {
	envelope, err := (NodeRuntime{}).envelope(worker, turn, attempt, resolution.ProjectDispatch)
	if err != nil { return err }
	_, err = r.LocalCommand(ctx, node.Command{Kind:node.CommandDispatch, Dispatch:&node.DispatchCommand{Metadata:(NodeRuntime{}).metadata(commandID,worker,turn,attempt,resolution.HarnessInstance.ID),Envelope:envelope}})
	return err
}

func (r legacyLocalTestRuntime) Resume(ctx context.Context, commandID string, worker core.Worker, turn core.Turn, attempt core.Phase4Attempt, prompt string) error {
	binding, err := bindingFromWorker(worker)
	if err != nil { return err }
	envelope, err := (NodeRuntime{}).envelope(worker,turn,attempt,binding)
	if err != nil { return err }
	envelope.ContinuationPrompt=prompt
	_,err=r.LocalCommand(ctx,node.Command{Kind:node.CommandResume,Resume:&node.ResumeCommand{Metadata:(NodeRuntime{}).metadata(commandID,worker,turn,attempt,envelope.HarnessInstance.ID),Envelope:envelope}})
	return err
}

func (r legacyLocalTestRuntime) Steer(ctx context.Context, commandID string, worker core.Worker, attempt core.Phase4Attempt, text string) error {
	_,err:=r.LocalCommand(ctx,node.Command{Kind:node.CommandSteering,Steering:&node.SteeringCommand{Metadata:(NodeRuntime{}).metadata(commandID,worker,core.Turn{ID:attempt.TurnID},attempt,core.HarnessInstanceID(worker.HarnessInstanceID)),Text:text}})
	return err
}

func (r legacyLocalTestRuntime) Respond(ctx context.Context, commandID string, worker core.Worker, attempt core.Phase4Attempt, requestID,response string) error {
	_,err:=r.LocalCommand(ctx,node.Command{Kind:node.CommandRespondWorker,RespondWorker:&node.RespondWorkerCommand{Metadata:(NodeRuntime{}).metadata(commandID,worker,core.Turn{ID:attempt.TurnID},attempt,core.HarnessInstanceID(worker.HarnessInstanceID)),RequestID:requestID,Response:response}})
	return err
}

func (r legacyLocalTestRuntime) Cancel(ctx context.Context, commandID string, worker core.Worker, attempt core.Phase4Attempt) error {
	_,err:=r.LocalCommand(ctx,node.Command{Kind:node.CommandCancel,Cancel:&node.CancelCommand{Metadata:(NodeRuntime{}).metadata(commandID,worker,core.Turn{ID:attempt.TurnID},attempt,core.HarnessInstanceID(worker.HarnessInstanceID))}})
	return err
}

var _ WorkerRuntime = legacyLocalTestRuntime{}
