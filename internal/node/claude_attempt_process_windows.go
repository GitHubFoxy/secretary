//go:build windows

package node

import (
	"context"
	"os/exec"
)

type claudeChild struct{}

func captureClaudeRoot(*exec.Cmd) (claudeChild, error) {
	return claudeChild{}, nil
}

func cleanupClaudeAttemptProcesses(context.Context, *exec.Cmd, claudeChild) error {
	return nil
}
