package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

type workspaceEdits []node.WorkspaceMapping

func (e *workspaceEdits) String() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%v", []node.WorkspaceMapping(*e))
}

func (e *workspaceEdits) Set(value string) error {
	project, path, ok := strings.Cut(value, "=")
	if !ok || strings.TrimSpace(project) == "" || !filepath.IsAbs(path) {
		return errors.New("--workspace requires project=absolute-path")
	}
	for _, existing := range *e {
		if existing.ProjectID == project {
			return fmt.Errorf("duplicate --workspace for project %q", project)
		}
	}
	*e = append(*e, node.WorkspaceMapping{ProjectID: project, Path: path})
	return nil
}

type deploymentEdits struct {
	ServerURL       *string
	NodeName        *string
	Capacity        *int
	Standalone      *bool
	ClearWorkspaces bool
	Workspaces      []node.WorkspaceMapping
}

func editDeploymentConfig(path string, edits deploymentEdits) error {
	if path == "" {
		return errors.New("--config is required for --edit-deployment-config")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("Node config must be a regular file, not a symlink")
	}
	current, err := node.LoadDeploymentConfig(path)
	if err != nil {
		return err
	}
	candidate := current
	if edits.ServerURL != nil {
		candidate.ServerURL = *edits.ServerURL
	}
	if edits.NodeName != nil {
		candidate.Node = core.NodeReference(*edits.NodeName)
	}
	if edits.Capacity != nil {
		candidate.Capacity = *edits.Capacity
	}
	if edits.Standalone != nil {
		candidate.Standalone = *edits.Standalone
	}
	if edits.ClearWorkspaces {
		candidate.Workspaces = nil
	}
	if len(edits.Workspaces) > 0 {
		candidate.Workspaces = append([]node.WorkspaceMapping(nil), candidate.Workspaces...)
		indices := make(map[string]int, len(candidate.Workspaces))
		for i, mapping := range candidate.Workspaces {
			indices[mapping.ProjectID] = i
		}
		for _, mapping := range edits.Workspaces {
			if i, ok := indices[mapping.ProjectID]; ok {
				candidate.Workspaces[i] = mapping
			} else {
				indices[mapping.ProjectID] = len(candidate.Workspaces)
				candidate.Workspaces = append(candidate.Workspaces, mapping)
			}
		}
	}
	if err := candidate.Validate(); err != nil {
		return err
	}
	if candidate.Node != current.Node || candidate.ServerURL != current.ServerURL {
		identityPath := filepath.Join(current.DataDir, "identity.json")
		if _, err := os.Lstat(identityPath); err == nil {
			return errors.New("paired Node name/server cannot be changed without explicit drain, revoke and re-enrollment")
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("check Node identity: %w", err)
		}
	}
	return node.SaveDeploymentConfig(path, candidate)
}
