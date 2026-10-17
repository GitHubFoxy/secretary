//go:build !windows

package node

import (
    "errors"
    "os/exec"
    "syscall"
)

func configureClaudeProcess(cmd *exec.Cmd) {
    cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killClaudeProcess(cmd *exec.Cmd) error {
    if cmd.Process == nil {
        return nil
    }
    if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
        return err
    }
    return nil
}
