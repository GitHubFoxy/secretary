package node

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
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

type pendingRequestMapping struct {
	RequestID         string                 `json:"request_id"`
	AttemptID         string                 `json:"attempt_id"`
	HarnessInstanceID core.HarnessInstanceID `json:"harness_instance_id"`
	Kind              ActivityKind           `json:"kind"`
}

type LocalSessionMapping = sessionMapping

type ProcessInspector interface {
	Inspect(context.Context, CommandRecord) (bool, error)
}

// An image response has two distinct native side effects: image steering and
// the interaction reply. Checkpoint steering independently of the reply ACK.
// "started" forbids a blind re-injection after a crash/timeout; "injected"
// allows retrying ONLY the still-pending native textual response.
type imageResponseCheckpoint struct {
	AttemptID string `json:"attempt_id"`
	Digest string `json:"digest"`
	Stage string `json:"stage"`
}

// localPrestartRecord is persisted in protected Node state before any local
// Worker dispatch/continuation can enter media preparation.
type localPrestartRecord struct {
	WorkerRef string `json:"worker_ref"`
	Stage string `json:"stage"` // preparing, canceled, handoff
}

type localState struct {
	Version                  int                              `json:"version"`
	NextSequence             uint64                           `json:"next_sequence"`
	LastAcknowledgedSequence uint64                           `json:"last_acknowledged_sequence"`
	Commands                 map[string]CommandRecord         `json:"commands"`
	Mappings                 map[string]sessionMapping        `json:"mappings"`
	PendingRequests          map[string]pendingRequestMapping `json:"pending_requests"`
	WorkerResponses          map[string]CommandOutcome        `json:"worker_responses"`
	ImageResponses           map[string]imageResponseCheckpoint `json:"image_responses,omitempty"`
	LocalPrestarts           map[string]localPrestartRecord  `json:"local_prestarts,omitempty"`
	Outbox                   []PendingEvent                   `json:"outbox"`
}

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
	store := &LocalStore{path: path, state: localState{Version: 1, NextSequence: 1, Commands: map[string]CommandRecord{}, Mappings: map[string]sessionMapping{}, PendingRequests: map[string]pendingRequestMapping{}, WorkerResponses: map[string]CommandOutcome{}, ImageResponses: map[string]imageResponseCheckpoint{}, LocalPrestarts:map[string]localPrestartRecord{}, Outbox: []PendingEvent{}}}
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
			if store.state.PendingRequests == nil {
				store.state.PendingRequests = map[string]pendingRequestMapping{}
			}
			if store.state.WorkerResponses == nil {
				store.state.WorkerResponses = map[string]CommandOutcome{}
			}
			if store.state.ImageResponses == nil {
				store.state.ImageResponses = map[string]imageResponseCheckpoint{}
			}
			if store.state.LocalPrestarts==nil {store.state.LocalPrestarts=map[string]localPrestartRecord{}}
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

// Preserve conservative unknown-execution recovery for commands unrelated to
// Artifact preparation. T12 video/image references can be proven unstarted
// while blocked on media, but ordinary Worker actions cannot.
func hasPrestartArtifactReference(command Command) bool {
	switch command.Kind {
	case CommandDispatch:
		if command.Dispatch==nil{return false}
		env:=command.Dispatch.Envelope
		return len(ArtifactIDs(env.OriginalUserIntent))>0 || len(ArtifactIDs(env.ContinuationPrompt))>0
	case CommandResume:
		if command.Resume==nil{return false}
		env:=command.Resume.Envelope
		return len(ArtifactIDs(env.OriginalUserIntent))>0 || len(ArtifactIDs(env.ContinuationPrompt))>0
	}
	return false
}

// Local pre-native state lives beside the durable response checkpoints,
// not under a Worker-controllable workspace. The handoff marker is persisted
// BEFORE native Start/Resume; on restart an uncertain handoff is never replayed.
var ErrLocalPrestartCanceled=errors.New("worker: canceled_before_handoff")
func (s *LocalStore) BeginLocalPrestart(attemptID,workerRef string) error {
	if attemptID=="" || workerRef=="" {return errors.New("node: incomplete local prestart identity")}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev,ok:=s.state.LocalPrestarts[attemptID];ok {
		if prev.WorkerRef!=workerRef{return errors.New("node: local prestart Worker identity conflict")}
		if prev.Stage=="canceled"{return ErrLocalPrestartCanceled}
		if prev.Stage=="handoff" || prev.Stage=="resuming"{return errors.New("node: local native handoff may already have occurred")}
		// A previous 'preparing' claim can safely be restarted: it never
		// crossed the durably marked native handoff boundary.
		return nil
	}
	s.state.LocalPrestarts[attemptID]=localPrestartRecord{WorkerRef:workerRef,Stage:"preparing"}
	if err:=s.persistLocked();err!=nil{delete(s.state.LocalPrestarts,attemptID);return err}
	return nil
}
// MarkLocalResumeInFlight closes the crash/restart replay window while the
// native provider is still blocking. Unlike a confirmed handoff, this state
// remains cancelable by its exact AttemptID.
func (s *LocalStore) MarkLocalResumeInFlight(attemptID, workerRef string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev,ok:=s.state.LocalPrestarts[attemptID]
	if !ok || prev.WorkerRef!=workerRef{return errors.New("node: local Resume prestart state missing")}
	if prev.Stage=="canceled"{return ErrLocalPrestartCanceled}
	if prev.Stage!="preparing"{return errors.New("node: native Resume already attempted")}
	next:=prev
	next.Stage="resuming"
	s.state.LocalPrestarts[attemptID]=next
	if err:=s.persistLocked();err!=nil{s.state.LocalPrestarts[attemptID]=prev;return err}
	return nil
}

func (s *LocalStore) CancelLocalPrestart(attemptID,workerRef string) (bool,error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev,ok:=s.state.LocalPrestarts[attemptID]
	if !ok || prev.WorkerRef!=workerRef{return false,nil}
	if prev.Stage=="canceled"{return true,nil}
	if prev.Stage!="preparing" && prev.Stage!="resuming"{return false,nil}
	next:=prev;next.Stage="canceled"
	s.state.LocalPrestarts[attemptID]=next
	if err:=s.persistLocked();err!=nil{s.state.LocalPrestarts[attemptID]=prev;return false,err}
	return true,nil
}
func (s *LocalStore) MarkLocalHandoff(attemptID,workerRef string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev,ok:=s.state.LocalPrestarts[attemptID]
	if !ok || prev.WorkerRef!=workerRef{return errors.New("node: local prestart state missing")}
	if prev.Stage=="canceled"{return ErrLocalPrestartCanceled}
	if prev.Stage!="preparing" && prev.Stage!="resuming"{return errors.New("node: local prestart handoff already attempted")}
	next:=prev;next.Stage="handoff"
	s.state.LocalPrestarts[attemptID]=next
	if err:=s.persistLocked();err!=nil{s.state.LocalPrestarts[attemptID]=prev;return err}
	return nil
}
func (s *LocalStore) ReleaseLocalPrestart(attemptID,workerRef string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev,ok:=s.state.LocalPrestarts[attemptID]
	if !ok || prev.WorkerRef!=workerRef || prev.Stage!="preparing"{return nil}
	delete(s.state.LocalPrestarts,attemptID)
	if err:=s.persistLocked();err!=nil{s.state.LocalPrestarts[attemptID]=prev;return err}
	return nil
}
func (s *LocalStore) LocalPrestartState(attemptID string) string {
	s.mu.Lock();defer s.mu.Unlock()
	return s.state.LocalPrestarts[attemptID].Stage
}

func (s *LocalStore) ClaimCommand(command Command) (CommandRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := command.Validate(command.Metadata().Node); err != nil {
		return CommandRecord{}, false, err
	}
	metadata := command.Metadata()
	if existing, ok := s.state.Commands[metadata.CommandID]; ok {
		if existing.Kind!=command.Kind{return CommandRecord{},true,errors.New("node: command replay kind mismatch")}
		if existing.State==CommandProcessing && existing.SafePrestart && existing.Replayable &&
			(command.Kind==CommandDispatch || command.Kind==CommandResume) {
			encoded,e:=commandJSON(command)
			if e!=nil{return CommandRecord{},true,e}
			if !bytes.Equal(encoded,existing.CommandJSON){
				// A JSON round-trip may normalize derived/default-valued wire
				// fields. Accept the canonicalized durable command, but never
				// accept a changed envelope under the same CommandID.
				durable,e:=commandFromJSON(existing.CommandJSON)
				if e!=nil{return CommandRecord{},true,e}
				canonical,e:=commandJSON(durable)
				if e!=nil{return CommandRecord{},true,e}
				if !bytes.Equal(encoded,canonical){return CommandRecord{},true,errors.New("node: mismatched replay envelope")}
			}
			previous:=existing
			existing.Replayable=false
			existing.UpdatedAt=time.Now().UTC()
			s.state.Commands[metadata.CommandID]=existing
			if e:=s.persistLocked();e!=nil{s.state.Commands[metadata.CommandID]=previous;return CommandRecord{},true,e}
			return existing,false,nil
		}
		if existing.State == CommandFailed && command.Kind == CommandRespondWorker {
			now := time.Now().UTC()
			existing.State = CommandProcessing
			existing.Outcome = CommandOutcome{CommandID: metadata.CommandID, Kind: command.Kind, State: CommandProcessing}
			existing.ClaimedAt = now
			existing.UpdatedAt = now
			s.state.Commands[metadata.CommandID] = existing
			if err := s.persistLocked(); err != nil {
				return CommandRecord{}, false, err
			}
			return existing, false, nil
		}
		return existing, true, nil
	}
	encoded, err := commandJSON(command)
	if err != nil {
		return CommandRecord{}, false, err
	}
	now := time.Now().UTC()
	record := CommandRecord{CommandID: metadata.CommandID, Kind: command.Kind, State: CommandProcessing, SafePrestart:hasPrestartArtifactReference(command), Outcome: CommandOutcome{CommandID: metadata.CommandID, Kind: command.Kind, State: CommandProcessing}, CommandJSON: encoded, ClaimedAt: now, UpdatedAt: now}
	s.state.Commands[record.CommandID] = record
	if err := s.persistLocked(); err != nil {
		delete(s.state.Commands, record.CommandID)
		return CommandRecord{}, false, err
	}
	return record, false, nil
}

// MarkNativeHandoff durably closes the pre-native replay window before
// any external native Start/Resume can produce an irreversible side effect.
func (s *LocalStore) MarkNativeHandoff(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e:=ctx.Err();e!=nil{return e}
	rec,ok:=s.state.Commands[id]
	if !ok || rec.State!=CommandProcessing{return errors.New("node: missing active command claim")}
	// Ordinary non-Artifact commands retain their existing unknown-execution
	// recovery behavior. They have no safe prestart marker to close.
	if !rec.SafePrestart{return nil}
	if rec.Replayable{return errors.New("node: pre-native command was not claimed for this execution")}
	prev:=rec
	rec.SafePrestart=false
	rec.UpdatedAt=time.Now().UTC()
	s.state.Commands[id]=rec
	if e:=s.persistLocked();e!=nil{s.state.Commands[id]=prev;return e}
	return nil
}

// YieldPrestart keeps the original claim/idempotency and makes it retryable.
func (s *LocalStore) YieldPrestart(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec,ok:=s.state.Commands[id]
	if !ok || rec.State!=CommandProcessing || !rec.SafePrestart{return nil}
	rec.Replayable=true
	rec.UpdatedAt=time.Now().UTC()
	s.state.Commands[id]=rec
	return s.persistLocked()
}
func (s *LocalStore) PendingPrestartCommands() ([]Command,error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for id,rec:=range s.state.Commands{
		if rec.State==CommandProcessing && rec.SafePrestart && rec.Replayable && (rec.Kind==CommandDispatch||rec.Kind==CommandResume){ids=append(ids,id)}
	}
	sort.Strings(ids)
	commands:=make([]Command,0,len(ids))
	for _,id:=range ids{
		cmd,e:=commandFromJSON(s.state.Commands[id].CommandJSON)
		if e!=nil{return nil,e}
		commands=append(commands,cmd)
	}
	return commands,nil
}
func (s *LocalStore) PrestartCanceled(attemptID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _,rec:=range s.state.Commands{
		if rec.State!=CommandFailed || rec.Outcome.ErrorCode!="canceled_before_handoff"{continue}
		if rec.Kind!=CommandDispatch && rec.Kind!=CommandResume{continue}
		cmd,e:=commandFromJSON(rec.CommandJSON)
		if e==nil && cmd.Metadata().AttemptID==attemptID{return true}
	}
	return false
}

func (s *LocalStore) SaveCommandReadiness(commandID string, outcome CommandOutcome) (CommandRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.state.Commands[commandID]
	if !ok || outcome.State != CommandAccepted || outcome.CommandID != commandID || outcome.Kind != record.Kind {
		return CommandRecord{}, errors.New("node: invalid command readiness")
	}
	if record.State != CommandProcessing {
		return record, nil
	}
	previous := record
	record.Outcome = outcome
	record.UpdatedAt = time.Now().UTC()
	s.state.Commands[commandID] = record
	if err := s.persistLocked(); err != nil {
		s.state.Commands[commandID] = previous
		return CommandRecord{}, err
	}
	return record, nil
}

// BeginImageResponse persists an irreversible guard BEFORE the first native
// image injection. A replay after an unconfirmed start requires reconciliation,
// never a second injection. The digest also fences different payloads and turns.
func (s *LocalStore) BeginImageResponse(requestID, attemptID, digest string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if requestID == "" || attemptID == "" || digest == "" {
		return "", errors.New("node: incomplete image-response identity")
	}
	if prior, ok := s.state.ImageResponses[requestID]; ok {
		if prior.AttemptID != attemptID || prior.Digest != digest {
			return "", errors.New("node: image-response identity mismatch")
		}
		return prior.Stage, nil
	}
	s.state.ImageResponses[requestID] = imageResponseCheckpoint{AttemptID: attemptID, Digest: digest, Stage: "started"}
	if err := s.persistLocked(); err != nil {
		delete(s.state.ImageResponses, requestID)
		return "", err
	}
	return "started-new", nil
}

func (s *LocalStore) ConfirmImageResponse(requestID, attemptID, digest string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prior, ok := s.state.ImageResponses[requestID]
	if !ok || prior.AttemptID != attemptID || prior.Digest != digest || prior.Stage != "started" {
		return errors.New("node: image-response injection checkpoint mismatch")
	}
	prior.Stage = "injected"
	s.state.ImageResponses[requestID] = prior
	if err := s.persistLocked(); err != nil {
		s.state.ImageResponses[requestID] = imageResponseCheckpoint{AttemptID: attemptID, Digest: digest, Stage: "started"}
		return err
	}
	return nil
}

func (s *LocalStore) ClaimWorkerResponse(requestID string) (CommandOutcome, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(requestID) == "" {
		return CommandOutcome{}, false, errors.New("node: worker response request_id is required")
	}
	if outcome, ok := s.state.WorkerResponses[requestID]; ok {
		if outcome.State == CommandFailed {
			outcome.State = CommandProcessing
			outcome.ErrorCode = ""
			outcome.ErrorMessage = ""
			s.state.WorkerResponses[requestID] = outcome
			if err := s.persistLocked(); err != nil {
				return CommandOutcome{}, false, err
			}
			return outcome, false, nil
		}
		if outcome.State == CommandProcessing {
			outcome.State = CommandInterrupted
			outcome.ErrorCode = "execution_state_unknown"
			s.state.WorkerResponses[requestID] = outcome
			if err := s.persistLocked(); err != nil {
				return CommandOutcome{}, false, err
			}
		}
		return outcome, true, nil
	}
	outcome := CommandOutcome{Kind: CommandRespondWorker, State: CommandProcessing}
	s.state.WorkerResponses[requestID] = outcome
	if err := s.persistLocked(); err != nil {
		delete(s.state.WorkerResponses, requestID)
		return CommandOutcome{}, false, err
	}
	return outcome, false, nil
}

func (s *LocalStore) RecoverWorkerResponse(requestID, commandID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	outcome, ok := s.state.WorkerResponses[requestID]
	if !ok || outcome.State != CommandProcessing {
		return nil
	}
	outcome.CommandID = commandID
	outcome.Kind = CommandRespondWorker
	outcome.State = CommandFailed
	outcome.ErrorCode = "execution_state_unknown"
	outcome.ErrorMessage = "Node could not prove that the worker response was delivered"
	s.state.WorkerResponses[requestID] = outcome
	return s.persistLocked()
}

func (s *LocalStore) CompleteWorkerResponse(requestID string, outcome CommandOutcome) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.state.WorkerResponses[requestID]; !ok {
		return errors.New("node: worker response claim not found")
	}
	s.state.WorkerResponses[requestID] = outcome
	return s.persistLocked()
}

func (s *LocalStore) CompleteCommand(commandID string, outcome CommandOutcome) (CommandRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.state.Commands[commandID]
	if !ok {
		return CommandRecord{}, errors.New("node: command claim not found")
	}
	if record.State != CommandProcessing {
		record.Outcome = sanitizeCommandOutcome(record.Outcome)
		s.state.Commands[commandID] = record
		if record.State == CommandFailed && (record.Kind == CommandDispatch || record.Kind == CommandResume || record.Kind == CommandSteering) {
			if _, err := s.queueCommandOutcomeLocked(record.Outcome); err != nil {
				return CommandRecord{}, err
			}
		} else if err := s.persistLocked(); err != nil {
			return CommandRecord{}, err
		}
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
	outcome = sanitizeCommandOutcome(outcome)
	record.State, record.Outcome, record.UpdatedAt = outcome.State, outcome, time.Now().UTC()
	s.state.Commands[commandID] = record
	if (outcome.State == CommandFailed || (outcome.State == CommandInterrupted && outcome.Kind == CommandSteering)) && (outcome.Kind == CommandDispatch || outcome.Kind == CommandResume || outcome.Kind == CommandSteering) {
		if _, err := s.queueCommandOutcomeLocked(outcome); err != nil {
			return CommandRecord{}, err
		}
	} else if err := s.persistLocked(); err != nil {
		return CommandRecord{}, err
	}
	return record, nil
}

func (s *LocalStore) CompleteCommandWithOutcome(commandID string, commandOutcome CommandOutcome, terminal core.AttemptOutcomeEnvelope) (CommandRecord, error) {
	if err := terminal.Validate(); err != nil {
		return CommandRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.state.Commands[commandID]
	if !ok {
		return CommandRecord{}, errors.New("node: command claim not found")
	}
	command, err := commandFromJSON(record.CommandJSON)
	if err != nil {
		return CommandRecord{}, err
	}
	metadata := command.Metadata()
	if (command.Kind != CommandDispatch && command.Kind != CommandResume) || (commandOutcome.State != CommandAccepted && commandOutcome.State != CommandInterrupted) ||
		commandOutcome.CommandID != commandID || commandOutcome.Kind != command.Kind || commandOutcome.TurnID != metadata.TurnID || commandOutcome.AttemptID != metadata.AttemptID ||
		(commandOutcome.State == CommandInterrupted && terminal.Status != core.OutcomeInterrupted) || terminal.Node != metadata.Node ||
		terminal.HarnessInstanceID != metadata.HarnessInstanceID || terminal.WorkerRef != metadata.WorkerRef ||
		terminal.TurnID != metadata.TurnID || terminal.AttemptID != metadata.AttemptID {
		return CommandRecord{}, errors.New("node: terminal outcome does not match the completed command")
	}
	commandOutcome = sanitizeCommandOutcome(commandOutcome)
	if record.State != CommandProcessing && record.State != CommandAccepted {
		return CommandRecord{}, errors.New("node: command is not eligible for terminal completion")
	}
	if !compatibleCommandCompletion(record.Outcome, commandOutcome) {
		return CommandRecord{}, errors.New("node: terminal completion conflicts with saved command outcome")
	}

	event := outcomeEvent(terminal)
	if err := event.Validate(); err != nil {
		return CommandRecord{}, err
	}
	previous := record
	previousNextSequence := s.state.NextSequence
	previousOutboxLength := len(s.state.Outbox)
	record.State, record.Outcome, record.UpdatedAt = commandOutcome.State, commandOutcome, time.Now().UTC()
	record.TerminalOutcomeQueued = true
	s.state.Commands[commandID] = record
	for _, pending := range s.state.Outbox {
		if pending.EventID == event.EventID {
			event.Sequence = pending.Sequence
			break
		}
	}
	if event.Sequence == 0 {
		event.Sequence = s.state.NextSequence
		if event.Sequence == 0 {
			event.Sequence = 1
		}
	}
	encodedEvent, err := json.Marshal(event)
	if err != nil {
		s.state.Commands[commandID] = previous
		return CommandRecord{}, err
	}
	pending, added, err := s.appendPendingEventLocked(event.EventID, "", encodedEvent)
	if err != nil {
		s.state.Commands[commandID] = previous
		s.state.NextSequence = previousNextSequence
		return CommandRecord{}, err
	}
	if !added {
		var saved NodeEvent
		if err := json.Unmarshal(pending.Payload, &saved); err != nil || saved.EventID != event.EventID || saved.Node != event.Node || saved.Kind != event.Kind || saved.Activity != nil || !reflect.DeepEqual(saved.Outcome, event.Outcome) {
			s.state.Commands[commandID] = previous
			return CommandRecord{}, errors.New("node: terminal event conflicts with saved outbox payload")
		}
	}
	if err := s.persistLocked(); err != nil {
		s.state.Commands[commandID] = previous
		s.state.Outbox = s.state.Outbox[:previousOutboxLength]
		s.state.NextSequence = previousNextSequence
		return CommandRecord{}, err
	}
	return record, nil
}

func compatibleCommandCompletion(existing, completed CommandOutcome) bool {
	if existing.State != CommandProcessing && existing.State != CommandAccepted {
		return false
	}
	return (existing.CommandID == "" || existing.CommandID == completed.CommandID) &&
		(existing.Kind == "" || existing.Kind == completed.Kind) &&
		(existing.TurnID == "" || existing.TurnID == completed.TurnID) &&
		(existing.AttemptID == "" || existing.AttemptID == completed.AttemptID)
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
	candidates := make([]CommandRecord, 0, 2)
	for _, record := range s.state.Commands {
		command, err := commandFromJSON(record.CommandJSON)
		if err != nil || (command.Kind != CommandDispatch && command.Kind != CommandResume) {
			continue
		}
		if command.Metadata().AttemptID == attemptID {
			candidates = append(candidates, record)
		}
	}
	if len(candidates) == 0 {
		return CommandRecord{}, errors.New("node: dispatch or resume command for attempt not found")
	}
	sort.Slice(candidates, func(i, j int) bool {
		if !candidates[i].UpdatedAt.Equal(candidates[j].UpdatedAt) {
			return candidates[i].UpdatedAt.After(candidates[j].UpdatedAt)
		}
		return candidates[i].CommandID < candidates[j].CommandID
	})
	return candidates[0], nil
}

func (s *LocalStore) lifecycleCommands(attemptID string) []CommandRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	var records []CommandRecord
	for _, record := range s.state.Commands {
		command, err := commandFromJSON(record.CommandJSON)
		if err == nil && (command.Kind == CommandDispatch || command.Kind == CommandResume) && command.Metadata().AttemptID == attemptID {
			records = append(records, record)
		}
	}
	return records
}

func (s *LocalStore) hasWorkerMapping(workerRef string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, mapping := range s.state.Mappings {
		if mapping.WorkerRef == workerRef {
			return true
		}
	}
	return false
}

func (s *LocalStore) nativeSessionOwnedBy(mapping sessionMapping) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, other := range s.state.Mappings {
		if other.RuntimeSessionID == mapping.RuntimeSessionID && (other.WorkerRef != mapping.WorkerRef || other.HarnessInstanceID != mapping.HarnessInstanceID) {
			return false
		}
	}
	return true
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

func (s *LocalStore) SavePendingRequest(requestID, attemptID string, kind ActivityKind, harnessInstanceID core.HarnessInstanceID) error {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" || strings.TrimSpace(attemptID) == "" {
		return errors.New("node: incomplete pending request mapping")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.state.PendingRequests[requestID]; ok {
		if existing.AttemptID != attemptID || existing.Kind != kind {
			return errors.New("node: pending request is bound to another attempt")
		}
		return nil
	}
	s.state.PendingRequests[requestID] = pendingRequestMapping{RequestID: requestID, AttemptID: attemptID, HarnessInstanceID: harnessInstanceID, Kind: kind}
	return s.persistLocked()
}

func (s *LocalStore) PendingRequests(attemptID string) []PendingRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]PendingRequest, 0)
	for requestID, mapping := range s.state.PendingRequests {
		if mapping.AttemptID == attemptID {
			result = append(result, PendingRequest{RequestID: requestID, Kind: mapping.Kind})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].RequestID < result[j].RequestID })
	return result
}

func (s *LocalStore) PendingRequestIDs(attemptID string) []string {
	pending := s.PendingRequests(attemptID)
	result := make([]string, 0, len(pending))
	for _, request := range pending {
		result = append(result, request.RequestID)
	}
	return result
}

func (s *LocalStore) ClearPendingRequest(requestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.state.PendingRequests[requestID]; !ok {
		return nil
	}
	delete(s.state.PendingRequests, requestID)
	return s.persistLocked()
}

func (s *LocalStore) NextEventSequence() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state.NextSequence == 0 {
		return 1
	}
	return s.state.NextSequence
}

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

func (s *LocalStore) QueueCommandOutcome(outcome CommandOutcome) (PendingEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queueCommandOutcomeLocked(outcome)
}

func (s *LocalStore) queueCommandOutcomeLocked(outcome CommandOutcome) (PendingEvent, error) {
	outcome = sanitizeCommandOutcome(outcome)
	if outcome.CommandID == "" || outcome.Kind == "" || outcome.State == CommandProcessing {
		return PendingEvent{}, errors.New("node: incomplete command outcome")
	}
	encoded, err := json.Marshal(outcome)
	if err != nil {
		return PendingEvent{}, err
	}
	return s.queuePendingEventLocked("command-outcome-"+outcome.CommandID, MessageCommandOutcome, encoded)
}

func (s *LocalStore) queueEvent(event NodeEvent) (PendingEvent, error) {
	if err := event.Validate(); err != nil {
		return PendingEvent{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	event.Sequence = s.state.NextSequence
	if event.Sequence == 0 {
		event.Sequence = 1
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		return PendingEvent{}, err
	}
	return s.queuePendingEventLocked(event.EventID, "", encoded)
}

func (s *LocalStore) queuePendingEventLocked(eventID string, kind MessageType, payload json.RawMessage) (PendingEvent, error) {
	pending, added, err := s.appendPendingEventLocked(eventID, kind, payload)
	if err != nil || !added {
		return pending, err
	}
	if err := s.persistLocked(); err != nil {
		s.state.Outbox = s.state.Outbox[:len(s.state.Outbox)-1]
		s.state.NextSequence = pending.Sequence
		return PendingEvent{}, err
	}
	return pending, nil
}

func (s *LocalStore) appendPendingEventLocked(eventID string, kind MessageType, payload json.RawMessage) (PendingEvent, bool, error) {
	for _, pending := range s.state.Outbox {
		if pending.EventID == eventID {
			return pending, false, nil
		}
	}
	sequence := s.state.NextSequence
	if sequence == 0 {
		sequence = 1
	}
	pending := PendingEvent{Type: kind, Sequence: sequence, EventID: eventID, Payload: payload, CreatedAt: time.Now().UTC()}
	s.state.NextSequence = sequence + 1
	s.state.Outbox = append(s.state.Outbox, pending)
	return pending, true, nil
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
		} else if strings.HasPrefix(event.EventID, "command-outcome-") {
			cmdID := strings.TrimPrefix(event.EventID, "command-outcome-")
			if rec, ok := s.state.Commands[cmdID]; ok {
				rec.Acknowledged = true
				s.state.Commands[cmdID] = rec
			}
		} else if strings.HasPrefix(event.EventID, "attempt-outcome-") {
			s.markAcceptedCommandTerminalQueuedLocked(event)
		}
	}
	s.state.Outbox = kept
	if sequence > s.state.LastAcknowledgedSequence {
		s.state.LastAcknowledgedSequence = sequence
	}
	return s.persistLocked()
}

func (s *LocalStore) markAcceptedCommandTerminalQueuedLocked(pending PendingEvent) {
	var event NodeEvent
	if json.Unmarshal(pending.Payload, &event) != nil || event.Outcome == nil || event.EventID != pending.EventID {
		return
	}
	for id, record := range s.state.Commands {
		if record.State != CommandAccepted || record.TerminalOutcomeQueued || (record.Kind != CommandDispatch && record.Kind != CommandResume) {
			continue
		}
		command, err := commandFromJSON(record.CommandJSON)
		if err != nil {
			continue
		}
		metadata := command.Metadata()
		outcome := event.Outcome
		if metadata.Node == outcome.Node && metadata.HarnessInstanceID == outcome.HarnessInstanceID && metadata.WorkerRef == outcome.WorkerRef && metadata.TurnID == outcome.TurnID && metadata.AttemptID == outcome.AttemptID {
			record.TerminalOutcomeQueued = true
			s.state.Commands[id] = record
		}
	}
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
		if record.State == CommandProcessing || (record.State == CommandAccepted && !record.TerminalOutcomeQueued && (record.Kind == CommandDispatch || record.Kind == CommandResume)) {
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
		command, err := commandFromJSON(record.CommandJSON)
		if err != nil {
			return err
		}
		metadata := command.Metadata()
		if command.Kind == CommandRespondWorker {
			if command.RespondWorker != nil {
				if err := s.RecoverWorkerResponse(command.RespondWorker.RequestID, record.CommandID); err != nil {
					return err
				}
			}
			outcome := CommandOutcome{CommandID: record.CommandID, Kind: record.Kind, State: CommandFailed, ErrorCode: "execution_state_unknown", ErrorMessage: "Node could not prove that the worker response was delivered"}
			if _, err := s.CompleteCommand(record.CommandID, outcome); err != nil {
				return err
			}
			continue
		}
		alive := false
		if inspector != nil {
			alive, err = inspector.Inspect(ctx, record)
			if err != nil {
				return err
			}
		}
		if alive {continue}
		if record.State==CommandProcessing && record.SafePrestart &&
			(command.Kind==CommandDispatch || command.Kind==CommandResume){
			if e:=s.YieldPrestart(record.CommandID);e!=nil{return e}
			continue
		}
		if command.Kind == CommandDispatch || command.Kind == CommandResume {
			if record.State == CommandAccepted {
				terminal, found, err := s.pendingTerminalOutcome(record.CommandID, metadata)
				if err != nil {
					return err
				}
				if found {
					if _, err := s.CompleteCommandWithOutcome(record.CommandID, acceptedOutcome(command), terminal); err != nil {
						return err
					}
					continue
				}
			}
			terminal := core.AttemptOutcomeEnvelope{EventID: "interrupted-" + record.CommandID, Node: metadata.Node, HarnessInstanceID: metadata.HarnessInstanceID, WorkerRef: metadata.WorkerRef, TurnID: metadata.TurnID, AttemptID: metadata.AttemptID, Status: core.OutcomeInterrupted, Classification: core.OutcomeFinal, Summary: "Attempt interrupted because execution state could not be proven", ErrorCode: "execution_state_unknown", OccurredAt: time.Now().UTC()}
			outcome := CommandOutcome{CommandID: record.CommandID, Kind: record.Kind, TurnID: metadata.TurnID, AttemptID: metadata.AttemptID, State: CommandInterrupted, ErrorCode: "execution_state_unknown", ErrorMessage: "Node could not prove that the side effect is still running"}
			if _, err := s.CompleteCommandWithOutcome(record.CommandID, outcome, terminal); err != nil {
				return err
			}
			continue
		}
		message := "Node could not prove that the side effect is still running"
		outcome := CommandOutcome{CommandID: record.CommandID, Kind: record.Kind, TurnID: metadata.TurnID, AttemptID: metadata.AttemptID, State: CommandInterrupted, ErrorCode: "execution_state_unknown", ErrorMessage: message}
		if command.Kind == CommandSteering {
			outcome.ErrorMessage = "Node could not prove whether steering reached the native runtime"
			outcome.AcceptanceUnknown = true
		}
		if _, err := s.CompleteCommand(record.CommandID, outcome); err != nil {
			return err
		}
	}
	s.mu.Lock()
	failedToRecover := make([]CommandOutcome, 0)
	for _, record := range s.state.Commands {
		if record.State == CommandFailed && (record.Kind == CommandDispatch || record.Kind == CommandResume || record.Kind == CommandSteering) && !record.Acknowledged {
			failedToRecover = append(failedToRecover, record.Outcome)
		}
	}
	for _, outcome := range failedToRecover {
		if _, err := s.queueCommandOutcomeLocked(outcome); err != nil {
			s.mu.Unlock()
			return err
		}
	}
	s.mu.Unlock()
	return nil
}

func (s *LocalStore) pendingTerminalOutcome(commandID string, metadata core.CommandMetadata) (core.AttemptOutcomeEnvelope, bool, error) {
	eventID := "attempt-outcome-" + metadata.AttemptID
	events, err := s.PendingEvents()
	if err != nil {
		return core.AttemptOutcomeEnvelope{}, false, err
	}
	for _, pending := range events {
		if pending.EventID != eventID {
			continue
		}
		var event NodeEvent
		if err := json.Unmarshal(pending.Payload, &event); err != nil {
			return core.AttemptOutcomeEnvelope{}, false, err
		}
		if err := event.Validate(); err != nil || event.Outcome == nil || event.Node != metadata.Node ||
			event.Outcome.EventID != eventID || event.Outcome.Node != metadata.Node ||
			event.Outcome.HarnessInstanceID != metadata.HarnessInstanceID || event.Outcome.WorkerRef != metadata.WorkerRef ||
			event.Outcome.TurnID != metadata.TurnID || event.Outcome.AttemptID != metadata.AttemptID {
			return core.AttemptOutcomeEnvelope{}, false, fmt.Errorf("node: saved terminal payload does not match command %s", commandID)
		}
		return *event.Outcome, true, nil
	}
	return core.AttemptOutcomeEnvelope{}, false, nil
}
