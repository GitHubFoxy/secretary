package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

type sessionMapping struct {
	WorkerRef         string                 `json:"worker_ref"`
	TurnID            string                 `json:"turn_id"`
	AttemptID         string                 `json:"attempt_id"`
	HarnessInstanceID core.HarnessInstanceID `json:"harness_instance_id"`
	Workspace         string                 `json:"workspace"`
	RuntimeSessionID  string                 `json:"runtime_session_id"`
}

// LocalSessionMapping is intentionally a Node-only type. It is never part of
// protocol payloads or server domain records.
type LocalSessionMapping = sessionMapping

// ProcessInspector is intentionally Node-local. The server never receives the
// native session identifier or the result of this inspection directly.
type ProcessInspector interface {
	Inspect(context.Context, CommandRecord) (bool, error)
}

type localState struct {
	Version                  int                       `json:"version"`
	NextSequence             uint64                    `json:"next_sequence"`
	LastAcknowledgedSequence uint64                    `json:"last_acknowledged_sequence"`
	Commands                 map[string]CommandRecord  `json:"commands"`
	Mappings                 map[string]sessionMapping `json:"mappings"`
	Outbox                   []PendingEvent            `json:"outbox"`
}

// LocalStore is the Node-owned durable command table, session mapping and
// normalized event outbox. It is deliberately separate from server delivery.
type LocalStore struct {
	mu    sync.Mutex
	path  string
	state localState
	file  *os.File
}

func OpenLocalStore(path string) (*LocalStore, error) {
	if path == "" {
		return nil, errors.New("node: local store path is required")
	}
	store := &LocalStore{path: path, state: localState{Version: 1, NextSequence: 1, Commands: map[string]CommandRecord{}, Mappings: map[string]sessionMapping{}, Outbox: []PendingEvent{}}}
	encoded, err := os.ReadFile(path)
	if err == nil {
		if len(encoded) > 0 {
			if err := json.Unmarshal(encoded, &store.state); err != nil {
				return nil, fmt.Errorf("node: decode local store: %w", err)
			}
			if store.state.Commands == nil {
				store.state.Commands = map[string]CommandRecord{}
			}
			if store.state.Mappings == nil {
				store.state.Mappings = map[string]sessionMapping{}
			}
			if store.state.NextSequence == 0 {
				store.state.NextSequence = 1
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *LocalStore) Close() error { return nil }

func (s *LocalStore) persistLocked() error {
	encoded, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *LocalStore) ClaimCommand(command Command) (CommandRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := command.Validate(command.Metadata().Node); err != nil {
		return CommandRecord{}, false, err
	}
	metadata := command.Metadata()
	if existing, ok := s.state.Commands[metadata.CommandID]; ok {
		return existing, true, nil
	}
	encoded, err := commandJSON(command)
	if err != nil {
		return CommandRecord{}, false, err
	}
	now := time.Now().UTC()
	record := CommandRecord{CommandID: metadata.CommandID, Kind: command.Kind, State: CommandProcessing, Outcome: CommandOutcome{CommandID: metadata.CommandID, Kind: command.Kind, State: CommandProcessing}, CommandJSON: encoded, ClaimedAt: now, UpdatedAt: now}
	s.state.Commands[record.CommandID] = record
	if err := s.persistLocked(); err != nil {
		delete(s.state.Commands, record.CommandID)
		return CommandRecord{}, false, err
	}
	return record, false, nil
}

func (s *LocalStore) CompleteCommand(commandID string, outcome CommandOutcome) (CommandRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.state.Commands[commandID]
	if !ok {
		return CommandRecord{}, errors.New("node: command claim not found")
	}
	if record.State != CommandProcessing {
		return record, nil
	}
	if outcome.CommandID == "" {
		outcome.CommandID = commandID
	}
	if outcome.Kind == "" {
		outcome.Kind = record.Kind
	}
	if outcome.State == CommandProcessing {
		return CommandRecord{}, errors.New("node: command outcome is still processing")
	}
	record.State, record.Outcome, record.UpdatedAt = outcome.State, outcome, time.Now().UTC()
	s.state.Commands[commandID] = record
	if err := s.persistLocked(); err != nil {
		return CommandRecord{}, err
	}
	return record, nil
}

func (s *LocalStore) Command(commandID string) (CommandRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.state.Commands[commandID]
	if !ok {
		return CommandRecord{}, errors.New("node: command not found")
	}
	return record, nil
}

func (s *LocalStore) CommandForAttempt(attemptID string) (CommandRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.state.Commands {
		command, err := commandFromJSON(record.CommandJSON)
		if err != nil {
			continue
		}
		if command.Metadata().AttemptID == attemptID {
			return record, nil
		}
	}
	return CommandRecord{}, errors.New("node: command for attempt not found")
}

func (s *LocalStore) SaveSessionMapping(mapping sessionMapping) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mapping.WorkerRef == "" || mapping.TurnID == "" || mapping.AttemptID == "" || mapping.RuntimeSessionID == "" {
		return errors.New("node: incomplete local session mapping")
	}
	s.state.Mappings[mapping.AttemptID] = mapping
	return s.persistLocked()
}

func (s *LocalStore) SessionMapping(attemptID string) (LocalSessionMapping, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	mapping, ok := s.state.Mappings[attemptID]
	return mapping, ok
}

func (s *LocalStore) NextEventSequence() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.NextSequence == 0 {
		return 1
	}
	return s.state.NextSequence
}

// ReserveEventSequence allocates a durable sequence for a Node message that
// is not itself stored in the outbox, such as inventory or heartbeat.
func (s *LocalStore) ReserveEventSequence() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sequence := s.state.NextSequence
	if sequence == 0 {
		sequence = 1
	}
	s.state.NextSequence = sequence + 1
	return sequence, s.persistLocked()
}

func (s *LocalStore) QueueActivity(activity core.Activity) (PendingEvent, error) {
	if err := activity.Metadata.Validate(); err != nil {
		return PendingEvent{}, err
	}
	event := NodeEvent{EventID: activity.Metadata.EventID, Node: activity.Metadata.Node, Kind: "activity", Activity: &activity}
	return s.queueEvent(event)
}

func (s *LocalStore) QueueOutcome(outcome core.AttemptOutcomeEnvelope) (PendingEvent, error) {
	if err := outcome.Validate(); err != nil {
		return PendingEvent{}, err
	}
	return s.queueEvent(outcomeEvent(outcome))
}

func (s *LocalStore) queueEvent(event NodeEvent) (PendingEvent, error) {
	if err := event.Validate(); err != nil {
		return PendingEvent{}, err
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return PendingEvent{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, pending := range s.state.Outbox {
		if pending.EventID == event.EventID {
			return pending, nil
		}
	}
	sequence := s.state.NextSequence
	if sequence == 0 {
		sequence = 1
	}
	event.Sequence = sequence
	encoded, err = json.Marshal(event)
	if err != nil {
		return PendingEvent{}, err
	}
	pending := PendingEvent{Sequence: sequence, EventID: event.EventID, Payload: encoded, CreatedAt: time.Now().UTC()}
	s.state.NextSequence = sequence + 1
	s.state.Outbox = append(s.state.Outbox, pending)
	if err := s.persistLocked(); err != nil {
		s.state.Outbox = s.state.Outbox[:len(s.state.Outbox)-1]
		s.state.NextSequence = sequence
		return PendingEvent{}, err
	}
	return pending, nil
}

func (s *LocalStore) PendingEvents() ([]PendingEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := append([]PendingEvent(nil), s.state.Outbox...)
	sort.Slice(pending, func(i, j int) bool { return pending[i].Sequence < pending[j].Sequence })
	return pending, nil
}

func (s *LocalStore) AckThrough(sequence uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.state.Outbox[:0]
	for _, event := range s.state.Outbox {
		if event.Sequence > sequence {
			kept = append(kept, event)
		}
	}
	s.state.Outbox = kept
	if sequence > s.state.LastAcknowledgedSequence {
		s.state.LastAcknowledgedSequence = sequence
	}
	return s.persistLocked()
}

func (s *LocalStore) LastAcknowledgedSequence() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.LastAcknowledgedSequence
}

func (s *LocalStore) RecoverRunning(ctx context.Context, inspector ProcessInspector) error {
	s.mu.Lock()
	ids := make([]string, 0)
	for id, record := range s.state.Commands {
		if record.State == CommandProcessing {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	sort.Strings(ids)
	for _, id := range ids {
		record, err := s.Command(id)
		if err != nil {
			return err
		}
		alive := false
		if inspector != nil {
			alive, err = inspector.Inspect(ctx, record)
			if err != nil {
				return err
			}
		}
		if alive {
			continue
		}
		outcome := CommandOutcome{CommandID: record.CommandID, Kind: record.Kind, State: CommandInterrupted, ErrorCode: "execution_state_unknown", ErrorMessage: "Node could not prove that the side effect is still running"}
		if _, err := s.CompleteCommand(record.CommandID, outcome); err != nil {
			return err
		}
		command, err := commandFromJSON(record.CommandJSON)
		if err != nil {
			return err
		}
		metadata := command.Metadata()
		if command.Kind == CommandDispatch || command.Kind == CommandResume {
			terminal := core.AttemptOutcomeEnvelope{EventID: "interrupted-" + record.CommandID, Node: metadata.Node, HarnessInstanceID: metadata.HarnessInstanceID, WorkerRef: metadata.WorkerRef, TurnID: metadata.TurnID, AttemptID: metadata.AttemptID, Status: core.OutcomeInterrupted, Classification: core.OutcomeFinal, Summary: "Attempt interrupted because execution state could not be proven", ErrorCode: "execution_state_unknown", OccurredAt: time.Now().UTC()}
			if _, err := s.QueueOutcome(terminal); err != nil {
				return err
			}
		}
	}
	return nil
}
