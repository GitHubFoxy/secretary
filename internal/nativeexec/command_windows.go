//go:build windows

package nativeexec

import (
    "errors"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "strings"
)

// Resolve normal Windows executables and npm .cmd shims. A PowerShell
// ExternalScript (.ps1) cannot be passed to Go CreateProcess.
func invocation(command string, args []string) (string, []string, error) {
    if strings.TrimSpace(command) == "" {
        return "", nil, errors.New("native executable is required")
    }
    extension := strings.ToLower(filepath.Ext(command))
    if extension != "" && extension != ".cmd" && extension != ".exe" {
        return "", nil, fmt.Errorf("unsupported Windows native command %q: use an .exe or .cmd shim (not PowerShell .ps1)", command)
    }
    var path string
    var err error
    if extension == "" {
        // Never resolve .ps1 via PATHEXT. Do not change an explicit executable.
        for _, suffix := range []string{".exe", ".cmd"} {
            path, err = exec.LookPath(command + suffix)
            if err == nil {
                break
            }
        }
        if err != nil {
            return "", nil, fmt.Errorf("Windows native executable %q (.exe/.cmd) unavailable: %w", command, err)
        }
    } else {
        path, err = exec.LookPath(command)
        if err != nil {
            return "", nil, fmt.Errorf("Windows native executable %q unavailable: %w", command, err)
        }
    }
    if strings.EqualFold(filepath.Ext(path), ".exe") {
        return path, args, nil
    }
    if !strings.EqualFold(filepath.Ext(path), ".cmd") {
        return "", nil, fmt.Errorf("unsupported Windows native command %q: expected .exe or .cmd", path)
    }

    // cmd.exe interprets metacharacters and expands %ENV%. Disallow such
    // characters in configured paths/argv rather than risk command injection.
    tokens := append([]string{path}, args...)
    quoted := make([]string, 0, len(tokens))
    for _, token := range tokens {
        if strings.ContainsAny(token, "\"
%&|<>^!") {
            return "", nil, errors.New("unsafe argument or path for Windows .cmd native shim")
        }
        quoted = append(quoted, "\"" + token + "\"")
    }
    root := os.Getenv("SystemRoot")
    if root == "" {
        return "", nil, errors.New("Windows SystemRoot is required to launch an npm .cmd shim")
    }
    cmdExe := filepath.Join(root, "System32", "cmd.exe")
    // The /S switch removes exactly the outer quote pair. Quoted shim
    // paths then work even if npm lives under Program Files.
    return cmdExe, []string{"/d", "/s", "/c", "\"" + strings.Join(quoted, " ") + "\""}, nil
}
