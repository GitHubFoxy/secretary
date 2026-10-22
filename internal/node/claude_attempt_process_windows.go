//go:build windows

package node

import "os/exec"

func cleanupClaudeAttemptProcesses(*exec.Cmd) error {
	return nil
}
