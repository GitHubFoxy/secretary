package node

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
)

// continuationMapping proves the exact server checkpoint against both the
// Node-local mapping and its durable lifecycle command. No native identity
// crosses the Node boundary and no current-default Profile is consulted.
func (n *ExecutionNode) continuationMapping(envelope WorkerEnvelope, attemptID string) (LocalSessionMapping, error) {
	mapping, ok := n.store.SessionMapping(attemptID)
	if !ok || mapping.AttemptID != attemptID || strings.TrimSpace(mapping.RuntimeSessionID) == "" || mapping.WorkerRef != envelope.WorkerRef || mapping.HarnessInstanceID != envelope.HarnessInstance.ID {
		return LocalSessionMapping{}, ErrRuntimeSessionUnavailable
	}
	if (attemptID == envelope.AttemptID && mapping.TurnID != envelope.TurnID) || !n.store.nativeSessionOwnedBy(mapping) {
		return LocalSessionMapping{}, ErrRuntimeSessionUnavailable
	}
	if attemptID == envelope.AttemptID && envelope.PreviousAttemptID != "" {
		previous, err := n.continuationMapping(envelope, envelope.PreviousAttemptID)
		if err != nil || previous.RuntimeSessionID != mapping.RuntimeSessionID {
			return LocalSessionMapping{}, ErrRuntimeSessionUnavailable
		}
	}
	var authoritative *WorkerEnvelope
	for _, record := range n.store.lifecycleCommands(attemptID) {
		// The current Resume claim is not prior evidence for its own checkpoint.
		if record.State != CommandAccepted && record.State != CommandInterrupted {
			// Active steering/cancel may use the session already registered by
			// this process. A fresh Node has no such proof; processing mapping
			// alone must never authorize loading/recovery of uncertain input.
			session, live := n.session(attemptID)
			if record.State != CommandProcessing || !live || session.ID() != mapping.RuntimeSessionID {
				continue
			}
		}
		command, err := commandFromJSON(record.CommandJSON)
		if err != nil {
			return LocalSessionMapping{}, ErrRuntimeSessionUnavailable
		}
		var prior WorkerEnvelope
		if command.Kind == CommandDispatch {
			prior = command.Dispatch.Envelope
		} else {
			prior = command.Resume.Envelope
		}
		if validationErr := command.Validate(n.node); validationErr != nil {
			// Older durable FX commands predate the template marker. The
			// authenticated current envelope must carry Core's explicit
			// pre-marker proof; harness kind alone never grants this path.
			if !errors.Is(validationErr, ErrManagedProfileRequired) || !envelope.LegacyWorkerTemplate || prior.HarnessInstance.Kind != core.HarnessFX || !reflect.DeepEqual(prior.Profile, ManagedProfile{}) {
				return LocalSessionMapping{}, ErrRuntimeSessionUnavailable
			}
			prior.LegacyWorkerTemplate = true
			if !envelopeMatchesMetadata(prior, command.Metadata()) || prior.Validate(n.node) != nil {
				return LocalSessionMapping{}, ErrRuntimeSessionUnavailable
			}
		}
		if prior.AttemptID != mapping.AttemptID || prior.TurnID != mapping.TurnID || prior.WorkerRef != mapping.WorkerRef || prior.HarnessInstance.ID != mapping.HarnessInstanceID {
			return LocalSessionMapping{}, ErrRuntimeSessionUnavailable
		}
		if authoritative != nil && !reflect.DeepEqual(*authoritative, prior) {
			return LocalSessionMapping{}, ErrRuntimeSessionUnavailable
		}
		copy := prior
		authoritative = &copy
	}
	if authoritative == nil {
		return LocalSessionMapping{}, ErrRuntimeSessionUnavailable
	}
	prior := *authoritative
	if prior.Workspace != "" && prior.ProjectID != "" {
		canonical, err := filepath.EvalSymlinks(prior.Workspace)
		if err != nil || filepath.Clean(canonical) != mapping.Workspace {
			return LocalSessionMapping{}, ErrRuntimeSessionUnavailable
		}
	}
	if !reflect.DeepEqual(prior.HarnessInstance, envelope.HarnessInstance) || !reflect.DeepEqual(prior.Profile, envelope.Profile) || prior.Model != envelope.Model || prior.Reasoning != envelope.Reasoning || prior.ApprovalPolicy != envelope.ApprovalPolicy || prior.ProjectID != envelope.ProjectID || !reflect.DeepEqual(prior.ProjectSnapshot, envelope.ProjectSnapshot) || prior.LegacyWorkerTemplate != envelope.LegacyWorkerTemplate || prior.Workspace != envelope.Workspace || (prior.ProjectID == "" && prior.Workspace != "" && prior.Workspace != mapping.Workspace) {
		return LocalSessionMapping{}, ErrRuntimeSessionUnavailable
	}
	return mapping, nil
}

// The ACK is historical readiness, not execution proof. Only this process's
// registered session can authorize replay while the durable claim is processing.
func (n *ExecutionNode) liveContinuationReadiness(command Command, record CommandRecord) bool {
	envelope, same := sameContinuationCommand(command, record)
	if !same {
		return false
	}
	mapping, err := n.continuationMapping(envelope, envelope.AttemptID)
	if err != nil {
		return false
	}
	session, live := n.session(envelope.AttemptID)
	return live && session.ID() == mapping.RuntimeSessionID
}

func sameContinuationCommand(command Command, record CommandRecord) (WorkerEnvelope, bool) {
	previous, err := commandFromJSON(record.CommandJSON)
	if err != nil || previous.Kind != command.Kind {
		return WorkerEnvelope{}, false
	}
	old, current := previous.Metadata(), command.Metadata()
	if old.CommandID != current.CommandID || old.Node != current.Node || old.HarnessInstanceID != current.HarnessInstanceID || old.WorkerRef != current.WorkerRef || old.TurnID != current.TurnID || old.AttemptID != current.AttemptID {
		return WorkerEnvelope{}, false
	}
	var expected, envelope WorkerEnvelope
	switch command.Kind {
	case CommandDispatch:
		if previous.Dispatch == nil || command.Dispatch == nil {
			return WorkerEnvelope{}, false
		}
		expected, envelope = previous.Dispatch.Envelope, command.Dispatch.Envelope
	case CommandResume:
		if previous.Resume == nil || command.Resume == nil {
			return WorkerEnvelope{}, false
		}
		expected, envelope = previous.Resume.Envelope, command.Resume.Envelope
	default:
		return WorkerEnvelope{}, false
	}
	left, err := json.Marshal(expected)
	if err != nil {
		return WorkerEnvelope{}, false
	}
	right, err := json.Marshal(envelope)
	return envelope, err == nil && envelope.PreviousAttemptID != "" && bytes.Equal(left, right)
}
