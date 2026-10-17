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

var (
	ErrNoProject               = errors.New("core: no Project selected")
	ErrNoEligibleNode          = errors.New("core: no eligible Node")
	ErrInvalidDispatchPin      = errors.New("core: invalid dispatch pin")
	ErrSelectedNodeUnavailable = errors.New("core: selected Node is unavailable")
	ErrMissingHarnessInventory = errors.New("core: HarnessInstance inventory is missing")
)

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
	request.ProjectID = strings.TrimSpace(request.ProjectID)
	if request.ProjectID == "" && request.Workspace != "" {
		return DispatchResolution{}, fmt.Errorf("%w: projectless Workers require a Node-managed workspace; select an authorized Project to use an explicit path", ErrWorkspaceOutsideRoot)
	}
	var project Project
	var err error
	if request.ProjectID != "" {
		project, err = s.Project(ctx, request.ProjectID)
		if err != nil {
			return DispatchResolution{}, err
		}
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
	var preferredReady *DispatchResolution
	preferredRank := 1000
	var firstError error
	for _, candidate := range candidates {
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
			if project.ID != "" {
				return resolved, nil
			}
			rank := projectlessHarnessRank(resolved.HarnessInstance.Kind, request.WorkerPolicy)
			if preferredReady == nil || rank < preferredRank {
				copy := resolved
				preferredReady = &copy
				preferredRank = rank
			}
			continue
		}
		if firstUnavailable == nil {
			copy := resolved
			firstUnavailable = &copy
		}
	}
	if preferredReady != nil {
		return *preferredReady, nil
	}
	if firstUnavailable != nil && (request.NodeID != "" || request.HarnessInstanceID != "") {
		if project.ID == "" {
			reason := "has no free execution capacity"
			if !firstUnavailable.nodeState.online {
				reason = "is offline"
			} else if firstUnavailable.nodeState.draining {
				reason = "is draining"
			}
			return DispatchResolution{}, fmt.Errorf("%w: Node %s %s; choose another available Node", ErrSelectedNodeUnavailable, firstUnavailable.Node, reason)
		}
		return *firstUnavailable, nil
	}
	if firstError != nil {
		return DispatchResolution{}, firstError
	}
	if request.HarnessInstanceID != "" {
		return DispatchResolution{}, fmt.Errorf("%w: HarnessInstance %q was not observed on an eligible Node; select an installed harness", ErrInvalidDispatchPin, request.HarnessInstanceID)
	}
	return DispatchResolution{}, fmt.Errorf("%w: connect an online Node with an installed and authenticated Worker harness", ErrNoEligibleNode)
}

func effectiveDispatchPins(policy ProjectPolicy, request DispatchResolutionRequest) (string, string, error) {
	model := policy.ModelPin()
	if requested := strings.TrimSpace(request.ModelID); requested != "" {
		if model != "" && model != requested {
			return "", "", fmt.Errorf("%w: model is forbidden by Project policy", ErrInvalidDispatchPin)
		}
		model = requested
	}
	if model == "" {
		model = strings.TrimSpace(request.WorkerPolicy.ModelID)
	}
	reasoning := policy.Reasoning
	if requested := strings.TrimSpace(request.Reasoning); requested != "" {
		if reasoning != "" && reasoning != requested {
			return "", "", fmt.Errorf("%w: reasoning is forbidden by Project policy", ErrInvalidDispatchPin)
		}
		reasoning = requested
	}
	if reasoning == "" {
		reasoning = strings.TrimSpace(request.WorkerPolicy.Reasoning)
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
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: Node %s is not enrolled; pair this Node first", ErrSelectedNodeUnavailable, explicit)
		}
		if err != nil {
			return nil, err
		}
		if record.Revoked {
			return nil, ErrNodeRevoked
		}
		if project.ID != "" {
			if _, ok := project.MappingForNode(explicit); !ok {
				return nil, fmt.Errorf("%w: %s", ErrProjectMappingMissing, explicit)
			}
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
		if project.ID != "" {
			if _, ok := project.MappingForNode(record.Node); !ok {
				continue
			}
		}
		order, ok := rank[record.Node]
		if !ok {
			order = len(preference)
		}
		if project.ID == "" && record.Node == "local" {
			order = -1
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
		if allowed := request.WorkerPolicy.AllowedHarnesses; len(allowed) > 0 && !containsHarnessKind(allowed, instance.Kind) {
			if firstError == nil {
				firstError = fmt.Errorf("%w: harness %q is not allowed by Worker preferences", ErrInvalidDispatchPin, instance.Kind)
			}
			continue
		}
		supported := true
		for _, capability := range request.WorkerPolicy.RequiredExecutionCapabilities {
			if !instance.Capabilities.SupportsExecution(capability) {
				supported = false
				break
			}
		}
		for _, capability := range request.WorkerPolicy.RequiredActivityCapabilities {
			if !instance.Capabilities.SupportsActivity(capability) {
				supported = false
				break
			}
		}
		if !supported {
			if firstError == nil {
				firstError = fmt.Errorf("%w: harness %q lacks required Worker capabilities", ErrInvalidDispatchPin, instance.ID)
			}
			continue
		}
		if err := instance.ValidatePins(model, reasoning); err != nil {
			if firstError == nil {
				firstError = fmt.Errorf("%w: %w", ErrInvalidDispatchPin, err)
			}
			continue
		}
		workspace := request.Workspace
		if workspace == "" && project.ID != "" {
			mapping, _ := project.MappingForNode(record.Node)
			workspace = mapping.Path
		}
		policy := project.Policy
		policy.ModelID, policy.Model, policy.Reasoning = model, "", reasoning
		var snapshot ProjectSnapshot
		if project.ID == "" {
			snapshot = ProjectSnapshot{Node: record.Node, Workspace: workspace, HarnessInstance: instance, Policy: policy}
		} else {
			projectForSelection := project
			projectForSelection.Policy = policy
			var err error
			snapshot, err = projectForSelection.ValidateDispatch(record.Node, instance, workspace)
			if err != nil {
				if firstError == nil {
					firstError = err
				}
				continue
			}
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
	if request.HarnessInstanceID != "" || request.HarnessKind != "" {
		return DispatchResolution{}, false, fmt.Errorf("%w: selected harness %q (%q) is not installed on Node %s", ErrInvalidDispatchPin, request.HarnessInstanceID, request.HarnessKind, record.Node)
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
			defaultKind = HarnessOpenCode
			if request.ProjectID == "" {
				defaultKind = HarnessCodex
			}
		}
		kinds = append(kinds, defaultKind)
		kinds = append(kinds, request.WorkerPolicy.PreferredHarnesses...)
		if request.ProjectID == "" {
			kinds = append(kinds, HarnessCodex, HarnessClaudeCode)
			kinds = append(kinds, request.WorkerPolicy.AllowedHarnesses...)
		}
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

func projectlessHarnessRank(kind HarnessKind, policy HarnessPolicy) int {
	defaultKind := policy.DefaultHarness
	if defaultKind == "" {
		defaultKind = HarnessCodex
	}
	order := []HarnessKind{defaultKind}
	order = append(order, policy.PreferredHarnesses...)
	order = append(order, HarnessCodex, HarnessClaudeCode)
	for rank, candidate := range order {
		if candidate == kind {
			return rank
		}
	}
	return len(order)
}

func (s *Store) ResolveAndCreateWorker(ctx context.Context, conversationID, intent string, request DispatchResolutionRequest, idempotencyKey string, origins ...SecretaryOriginIdentity) (Worker, Turn, Phase4Attempt, DispatchResolution, error) {
	return s.ResolveAndCreateWorkerWithProfileSnapshot(ctx, conversationID, intent, request, idempotencyKey, "", origins...)
}

func (s *Store) ResolveAndCreateWorkerWithProfileSnapshot(ctx context.Context, conversationID, intent string, request DispatchResolutionRequest, idempotencyKey, profileSnapshot string, origins ...SecretaryOriginIdentity) (Worker, Turn, Phase4Attempt, DispatchResolution, error) {
	return s.ResolveAndCreateWorkerWithProfileFactory(ctx, conversationID, intent, request, idempotencyKey, func(DispatchResolution) (string, error) {
		return profileSnapshot, nil
	}, origins...)
}

// ResolveAndCreateWorkerWithProfileFactory resolves the final Node/harness once and
// builds its managed Profile against the very same immutable dispatch selection.
// The factory is not called on idempotent replays.
func (s *Store) ResolveAndCreateWorkerWithProfileFactory(ctx context.Context, conversationID, intent string, request DispatchResolutionRequest, idempotencyKey string, profileFactory func(DispatchResolution) (string, error), origins ...SecretaryOriginIdentity) (Worker, Turn, Phase4Attempt, DispatchResolution, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if len(origins) > 1 {
		return Worker{}, Turn{}, Phase4Attempt{}, DispatchResolution{}, ErrInvalidSecretaryOrigin
	}
	var origin *SecretaryOriginIdentity
	if len(origins) == 1 {
		value := origins[0]
		value.PersonID, value.Capability = strings.TrimSpace(value.PersonID), strings.TrimSpace(value.Capability)
		value.SecretaryTurnID, value.InputID = strings.TrimSpace(value.SecretaryTurnID), strings.TrimSpace(value.InputID)
		if value.PersonID == "" || value.Capability == "" || value.SecretaryTurnID == "" || value.InputID == "" {
			return Worker{}, Turn{}, Phase4Attempt{}, DispatchResolution{}, ErrInvalidSecretaryOrigin
		}
		origin = &value
	}
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
	if profileFactory == nil {
		return Worker{}, Turn{}, Phase4Attempt{}, DispatchResolution{}, errors.New("core: Worker profile factory is required")
	}
	profileSnapshot, err := profileFactory(resolved)
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
	var queuedOrigin *SecretaryOriginIdentity
	if resolved.Queued {
		queuedOrigin = origin
	}
	worker, turn, attempt, err := s.createWorkerWithOrigin(ctx, conversationID, WorkerSpec{Title: intent, Intent: intent, ProjectID: resolved.Project.ID, NodeID: string(resolved.Node), HarnessInstanceID: string(resolved.HarnessInstance.ID), PolicySnapshot: string(policySnapshot), ProjectSnapshot: string(projectSnapshot), ProfileSnapshot: profileSnapshot, Workspace: resolved.Workspace, ProjectRevision: resolved.Project.Revision, IdempotencyKey: idempotencyKey, ExpectedNodeOnline: resolved.nodeState.online, ExpectedNodeDraining: resolved.nodeState.draining, ExpectedNodeRevoked: resolved.nodeState.revoked, ExpectedInventoryJSON: resolved.nodeState.inventoryJSON}, TurnSpec{Input: intent, IdempotencyKey: idempotencyKey}, idempotencyKey, queuedOrigin)
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

func (s *Store) ReplayWorkerCreation(ctx context.Context, idempotencyKey string) (Worker, Turn, Phase4Attempt, DispatchResolution, bool, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return Worker{}, Turn{}, Phase4Attempt{}, DispatchResolution{}, false, nil
	}
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	worker, turn, attempt, found, err := s.workerCreationReplay(ctx, idempotencyKey)
	if err != nil || !found {
		return Worker{}, Turn{}, Phase4Attempt{}, DispatchResolution{}, found, err
	}
	binding, err := s.ResolveWorkerBinding(ctx, worker.ID)
	if err != nil {
		return Worker{}, Turn{}, Phase4Attempt{}, DispatchResolution{}, false, err
	}
	return worker, turn, attempt, DispatchResolution{ProjectDispatch: binding, Queued: worker.Status == WorkerQueued}, true, nil
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
	if err := loadWorkerTemplateSnapshot(ctx, s.db, &stored.Worker); err != nil {
		return Worker{}, Turn{}, Phase4Attempt{}, false, err
	}
	return stored.Worker, stored.Turn, stored.Attempt, true, nil
}

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
