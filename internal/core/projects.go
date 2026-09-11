package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	ErrProjectValidation       = errors.New("core: invalid Project")
	ErrProjectRevisionConflict = errors.New("core: Project revision conflict")
	ErrProjectPolicyDenied     = errors.New("core: Project policy denied")
	ErrProjectMappingMissing   = errors.New("core: Project has no mapping for Node")
	ErrWorkspaceOutsideRoot    = errors.New("core: workspace is outside Project root")
	ErrWorkspaceMissing        = errors.New("core: workspace path is missing")
)

type ProjectPathMapping struct {
	Node   NodeReference `json:"node"`
	NodeID string        `json:"node_id,omitempty"`
	Path   string        `json:"path"`
}

func (m ProjectPathMapping) node() NodeReference {
	if m.Node != "" {
		return m.Node
	}
	return NodeReference(strings.TrimSpace(m.NodeID))
}

type NodePolicy struct {
	Default   NodeReference   `json:"default,omitempty"`
	Preferred []NodeReference `json:"preferred,omitempty"`
}

type ExecutionPolicy struct {
	WorkspaceRoot       string                `json:"workspace_root,omitempty"`
	RequireApproval     bool                  `json:"require_approval,omitempty"`
	AllowShell          bool                  `json:"allow_shell,omitempty"`
	AllowEdit           bool                  `json:"allow_edit,omitempty"`
	AllowedCapabilities []ExecutionCapability `json:"allowed_capabilities,omitempty"`
	DeniedCapabilities  []ExecutionCapability `json:"denied_capabilities,omitempty"`
}

type ProjectPolicy struct {
	AllowedHarnessInstances       []HarnessInstanceID   `json:"allowed_harness_instances,omitempty"`
	AllowedHarnessKinds           []HarnessKind         `json:"allowed_harness_kinds,omitempty"`
	DefaultNode                   NodeReference         `json:"default_node,omitempty"`
	DefaultNodePolicy             NodePolicy            `json:"default_node_policy,omitempty"`
	RequiredCapabilities          []ExecutionCapability `json:"required_capabilities,omitempty"`
	RequiredExecutionCapabilities []ExecutionCapability `json:"required_execution_capabilities,omitempty"`
	RequiredActivityCapabilities  []ActivityCapability  `json:"required_activity_capabilities,omitempty"`
	DefaultHarness                HarnessKind           `json:"default_harness,omitempty"`
	PreferredHarnesses            []HarnessKind         `json:"preferred_harnesses,omitempty"`
	ModelID                       string                `json:"model_id,omitempty"`
	Model                         string                `json:"model,omitempty"`
	Reasoning                     string                `json:"reasoning,omitempty"`
	Execution                     ExecutionPolicy       `json:"execution,omitempty"`
	ExecutionPolicy               ExecutionPolicy       `json:"execution_policy,omitempty"`
}

func (p ProjectPolicy) execution() ExecutionPolicy {
	result := p.Execution
	if p.ExecutionPolicy.WorkspaceRoot != "" {
		result.WorkspaceRoot = p.ExecutionPolicy.WorkspaceRoot
	}
	if p.ExecutionPolicy.RequireApproval {
		result.RequireApproval = true
	}
	if p.ExecutionPolicy.AllowShell {
		result.AllowShell = true
	}
	if p.ExecutionPolicy.AllowEdit {
		result.AllowEdit = true
	}
	if len(p.ExecutionPolicy.AllowedCapabilities) > 0 {
		result.AllowedCapabilities = p.ExecutionPolicy.AllowedCapabilities
	}
	if len(p.ExecutionPolicy.DeniedCapabilities) > 0 {
		result.DeniedCapabilities = p.ExecutionPolicy.DeniedCapabilities
	}
	return result
}

func (p ProjectPolicy) modelPin() string {
	if p.ModelID != "" {
		return p.ModelID
	}
	return p.Model
}

func (p ProjectPolicy) ModelPin() string { return p.modelPin() }

func (p ProjectPolicy) defaultNode() NodeReference {
	if p.DefaultNode != "" {
		return p.DefaultNode
	}
	return p.DefaultNodePolicy.Default
}
func (p ProjectPolicy) requiredExecution() []ExecutionCapability {
	result := append([]ExecutionCapability(nil), p.RequiredCapabilities...)
	for _, capability := range p.RequiredExecutionCapabilities {
		if !containsExecution(result, capability) {
			result = append(result, capability)
		}
	}
	return result
}
func containsExecution(values []ExecutionCapability, want ExecutionCapability) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// Project is a manually managed registry record. Its paths are metadata, not
// proof that a directory exists. Only a Node may inspect the local filesystem.
type Project struct {
	ID           string                   `json:"id"`
	Name         string                   `json:"name"`
	Description  string                   `json:"description,omitempty"`
	Mappings     []ProjectPathMapping     `json:"mappings"`
	PathMappings map[NodeReference]string `json:"path_mappings,omitempty"`
	Policy       ProjectPolicy            `json:"policy"`
	Revision     int64                    `json:"revision"`
	CreatedAt    time.Time                `json:"created_at"`
	UpdatedAt    time.Time                `json:"updated_at"`
}

type ProjectSpec struct {
	ID           string                   `json:"id,omitempty"`
	Name         string                   `json:"name"`
	Description  string                   `json:"description,omitempty"`
	Mappings     []ProjectPathMapping     `json:"mappings,omitempty"`
	PathMappings map[NodeReference]string `json:"path_mappings,omitempty"`
	Policy       ProjectPolicy            `json:"policy"`
}

type ProjectSnapshot struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	Description     string               `json:"description,omitempty"`
	Mappings        []ProjectPathMapping `json:"mappings"`
	Policy          ProjectPolicy        `json:"policy"`
	Revision        int64                `json:"revision"`
	Node            NodeReference        `json:"node"`
	Workspace       string               `json:"workspace"`
	HarnessInstance HarnessInstance      `json:"harness_instance"`
}

func (p ProjectSpec) project() Project {
	mappings := append([]ProjectPathMapping(nil), p.Mappings...)
	seen := make(map[NodeReference]bool, len(mappings))
	for i := range mappings {
		mappings[i].Node = mappings[i].node()
		seen[mappings[i].Node] = true
	}
	for node, path := range p.PathMappings {
		if !seen[node] {
			mappings = append(mappings, ProjectPathMapping{Node: node, Path: path})
		}
	}
	return Project{ID: strings.TrimSpace(p.ID), Name: strings.TrimSpace(p.Name), Description: strings.TrimSpace(p.Description), Mappings: mappings, Policy: p.Policy}
}

func (p Project) Validate() error {
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("%w: id and name are required", ErrProjectValidation)
	}
	if len(p.Name) > 200 || len(p.Description) > 10000 {
		return fmt.Errorf("%w: name or description is too long", ErrProjectValidation)
	}
	seen := make(map[NodeReference]bool, len(p.Mappings))
	for _, mapping := range p.Mappings {
		node := mapping.node()
		if strings.TrimSpace(string(node)) == "" {
			return fmt.Errorf("%w: mapping Node is required", ErrProjectValidation)
		}
		if seen[node] {
			return fmt.Errorf("%w: duplicate mapping for Node %q", ErrProjectValidation, node)
		}
		seen[node] = true
		if err := validateRootPath(mapping.Path); err != nil {
			return fmt.Errorf("%w: mapping %s: %v", ErrProjectValidation, node, err)
		}
	}
	if len(p.Mappings) == 0 {
		return fmt.Errorf("%w: at least one Node mapping is required", ErrProjectValidation)
	}
	if node := p.Policy.defaultNode(); node != "" && !seen[node] {
		return fmt.Errorf("%w: default Node %q has no mapping", ErrProjectValidation, node)
	}
	for _, node := range p.Policy.DefaultNodePolicy.Preferred {
		if node == "" || !seen[node] {
			return fmt.Errorf("%w: preferred Node %q has no mapping", ErrProjectValidation, node)
		}
	}
	if p.Policy.execution().WorkspaceRoot != "" {
		if err := validateRootPath(p.Policy.execution().WorkspaceRoot); err != nil {
			return fmt.Errorf("%w: execution workspace root: %v", ErrProjectValidation, err)
		}
	}
	if err := validatePolicyValues(p.Policy); err != nil {
		return fmt.Errorf("%w: %v", ErrProjectValidation, err)
	}
	return nil
}

func validateRootPath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || !filepath.IsAbs(path) {
		return errors.New("path must be absolute")
	}
	if path != filepath.Clean(path) {
		return errors.New("path must be clean")
	}
	if path == string(filepath.Separator) {
		return errors.New("filesystem root is not an allowed workspace root")
	}
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return errors.New("path traversal is not allowed")
		}
	}
	return nil
}
func validatePolicyValues(policy ProjectPolicy) error {
	execution := policy.execution()
	seenExecutionPolicy := map[ExecutionCapability]bool{}
	for _, capability := range append(append([]ExecutionCapability(nil), execution.AllowedCapabilities...), execution.DeniedCapabilities...) {
		if capability == "" || seenExecutionPolicy[capability] {
			return errors.New("invalid or duplicate execution policy capability")
		}
		seenExecutionPolicy[capability] = true
	}
	seenKinds := map[HarnessKind]bool{}
	for _, kind := range policy.AllowedHarnessKinds {
		if kind == "" || seenKinds[kind] {
			return errors.New("invalid or duplicate allowed harness kind")
		}
		seenKinds[kind] = true
	}
	seenIDs := map[HarnessInstanceID]bool{}
	for _, id := range policy.AllowedHarnessInstances {
		if strings.TrimSpace(string(id)) == "" || seenIDs[id] {
			return errors.New("invalid or duplicate allowed HarnessInstance")
		}
		seenIDs[id] = true
	}
	seenCaps := map[ExecutionCapability]bool{}
	for _, cap := range policy.requiredExecution() {
		if cap == "" || seenCaps[cap] {
			return errors.New("invalid or duplicate required execution capability")
		}
		seenCaps[cap] = true
	}
	seenActivity := map[ActivityCapability]bool{}
	for _, cap := range policy.RequiredActivityCapabilities {
		if cap == "" || seenActivity[cap] {
			return errors.New("invalid or duplicate required activity capability")
		}
		seenActivity[cap] = true
	}
	return nil
}

func (p ProjectSnapshot) Validate() error {
	if strings.TrimSpace(p.ID) == "" || strings.TrimSpace(string(p.Node)) == "" || strings.TrimSpace(p.Workspace) == "" {
		return fmt.Errorf("%w: incomplete Project snapshot", ErrProjectValidation)
	}
	project := Project{ID: p.ID, Name: p.Name, Description: p.Description, Mappings: p.Mappings, Policy: p.Policy, Revision: p.Revision}
	_, err := project.ValidateDispatch(p.Node, p.HarnessInstance, p.Workspace)
	return err
}

func (p Project) MappingForNode(node NodeReference) (ProjectPathMapping, bool) {
	for _, mapping := range p.Mappings {
		if mapping.node() == node {
			mapping.Node = node
			return mapping, true
		}
	}
	return ProjectPathMapping{}, false
}

func (p Project) Snapshot(node NodeReference, workspace string, instance HarnessInstance) ProjectSnapshot {
	return ProjectSnapshot{ID: p.ID, Name: p.Name, Description: p.Description, Mappings: append([]ProjectPathMapping(nil), p.Mappings...), Policy: p.Policy, Revision: p.Revision, Node: node, Workspace: workspace, HarnessInstance: instance}
}

func (p Project) ValidateDispatch(node NodeReference, instance HarnessInstance, workspace string) (ProjectSnapshot, error) {
	if err := p.Validate(); err != nil {
		return ProjectSnapshot{}, err
	}
	mapping, ok := p.MappingForNode(node)
	if !ok {
		return ProjectSnapshot{}, fmt.Errorf("%w: %s", ErrProjectMappingMissing, node)
	}
	if workspace == "" {
		workspace = mapping.Path
	}
	if err := validateWorkspaceWithin(mapping.Path, p.Policy.execution().WorkspaceRoot, workspace); err != nil {
		return ProjectSnapshot{}, err
	}
	if len(p.Policy.AllowedHarnessInstances) > 0 && !containsHarnessInstance(p.Policy.AllowedHarnessInstances, instance.ID) {
		return ProjectSnapshot{}, fmt.Errorf("%w: HarnessInstance %q is not allowed", ErrProjectPolicyDenied, instance.ID)
	}
	if len(p.Policy.AllowedHarnessKinds) > 0 && !containsHarnessKind(p.Policy.AllowedHarnessKinds, instance.Kind) {
		return ProjectSnapshot{}, fmt.Errorf("%w: harness kind %q is not allowed", ErrProjectPolicyDenied, instance.Kind)
	}
	required := p.Policy.requiredExecution()
	if p.Policy.execution().AllowShell && !containsExecution(required, CapabilityShell) {
		required = append(required, CapabilityShell)
	}
	if p.Policy.execution().AllowEdit && !containsExecution(required, CapabilityEdit) {
		required = append(required, CapabilityEdit)
	}
	for _, capability := range required {
		if !instance.Capabilities.SupportsExecution(capability) {
			return ProjectSnapshot{}, fmt.Errorf("%w: required capability %q is unavailable", ErrProjectPolicyDenied, capability)
		}
	}
	for _, capability := range p.Policy.execution().DeniedCapabilities {
		if instance.Capabilities.SupportsExecution(capability) {
			return ProjectSnapshot{}, fmt.Errorf("%w: execution capability %q is forbidden", ErrProjectPolicyDenied, capability)
		}
	}
	for _, capability := range p.Policy.execution().AllowedCapabilities {
		if !instance.Capabilities.SupportsExecution(capability) {
			return ProjectSnapshot{}, fmt.Errorf("%w: execution capability %q is not allowed by execution policy", ErrProjectPolicyDenied, capability)
		}
	}
	for _, capability := range p.Policy.RequiredActivityCapabilities {
		if !instance.Capabilities.SupportsActivity(capability) {
			return ProjectSnapshot{}, fmt.Errorf("%w: required activity capability %q is unavailable", ErrProjectPolicyDenied, capability)
		}
	}
	if err := instance.ValidateSelection(p.Policy.modelPin(), p.Policy.Reasoning); err != nil {
		return ProjectSnapshot{}, err
	}
	return p.Snapshot(node, workspace, instance), nil
}
func containsHarnessInstance(values []HarnessInstanceID, want HarnessInstanceID) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func containsHarnessKind(values []HarnessKind, want HarnessKind) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func ValidateProjectWorkspace(project Project, node NodeReference, workspace string) error {
	mapping, ok := project.MappingForNode(node)
	if !ok {
		return fmt.Errorf("%w: %s", ErrProjectMappingMissing, node)
	}
	return validateWorkspaceWithin(mapping.Path, project.Policy.execution().WorkspaceRoot, workspace)
}

func validateWorkspaceWithin(mappingRoot, policyRoot, workspace string) error {
	if err := validateRootPath(mappingRoot); err != nil {
		return fmt.Errorf("%w: mapping root: %v", ErrWorkspaceOutsideRoot, err)
	}
	if err := validateRootPath(workspace); err != nil {
		return fmt.Errorf("%w: %v", ErrWorkspaceOutsideRoot, err)
	}
	root := mappingRoot
	if policyRoot != "" {
		if err := validateRootPath(policyRoot); err != nil {
			return fmt.Errorf("%w: policy root: %v", ErrWorkspaceOutsideRoot, err)
		}
		if !pathContained(mappingRoot, policyRoot) {
			return fmt.Errorf("%w: execution root is outside mapping", ErrWorkspaceOutsideRoot)
		}
		root = policyRoot
	}
	if !pathContained(root, workspace) {
		return fmt.Errorf("%w: %s is not under %s", ErrWorkspaceOutsideRoot, workspace, root)
	}
	return nil
}
func pathContained(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func (s *Store) migratePhase4Projects(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS phase4_projects (
 id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE, description TEXT NOT NULL DEFAULT '', mappings_json TEXT NOT NULL,
 policy_json TEXT NOT NULL, revision INTEGER NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);`)
	if err != nil {
		return fmt.Errorf("core: migrate Projects registry: %w", err)
	}
	return nil
}

func (s *Store) CreateProject(ctx context.Context, spec ProjectSpec, idempotencyKeys ...string) (Project, error) {
	project := spec.project()
	if project.ID == "" {
		project.ID = newID("prj")
	}
	if err := project.Validate(); err != nil {
		return Project{}, err
	}
	key := ""
	if len(idempotencyKeys) > 1 {
		return Project{}, errors.New("core: at most one Project idempotency key is allowed")
	}
	if len(idempotencyKeys) == 1 {
		key = strings.TrimSpace(idempotencyKeys[0])
	}
	if key != "" {
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
	}
	return withTx(s, ctx, func(tx *sql.Tx) (Project, error) {
		if key != "" {
			var encoded string
			if err := tx.QueryRowContext(ctx, `SELECT outcome_json FROM idempotency_records WHERE operation = 'project.create' AND idempotency_key = ?`, key).Scan(&encoded); err == nil {
				var stored Project
				if json.Unmarshal([]byte(encoded), &stored) != nil {
					return Project{}, errors.New("core: invalid stored Project outcome")
				}
				return stored, nil
			} else if !errors.Is(err, sql.ErrNoRows) {
				return Project{}, err
			}
		}
		now := s.now()
		project.Revision = 1
		project.CreatedAt = now
		project.UpdatedAt = now
		mappings, err := json.Marshal(project.Mappings)
		if err != nil {
			return Project{}, err
		}
		policy, err := json.Marshal(project.Policy)
		if err != nil {
			return Project{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO phase4_projects(id, name, description, mappings_json, policy_json, revision, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?)`, project.ID, project.Name, project.Description, mappings, policy, project.Revision, timestamp(now), timestamp(now)); err != nil {
			return Project{}, err
		}
		if key != "" {
			encoded, _ := json.Marshal(project)
			if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, outcome_json, created_at) VALUES('project.create', ?, ?, ?)`, key, encoded, timestamp(now)); err != nil {
				return Project{}, err
			}
		}
		return project, nil
	})
}

func (s *Store) Project(ctx context.Context, id string) (Project, error) {
	var p Project
	var mappings, policy, created, updated string
	err := s.db.QueryRowContext(ctx, `SELECT id, name, description, mappings_json, policy_json, revision, created_at, updated_at FROM phase4_projects WHERE id = ?`, id).Scan(&p.ID, &p.Name, &p.Description, &mappings, &policy, &p.Revision, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	if err := json.Unmarshal([]byte(mappings), &p.Mappings); err != nil {
		return Project{}, err
	}
	for i := range p.Mappings {
		p.Mappings[i].Node = p.Mappings[i].node()
	}
	if err := json.Unmarshal([]byte(policy), &p.Policy); err != nil {
		return Project{}, err
	}
	p.PathMappings = make(map[NodeReference]string, len(p.Mappings))
	for _, mapping := range p.Mappings {
		p.PathMappings[mapping.node()] = mapping.Path
	}
	p.CreatedAt, err = parseTimestamp(created)
	if err != nil {
		return Project{}, err
	}
	p.UpdatedAt, err = parseTimestamp(updated)
	return p, err
}
func (s *Store) GetProject(ctx context.Context, id string) (Project, error) {
	return s.Project(ctx, id)
}

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) { return s.Projects(ctx) }

func (s *Store) Projects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM phase4_projects ORDER BY name, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := []Project{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		p, err := s.Project(ctx, id)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (s *Store) UpdateProject(ctx context.Context, id string, spec ProjectSpec, expectedRevision int64, idempotencyKeys ...string) (Project, error) {
	if expectedRevision <= 0 {
		return Project{}, ErrProjectRevisionConflict
	}
	if len(idempotencyKeys) > 1 {
		return Project{}, errors.New("core: at most one Project idempotency key is allowed")
	}
	key := ""
	if len(idempotencyKeys) == 1 {
		key = strings.TrimSpace(idempotencyKeys[0])
	}
	if key != "" {
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
	}
	candidate := spec.project()
	candidate.ID = id
	if err := candidate.Validate(); err != nil {
		return Project{}, err
	}
	return withTx(s, ctx, func(tx *sql.Tx) (Project, error) {
		if key != "" {
			var encoded string
			if err := tx.QueryRowContext(ctx, `SELECT outcome_json FROM idempotency_records WHERE operation = ? AND idempotency_key = ?`, "project.update:"+id, key).Scan(&encoded); err == nil {
				var stored Project
				if json.Unmarshal([]byte(encoded), &stored) != nil {
					return Project{}, errors.New("core: invalid stored Project outcome")
				}
				return stored, nil
			} else if !errors.Is(err, sql.ErrNoRows) {
				return Project{}, err
			}
		}
		current, err := projectTx(ctx, tx, id)
		if err != nil {
			return Project{}, err
		}
		if current.Revision != expectedRevision {
			return Project{}, ErrProjectRevisionConflict
		}
		now := s.now()
		candidate.Revision = current.Revision + 1
		candidate.CreatedAt = current.CreatedAt
		candidate.UpdatedAt = now
		mappings, _ := json.Marshal(candidate.Mappings)
		policy, _ := json.Marshal(candidate.Policy)
		result, err := tx.ExecContext(ctx, `UPDATE phase4_projects SET name = ?, description = ?, mappings_json = ?, policy_json = ?, revision = ?, updated_at = ? WHERE id = ? AND revision = ?`, candidate.Name, candidate.Description, mappings, policy, candidate.Revision, timestamp(now), id, expectedRevision)
		if err != nil {
			return Project{}, err
		}
		affected, _ := result.RowsAffected()
		if affected != 1 {
			return Project{}, ErrProjectRevisionConflict
		}
		if key != "" {
			encoded, _ := json.Marshal(candidate)
			if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, outcome_json, created_at) VALUES(?, ?, ?, ?)`, "project.update:"+id, key, encoded, timestamp(now)); err != nil {
				return Project{}, err
			}
		}
		return candidate, nil
	})
}

func (s *Store) DeleteProject(ctx context.Context, id string, expectedRevision int64, idempotencyKeys ...string) error {
	if expectedRevision <= 0 {
		return ErrProjectRevisionConflict
	}
	if len(idempotencyKeys) > 1 {
		return errors.New("core: at most one Project idempotency key is allowed")
	}
	key := ""
	if len(idempotencyKeys) == 1 {
		key = strings.TrimSpace(idempotencyKeys[0])
	}
	if key != "" {
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
	}
	return withTxErr(s, ctx, func(tx *sql.Tx) error {
		if key != "" {
			var ignored string
			if err := tx.QueryRowContext(ctx, `SELECT outcome_json FROM idempotency_records WHERE operation = ? AND idempotency_key = ?`, "project.delete:"+id, key).Scan(&ignored); err == nil {
				return nil
			} else if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		var revision int64
		err := tx.QueryRowContext(ctx, `SELECT revision FROM phase4_projects WHERE id = ?`, id).Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if revision != expectedRevision {
			return ErrProjectRevisionConflict
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM phase4_projects WHERE id = ? AND revision = ?`, id, expectedRevision); err != nil {
			return err
		}
		if key != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records(operation, idempotency_key, outcome_json, created_at) VALUES(?, ?, '{}', ?)`, "project.delete:"+id, key, timestamp(s.now())); err != nil {
				return err
			}
		}
		return nil
	})
}
func projectTx(ctx context.Context, tx *sql.Tx, id string) (Project, error) {
	var p Project
	var mappings, policy, created, updated string
	err := tx.QueryRowContext(ctx, `SELECT id, name, description, mappings_json, policy_json, revision, created_at, updated_at FROM phase4_projects WHERE id = ?`, id).Scan(&p.ID, &p.Name, &p.Description, &mappings, &policy, &p.Revision, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	if err := json.Unmarshal([]byte(mappings), &p.Mappings); err != nil {
		return Project{}, err
	}
	for i := range p.Mappings {
		p.Mappings[i].Node = p.Mappings[i].node()
	}
	if err := json.Unmarshal([]byte(policy), &p.Policy); err != nil {
		return Project{}, err
	}
	p.CreatedAt, err = parseTimestamp(created)
	if err != nil {
		return Project{}, err
	}
	p.UpdatedAt, err = parseTimestamp(updated)
	return p, err
}

// ProjectDispatchRequest is the narrow resolver seam used before immutable Worker creation.
type ProjectDispatchRequest struct {
	ProjectID         string
	NodeID            NodeReference
	HarnessInstanceID HarnessInstanceID
	HarnessKind       HarnessKind
	Workspace         string
	ModelID           string
	Reasoning         string
}
type ProjectDispatch struct {
	Project         Project
	Node            NodeReference
	Workspace       string
	HarnessInstance HarnessInstance
	Snapshot        ProjectSnapshot
}

func (s *Store) ResolveProjectDispatch(ctx context.Context, request ProjectDispatchRequest) (ProjectDispatch, error) {
	project, err := s.Project(ctx, request.ProjectID)
	if err != nil {
		return ProjectDispatch{}, err
	}
	node := request.NodeID
	if node == "" {
		node = project.Policy.defaultNode()
	}
	if node == "" && len(project.Policy.DefaultNodePolicy.Preferred) > 0 {
		node = project.Policy.DefaultNodePolicy.Preferred[0]
	}
	if node == "" {
		mappings := append([]ProjectPathMapping(nil), project.Mappings...)
		sort.Slice(mappings, func(i, j int) bool { return string(mappings[i].node()) < string(mappings[j].node()) })
		if len(mappings) > 0 {
			node = mappings[0].node()
		}
	}
	mapping, ok := project.MappingForNode(node)
	if !ok {
		return ProjectDispatch{}, fmt.Errorf("%w: %s", ErrProjectMappingMissing, node)
	}
	record, err := s.NodeRecord(ctx, node)
	if err != nil {
		return ProjectDispatch{}, err
	}
	if record.Revoked {
		return ProjectDispatch{}, ErrNodeRevoked
	}
	workspace := request.Workspace
	if workspace == "" {
		workspace = mapping.Path
	}
	if request.ModelID != "" {
		if project.Policy.modelPin() != "" && request.ModelID != project.Policy.modelPin() {
			return ProjectDispatch{}, fmt.Errorf("%w: model pin cannot be overridden", ErrProjectPolicyDenied)
		}
		project.Policy.ModelID = request.ModelID
		project.Policy.Model = ""
	}
	if request.Reasoning != "" {
		if project.Policy.Reasoning != "" && request.Reasoning != project.Policy.Reasoning {
			return ProjectDispatch{}, fmt.Errorf("%w: reasoning pin cannot be overridden", ErrProjectPolicyDenied)
		}
		project.Policy.Reasoning = request.Reasoning
	}
	instanceID := request.HarnessInstanceID
	if instanceID != "" {
		if len(project.Policy.AllowedHarnessInstances) > 0 && !containsHarnessInstance(project.Policy.AllowedHarnessInstances, instanceID) {
			return ProjectDispatch{}, fmt.Errorf("%w: HarnessInstance %q is not allowed", ErrProjectPolicyDenied, instanceID)
		}
		instance, found := record.Inventory.Instance(instanceID)
		if !found {
			return ProjectDispatch{}, fmt.Errorf("%w: HarnessInstance %q", ErrHarnessUnavailable, instanceID)
		}
		if request.HarnessKind != "" && instance.Kind != request.HarnessKind {
			return ProjectDispatch{}, fmt.Errorf("%w: harness kind mismatch", ErrProjectPolicyDenied)
		}
		snapshot, err := project.ValidateDispatch(node, instance, workspace)
		if err != nil {
			return ProjectDispatch{}, err
		}
		return ProjectDispatch{Project: project, Node: node, Workspace: workspace, HarnessInstance: instance, Snapshot: snapshot}, nil
	}
	candidates := append([]HarnessInstance(nil), record.Inventory.Instances...)
	sort.SliceStable(candidates, func(i, j int) bool { return string(candidates[i].ID) < string(candidates[j].ID) })
	ordered := make([]HarnessInstance, 0, len(candidates))
	for _, kind := range append([]HarnessKind{project.Policy.DefaultHarness}, project.Policy.PreferredHarnesses...) {
		if kind == "" {
			continue
		}
		for _, candidate := range candidates {
			if candidate.Kind == kind {
				ordered = append(ordered, candidate)
			}
		}
	}
	for _, candidate := range candidates {
		found := false
		for _, existing := range ordered {
			if existing.ID == candidate.ID {
				found = true
				break
			}
		}
		if !found {
			ordered = append(ordered, candidate)
		}
	}
	var firstErr error
	for _, candidate := range ordered {
		if request.HarnessKind != "" && candidate.Kind != request.HarnessKind {
			continue
		}
		if len(project.Policy.AllowedHarnessInstances) > 0 && !containsHarnessInstance(project.Policy.AllowedHarnessInstances, candidate.ID) {
			continue
		}
		if len(project.Policy.AllowedHarnessKinds) > 0 && !containsHarnessKind(project.Policy.AllowedHarnessKinds, candidate.Kind) {
			continue
		}
		snapshot, candidateErr := project.ValidateDispatch(node, candidate, workspace)
		if candidateErr == nil {
			return ProjectDispatch{Project: project, Node: node, Workspace: workspace, HarnessInstance: candidate, Snapshot: snapshot}, nil
		}
		if firstErr == nil {
			firstErr = candidateErr
		}
	}
	if firstErr != nil {
		return ProjectDispatch{}, firstErr
	}
	return ProjectDispatch{}, fmt.Errorf("%w: no allowed HarnessInstance on %s", ErrProjectPolicyDenied, node)
}

func (s *Store) CreateWorkerFromDispatch(ctx context.Context, conversationID, intent string, dispatch ProjectDispatch, idempotencyKey string) (Worker, Turn, Phase4Attempt, error) {
	if strings.TrimSpace(dispatch.Project.ID) == "" {
		return Worker{}, Turn{}, Phase4Attempt{}, ErrProjectValidation
	}
	registered, err := s.Project(ctx, dispatch.Project.ID)
	if err != nil {
		return Worker{}, Turn{}, Phase4Attempt{}, err
	}
	if dispatch.Project.Revision != 0 && registered.Revision != dispatch.Project.Revision {
		return Worker{}, Turn{}, Phase4Attempt{}, ErrProjectRevisionConflict
	}
	dispatch.Project = registered
	if dispatch.Snapshot.ID == "" {
		snapshot, err := dispatch.Project.ValidateDispatch(dispatch.Node, dispatch.HarnessInstance, dispatch.Workspace)
		if err != nil {
			return Worker{}, Turn{}, Phase4Attempt{}, err
		}
		dispatch.Snapshot = snapshot
	} else if dispatch.Snapshot.ID != dispatch.Project.ID || (dispatch.Snapshot.Revision != 0 && dispatch.Snapshot.Revision != dispatch.Project.Revision) {
		return Worker{}, Turn{}, Phase4Attempt{}, ErrProjectRevisionConflict
	}
	if err := dispatch.Snapshot.Validate(); err != nil {
		return Worker{}, Turn{}, Phase4Attempt{}, err
	}
	if dispatch.Workspace == "" {
		dispatch.Workspace = dispatch.Snapshot.Workspace
	} else if dispatch.Workspace != dispatch.Snapshot.Workspace {
		return Worker{}, Turn{}, Phase4Attempt{}, fmt.Errorf("%w: dispatch workspace differs from snapshot", ErrWorkspaceOutsideRoot)
	}
	encodedSnapshot, err := json.Marshal(dispatch.Snapshot)
	if err != nil {
		return Worker{}, Turn{}, Phase4Attempt{}, err
	}
	encodedPolicy, err := json.Marshal(dispatch.Snapshot.Policy)
	if err != nil {
		return Worker{}, Turn{}, Phase4Attempt{}, err
	}
	return s.CreateWorker(ctx, conversationID, WorkerSpec{WorkerRef: "", Title: intent, Intent: intent, ProjectID: dispatch.Project.ID, NodeID: string(dispatch.Node), HarnessInstanceID: string(dispatch.HarnessInstance.ID), PolicySnapshot: string(encodedPolicy), ProjectSnapshot: string(encodedSnapshot), Workspace: dispatch.Workspace, IdempotencyKey: idempotencyKey}, TurnSpec{Input: intent, IdempotencyKey: idempotencyKey})
}
