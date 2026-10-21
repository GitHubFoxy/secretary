//go:build !windows

package acp

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Native ACP harnesses may start shells, code-mode hosts and MCP children.
// A separate process group preserves lifecycle cancellation; it is not a sandbox.
func configureACPProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
			if directErr := cmd.Process.Kill(); directErr != nil && !errors.Is(directErr, os.ErrProcessDone) {
				return errors.Join(err, directErr)
			}
		}
		return nil
	}
}
