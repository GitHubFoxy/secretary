//go:build windows

package node

import (
	"context"
	"os/exec"
)

type claudeChild struct{ pid int }

func captureClaudeRoot(cmd *exec.Cmd) (claudeChild, error) {
	return claudeChild{pid: cmd.Process.Pid}, nil
}

func cleanupClaudeAttemptProcesses(context.Context, *exec.Cmd, claudeChild) error {
	return nil
}
