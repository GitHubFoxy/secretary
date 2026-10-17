package node

import "github.com/beruseruko/secretary/internal/core"

// authorizedNodeTestWorkspace is used only for test fixtures that intentionally
// exercise an explicit Project workspace rather than a projectless Node-owned one.
func authorizedNodeTestWorkspace(envelope WorkerEnvelope) WorkerEnvelope {
	project := core.Project{
		ID: "authorized-node-fixture-project",
		Name: "Authorized Node fixture Project",
		Mappings: []core.ProjectPathMapping{{Node: envelope.HarnessInstance.Node, Path: envelope.Workspace}},
		Revision: 1,
	}
	envelope.ProjectID = project.ID
	envelope.ProjectSnapshot = project.Snapshot(envelope.HarnessInstance.Node, envelope.Workspace, envelope.HarnessInstance)
	return envelope
}
