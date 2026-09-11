package node

import (
	"context"
	"fmt"
	"os"
)

// OpenCodeRuntime uses OpenCode's native agent configuration. The effective
// Profile is written to a per-session opencode.json and selected as the
// default primary agent, so it becomes OpenCode's agent system instruction.
type OpenCodeRuntime struct {
	Command        string
	Arguments      []string
	RawLogDir      string
	RawLogMaxBytes int64
	RawLogFiles    int
}

func (r OpenCodeRuntime) Start(ctx context.Context, request StartRequest) (Session, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	prepared, err := r.prepare(request)
	if err != nil {
		return nil, err
	}
	return prepared.Start(ctx, request)
}

func (r OpenCodeRuntime) Resume(ctx context.Context, request StartRequest, runtimeSessionID string) (Session, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	prepared, err := r.prepare(request)
	if err != nil {
		return nil, err
	}
	return prepared.Resume(ctx, request, runtimeSessionID)
}

func (r OpenCodeRuntime) prepare(request StartRequest) (ACPRuntime, error) {
	if request.Workspace == "" {
		return ACPRuntime{}, fmt.Errorf("opencode: workspace is required")
	}
	if request.Profile.Name == "" || request.Profile.Content == "" {
		return ACPRuntime{}, fmt.Errorf("opencode: managed Profile is required")
	}
	configPath, err := request.Profile.openCodeConfig(request.Workspace)
	if err != nil {
		return ACPRuntime{}, err
	}
	configContent, err := os.ReadFile(configPath)
	if err != nil {
		return ACPRuntime{}, fmt.Errorf("read OpenCode config: %w", err)
	}
	command := r.Command
	if command == "" {
		command = "opencode"
	}
	arguments := append([]string(nil), r.Arguments...)
	if len(arguments) == 0 {
		arguments = []string{"acp"}
	}
	return ACPRuntime{Command: command, Arguments: arguments, RawLogDir: r.RawLogDir, RawLogMaxBytes: r.RawLogMaxBytes, RawLogFiles: r.RawLogFiles, Environment: []string{"OPENCODE_CONFIG=" + configPath, "OPENCODE_CONFIG_CONTENT=" + string(configContent)}}, nil
}
