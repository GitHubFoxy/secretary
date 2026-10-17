package ctl

import (
	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

// projectScopedNodeEnvelope keeps checkpoint fixture paths explicitly authorized
// under a Project instead of using an untrusted projectless Workspace override.
func projectScopedNodeEnvelope(envelope node.WorkerEnvelope) node.WorkerEnvelope {
	project := core.Project{
		ID: "synthetic-checkpoint-project",
		Name: "Synthetic checkpoint project",
		Mappings: []core.ProjectPathMapping{{Node: envelope.HarnessInstance.Node, Path: envelope.Workspace}},
		Revision: 1,
	}
	envelope.ProjectID = project.ID
	envelope.ProjectSnapshot = project.Snapshot(envelope.HarnessInstance.Node, envelope.Workspace, envelope.HarnessInstance)
	return envelope
}
