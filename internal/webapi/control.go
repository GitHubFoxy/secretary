package webapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

// ControlOptions contains server-owned operator actions. The normal User UI
// never receives these routes. The main process supplies callbacks so this
// package does not own config or runtime lifecycle state.
type ControlOptions struct {
	ConfigPath              string
	ConfigContent           func() (string, error)
	ConfigRevision          func() string
	RequireExpectedRevision bool
	ConfigSnapshot          func() any
	WriteConfig             func([]byte) error
	ApplyConfig             func([]byte) (any, error)
	ReloadConfig            func() (any, error)
	ProfileFiles            func() ([]ProfileFile, error)
	ApplyProfile            func(string, []byte) error
	RuntimeRestart          func(context.Context) error
	RetryTask               func(context.Context, string) (core.Task, error)
	CloseTask               func(context.Context, string) (core.CloseOutcome, error)
	RawLogDir               string
}

type ProfileFile struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	Hash      string `json:"hash"`
	Revision  string `json:"revision,omitempty"`
	Editable  bool   `json:"editable"`
	Runtime   string `json:"runtime"`
	Model     string `json:"model"`
	Reasoning string `json:"reasoning"`
}

type controlProfile struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	Hash      string `json:"hash"`
	Revision  string `json:"revision,omitempty"`
	Editable  bool   `json:"editable"`
	Runtime   string `json:"runtime"`
	Model     string `json:"model"`
	Reasoning string `json:"reasoning"`
}

type controlOverview struct {
	DebugOnly   bool              `json:"debug_only"`
	GeneratedAt time.Time         `json:"generated_at"`
	Nodes       []controlNode     `json:"nodes"`
	Workers     []controlWorker   `json:"workers"`
	Clients     []controlClient   `json:"clients"`
	Projects    []controlProject  `json:"projects"`
	Approvals   []controlApproval `json:"approvals"`
	Events      []controlEvent    `json:"events"`
	Deliveries  []controlDelivery `json:"deliveries"`
	Commands    []controlCommand  `json:"commands"`
	Config      any               `json:"config,omitempty"`
}

type controlNode struct {
	Node            string                 `json:"node"`
	Online          bool                   `json:"online"`
	Draining        bool                   `json:"draining"`
	Revoked         bool                   `json:"revoked"`
	EnrolledAt      time.Time              `json:"enrolled_at"`
	LastSeenAt      time.Time              `json:"last_seen_at,omitempty"`
	LastHeartbeatAt time.Time              `json:"last_heartbeat_at,omitempty"`
	Capacity        int                    `json:"capacity"`
	ActiveAttempts  []controlActiveAttempt `json:"active_attempts,omitempty"`
	LastCommandID   string                 `json:"last_command_id,omitempty"`
	Health          string                 `json:"health"`
	Inventory       controlInventory       `json:"inventory"`
}

type controlActiveAttempt struct {
	WorkerRef string `json:"worker_ref"`
	TurnID    string `json:"turn_id"`
	AttemptID string `json:"attempt_id"`
}

type controlInventory struct {
	Node       string           `json:"node"`
	ObservedAt time.Time        `json:"observed_at"`
	Instances  []controlHarness `json:"instances"`
}

type controlHarness struct {
	ID              string                   `json:"id"`
	Node            string                   `json:"node"`
	Harness         string                   `json:"harness"`
	Version         string                   `json:"version"`
	Authentication  controlAuthentication    `json:"authentication"`
	Status          string                   `json:"status"`
	Capabilities    core.HarnessCapabilities `json:"capabilities"`
	ModelIDs        []string                 `json:"model_ids"`
	ReasoningLevels []string                 `json:"reasoning_levels"`
}

type controlAuthentication struct {
	Authenticated bool   `json:"authenticated"`
	Method        string `json:"method,omitempty"`
}

type controlWorker struct {
	WorkerRef       string           `json:"worker_ref"`
	Title           string           `json:"title"`
	Status          string           `json:"status"`
	ProjectID       string           `json:"project_id"`
	Node            string           `json:"node"`
	HarnessInstance string           `json:"harness_instance"`
	Workspace       string           `json:"workspace,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
	Binding         controlBinding   `json:"binding"`
	Turns           []controlTurn    `json:"turns"`
	Attempts        []controlAttempt `json:"attempts"`
	AttemptOutcomes []controlOutcome `json:"attempt_outcomes"`
	Results         []controlResult  `json:"results"`
	Recovery        controlRecovery  `json:"recovery"`
}

type controlBinding struct {
	WorkerRef       string `json:"worker_ref"`
	Node            string `json:"node"`
	HarnessInstance string `json:"harness_instance"`
	ProjectID       string `json:"project_id"`
	Workspace       string `json:"workspace,omitempty"`
	Archived        bool   `json:"archived"`
}

type controlTurn struct {
	ID               string    `json:"id"`
	State            string    `json:"state"`
	CurrentAttemptID string    `json:"current_attempt_id,omitempty"`
	ResultID         string    `json:"result_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type controlAttempt struct {
	ID              string    `json:"id"`
	TurnID          string    `json:"turn_id"`
	Number          int       `json:"number"`
	Node            string    `json:"node"`
	HarnessInstance string    `json:"harness_instance"`
	State           string    `json:"state"`
	CorrelationID   string    `json:"correlation_id,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type controlOutcome struct {
	ID             string    `json:"id"`
	AttemptID      string    `json:"attempt_id"`
	Status         string    `json:"status"`
	Classification string    `json:"classification"`
	ErrorCode      string    `json:"error_code,omitempty"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	Diagnostics    string    `json:"diagnostics,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type controlResult struct {
	ID            string    `json:"id"`
	Status        string    `json:"status"`
	Summary       string    `json:"summary"`
	FailureCode   string    `json:"failure_code,omitempty"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type controlRecovery struct {
	State       string `json:"state"`
	Health      string `json:"health"`
	LastAttempt string `json:"last_attempt,omitempty"`
}

type controlClient struct {
	ID          string     `json:"id"`
	DeviceID    string     `json:"device_id"`
	DisplayName string     `json:"display_name"`
	Platform    string     `json:"platform"`
	Scopes      []string   `json:"scopes"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

type controlProject struct {
	ID          string                  `json:"id"`
	Name        string                  `json:"name"`
	Description string                  `json:"description,omitempty"`
	Mappings    []controlProjectMapping `json:"mappings"`
	Revision    int64                   `json:"revision"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`
}

type controlProjectMapping struct {
	Node string `json:"node"`
	Path string `json:"path"`
}

type controlApproval struct {
	ID            string     `json:"id"`
	WorkerID      string     `json:"worker_id"`
	TurnID        string     `json:"turn_id"`
	AttemptID     string     `json:"attempt_id"`
	Node          string     `json:"node"`
	ProjectID     string     `json:"project_id"`
	Kind          string     `json:"kind"`
	ActionSummary string     `json:"action_summary"`
	RiskCategory  string     `json:"risk_category,omitempty"`
	RequestedAt   time.Time  `json:"requested_at"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	State         string     `json:"state"`
	ResolvedAt    *time.Time `json:"resolved_at,omitempty"`
	AuditEventID  string     `json:"audit_event_id,omitempty"`
}

type controlEvent struct {
	ID            string    `json:"id"`
	Sequence      int64     `json:"sequence"`
	Kind          string    `json:"kind"`
	AggregateType string    `json:"aggregate_type,omitempty"`
	Source        string    `json:"source,omitempty"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	CausationID   string    `json:"causation_id,omitempty"`
	WorkerRef     string    `json:"worker_ref,omitempty"`
	AttemptID     string    `json:"attempt_id,omitempty"`
	Payload       any       `json:"payload,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type controlDelivery struct {
	ID          string     `json:"id"`
	EventID     string     `json:"event_id"`
	EntryID     string     `json:"entry_id,omitempty"`
	Target      string     `json:"target"`
	State       string     `json:"state"`
	RetryCount  int        `json:"retry_count"`
	LastError   string     `json:"last_error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
}

type controlCommand struct {
	ID         string    `json:"id"`
	Kind       string    `json:"kind"`
	DedupeKey  string    `json:"dedupe_key"`
	WorkerID   string    `json:"worker_id"`
	AttemptID  string    `json:"attempt_id"`
	State      string    `json:"state"`
	LastError  string    `json:"last_error,omitempty"`
	LeaseUntil time.Time `json:"lease_until,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// workerDiagnosticResponse is deliberately separate from WorkerDetails. Raw
// harness state is an opt-in observer surface and never part of normal UI DTOs.
type workerDiagnosticResponse struct {
	WorkerRef         string                    `json:"worker_ref"`
	Recovery          controlRecovery           `json:"recovery"`
	AttemptOutcomes   []controlOutcome          `json:"attempt_outcomes"`
	RawHarnessDetails []harnessDiagnosticDetail `json:"raw_harness_details"`
}

func (s *Server) ControlHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/control/overview", s.controlOverview)
	mux.HandleFunc("GET /v1/control/nodes", s.controlNodes)
	mux.HandleFunc("GET /v1/control/workers", s.controlWorkers)
	mux.HandleFunc("GET /v1/control/clients", s.controlClients)
	mux.HandleFunc("GET /v1/control/projects", s.controlProjects)
	mux.HandleFunc("GET /v1/control/approvals", s.controlApprovals)
	mux.HandleFunc("GET /v1/control/events", s.controlEvents)
	mux.HandleFunc("GET /v1/control/deliveries", s.controlDeliveries)
	mux.HandleFunc("GET /v1/control/commands", s.controlCommands)
	mux.HandleFunc("GET /v1/control/config", s.controlConfig)
	mux.HandleFunc("PUT /v1/control/config", s.controlConfigWrite)
	mux.HandleFunc("POST /v1/control/config/reload", s.controlConfigReload)
	mux.HandleFunc("GET /v1/control/profiles", s.controlProfiles)
	mux.HandleFunc("POST /v1/control/profiles/reload", s.controlProfilesReload)
	mux.HandleFunc("/v1/control/profiles/", s.controlProfileWrite)
	mux.HandleFunc("GET /v1/control/diagnostics/", s.controlDiagnostics)
	mux.HandleFunc("GET /v1/control/raw-log/", s.controlDiagnostics)
	mux.HandleFunc("/v1/control/clients/", s.controlClientAction)
	mux.HandleFunc("/v1/control/nodes/", s.controlNodeAction)
	mux.HandleFunc("GET /v1/control/export", s.controlExport)
	return mux
}

func (s *Server) controlAllowed(w http.ResponseWriter, r *http.Request) bool {
	if !s.debug {
		http.NotFound(w, r)
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		http.Error(w, "owner web session required", http.StatusUnauthorized)
		return false
	}
	person, err := s.store.WebSessionPerson(r.Context(), cookie.Value)
	if err != nil || person.ID != s.owner.ID {
		http.Error(w, "owner web session required", http.StatusUnauthorized)
		return false
	}
	return true
}

func (s *Server) controlOverview(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	overview, err := s.buildControlOverview(r.Context())
	if err != nil {
		http.Error(w, "read control room: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

func (s *Server) buildControlOverview(ctx context.Context) (controlOverview, error) {
	conversation, err := s.store.ConversationForPerson(ctx, s.owner.ID)
	if err != nil {
		return controlOverview{}, err
	}
	nodes, err := s.store.NodeRecords(ctx)
	if err != nil {
		return controlOverview{}, err
	}
	workers, err := s.store.WorkersForConversation(ctx, conversation.ID)
	if err != nil {
		return controlOverview{}, err
	}
	projects, err := s.store.Projects(ctx)
	if err != nil {
		return controlOverview{}, err
	}
	clients, err := s.store.Clients(ctx, s.owner.ID)
	if err != nil {
		return controlOverview{}, err
	}
	approvals, err := s.store.Approvals(ctx)
	if err != nil {
		return controlOverview{}, err
	}
	events, err := s.store.EventsRecent(ctx, 100)
	if err != nil {
		return controlOverview{}, err
	}
	deliveries, err := s.store.Deliveries(ctx, "", 100)
	if err != nil {
		return controlOverview{}, err
	}
	commands, err := s.store.WorkerCommands(ctx, 100)
	if err != nil {
		return controlOverview{}, err
	}
	result := controlOverview{DebugOnly: true, GeneratedAt: time.Now().UTC(), Nodes: controlNodesDTO(nodes), Workers: make([]controlWorker, 0, len(workers)), Clients: controlClientsDTO(clients), Projects: controlProjectsDTO(projects), Approvals: controlApprovalsDTO(approvals), Events: controlEventsDTO(events), Deliveries: controlDeliveriesDTO(deliveries), Commands: controlCommandsDTO(commands)}
	for _, worker := range workers {
		details, err := s.store.WorkerDetailsForConversation(ctx, conversation.ID, worker.WorkerRef)
		if err != nil {
			return controlOverview{}, err
		}
		result.Workers = append(result.Workers, controlWorkerDTO(details, nodes))
	}
	if s.control.ConfigSnapshot != nil {
		result.Config = sanitizeControlAny(s.control.ConfigSnapshot())
	}
	return result, nil
}

func controlNodesDTO(records []core.NodeRecord) []controlNode {
	result := make([]controlNode, 0, len(records))
	for _, record := range records {
		active := make([]controlActiveAttempt, 0, len(record.ActiveAttempts))
		for _, attempt := range record.ActiveAttempts {
			active = append(active, controlActiveAttempt{WorkerRef: attempt.WorkerRef, TurnID: attempt.TurnID, AttemptID: attempt.AttemptID})
		}
		harnesses := make([]controlHarness, 0, len(record.Inventory.Instances))
		for _, instance := range record.Inventory.Instances {
			models := make([]string, 0, len(instance.ModelIDs))
			for _, model := range instance.ModelIDs {
				models = append(models, string(model))
			}
			reasoning := make([]string, 0, len(instance.ReasoningLevels))
			for _, value := range instance.ReasoningLevels {
				reasoning = append(reasoning, string(value))
			}
			harnesses = append(harnesses, controlHarness{ID: string(instance.ID), Node: string(instance.Node), Harness: string(instance.Kind), Version: instance.Version, Authentication: controlAuthentication{Authenticated: instance.Authentication.Authenticated, Method: instance.Authentication.Method}, Status: string(instance.Status), Capabilities: instance.Capabilities, ModelIDs: models, ReasoningLevels: reasoning})
		}
		health := "offline"
		if record.Revoked {
			health = "revoked"
		} else if record.Draining {
			health = "draining"
		} else if record.Online {
			health = "healthy"
		}
		result = append(result, controlNode{Node: string(record.Node), Online: record.Online, Draining: record.Draining, Revoked: record.Revoked, EnrolledAt: record.EnrolledAt, LastSeenAt: record.LastSeenAt, LastHeartbeatAt: record.LastHeartbeatAt, Capacity: record.Capacity, ActiveAttempts: active, LastCommandID: record.LastProcessedCommand, Health: health, Inventory: controlInventory{Node: string(record.Inventory.Node), ObservedAt: record.Inventory.ObservedAt, Instances: harnesses}})
	}
	return result
}

func controlWorkerDTO(details core.WorkerDetails, nodes []core.NodeRecord) controlWorker {
	worker := details.Worker
	attempts := make([]controlAttempt, 0, len(details.Attempts))
	outcomes := make([]controlOutcome, 0, len(details.Outcomes))
	turns := make([]controlTurn, 0, len(details.Turns))
	results := make([]controlResult, 0, len(details.Results))
	for _, value := range details.Turns {
		turns = append(turns, controlTurn{ID: value.ID, State: string(value.State), CurrentAttemptID: value.CurrentAttemptID, ResultID: value.ResultID, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt})
	}
	for _, value := range details.Attempts {
		attempts = append(attempts, controlAttempt{ID: value.ID, TurnID: value.TurnID, Number: value.Number, Node: string(value.NodeID), HarnessInstance: string(value.HarnessInstanceID), State: string(value.State), CorrelationID: value.CorrelationID, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt})
	}
	for _, value := range details.Outcomes {
		outcomes = append(outcomes, controlOutcome{ID: value.ID, AttemptID: value.AttemptID, Status: string(value.Status), Classification: string(value.Classification), ErrorCode: sanitizeDiagnosticText(value.ErrorCode), ErrorMessage: sanitizeDiagnosticText(value.ErrorMessage), Diagnostics: sanitizeDiagnosticText(value.Diagnostics), CreatedAt: value.CreatedAt})
	}
	for _, value := range details.Results {
		results = append(results, controlResult{ID: value.ID, Status: string(value.Status), Summary: sanitizeDiagnosticText(value.Summary), FailureCode: sanitizeDiagnosticText(value.FailureCode), CorrelationID: value.CorrelationID, CreatedAt: value.CreatedAt})
	}
	health := "unknown"
	recoveryState := "idle"
	lastAttempt := ""
	if len(attempts) > 0 {
		current := attempts[len(attempts)-1]
		lastAttempt = current.ID
		if current.State == string(core.AttemptInterrupted) {
			recoveryState = "interrupted"
		} else if !core.AttemptState(current.State).Terminal() {
			recoveryState = "recovering"
			health = "unknown"
		} else {
			recoveryState = "complete"
		}
	}
	for _, node := range nodes {
		if node.Node == core.NodeReference(worker.NodeID) {
			if node.Revoked {
				health = "revoked"
			} else if node.Online {
				health = "healthy"
			} else if health == "unknown" {
				health = "offline"
			}
			break
		}
	}
	return controlWorker{WorkerRef: worker.WorkerRef, Title: sanitizeDiagnosticText(worker.Title), Status: string(worker.Status), ProjectID: worker.ProjectID, Node: worker.NodeID, HarnessInstance: worker.HarnessInstanceID, Workspace: worker.Workspace, CreatedAt: worker.CreatedAt, UpdatedAt: worker.UpdatedAt, Binding: controlBinding{WorkerRef: worker.WorkerRef, Node: worker.NodeID, HarnessInstance: worker.HarnessInstanceID, ProjectID: worker.ProjectID, Workspace: worker.Workspace, Archived: worker.Archived}, Turns: turns, Attempts: attempts, AttemptOutcomes: outcomes, Results: results, Recovery: controlRecovery{State: recoveryState, Health: health, LastAttempt: lastAttempt}}
}

func controlClientsDTO(values []core.Client) []controlClient {
	result := make([]controlClient, 0, len(values))
	for _, value := range values {
		scopes := make([]string, 0, len(value.Scopes))
		for _, scope := range value.Scopes {
			scopes = append(scopes, string(scope))
		}
		result = append(result, controlClient{ID: value.ID, DeviceID: value.DeviceID, DisplayName: sanitizeDiagnosticText(value.DisplayName), Platform: value.Platform, Scopes: scopes, Status: string(value.Status), CreatedAt: value.CreatedAt, RevokedAt: value.RevokedAt})
	}
	return result
}
func controlProjectsDTO(values []core.Project) []controlProject {
	result := make([]controlProject, 0, len(values))
	for _, value := range values {
		mappings := make([]controlProjectMapping, 0, len(value.Mappings))
		for _, mapping := range value.Mappings {
			mappings = append(mappings, controlProjectMapping{Node: string(mapping.Node), Path: mapping.Path})
		}
		result = append(result, controlProject{ID: value.ID, Name: sanitizeDiagnosticText(value.Name), Description: sanitizeDiagnosticText(value.Description), Mappings: mappings, Revision: value.Revision, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt})
	}
	return result
}
func controlApprovalsDTO(values []core.Approval) []controlApproval {
	result := make([]controlApproval, 0, len(values))
	for _, value := range values {
		result = append(result, controlApproval{ID: value.ID, WorkerID: value.WorkerID, TurnID: value.TurnID, AttemptID: value.AttemptID, Node: value.NodeID, ProjectID: value.ProjectID, Kind: string(value.Kind), ActionSummary: sanitizeDiagnosticText(value.ActionSummary), RiskCategory: sanitizeDiagnosticText(value.RiskCategory), RequestedAt: value.RequestedAt, ExpiresAt: value.ExpiresAt, State: string(value.State), ResolvedAt: value.ResolvedAt, AuditEventID: value.AuditEventID})
	}
	return result
}
func controlEventsDTO(values []core.Event) []controlEvent {
	result := make([]controlEvent, 0, len(values))
	for _, value := range values {
		result = append(result, controlEvent{ID: value.ID, Sequence: value.Seq, Kind: value.Kind, AggregateType: value.AggregateType, Source: value.Source, CorrelationID: value.CorrelationID, CausationID: value.CausationID, WorkerRef: value.WorkerRef, AttemptID: value.AttemptID, Payload: sanitizeControlJSON(value.Payload), CreatedAt: value.CreatedAt})
	}
	return result
}
func controlDeliveriesDTO(values []core.Delivery) []controlDelivery {
	result := make([]controlDelivery, 0, len(values))
	for _, value := range values {
		result = append(result, controlDelivery{ID: value.ID, EventID: value.EventID, EntryID: value.EntryID, Target: sanitizeDiagnosticText(value.Target), State: string(value.State), RetryCount: value.RetryCount, LastError: sanitizeDiagnosticText(value.LastError), CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt, DeliveredAt: value.DeliveredAt})
	}
	return result
}
func controlCommandsDTO(values []core.WorkerCommand) []controlCommand {
	result := make([]controlCommand, 0, len(values))
	for _, value := range values {
		result = append(result, controlCommand{ID: value.ID, Kind: value.Kind, DedupeKey: value.DedupeKey, WorkerID: value.WorkerID, AttemptID: value.AttemptID, State: string(value.State), LastError: sanitizeDiagnosticText(value.LastError), LeaseUntil: value.LeaseUntil, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt})
	}
	return result
}

func (s *Server) controlNodes(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	values, err := s.store.NodeRecords(r.Context())
	if err != nil {
		http.Error(w, "read Nodes", 500)
		return
	}
	writeJSON(w, 200, controlNodesDTO(values))
}
func (s *Server) controlWorkers(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	conversation, err := s.store.ConversationForPerson(r.Context(), s.owner.ID)
	if err != nil {
		http.Error(w, "read conversation", 500)
		return
	}
	workers, err := s.store.WorkersForConversation(r.Context(), conversation.ID)
	if err != nil {
		http.Error(w, "read Workers", 500)
		return
	}
	nodes, err := s.store.NodeRecords(r.Context())
	if err != nil {
		http.Error(w, "read Nodes", 500)
		return
	}
	result := make([]controlWorker, 0, len(workers))
	for _, worker := range workers {
		details, detailErr := s.store.WorkerDetailsForConversation(r.Context(), conversation.ID, worker.WorkerRef)
		if detailErr != nil {
			http.Error(w, "read Worker details", 500)
			return
		}
		result = append(result, controlWorkerDTO(details, nodes))
	}
	writeJSON(w, 200, result)
}
func (s *Server) controlClients(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	values, err := s.store.Clients(r.Context(), s.owner.ID)
	if err != nil {
		http.Error(w, "read Clients", 500)
		return
	}
	writeJSON(w, 200, controlClientsDTO(values))
}
func (s *Server) controlProjects(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	values, err := s.store.Projects(r.Context())
	if err != nil {
		http.Error(w, "read Projects", 500)
		return
	}
	writeJSON(w, 200, controlProjectsDTO(values))
}
func (s *Server) controlApprovals(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	values, err := s.store.Approvals(r.Context())
	if err != nil {
		http.Error(w, "read approvals", 500)
		return
	}
	writeJSON(w, 200, controlApprovalsDTO(values))
}
func (s *Server) controlDeliveries(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	limit, err := controlLimit(r)
	if err != nil {
		http.Error(w, "invalid limit", 400)
		return
	}
	values, err := s.store.Deliveries(r.Context(), "", limit)
	if err != nil {
		http.Error(w, "read deliveries", 500)
		return
	}
	writeJSON(w, 200, controlDeliveriesDTO(values))
}
func (s *Server) controlCommands(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	limit, err := controlLimit(r)
	if err != nil {
		http.Error(w, "invalid limit", 400)
		return
	}
	values, err := s.store.WorkerCommands(r.Context(), limit)
	if err != nil {
		http.Error(w, "read commands", 500)
		return
	}
	writeJSON(w, 200, controlCommandsDTO(values))
}

func controlLimit(r *http.Request) (int, error) {
	value := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			return 0, errors.New("invalid limit")
		}
		value = parsed
	}
	return value, nil
}

func (s *Server) controlEvents(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	after, err := parseControlTime(r.URL.Query().Get("after"))
	if err != nil {
		http.Error(w, "invalid after timestamp", 400)
		return
	}
	limit, err := controlLimit(r)
	if err != nil {
		http.Error(w, "invalid event limit", 400)
		return
	}
	values, err := s.store.EventsAfter(r.Context(), after, limit)
	if err != nil {
		http.Error(w, "read events", 500)
		return
	}
	writeJSON(w, 200, controlEventsDTO(values))
}

func (s *Server) controlClientAction(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/control/clients/"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "revoke" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	key, ok := requireIdempotencyKey(w, r, "")
	if !ok {
		return
	}
	client, err := s.store.RevokeClient(r.Context(), parts[0], key)
	if err != nil {
		writeClientMutationError(w, err)
		return
	}
	s.mu.Lock()
	s.cancelClientStreamsLocked(client.ID)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, controlClientsDTO([]core.Client{client})[0])
}
func (s *Server) controlNodeAction(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/control/nodes/"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] != "revoke" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	key, ok := requireIdempotencyKey(w, r, "")
	if !ok {
		return
	}
	record, err := s.store.RevokeNode(r.Context(), core.NodeReference(parts[0]), key)
	if err != nil {
		writeClientMutationError(w, err)
		return
	}
	_, _ = s.store.RecordEventWithMetadata(r.Context(), core.EventInput{Kind: "control.node_revoked", AggregateType: "node", AggregateID: string(record.Node), Source: "control-room", Payload: map[string]string{"node": string(record.Node)}})
	writeJSON(w, http.StatusOK, controlNodesDTO([]core.NodeRecord{record})[0])
}

func (s *Server) controlConfig(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	result := map[string]any{"path": s.control.ConfigPath, "editable": true}
	if s.control.ConfigContent != nil {
		content, err := s.control.ConfigContent()
		if err != nil {
			http.Error(w, "read config: "+err.Error(), 500)
			return
		}
		redacted := redactControlConfigText(content)
		result["content"] = redacted
		result["editable"] = redacted == content
		result["revision"] = s.controlRevision(content)
	}
	if s.control.ConfigSnapshot != nil {
		result["snapshot"] = sanitizeControlAny(s.control.ConfigSnapshot())
	}
	writeJSON(w, 200, result)
}

type controlWriteRequest struct {
	Content          string `json:"content"`
	TOML             string `json:"toml"`
	Markdown         string `json:"markdown"`
	ExpectedRevision string `json:"expected_revision"`
}

func (s *Server) controlConfigWrite(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	var request controlWriteRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	content := request.Content
	if content == "" {
		content = request.TOML
	}
	if content == "" {
		http.Error(w, "config content is required", 400)
		return
	}
	if hasControlRedactionMarker(content) {
		http.Error(w, "config contains redaction placeholders; reload and edit without opaque values", 400)
		return
	}
	s.controlWriteMu.Lock()
	defer s.controlWriteMu.Unlock()
	if revision, ok := s.currentConfigRevision(); ok {
		if request.ExpectedRevision == "" && s.control.RequireExpectedRevision {
			http.Error(w, "config expected_revision is required", http.StatusPreconditionRequired)
			return
		}
		if request.ExpectedRevision != "" && request.ExpectedRevision != revision {
			http.Error(w, "config revision is stale", http.StatusConflict)
			return
		}
	}
	var result any
	var err error
	if s.control.ApplyConfig != nil {
		result, err = s.control.ApplyConfig([]byte(content))
	} else if s.control.WriteConfig != nil {
		err = s.control.WriteConfig([]byte(content))
		if err == nil && s.control.ReloadConfig != nil {
			result, err = s.control.ReloadConfig()
		}
	} else {
		err = errors.New("config editing is not configured")
	}
	if err != nil {
		http.Error(w, "apply config: "+err.Error(), 400)
		return
	}
	_, _ = s.store.RecordEvent(r.Context(), "control.config_changed", "", "", "", map[string]string{"path": s.control.ConfigPath})
	writeJSON(w, 200, sanitizeControlAny(result))
}
func (s *Server) controlRevision(content string) string {
	if s.control.ConfigRevision != nil {
		if revision := strings.TrimSpace(s.control.ConfigRevision()); revision != "" {
			return revision
		}
	}
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}

func (s *Server) currentConfigRevision() (string, bool) {
	if s.control.ConfigRevision != nil {
		if revision := strings.TrimSpace(s.control.ConfigRevision()); revision != "" {
			return revision, true
		}
	}
	if s.control.ConfigContent == nil {
		return "", false
	}
	content, err := s.control.ConfigContent()
	if err != nil {
		return "", false
	}
	return s.controlRevision(content), true
}

func hasControlRedactionMarker(content string) bool {
	lower := strings.ToLower(content)
	for _, marker := range []string{"[redacted]", "[secret]", "[opaque]", "<redacted>", "<secret>"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return containsKnownControlCredential(content)
}

func (s *Server) controlConfigReload(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	if s.control.ReloadConfig == nil {
		http.Error(w, "config reload is not configured", 501)
		return
	}
	result, err := s.control.ReloadConfig()
	if err != nil {
		http.Error(w, "reload config: "+err.Error(), 400)
		return
	}
	_, _ = s.store.RecordEvent(r.Context(), "control.config_reloaded", "", "", "", map[string]string{"path": s.control.ConfigPath})
	writeJSON(w, 200, sanitizeControlAny(result))
}
func (s *Server) controlProfiles(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	if s.control.ProfileFiles == nil {
		http.Error(w, "profile editing is not configured", 501)
		return
	}
	profiles, err := s.control.ProfileFiles()
	if err != nil {
		http.Error(w, "read profiles: "+err.Error(), 500)
		return
	}
	result := make([]controlProfile, 0, len(profiles))
	for _, profile := range profiles {
		result = append(result, controlProfileDTO(profile))
	}
	writeJSON(w, 200, result)
}

func controlProfileDTO(profile ProfileFile) controlProfile {
	redactedContent := redactControlProfileText(profile.Content)
	revision := profile.Revision
	if revision == "" {
		revision = profile.Hash
	}
	return controlProfile{
		Name: profile.Name, Path: profile.Path, Content: redactedContent, Hash: profile.Hash, Revision: revision,
		Editable: redactedContent == profile.Content,
		Runtime:  redactControlProfileMetadata(profile.Runtime), Model: redactControlProfileMetadata(profile.Model), Reasoning: redactControlProfileMetadata(profile.Reasoning),
	}
}
func (s *Server) controlProfilesReload(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	if s.control.ReloadConfig == nil {
		http.Error(w, "profile reload is not configured", 501)
		return
	}
	result, err := s.control.ReloadConfig()
	if err != nil {
		http.Error(w, "reload profiles: "+err.Error(), 400)
		return
	}
	_, _ = s.store.RecordEvent(r.Context(), "control.profiles_reloaded", "", "", "", map[string]string{})
	writeJSON(w, 200, sanitizeControlAny(result))
}
func (s *Server) controlProfileWrite(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", http.MethodPut)
		http.Error(w, "method not allowed", 405)
		return
	}
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/control/profiles/"), "/")
	if !validProfileName(name) {
		http.Error(w, "invalid profile name", 400)
		return
	}
	if s.control.ApplyProfile == nil {
		http.Error(w, "profile editing is not configured", 501)
		return
	}
	var request controlWriteRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	content := request.Content
	if content == "" {
		content = request.Markdown
	}
	if strings.TrimSpace(content) == "" {
		http.Error(w, "profile content is required", 400)
		return
	}
	if hasControlRedactionMarker(content) {
		http.Error(w, "profile contains redaction placeholders; reload and edit without opaque values", 400)
		return
	}
	s.controlWriteMu.Lock()
	defer s.controlWriteMu.Unlock()
	if revision, ok := s.currentProfileRevision(name); ok {
		if request.ExpectedRevision == "" && s.control.RequireExpectedRevision {
			http.Error(w, "profile expected_revision is required", http.StatusPreconditionRequired)
			return
		}
		if request.ExpectedRevision != "" && request.ExpectedRevision != revision {
			http.Error(w, "profile revision is stale", http.StatusConflict)
			return
		}
	}
	if err := s.control.ApplyProfile(name, []byte(content)); err != nil {
		http.Error(w, "apply profile: "+err.Error(), 400)
		return
	}
	_, _ = s.store.RecordEvent(r.Context(), "control.profile_changed", "", "", "", map[string]string{"profile": name})
	writeJSON(w, 200, map[string]string{"name": name, "status": "updated"})
}
func validProfileName(name string) bool {
	return name == "secretary" || name == "worker" || name == "child_worker"
}

func (s *Server) currentProfileRevision(name string) (string, bool) {
	if s.control.ProfileFiles == nil {
		return "", false
	}
	profiles, err := s.control.ProfileFiles()
	if err != nil {
		return "", false
	}
	for _, profile := range profiles {
		if profile.Name != name {
			continue
		}
		if profile.Revision != "" {
			return profile.Revision, true
		}
		if profile.Hash != "" {
			return profile.Hash, true
		}
		return s.controlRevision(profile.Content), true
	}
	return "", false
}

func (s *Server) controlDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	workerRef := strings.Trim(strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/"), "/v1/control/diagnostics/"), "/")
	if strings.HasPrefix(r.URL.Path, "/v1/control/raw-log/") {
		workerRef = strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/control/raw-log/"), "/")
	}
	if workerRef == "" || workerRef != filepath.Base(workerRef) || strings.ContainsAny(workerRef, `/\\`) {
		http.Error(w, "invalid worker reference", 400)
		return
	}
	conversation, err := s.store.ConversationForPerson(r.Context(), s.owner.ID)
	if err != nil {
		http.Error(w, "read conversation", 500)
		return
	}
	details, err := s.store.WorkerDetailsForConversation(r.Context(), conversation.ID, workerRef)
	if err != nil {
		http.Error(w, "read Worker", 404)
		return
	}
	diagnostics, err := s.buildWorkerDiagnostics(details)
	if err != nil {
		http.Error(w, "read diagnostics", 500)
		return
	}
	writeJSON(w, 200, diagnostics)
}
func (s *Server) buildWorkerDiagnostics(details core.WorkerDetails) (workerDiagnosticResponse, error) {
	outcomes := make([]controlOutcome, 0, len(details.Outcomes))
	for _, value := range details.Outcomes {
		outcomes = append(outcomes, controlOutcome{ID: value.ID, AttemptID: value.AttemptID, Status: string(value.Status), Classification: string(value.Classification), ErrorCode: sanitizeDiagnosticText(value.ErrorCode), ErrorMessage: sanitizeDiagnosticText(value.ErrorMessage), Diagnostics: sanitizeDiagnosticText(value.Diagnostics), CreatedAt: value.CreatedAt})
	}
	recovery := controlRecovery{State: "idle", Health: "unknown"}
	if len(details.Attempts) > 0 {
		attempt := details.Attempts[len(details.Attempts)-1]
		recovery.LastAttempt = attempt.ID
		recovery.State = string(attempt.State)
		if attempt.State == core.AttemptInterrupted {
			recovery.Health = "interrupted"
		} else if !attempt.State.Terminal() {
			recovery.Health = "unknown"
		} else {
			recovery.Health = "complete"
		}
	}
	raw := []harnessDiagnosticDetail{}
	if strings.TrimSpace(s.diagnosticLogDir) != "" {
		var err error
		raw, err = readHarnessDiagnostics(s.diagnosticLogDir, details.Worker.WorkerRef)
		if err != nil {
			return workerDiagnosticResponse{}, err
		}
	}
	return workerDiagnosticResponse{WorkerRef: details.Worker.WorkerRef, Recovery: recovery, AttemptOutcomes: outcomes, RawHarnessDetails: raw}, nil
}
func (s *Server) controlExport(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	overview, err := s.buildControlOverview(r.Context())
	if err != nil {
		http.Error(w, "read control room: "+err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="secretary-diagnostics.json"`)
	writeJSON(w, 200, overview)
}
func parseControlTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, value)
}

func sanitizeControlJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return "[redacted]"
	}
	return sanitizeControlAny(value)
}
func sanitizeControlAny(value any) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var decoded any
	if json.Unmarshal(encoded, &decoded) != nil {
		return nil
	}
	return sanitizeDiagnosticValue(decoded)
}

// redactControlText is kept as the config policy name for package-local callers.
// Diagnostic payloads must continue to use sanitizeDiagnosticValue instead. Its
// denylist intentionally has a different, stricter contract than editable text.
func redactControlText(value string) string {
	return redactControlConfigText(value)
}

func redactControlConfigText(value string) string {
	return redactControlTextLines(value)
}

func redactControlProfileText(value string) string {
	return redactControlTextLines(value)
}

func redactControlProfileMetadata(value string) string {
	if containsKnownControlCredential(value) {
		return "[redacted]"
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"native", "session", "callback", "task", "<think", "</think", "chain-of-thought", "chain_of_thought",
		"chain of thought", "internal reasoning", "thought process", "analysis:", "reasoning:", "thought:", "cot",
	} {
		if strings.Contains(lower, marker) {
			return "[redacted]"
		}
	}
	return value
}

func isControlProfileMetadataKey(key string) bool {
	key = strings.ToLower(strings.TrimSpace(key))
	return key == "runtime" || key == "model" || key == "reasoning"
}

func isControlProfileName(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "secretary", "worker", "child_worker":
		return true
	default:
		return false
	}
}

func redactControlTextLines(value string) string {
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		if controlTextSensitiveLine(line) {
			lines[i] = "[redacted]"
		}
	}
	return strings.Join(lines, "\n")
}

func controlTextSensitiveLine(line string) bool {
	key := controlTextAssignmentKey(line)
	if controlTextSensitiveKey(key) || controlTextThoughtKey(key) {
		return true
	}
	// Also reject an opaque value embedded in an otherwise ordinary value.
	// This catches credentials in safe product keys and free Markdown without
	// making ordinary `content`, `skills`, or `reasoning` fields read-only.
	if containsKnownControlCredential(line) {
		return true
	}
	lower := strings.ToLower(line)
	for _, marker := range []string{"<think", "</think", "chain-of-thought", "internal reasoning", "thought process"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func containsKnownControlCredential(value string) bool {
	lower := strings.ToLower(value)
	for _, prefix := range []string{"sk-", "ghp_", "xoxb-", "xoxb_"} {
		if strings.Contains(lower, prefix) {
			return true
		}
	}
	for _, key := range []string{"token", "secret", "credential"} {
		for _, separator := range []string{"=", ":"} {
			if containsControlAssignment(lower, key, separator) {
				return true
			}
		}
	}
	for _, marker := range []string{"api_key=", "apikey=", "access_token=", "password=", "callback=", "auth=", "session_id=", "session-id=", "native_id=", "task_id=", "bearer "} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func containsControlAssignment(value, key, separator string) bool {
	for start := 0; start < len(value); {
		index := strings.Index(value[start:], key)
		if index < 0 {
			return false
		}
		index += start + len(key)
		for index < len(value) && (value[index] == ' ' || value[index] == '\t') {
			index++
		}
		if strings.HasPrefix(value[index:], separator) {
			index += len(separator)
			if strings.TrimSpace(value[index:]) != "" {
				return true
			}
		}
		start = index
	}
	return false
}

func controlTextAssignmentKey(line string) string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "[") {
		return ""
	}
	if strings.HasPrefix(trimmed, "-") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
	}
	index := strings.IndexAny(trimmed, "=:")
	if index <= 0 {
		return ""
	}
	key := strings.TrimSpace(trimmed[:index])
	key = strings.Trim(key, "\\\"'")
	return key
}

func controlTextCompactKey(key string) string {
	return strings.ToLower(strings.Map(func(character rune) rune {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') {
			return character
		}
		return -1
	}, key))
}

func controlTextSensitiveKey(key string) bool {
	compact := controlTextCompactKey(key)
	if compact == "" {
		return false
	}
	for _, marker := range []string{"secret", "credential", "token", "password", "callback", "session", "native", "task", "authorization", "apikey", "accesskey", "privatekey", "bearer", "opaque"} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func controlTextThoughtKey(key string) bool {
	compact := controlTextCompactKey(key)
	return compact == "auth" || compact == "authentication" || strings.Contains(compact, "thought") || strings.Contains(compact, "chainofthought") || strings.Contains(compact, "analysis") || strings.Contains(compact, "cot")
}
