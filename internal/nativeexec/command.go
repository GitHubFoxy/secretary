// Package nativeexec launches native harnesses without treating npm PowerShell
// shims as executables. Windows .cmd shims require cmd.exe; .ps1 files are
// never executed or silently substituted with another harness.
package nativeexec

import (
    "context"
    "os/exec"
)

func CommandContext(ctx context.Context, command string, args ...string) (*exec.Cmd, error) {
    executable, argv, err := invocation(command, args)
    if err != nil {
        return nil, err
    }
    if policy, ok := workerSandboxFromContext(ctx); ok {
        executable, argv, err = sandboxInvocation(policy, executable, argv)
        if err != nil { return nil, err }
    }
    cmd := exec.CommandContext(ctx, executable, argv...)
    configureCommandLine(cmd, executable, argv)
    if policy, ok := workerSandboxFromContext(ctx); ok {
        if err := applyWorkerSandbox(policy, cmd); err != nil { return nil, err }
    }
    return cmd, nil
}
