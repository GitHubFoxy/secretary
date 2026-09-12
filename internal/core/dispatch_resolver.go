package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Resolver errors distinguish a request that cannot be routed from a selected
// binding that is deliberately waiting for its Node. They are never a signal to
// silently select fx or another Node.
var (
	ErrNoProject               = errors.New("core: no Project selected")
	ErrNoEligibleNode          = errors.New("core: no eligible Node")
	ErrInvalidDispatchPin      = errors.New("core: invalid dispatch pin")
	ErrSelectedNodeUnavailable = errors.New("core: selected Node is unavailable")
	ErrMissingHarnessInventory = errors.New("core: HarnessInstance inventory is missing")
)

// DispatchResolutionRequest contains intent preferences, not a Project or
// HarnessInstance snapshot. The resolver always reads those observations from
// the server store before binding a Worker.
type DispatchResolutionRequest struct {
	ProjectID         string
	NodeID            NodeReference
	HarnessInstanceID HarnessInstanceID
	HarnessKind       HarnessKind
	Workspace         string
	ModelID           string
	Reasoning         string
	WorkerPolicy      HarnessPolicy
}

// DispatchResolution is the server-owned result used to create a Worker. A
// queued result has a fully immutable binding but its Node cannot start work
// right now.
type DispatchResolution struct {
	ProjectDispatch
	Queued bool

	nodeState dispatchNodeState
}

type dispatchNodeState struct {
	online        bool
	draining      bool
	revoked       bool
	inventoryJSON string
}

func (s *Store) ResolveDispatch(ctx context.Context, request DispatchResolutionRequest) (DispatchResolution, error) {
	if strings.TrimSpace(request.ProjectID) == "" {
		return DispatchResolution{}, ErrNoProject
	}
	project, err := s.Project(ctx, request.ProjectID)
	if err != nil {
		return DispatchResolution{}, err
	}
	model, reasoning, err := effectiveDispatchPins(project.Policy, request)
	if err != nil {
		return DispatchResolution{}, err
	}
	candidates, err := s.dispatchNodeCandidates(ctx, project, request.NodeID)
	if err != nil {
		return DispatchResolution{}, err
	}

	var firstUnavailable *DispatchResolution
	var firstError error
	for _, candidate := range candidates {
		// An instance ID is globally unique in observed inventory. Without an
		// explicit Node, ignore Nodes that did not report it and continue to its
		// actual owner.
		if request.NodeID == "" && request.HarnessInstanceID != "" {
			if _, found := candidate.record.Inventory.Instance(request.HarnessInstanceID); !found {
				continue
			}
		}
		resolved, available, err := resolveDispatchOnNode(project, candidate, request, model, reasoning)
		if err != nil {
			if firstError == nil {
				firstError = err
			}
			continue
		}
		if available {
			return resolved, nil
		}
		if firstUnavailable == nil {
			copy := resolved
			firstUnavailable = &copy
		}
	}
	if firstUnavailable != nil && (request.NodeID != "" || request.HarnessInstanceID != "") {
		return *firstUnavailable, nil
	}
	if firstError != nil {
		return DispatchResolution{}, firstError
	}
	if request.HarnessInstanceID != "" {
		return DispatchResolution{}, fmt.Errorf("%w: HarnessInstance %q was not observed for this Project", ErrInvalidDispatchPin, request.HarnessInstanceID)
	}
	return DispatchResolution{}, ErrNoEligibleNode
}

func effectiveDispatchPins(policy ProjectPolicy, request DispatchResolutionRequest) (string, string, error) {
	model := policy.ModelPin()
	if requested := strings.TrimSpace(request.ModelID); requested != "" {
		if model != "" && model != requested {
			return "", "", fmt.Errorf("%w: model is forbidden by Project policy", ErrInvalidDispatchPin)
		}
		model = requested
	}
	reasoning := policy.Reasoning
	if requested := strings.TrimSpace(request.Reasoning); requested != "" {
		if reasoning != "" && reasoning != requested {
			return "", "", fmt.Errorf("%w: reasoning is forbidden by Project policy", ErrInvalidDispatchPin)
		}
		reasoning = requested
	}
	return model, reasoning, nil
}

type dispatchNodeCandidate struct {
	record NodeRecord
	order  int
}

func (s *Store) dispatchNodeCandidates(ctx context.Context, project Project, explicit NodeReference) ([]dispatchNodeCandidate, error) {
	if explicit != "" {
		record, err := s.NodeRecord(ctx, explicit)
		if err != nil {
			return nil, err
		}
		if record.Revoked {
			return nil, ErrNodeRevoked
		}
		if _, ok := project.MappingForNode(explicit); !ok {
			return nil, fmt.Errorf("%w: %s", ErrProjectMappingMissing, explicit)
		}
		return []dispatchNodeCandidate{{record: record}}, nil
	}
	records, err := s.NodeRecords(ctx)
	if err != nil {
		return nil, err
	}
	preference := projectNodeOrder(project)
	rank := make(map[NodeReference]int, len(preference))
	for index, node := range preference {
		rank[node] = index
	}
	candidates := make([]dispatchNodeCandidate, 0, len(records))
	for _, record := range records {
		if record.Revoked || record.Draining {
			continue
		}
		if _, ok := project.MappingForNode(record.Node); !ok {
			continue
		}
		order, ok := rank[record.Node]
		if !ok {
			order = len(preference)
		}
		candidates = append(candidates, dispatchNodeCandidate{record: record, order: order})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].order != candidates[j].order {
			return candidates[i].order < candidates[j].order
		}
		return candidates[i].record.Node < candidates[j].record.Node
	})
	return candidates, nil
}

func projectNodeOrder(project Project) []NodeReference {
	result := make([]NodeReference, 0, len(project.Mappings)+1)
	appendUnique := func(node NodeReference) {
		if node == "" {
			return
		}
		for _, existing := range result {
			if existing == node {
				return
			}
		}
		result = append(result, node)
	}
	appendUnique(project.Policy.defaultNode())
	for _, node := range project.Policy.DefaultNodePolicy.Preferred {
		appendUnique(node)
	}
	mappings := append([]ProjectPathMapping(nil), project.Mappings...)
	sort.Slice(mappings, func(i, j int) bool { return mappings[i].node() < mappings[j].node() })
	for _, mapping := range mappings {
		appendUnique(mapping.node())
	}
	return result
}

func resolveDispatchOnNode(project Project, candidate dispatchNodeCandidate, request DispatchResolutionRequest, model, reasoning string) (DispatchResolution, bool, error) {
	record := candidate.record
	if record.Inventory.Node == "" || record.Inventory.ObservedAt.IsZero() {
		return DispatchResolution{}, false, fmt.Errorf("%w: Node %s", ErrMissingHarnessInventory, record.Node)
	}
	instances := orderedDispatchInstances(record.Inventory.Instances, request)
	if len(instances) == 0 {
		return DispatchResolution{}, false, fmt.Errorf("%w: requested HarnessInstance was not observed", ErrInvalidDispatchPin)
	}
	var firstError error
	for _, instance := range instances {
		if request.HarnessInstanceID != "" && instance.ID != request.HarnessInstanceID {
			continue
		}
		if request.HarnessKind != "" && instance.Kind != request.HarnessKind {
			continue
		}
		if err := instance.ValidatePins(model, reasoning); err != nil {
			if firstError == nil {
				firstError = fmt.Errorf("%w: %w", ErrInvalidDispatchPin, err)
			}
			continue
		}
		workspace := request.Workspace
		if workspace == "" {
			mapping, _ := project.MappingForNode(record.Node)
			workspace = mapping.Path
		}
		policy := project.Policy
		policy.ModelID, policy.Model, policy.Reasoning = model, "", reasoning
		projectForSelection := project
		projectForSelection.Policy = policy
		snapshot, err := projectForSelection.ValidateDispatch(record.Node, instance, workspace)
		if err != nil {
			if firstError == nil {
				firstError = err
			}
			continue
		}
		inventoryJSON, err := json.Marshal(record.Inventory)
		if err != nil {
			return DispatchResolution{}, false, err
		}
		resolved := DispatchResolution{
			ProjectDispatch: ProjectDispatch{Project: project, Node: record.Node, Workspace: workspace, HarnessInstance: instance, Snapshot: snapshot},
			nodeState:       dispatchNodeState{online: record.Online, draining: record.Draining, revoked: record.Revoked, inventoryJSON: string(inventoryJSON)},
		}
		available := record.Online && !record.Draining && !record.Revoked && record.Capacity > len(record.ActiveAttempts)
		resolved.Queued = !available
		return resolved, available, nil
	}
	if firstError != nil {
		return DispatchResolution{}, false, firstError
	}
	return DispatchResolution{}, false, fmt.Errorf("%w: no eligible HarnessInstance", ErrNoEligibleNode)
}

func orderedDispatchInstances(instances []HarnessInstance, request DispatchResolutionRequest) []HarnessInstance {
	ordered := append([]HarnessInstance(nil), instances...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	if request.HarnessInstanceID != "" {
		return ordered
	}
	kinds := make([]HarnessKind, 0, 1+len(request.WorkerPolicy.PreferredHarnesses))
	if request.HarnessKind != "" {
		kinds = append(kinds, request.HarnessKind)
	} else {
		defaultKind := request.WorkerPolicy.DefaultHarness
		if defaultKind == "" {
			defaultKind = HarnessFX
		}
		kinds = append(kinds, defaultKind)
		kinds = append(kinds, request.WorkerPolicy.PreferredHarnesses...)
	}
	result := make([]HarnessInstance, 0, len(ordered))
	for _, kind := range kinds {
		for _, instance := range ordered {
			if instance.Kind != kind {
				continue
			}
			already := false
			for _, current := range result {
				already = already || current.ID == instance.ID
			}
			if !already {
				result = append(result, instance)
			}
		}
	}
	// A model or reasoning pin is an adapter capability constraint, not a
	// request to force the default harness. Try every observed adapter before
	// reporting that no Node supports the pin.
	if request.ModelID != "" || request.Reasoning != "" {
		for _, instance := range ordered {
			already := false
			for _, current := range result {
				already = already || current.ID == instance.ID
			}
			if !already {
				result = append(result, instance)
			}
		}
	}
	return result
}

// ResolveAndCreateWorker is the only creation path exposed by this resolver.
// It accepts preferences, never caller-built snapshots. A new call intentionally
// creates a new Worker for the same intent when a different binding is wanted.
func (s *Store) ResolveAndCreateWorker(ctx context.Context, conversationID, intent string, request DispatchResolutionRequest, idempotencyKey string) (Worker, Turn, Phase4Attempt, DispatchResolution, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey != "" {
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
		if worker, turn, attempt, found, err := s.workerCreationReplay(ctx, idempotencyKey); err != nil || found {
			if err != nil {
				return Worker{}, Turn{}, Phase4Attempt{}, DispatchResolution{}, err
			}
			binding, err := s.ResolveWorkerBinding(ctx, worker.ID)
			if err != nil {
				return Worker{}, Turn{}, Phase4Attempt{}, DispatchResolution{}, err
			}
			return worker, turn, attempt, DispatchResolution{ProjectDispatch: binding, Queued: worker.Status == WorkerQueued}, nil
		}
	}

	resolved, err := s.ResolveDispatch(ctx, request)
	if err != nil {
		return Worker{}, Turn{}, Phase4Attempt{}, DispatchResolution{}, err
	}
	projectSnapshot, err := json.Marshal(resolved.Snapshot)
	if err != nil {
		return Worker{}, Turn{}, Phase4Attempt{}, DispatchResolution{}, err
	}
	policySnapshot, err := json.Marshal(resolved.Snapshot.Policy)
	if err != nil {
		return Worker{}, Turn{}, Phase4Attempt{}, DispatchResolution{}, err
	}
	if s.beforeResolvedWorkerCreate != nil {
		s.beforeResolvedWorkerCreate()
	}
	worker, turn, attempt, err := s.createWorker(ctx, conversationID, WorkerSpec{Title: intent, Intent: intent, ProjectID: resolved.Project.ID, NodeID: string(resolved.Node), HarnessInstanceID: string(resolved.HarnessInstance.ID), PolicySnapshot: string(policySnapshot), ProjectSnapshot: string(projectSnapshot), Workspace: resolved.Workspace, ProjectRevision: resolved.Project.Revision, IdempotencyKey: idempotencyKey, ExpectedNodeOnline: resolved.nodeState.online, ExpectedNodeDraining: resolved.nodeState.draining, ExpectedNodeRevoked: resolved.nodeState.revoked, ExpectedInventoryJSON: resolved.nodeState.inventoryJSON}, TurnSpec{Input: intent, IdempotencyKey: idempotencyKey}, idempotencyKey)
	if err != nil {
		return Worker{}, Turn{}, Phase4Attempt{}, DispatchResolution{}, err
	}
	binding, err := s.ResolveWorkerBinding(ctx, worker.ID)
	if err != nil {
		return Worker{}, Turn{}, Phase4Attempt{}, DispatchResolution{}, err
	}
	resolved.ProjectDispatch = binding
	return worker, turn, attempt, resolved, nil
}

func (s *Store) workerCreationReplay(ctx context.Context, idempotencyKey string) (Worker, Turn, Phase4Attempt, bool, error) {
	var encoded string
	err := s.db.QueryRowContext(ctx, `SELECT outcome_json FROM idempotency_records WHERE operation = ? AND idempotency_key = ?`, "worker.create", idempotencyKey).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return Worker{}, Turn{}, Phase4Attempt{}, false, nil
	}
	if err != nil {
		return Worker{}, Turn{}, Phase4Attempt{}, false, err
	}
	var stored workerCreationOutcome
	if err := json.Unmarshal([]byte(encoded), &stored); err != nil {
		return Worker{}, Turn{}, Phase4Attempt{}, false, fmt.Errorf("core: decode durable Worker outcome: %w", err)
	}
	return stored.Worker, stored.Turn, stored.Attempt, true, nil
}

// ResolveWorkerBinding returns the durable immutable binding for repeat calls.
// It intentionally never reads current Node inventory or Project policy.
func (s *Store) ResolveWorkerBinding(ctx context.Context, workerID string) (ProjectDispatch, error) {
	worker, err := s.Worker(ctx, workerID)
	if err != nil {
		return ProjectDispatch{}, err
	}
	var snapshot ProjectSnapshot
	if err := json.Unmarshal([]byte(worker.ProjectSnapshot), &snapshot); err != nil {
		return ProjectDispatch{}, fmt.Errorf("core: decode immutable Worker Project snapshot: %w", err)
	}
	return ProjectDispatch{Project: Project{ID: worker.ProjectID, Revision: snapshot.Revision}, Node: NodeReference(worker.NodeID), Workspace: worker.Workspace, HarnessInstance: snapshot.HarnessInstance, Snapshot: snapshot}, nil
}
