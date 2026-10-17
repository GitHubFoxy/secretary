//go:build windows

package node

import (
    "errors"
    "os"
    "os/exec"
    "path/filepath"
    "strconv"
)

func configureClaudeProcess(cmd *exec.Cmd) {}

// taskkill /T terminates descendants as well as the native harness process.
// A missing or failing taskkill falls back to terminating the direct process.
func killClaudeProcess(cmd *exec.Cmd) error {
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
