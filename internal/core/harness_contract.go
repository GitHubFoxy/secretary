package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// NodeReference and HarnessInstanceID are opaque references owned by the
// server. A native runtime session is deliberately not part of this contract.
type NodeReference string
type HarnessInstanceID string
type HarnessKind string
type ObservedModelID string
type ObservedReasoningLevel string

const (
	HarnessClaudeCode HarnessKind = "claude_code"
	HarnessCodex      HarnessKind = "codex"
	HarnessFX         HarnessKind = "fx"
	HarnessOpenCode   HarnessKind = "opencode"
)

// HarnessAuthentication is observed by the adapter, not inferred by policy.
type HarnessAuthentication struct {
	Authenticated bool   `json:"authenticated"`
	Method        string `json:"method,omitempty"`
}

type HarnessStatus string

const (
	HarnessUnknown     HarnessStatus = "unknown"
	HarnessReady       HarnessStatus = "ready"
	HarnessDegraded    HarnessStatus = "degraded"
	HarnessUnavailable HarnessStatus = "unavailable"
)

// ExecutionCapability names operations the Node can perform for this instance.
type ExecutionCapability string

const (
	CapabilityShell     ExecutionCapability = "shell"
	CapabilityEdit      ExecutionCapability = "edit"
	CapabilityCancel    ExecutionCapability = "cancel"
	CapabilitySteering  ExecutionCapability = "steering"
	CapabilityApprovals ExecutionCapability = "approvals"
)

// ActivityCapability names normalized observations the adapter can actually
// produce. Unsupported observations must not be synthesized.
type ActivityCapability string

const (
	ActivitySessionStarted     ActivityCapability = "session_started"
	ActivityThinkingSummary    ActivityCapability = "thinking_summary"
	ActivityAssistantTextDelta ActivityCapability = "assistant_text_delta"
	ActivityToolCall           ActivityCapability = "tool_call"
	ActivityToolResult         ActivityCapability = "tool_result"
	ActivitySubagentStarted    ActivityCapability = "subagent_started"
	ActivitySubagentProgress   ActivityCapability = "subagent_progress"
	ActivitySubagentCompleted  ActivityCapability = "subagent_completed"
	ActivityPermissionRequest  ActivityCapability = "permission_request"
	ActivityUserInputRequest   ActivityCapability = "user_input_request"
	ActivityProgress           ActivityCapability = "progress"
	ActivityStatus             ActivityCapability = "status"
	ActivityAttemptOutcome     ActivityCapability = "attempt_outcome"

	ActivityInternalSubagentStarted   = ActivitySubagentStarted
	ActivityInternalSubagentProgress  = ActivitySubagentProgress
	ActivityInternalSubagentCompleted = ActivitySubagentCompleted
)

// HarnessCapabilities keeps execution controls separate from normalized
// activity observations.
type HarnessCapabilities struct {
	Execution []ExecutionCapability `json:"execution"`
	Activity  []ActivityCapability  `json:"activity"`
}

func (c HarnessCapabilities) SupportsExecution(want ExecutionCapability) bool {
	for _, got := range c.Execution {
		if got == want {
			return true
		}
	}
	return false
}

func (c HarnessCapabilities) SupportsActivity(want ActivityCapability) bool {
	for _, got := range c.Activity {
		if got == want {
			return true
		}
	}
	return false
}

// HarnessInstance is one observed Node/harness combination. Model IDs and
// reasoning levels are adapter observations, not a server-wide catalog.
type HarnessInstance struct {
	ID              HarnessInstanceID        `json:"id"`
	Node            NodeReference            `json:"node"`
	Kind            HarnessKind              `json:"harness"`
	Version         string                   `json:"version"`
	Authentication  HarnessAuthentication    `json:"authentication"`
	Status          HarnessStatus            `json:"status"`
	Capabilities    HarnessCapabilities      `json:"capabilities"`
	ModelIDs        []ObservedModelID        `json:"model_ids"`
	ReasoningLevels []ObservedReasoningLevel `json:"reasoning_levels"`
}

// HarnessInventorySnapshot is a point-in-time observed inventory for one Node.
type HarnessInventorySnapshot struct {
	Node       NodeReference     `json:"node"`
	Instances  []HarnessInstance `json:"instances"`
	ObservedAt time.Time         `json:"observed_at"`
}

// HarnessInventory is the short name used by Node adapters.
type HarnessInventory = HarnessInventorySnapshot

func (h HarnessInstance) Validate() error {
	if strings.TrimSpace(string(h.ID)) == "" || strings.TrimSpace(string(h.Node)) == "" || strings.TrimSpace(string(h.Kind)) == "" {
		return fmt.Errorf("core: HarnessInstance id, Node and harness kind are required")
	}
	if strings.TrimSpace(h.Version) == "" && h.Status != HarnessUnavailable {
		return fmt.Errorf("core: HarnessInstance version is required unless unavailable")
	}
	return validateCapabilities(h.Capabilities)
}

// ErrHarnessUnavailable is returned when an observed instance cannot accept
// work. Its state remains visible in inventory instead of falling back.
var ErrHarnessUnavailable = errors.New("core: harness instance is unavailable")

// ErrObservedPinUnavailable is returned when a requested pin was not observed
// by the selected HarnessInstance.
var ErrObservedPinUnavailable = errors.New("core: requested harness pin is unavailable")

func (h HarnessInstance) Available() bool {
	return h.Status == HarnessReady && h.Authentication.Authenticated && strings.TrimSpace(h.Version) != ""
}

func (h HarnessInstance) SupportsModel(model ObservedModelID) bool {
	for _, observed := range h.ModelIDs {
		if observed == model {
			return true
		}
	}
	return false
}

func (h HarnessInstance) SupportsReasoning(reasoning ObservedReasoningLevel) bool {
	for _, observed := range h.ReasoningLevels {
		if observed == reasoning {
			return true
		}
	}
	return false
}

// ValidatePins checks only explicit pins. Empty values mean that policy did
// not pin that dimension, while a non-empty value must be observed locally.
func (h HarnessInstance) ValidatePins(model string, reasoning string) error {
	if !h.Available() {
		return fmt.Errorf("%w: %s", ErrHarnessUnavailable, h.ID)
	}
	if model != "" && !h.SupportsModel(ObservedModelID(model)) {
		return fmt.Errorf("%w: model %q is not observed by %s", ErrObservedPinUnavailable, model, h.ID)
	}
	if reasoning != "" && !h.SupportsReasoning(ObservedReasoningLevel(reasoning)) {
		return fmt.Errorf("%w: reasoning %q is not observed by %s", ErrObservedPinUnavailable, reasoning, h.ID)
	}
	return nil
}

func (h HarnessInstance) ValidateSelection(model string, reasoning string) error {
	if err := h.Validate(); err != nil {
		return err
	}
	return h.ValidatePins(model, reasoning)
}

func (s HarnessInventorySnapshot) Instance(id HarnessInstanceID) (HarnessInstance, bool) {
	for _, instance := range s.Instances {
		if instance.ID == id {
			return instance, true
		}
	}
	return HarnessInstance{}, false
}

func (s HarnessInventorySnapshot) ValidateSelection(id HarnessInstanceID, model string, reasoning string) error {
	if err := s.Validate(); err != nil {
		return err
	}
	instance, ok := s.Instance(id)
	if !ok {
		return fmt.Errorf("%w: HarnessInstance %q was not observed on %s", ErrHarnessUnavailable, id, s.Node)
	}
	return instance.ValidateSelection(model, reasoning)
}

// ValidateHarnessSelection is the server-side selection boundary. Routing
// policy chooses an instance elsewhere, while this function only checks that
// the chosen observed record can honor explicit pins.
func ValidateHarnessSelection(inventory HarnessInventorySnapshot, id HarnessInstanceID, model string, reasoning string) (HarnessInstance, error) {
	if err := inventory.Validate(); err != nil {
		return HarnessInstance{}, err
	}
	instance, ok := inventory.Instance(id)
	if !ok {
		return HarnessInstance{}, fmt.Errorf("%w: HarnessInstance %q was not observed on %s", ErrHarnessUnavailable, id, inventory.Node)
	}
	if err := instance.ValidateSelection(model, reasoning); err != nil {
		return HarnessInstance{}, err
	}
	return instance, nil
}

func (s HarnessInventorySnapshot) Validate() error {
	if strings.TrimSpace(string(s.Node)) == "" {
		return fmt.Errorf("core: inventory Node is required")
	}
	if s.ObservedAt.IsZero() {
		return fmt.Errorf("core: inventory observation time is required")
	}
	seen := make(map[HarnessInstanceID]struct{}, len(s.Instances))
	for _, instance := range s.Instances {
		if err := instance.Validate(); err != nil {
			return err
		}
		if instance.Node != s.Node {
			return fmt.Errorf("core: HarnessInstance %q belongs to Node %q, inventory is for %q", instance.ID, instance.Node, s.Node)
		}
		if _, ok := seen[instance.ID]; ok {
			return fmt.Errorf("core: duplicate HarnessInstance %q", instance.ID)
		}
		seen[instance.ID] = struct{}{}
	}
	return nil
}

func validateCapabilities(capabilities HarnessCapabilities) error {
	seenExecution := make(map[ExecutionCapability]struct{}, len(capabilities.Execution))
	for _, capability := range capabilities.Execution {
		if strings.TrimSpace(string(capability)) == "" {
			return fmt.Errorf("core: empty execution capability")
		}
		if _, ok := seenExecution[capability]; ok {
			return fmt.Errorf("core: duplicate execution capability %q", capability)
		}
		seenExecution[capability] = struct{}{}
	}
	seenActivity := make(map[ActivityCapability]struct{}, len(capabilities.Activity))
	for _, capability := range capabilities.Activity {
		if strings.TrimSpace(string(capability)) == "" {
			return fmt.Errorf("core: empty activity capability")
		}
		if _, ok := seenActivity[capability]; ok {
			return fmt.Errorf("core: duplicate activity capability %q", capability)
		}
		seenActivity[capability] = struct{}{}
	}
	return nil
}

type ActivityKind = ActivityCapability

const (
	ActivityKindSessionStarted     ActivityKind = ActivitySessionStarted
	ActivityKindThinkingSummary    ActivityKind = ActivityThinkingSummary
	ActivityKindAssistantTextDelta ActivityKind = ActivityAssistantTextDelta
	ActivityKindToolCall           ActivityKind = ActivityToolCall
	ActivityKindToolResult         ActivityKind = ActivityToolResult
	ActivityKindSubagentStarted    ActivityKind = ActivitySubagentStarted
	ActivityKindSubagentProgress   ActivityKind = ActivitySubagentProgress
	ActivityKindSubagentCompleted  ActivityKind = ActivitySubagentCompleted
	ActivityKindPermissionRequest  ActivityKind = ActivityPermissionRequest
	ActivityKindUserInputRequest   ActivityKind = ActivityUserInputRequest
	ActivityKindProgress           ActivityKind = ActivityProgress
	ActivityKindStatus             ActivityKind = ActivityStatus
	ActivityKindAttemptOutcome     ActivityKind = ActivityAttemptOutcome
)

// ActivityMetadata identifies an observed event without exposing a native
// runtime session identifier.
type ActivityMetadata struct {
	EventID           string            `json:"event_id"`
	Node              NodeReference     `json:"node"`
	HarnessInstanceID HarnessInstanceID `json:"harness_instance_id"`
	WorkerRef         string            `json:"worker_ref"`
	TurnID            string            `json:"turn_id"`
	AttemptID         string            `json:"attempt_id"`
	Sequence          uint64            `json:"sequence"`
	ObservedAt        time.Time         `json:"observed_at"`
	CorrelationID     string            `json:"correlation_id,omitempty"`
}

// Validate checks the identity and ordering fields required to transport an
// observed activity safely from Node to server.
func (m ActivityMetadata) Validate() error {
	if strings.TrimSpace(m.EventID) == "" {
		return fmt.Errorf("core: activity event id is required")
	}
	if strings.TrimSpace(string(m.Node)) == "" {
		return fmt.Errorf("core: activity Node is required")
	}
	if strings.TrimSpace(string(m.HarnessInstanceID)) == "" {
		return fmt.Errorf("core: activity HarnessInstance is required")
	}
	if strings.TrimSpace(m.AttemptID) == "" {
		return fmt.Errorf("core: activity Attempt is required")
	}
	if m.Sequence == 0 {
		return fmt.Errorf("core: activity sequence must be greater than zero")
	}
	if m.ObservedAt.IsZero() {
		return fmt.Errorf("core: activity observation time is required")
	}
	return nil
}

type ToolCall struct {
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type ToolResult struct {
	CallID string `json:"call_id,omitempty"`
	Name   string `json:"name,omitempty"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

type SubagentActivity struct {
	SubagentID string `json:"subagent_id"`
	Summary    string `json:"summary,omitempty"`
}

type ActivityRequest struct {
	RequestID    string     `json:"request_id"`
	Summary      string     `json:"summary"`
	RiskCategory string     `json:"risk_category,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

type ActivityProgressData struct {
	Message string   `json:"message"`
	Percent *float64 `json:"percent,omitempty"`
}

// Activity is a normalized observed event. Only the payload matching Kind is
// populated by an adapter.
type Activity struct {
	Metadata   ActivityMetadata        `json:"metadata"`
	Kind       ActivityKind            `json:"kind"`
	Text       string                  `json:"text,omitempty"`
	ToolCall   *ToolCall               `json:"tool_call,omitempty"`
	ToolResult *ToolResult             `json:"tool_result,omitempty"`
	Subagent   *SubagentActivity       `json:"subagent,omitempty"`
	Request    *ActivityRequest        `json:"request,omitempty"`
	Progress   *ActivityProgressData   `json:"progress,omitempty"`
	Status     string                  `json:"status,omitempty"`
	Outcome    *AttemptOutcomeEnvelope `json:"outcome,omitempty"`
}

func (a Activity) Validate(capabilities HarnessCapabilities) error {
	if err := a.Metadata.Validate(); err != nil {
		return err
	}
	capability, ok := activityCapabilityFor(a.Kind)
	if !ok {
		return fmt.Errorf("core: unknown activity kind %q", a.Kind)
	}
	if !capabilities.SupportsActivity(capability) {
		return fmt.Errorf("core: activity %q is not observed by this HarnessInstance", a.Kind)
	}
	return a.validatePayload()
}

// ValidatePayload checks the normalized kind and matching payload without
// consulting mutable current inventory. Replay uses this for events observed
// under an older HarnessInstance capability snapshot.
func (a Activity) ValidatePayload() error {
	if err := a.Metadata.Validate(); err != nil {
		return err
	}
	if _, ok := activityCapabilityFor(a.Kind); !ok {
		return fmt.Errorf("core: unknown activity kind %q", a.Kind)
	}
	return a.validatePayload()
}

func (a Activity) validatePayload() error {
	switch a.Kind {
	case ActivityKindThinkingSummary, ActivityKindAssistantTextDelta:
		if strings.TrimSpace(a.Text) == "" {
			return fmt.Errorf("core: activity %q requires text", a.Kind)
		}
	case ActivityKindToolCall:
		if a.ToolCall == nil || strings.TrimSpace(a.ToolCall.Name) == "" {
			return fmt.Errorf("core: tool_call activity requires a named tool call")
		}
	case ActivityKindToolResult:
		if a.ToolResult == nil || strings.TrimSpace(a.ToolResult.Name) == "" {
			return fmt.Errorf("core: tool_result activity requires a named tool result")
		}
	case ActivityKindSubagentStarted, ActivityKindSubagentProgress, ActivityKindSubagentCompleted:
		if a.Subagent == nil {
			return fmt.Errorf("core: subagent activity requires subagent data")
		}
	case ActivityKindPermissionRequest, ActivityKindUserInputRequest:
		if a.Request == nil || strings.TrimSpace(a.Request.RequestID) == "" {
			return fmt.Errorf("core: request activity requires request_id and request data")
		}
	case ActivityKindProgress:
		if a.Progress == nil {
			return fmt.Errorf("core: progress activity requires progress data")
		}
	case ActivityKindStatus:
		if strings.TrimSpace(a.Status) == "" {
			return fmt.Errorf("core: status activity requires status")
		}
	case ActivityKindAttemptOutcome:
		if a.Outcome == nil {
			return fmt.Errorf("core: attempt_outcome activity requires outcome")
		}
		if err := a.Outcome.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ValidateFor additionally binds an activity to the exact observed instance
// that produced it, preventing cross-Node or cross-harness delivery.
func (a Activity) ValidateFor(instance HarnessInstance) error {
	if err := a.Validate(instance.Capabilities); err != nil {
		return err
	}
	if a.Metadata.Node != instance.Node {
		return fmt.Errorf("core: activity Node %q does not match HarnessInstance Node %q", a.Metadata.Node, instance.Node)
	}
	if a.Metadata.HarnessInstanceID != instance.ID {
		return fmt.Errorf("core: activity HarnessInstance %q does not match %q", a.Metadata.HarnessInstanceID, instance.ID)
	}
	return nil
}

func activityCapabilityFor(kind ActivityKind) (ActivityCapability, bool) {
	switch kind {
	case ActivityKindSessionStarted,
		ActivityKindThinkingSummary,
		ActivityKindAssistantTextDelta,
		ActivityKindToolCall,
		ActivityKindToolResult,
		ActivityKindSubagentStarted,
		ActivityKindSubagentProgress,
		ActivityKindSubagentCompleted,
		ActivityKindPermissionRequest,
		ActivityKindUserInputRequest,
		ActivityKindProgress,
		ActivityKindStatus,
		ActivityKindAttemptOutcome:
		return ActivityCapability(kind), true
	default:
		return "", false
	}
}

// ArtifactRef identifies a durable result reference produced by the Node,
// such as a commit or a workspace path.
type ArtifactRef struct {
	Kind string `json:"kind"`
	Ref  string `json:"ref"`
}

func (r ArtifactRef) Validate() error {
	if strings.TrimSpace(r.Kind) == "" || strings.TrimSpace(r.Ref) == "" {
		return fmt.Errorf("core: artifact reference kind and ref are required")
	}
	return nil
}

// AttemptOutcomeEnvelope is the Node transport form of a terminal Attempt
// observation. The durable core AttemptOutcome remains a separate record.
type AttemptOutcomeEnvelope struct {
	EventID           string                `json:"event_id"`
	Node              NodeReference         `json:"node"`
	HarnessInstanceID HarnessInstanceID     `json:"harness_instance_id"`
	WorkerRef         string                `json:"worker_ref"`
	TurnID            string                `json:"turn_id"`
	AttemptID         string                `json:"attempt_id"`
	Status            AttemptOutcomeStatus  `json:"status"`
	Classification    OutcomeClassification `json:"classification"`
	Summary           string                `json:"summary,omitempty"`
	ErrorCode         string                `json:"error_code,omitempty"`
	ErrorMessage      string                `json:"error_message,omitempty"`
	Diagnostics       string                `json:"diagnostics,omitempty"`
	FailureCode       string                `json:"failure_code,omitempty"`
	ArtifactRefs      []ArtifactRef         `json:"artifact_refs,omitempty"`
	CorrelationID     string                `json:"correlation_id,omitempty"`
	OccurredAt        time.Time             `json:"occurred_at"`
}

func (o AttemptOutcomeEnvelope) Validate() error {
	if strings.TrimSpace(o.AttemptID) == "" || !o.Status.Valid() || !o.Classification.Valid() {
		return fmt.Errorf("core: complete AttemptOutcome envelope is required")
	}
	if o.Status == OutcomeInterrupted && o.Classification != OutcomeFinal {
		return fmt.Errorf("core: interrupted outcome must be final")
	}
	if o.Status == OutcomeSucceeded && o.Classification != OutcomeFinal {
		return fmt.Errorf("core: succeeded outcome must be final")
	}
	if o.Status == OutcomeCanceled && o.Classification == OutcomeRetryable {
		return fmt.Errorf("core: canceled outcome cannot be retryable")
	}
	if o.Classification == OutcomeFinal && strings.TrimSpace(o.Summary) == "" {
		return fmt.Errorf("core: final outcome summary is required")
	}
	for _, artifact := range o.ArtifactRefs {
		if err := artifact.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// CommandMetadata is shared by server-to-Node commands. It contains routing
// and idempotency metadata only, never credentials or callback capabilities.
type CommandMetadata struct {
	CommandID         string            `json:"command_id"`
	Node              NodeReference     `json:"node"`
	HarnessInstanceID HarnessInstanceID `json:"harness_instance_id,omitempty"`
	WorkerRef         string            `json:"worker_ref,omitempty"`
	TurnID            string            `json:"turn_id,omitempty"`
	AttemptID         string            `json:"attempt_id,omitempty"`
	CorrelationID     string            `json:"correlation_id,omitempty"`
	CausationID       string            `json:"causation_id,omitempty"`
	IssuedAt          time.Time         `json:"issued_at"`
}

func (m CommandMetadata) Validate() error {
	if strings.TrimSpace(m.CommandID) == "" || strings.TrimSpace(string(m.Node)) == "" {
		return fmt.Errorf("core: command id and Node are required")
	}
	return nil
}

// HarnessPolicy contains server-owned selection constraints. It intentionally
// has no observed inventory, health, model catalog, or activity fields.
type HarnessPolicy struct {
	DefaultHarness                HarnessKind           `json:"default_harness,omitempty"`
	PreferredHarnesses            []HarnessKind         `json:"preferred_harnesses,omitempty"`
	AllowedHarnesses              []HarnessKind         `json:"allowed_harnesses,omitempty"`
	RequiredExecutionCapabilities []ExecutionCapability `json:"required_execution_capabilities,omitempty"`
	RequiredActivityCapabilities  []ActivityCapability  `json:"required_activity_capabilities,omitempty"`
	ModelID                       string                `json:"model_id,omitempty"`
	Reasoning                     string                `json:"reasoning,omitempty"`
}
