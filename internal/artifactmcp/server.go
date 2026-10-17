package artifactmcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/mcp"
	"github.com/beruseruko/secretary/internal/node"
)

type Handler struct {
	Publisher node.ArtifactPublisher
}

func FromEnvironment() (Handler, error) {
	publisher := node.ArtifactPublisher{
		ServerURL: os.Getenv("SECRETARY_ARTIFACT_SERVER"),
		Node: core.NodeReference(os.Getenv("SECRETARY_ARTIFACT_NODE")),
		WorkerRef: os.Getenv("SECRETARY_ARTIFACT_WORKER"),
		AttemptID: os.Getenv("SECRETARY_ARTIFACT_ATTEMPT"),
		Workspace: os.Getenv("SECRETARY_ARTIFACT_WORKSPACE"),
		Capability: os.Getenv("SECRETARY_ARTIFACT_CAPABILITY"),
	}
	if publisher.ServerURL == "" || publisher.Node == "" || publisher.WorkerRef == "" || publisher.AttemptID == "" || publisher.Workspace == "" || publisher.Capability == "" {
		return Handler{}, errors.New("artifact MCP: missing scoped Worker publication configuration")
	}
	return Handler{Publisher: publisher}, nil
}

func (h Handler) Tools() []mcp.Tool {
	return []mcp.Tool{{
		Name: "publish_artifact",
		Description: "Explicitly publish a completed regular output file from this Worker's workspace to the owner's durable Secretary Artifact Store. Returns an artifact:// reference and Web download. Only relative paths to allowed documents, images or ZIP files, up to 8 MiB; no symlinks or hidden paths.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]string{"type": "string", "description": "Path relative to the Worker workspace"},
				"name": map[string]string{"type": "string", "description": "Optional download filename"},
			},
			"required": []string{"path"},
		},
	}}
}

func (h Handler) Call(ctx context.Context, name string, raw json.RawMessage) (any, error) {
	if name != "publish_artifact" {
		return nil, errors.New("artifact MCP: unknown tool")
	}
	var args struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, errors.New("artifact MCP: invalid input")
	}
	artifact, err := h.Publisher.Publish(ctx, args.Path, args.Name)
	if err != nil {
		return nil, err
	}
	return map[string]any{"reference": "artifact://" + artifact.ID, "artifact": artifact}, nil
}

func Serve(ctx context.Context) error {
	handler, err := FromEnvironment()
	if err != nil {
		return err
	}
	return (mcp.Server{Handler: handler}).Serve(ctx, os.Stdin, os.Stdout)
}
