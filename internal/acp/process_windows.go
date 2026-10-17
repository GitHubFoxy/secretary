//go:build windows

package acp

import (
    "errors"
    "os"
    "os/exec"
    "path/filepath"
    "strconv"
    "time"
)

// Native Windows harnesses installed via npm may launch through .cmd shims.
// Killing only cmd.exe can leave the actual ACP agent and its pipe handles alive.
func configureACPProcess(cmd *exec.Cmd) {
    cmd.WaitDelay = 2 * time.Second
    cmd.Cancel = func() error {
        if cmd.Process == nil {
            return nil
        }
        if root := os.Getenv("SystemRoot"); root != "" {
            binary := filepath.Join(root, "System32", "taskkill.exe")
            if err := exec.Command(binary, "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err == nil {
                return nil
            }
        }
        err := cmd.Process.Kill()
        if errors.Is(err, os.ErrProcessDone) {
            return nil
        }
        return err
    }
}
