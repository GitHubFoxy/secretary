package artifactmcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/beruseruko/secretary/internal/mcp"
	"github.com/beruseruko/secretary/internal/node"
)

type Handler struct {
	Endpoint string
}

func FromEnvironment() (Handler, error) {
	endpoint := os.Getenv("SECRETARY_ARTIFACT_BROKER_URL")
	if endpoint == "" {
		return Handler{}, errors.New("artifact MCP: scoped Node broker is unavailable")
	}
	return Handler{Endpoint: endpoint}, nil
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
	artifact, err := node.RequestWorkerArtifact(ctx, h.Endpoint, args.Path, args.Name)
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
