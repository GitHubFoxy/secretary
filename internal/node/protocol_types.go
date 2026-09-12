package node

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

const ProtocolVersion = 1

type MessageType string

const (
	MessageHandshake         MessageType = "handshake"
	MessageHandshakeAccepted MessageType = "handshake.accepted"
	MessageHeartbeat         MessageType = "heartbeat"
	MessageInventory         MessageType = "inventory"
	MessageCommandDispatch   MessageType = "command.dispatch"
	MessageCommandCancel     MessageType = "command.cancel"
	MessageCommandSteering   MessageType = "command.steering"
	MessageCommandResume     MessageType = "command.resume"
	MessageCommandRespond    MessageType = "command.respond_worker"
	MessageCommandOutcome    MessageType = "command.outcome"
	MessageActivity          MessageType = "activity"
	MessageAttemptOutcome    MessageType = "attempt.outcome"
	MessageEventAck          MessageType = "event.ack"
)

var knownMessageTypes = map[MessageType]bool{
	MessageHandshake: true, MessageHandshakeAccepted: true, MessageHeartbeat: true, MessageInventory: true,
	MessageCommandDispatch: true, MessageCommandCancel: true, MessageCommandSteering: true,
	MessageCommandResume: true, MessageCommandRespond: true, MessageCommandOutcome: true,
	MessageActivity: true, MessageAttemptOutcome: true, MessageEventAck: true,
}

// Envelope is the authenticated wire frame. Payloads are typed by Type.
type Envelope struct {
	Version   int                `json:"version"`
	Type      MessageType        `json:"type"`
	Node      core.NodeReference `json:"node"`
	Sequence  uint64             `json:"sequence,omitempty"`
	Ack       uint64             `json:"ack,omitempty"`
	Payload   json.RawMessage    `json:"payload"`
	Signature string             `json:"signature"`
}

func NewEnvelope(kind MessageType, node core.NodeReference, sequence, ack uint64, payload []byte, auth Authenticator) (Envelope, error) {
	e := Envelope{Version: ProtocolVersion, Type: kind, Node: node, Sequence: sequence, Ack: ack, Payload: append(json.RawMessage(nil), payload...)}
	if err := e.Validate(); err != nil {
		return Envelope{}, err
	}
	e.Signature = auth.Sign(e)
	return e, nil
}

func (e Envelope) Validate() error {
	if e.Version != ProtocolVersion {
		return fmt.Errorf("node protocol: unsupported version %d", e.Version)
	}
	if !knownMessageTypes[e.Type] {
		return fmt.Errorf("node protocol: unknown message type %q", e.Type)
	}
	if strings.TrimSpace(string(e.Node)) == "" {
		return errors.New("node protocol: node identity is required")
	}
	if len(bytes.TrimSpace(e.Payload)) == 0 || bytes.Equal(bytes.TrimSpace(e.Payload), []byte("null")) {
		return errors.New("node protocol: payload is required")
	}
	return nil
}

func DecodeEnvelope(encoded []byte) (Envelope, error) {
	var e Envelope
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&e); err != nil {
		return Envelope{}, fmt.Errorf("node protocol: decode envelope: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Envelope{}, errors.New("node protocol: trailing envelope data")
		}
		return Envelope{}, fmt.Errorf("node protocol: trailing envelope data: %w", err)
	}
	if err := e.Validate(); err != nil {
		return Envelope{}, err
	}
	if strings.TrimSpace(e.Signature) == "" {
		return Envelope{}, errors.New("node protocol: signature is required")
	}
	return e, nil
}

// Authenticator uses a per-Node secret. The signed input is deterministic and
// does not include the signature itself.
type Authenticator struct{ secret []byte }

func NewAuthenticator(secret []byte) Authenticator {
	return Authenticator{secret: append([]byte(nil), secret...)}
}

func (a Authenticator) Sign(e Envelope) string {
	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write([]byte(fmt.Sprintf("%d\n%s\n%s\n%d\n%d\n%s", e.Version, e.Type, e.Node, e.Sequence, e.Ack, e.Payload)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a Authenticator) SignNonce(node core.NodeReference, nonce string) string {
	mac := hmac.New(sha256.New, a.secret)
	_, _ = mac.Write([]byte("nonce\n" + string(node) + "\n" + nonce))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a Authenticator) VerifyNonce(node core.NodeReference, nonce, signature string) error {
	if len(a.secret) == 0 || !hmac.Equal([]byte(a.SignNonce(node, nonce)), []byte(signature)) {
		return errors.New("node protocol: nonce authentication failed")
	}
	return nil
}

func (a Authenticator) Verify(e Envelope) error {
	if err := e.Validate(); err != nil {
		return err
	}
	if len(a.secret) == 0 || strings.TrimSpace(e.Signature) == "" {
		return errors.New("node protocol: authentication failed")
	}
	expected := a.Sign(e)
	if !hmac.Equal([]byte(expected), []byte(e.Signature)) {
		return errors.New("node protocol: authentication failed")
	}
	return nil
}

type SequenceTracker struct {
	mu   sync.Mutex
	last uint64
}

func (t *SequenceTracker) Accept(e Envelope) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e.Sequence == 0 || e.Sequence <= t.last {
		return fmt.Errorf("node protocol: sequence %d is not after %d", e.Sequence, t.last)
	}
	t.last = e.Sequence
	return nil
}

func (t *SequenceTracker) Last() uint64 { t.mu.Lock(); defer t.mu.Unlock(); return t.last }
func (t *SequenceTracker) SetLast(sequence uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.last = sequence
}

type Workspace struct {
	ProjectID string `json:"project_id"`
	Path      string `json:"path"`
}

type Handshake struct {
	Node                     core.NodeReference            `json:"node"`
	ProtocolVersion          int                           `json:"protocol_version"`
	Capabilities             []string                      `json:"capabilities,omitempty"`
	Inventory                core.HarnessInventorySnapshot `json:"inventory"`
	Workspaces               []Workspace                   `json:"workspaces,omitempty"`
	Nonce                    string                        `json:"nonce"`
	NonceSignature           string                        `json:"nonce_signature"`
	LastAcknowledgedSequence uint64                        `json:"last_acknowledged_sequence"`
}

func (h Handshake) Validate() error {
	if strings.TrimSpace(string(h.Node)) == "" || h.ProtocolVersion != ProtocolVersion || strings.TrimSpace(h.Nonce) == "" || strings.TrimSpace(h.NonceSignature) == "" {
		return errors.New("node protocol: invalid handshake")
	}
	if err := h.Inventory.Validate(); err != nil {
		return fmt.Errorf("node protocol: invalid inventory: %w", err)
	}
	if h.Inventory.Node != h.Node {
		return errors.New("node protocol: handshake inventory belongs to another node")
	}
	return nil
}

type HandshakeAccepted struct {
	Node               core.NodeReference `json:"node"`
	ProtocolVersion    int                `json:"protocol_version"`
	ReplayFromSequence uint64             `json:"replay_from_sequence"`
	PolicyVersion      string             `json:"policy_version,omitempty"`
}

type ActiveAttempt = core.NodeActiveAttempt

type Heartbeat struct {
	Node                 core.NodeReference            `json:"node"`
	Online               bool                          `json:"online"`
	ActiveAttempts       []ActiveAttempt               `json:"active_attempts,omitempty"`
	Capacity             int                           `json:"capacity"`
	Inventory            core.HarnessInventorySnapshot `json:"inventory"`
	LastProcessedCommand string                        `json:"last_processed_command,omitempty"`
	ResourceSummary      map[string]string             `json:"resource_summary,omitempty"`
}

type WorkerEnvelope struct {
	WorkerRef           string               `json:"worker_ref"`
	TurnID              string               `json:"turn_id"`
	AttemptID           string               `json:"attempt_id"`
	OriginalUserIntent  string               `json:"original_user_intent"`
	NormalizedGoal      string               `json:"normalized_goal,omitempty"`
	CompletionContract  string               `json:"completion_contract,omitempty"`
	ConversationContext []string             `json:"conversation_context,omitempty"`
	ProjectID           string               `json:"project_id,omitempty"`
	ProjectSnapshot     core.ProjectSnapshot `json:"project_snapshot,omitempty"`
	Workspace           string               `json:"workspace"`
	HarnessInstance     core.HarnessInstance `json:"harness_instance"`
	Model               string               `json:"model,omitempty"`
	Reasoning           string               `json:"reasoning,omitempty"`
	AllowedTools        []string             `json:"allowed_tools,omitempty"`
	Constraints         []string             `json:"constraints,omitempty"`
	ApprovalPolicy      string               `json:"approval_policy,omitempty"`
	Profile             ManagedProfile       `json:"profile,omitempty"`
}

func (w WorkerEnvelope) ValidateAgainstInventory(node core.NodeReference, inventory core.HarnessInventorySnapshot) error {
	if err := w.Validate(node); err != nil {
		return err
	}
	if inventory.Node != node {
		return errors.New("node protocol: inventory belongs to another node")
	}
	if err := inventory.Validate(); err != nil {
		return fmt.Errorf("node protocol: invalid observed inventory: %w", err)
	}
	observed, err := core.ValidateHarnessSelection(inventory, w.HarnessInstance.ID, w.Model, w.Reasoning)
	if err != nil {
		return fmt.Errorf("node protocol: selected HarnessInstance is not available: %w", err)
	}
	if !reflect.DeepEqual(observed, w.HarnessInstance) {
		return errors.New("node protocol: Worker envelope does not match observed HarnessInstance")
	}
	return nil
}

func (w WorkerEnvelope) Validate(node core.NodeReference) error {
	if strings.TrimSpace(w.WorkerRef) == "" || strings.TrimSpace(w.TurnID) == "" || strings.TrimSpace(w.AttemptID) == "" || strings.TrimSpace(w.OriginalUserIntent) == "" {
		return errors.New("node protocol: incomplete Worker envelope")
	}
	if err := w.HarnessInstance.Validate(); err != nil {
		return err
	}
	if w.HarnessInstance.Node != node {
		return errors.New("node protocol: Worker envelope is bound to another node")
	}
	if err := w.HarnessInstance.ValidatePins(w.Model, w.Reasoning); err != nil {
		return fmt.Errorf("node protocol: %w", err)
	}
	if w.ProjectID != "" {
		if w.ProjectSnapshot.ID == "" {
			return errors.New("node protocol: Project policy snapshot is required")
		}
		if w.ProjectSnapshot.ID != w.ProjectID {
			return errors.New("node protocol: Project snapshot identity mismatch")
		}
		if w.ProjectSnapshot.Node != node || !reflect.DeepEqual(w.ProjectSnapshot.HarnessInstance, w.HarnessInstance) {
			return errors.New("node protocol: Project snapshot binding mismatch")
		}
		modelPin := w.ProjectSnapshot.Policy.ModelPin()
		if modelPin != "" && w.Model == "" {
			return errors.New("node protocol: execution model pin is required")
		}
		if modelPin != "" && modelPin != w.Model {
			return errors.New("node protocol: model pin differs from Project policy")
		}
		if w.ProjectSnapshot.Policy.Reasoning != "" && w.Reasoning == "" {
			return errors.New("node protocol: execution reasoning pin is required")
		}
		if w.ProjectSnapshot.Policy.Reasoning != "" && w.ProjectSnapshot.Policy.Reasoning != w.Reasoning {
			return errors.New("node protocol: reasoning pin differs from Project policy")
		}
		if w.ProjectSnapshot.Policy.EffectiveExecution().RequireApproval && w.ApprovalPolicy != "required" {
			return errors.New("node protocol: approval policy is required for this Project")
		}
		if !w.ProjectSnapshot.Policy.EffectiveExecution().RequireApproval && w.ApprovalPolicy == "required" {
			return errors.New("node protocol: approval policy conflicts with Project policy")
		}
		if err := w.ProjectSnapshot.Validate(); err != nil {
			return fmt.Errorf("node protocol: invalid Project snapshot: %w", err)
		}
	}
	return nil
}

type DispatchCommand struct {
	Metadata core.CommandMetadata `json:"metadata"`
	Envelope WorkerEnvelope       `json:"envelope"`
}
type CancelCommand struct {
	Metadata core.CommandMetadata `json:"metadata"`
	Reason   string               `json:"reason,omitempty"`
}
type SteeringCommand struct {
	Metadata core.CommandMetadata `json:"metadata"`
	Text     string               `json:"text"`
}
type ResumeCommand struct {
	Metadata core.CommandMetadata `json:"metadata"`
	Envelope WorkerEnvelope       `json:"envelope"`
}
type RespondWorkerCommand struct {
	Metadata  core.CommandMetadata `json:"metadata"`
	RequestID string               `json:"request_id"`
	Response  string               `json:"response"`
}

type CommandKind string

const (
	CommandDispatch      CommandKind = "dispatch"
	CommandCancel        CommandKind = "cancel"
	CommandSteering      CommandKind = "steering"
	CommandResume        CommandKind = "resume"
	CommandRespondWorker CommandKind = "respond_worker"
)

type Command struct {
	Kind          CommandKind
	Dispatch      *DispatchCommand
	Cancel        *CancelCommand
	Steering      *SteeringCommand
	Resume        *ResumeCommand
	RespondWorker *RespondWorkerCommand
}

func (c Command) Metadata() core.CommandMetadata {
	switch c.Kind {
	case CommandDispatch:
		if c.Dispatch != nil {
			return c.Dispatch.Metadata
		}
	case CommandCancel:
		if c.Cancel != nil {
			return c.Cancel.Metadata
		}
	case CommandSteering:
		if c.Steering != nil {
			return c.Steering.Metadata
		}
	case CommandResume:
		if c.Resume != nil {
			return c.Resume.Metadata
		}
	case CommandRespondWorker:
		if c.RespondWorker != nil {
			return c.RespondWorker.Metadata
		}
	}
	return core.CommandMetadata{}
}
func (c Command) Validate(node core.NodeReference) error {
	metadata := c.Metadata()
	if err := metadata.Validate(); err != nil {
		return err
	}
	if metadata.Node != node {
		return errors.New("node protocol: command is bound to another node")
	}
	switch c.Kind {
	case CommandDispatch:
		if c.Dispatch == nil {
			return errors.New("node protocol: dispatch payload is required")
		}
		if c.Dispatch.Metadata.HarnessInstanceID != c.Dispatch.Envelope.HarnessInstance.ID {
			return errors.New("node protocol: dispatch metadata binding mismatch")
		}
		return c.Dispatch.Envelope.Validate(node)
	case CommandCancel:
		if c.Cancel == nil {
			return errors.New("node protocol: cancel payload is required")
		}
	case CommandSteering:
		if c.Steering == nil || strings.TrimSpace(c.Steering.Text) == "" {
			return errors.New("node protocol: steering text is required")
		}
	case CommandResume:
		if c.Resume == nil {
			return errors.New("node protocol: resume payload is required")
		}
		if c.Resume.Metadata.HarnessInstanceID != c.Resume.Envelope.HarnessInstance.ID {
			return errors.New("node protocol: resume metadata binding mismatch")
		}
		return c.Resume.Envelope.Validate(node)
	case CommandRespondWorker:
		if c.RespondWorker == nil || strings.TrimSpace(c.RespondWorker.RequestID) == "" {
			return errors.New("node protocol: response request id is required")
		}
		if strings.TrimSpace(c.RespondWorker.Response) == "" {
			return errors.New("node protocol: typed worker response is required")
		}
	default:
		return fmt.Errorf("node protocol: unknown command %q", c.Kind)
	}
	return nil
}

type CommandState string

const (
	CommandProcessing  CommandState = "processing"
	CommandAccepted    CommandState = "accepted"
	CommandFailed      CommandState = "failed"
	CommandInterrupted CommandState = "interrupted"
)

type CommandOutcome struct {
	CommandID    string       `json:"command_id"`
	Kind         CommandKind  `json:"kind"`
	State        CommandState `json:"state"`
	ErrorCode    string       `json:"error_code,omitempty"`
	ErrorMessage string       `json:"error_message,omitempty"`
}

type CommandRecord struct {
	CommandID   string          `json:"command_id"`
	Kind        CommandKind     `json:"kind"`
	State       CommandState    `json:"state"`
	Outcome     CommandOutcome  `json:"outcome"`
	CommandJSON json.RawMessage `json:"command_json,omitempty"`
	ClaimedAt   time.Time       `json:"claimed_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type NodeEvent struct {
	EventID  string                       `json:"event_id"`
	Node     core.NodeReference           `json:"node"`
	Kind     string                       `json:"kind"`
	Sequence uint64                       `json:"sequence,omitempty"`
	Activity *core.Activity               `json:"activity,omitempty"`
	Outcome  *core.AttemptOutcomeEnvelope `json:"outcome,omitempty"`
}

func (e NodeEvent) Validate() error {
	if strings.TrimSpace(e.EventID) == "" || strings.TrimSpace(string(e.Node)) == "" || strings.TrimSpace(e.Kind) == "" {
		return errors.New("node protocol: incomplete node event")
	}
	if (e.Activity == nil) == (e.Outcome == nil) {
		return errors.New("node protocol: event must contain one payload")
	}
	if e.Outcome != nil {
		return e.Outcome.Validate()
	}
	return nil
}

type PendingEvent struct {
	Sequence  uint64          `json:"sequence"`
	EventID   string          `json:"event_id"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

func commandJSON(c Command) ([]byte, error) {
	type wire struct {
		Kind          CommandKind           `json:"kind"`
		Dispatch      *DispatchCommand      `json:"dispatch,omitempty"`
		Cancel        *CancelCommand        `json:"cancel,omitempty"`
		Steering      *SteeringCommand      `json:"steering,omitempty"`
		Resume        *ResumeCommand        `json:"resume,omitempty"`
		RespondWorker *RespondWorkerCommand `json:"respond_worker,omitempty"`
	}
	return json.Marshal(wire{c.Kind, c.Dispatch, c.Cancel, c.Steering, c.Resume, c.RespondWorker})
}

func commandFromJSON(data []byte) (Command, error) {
	var w struct {
		Kind          CommandKind           `json:"kind"`
		Dispatch      *DispatchCommand      `json:"dispatch"`
		Cancel        *CancelCommand        `json:"cancel"`
		Steering      *SteeringCommand      `json:"steering"`
		Resume        *ResumeCommand        `json:"resume"`
		RespondWorker *RespondWorkerCommand `json:"respond_worker"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return Command{}, err
	}
	return Command{Kind: w.Kind, Dispatch: w.Dispatch, Cancel: w.Cancel, Steering: w.Steering, Resume: w.Resume, RespondWorker: w.RespondWorker}, nil
}

func outcomeEvent(outcome core.AttemptOutcomeEnvelope) NodeEvent {
	return NodeEvent{EventID: outcome.EventID, Node: outcome.Node, Kind: "attempt.outcome", Outcome: &outcome}
}
