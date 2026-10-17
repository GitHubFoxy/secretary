package core

import (
    "errors"
    "strings"
    "unicode"
)

// Workspace paths are persisted and checked on the Server, which can run a
// different OS from the Execution Node. This is purely lexical validation:
// filesystem existence, symlinks and authoritative containment are checked
// separately on the Node running the Task.
type workspacePathStyle uint8
const (
    workspacePathInvalid workspacePathStyle = iota
    workspacePathPOSIX
    workspacePathWindows
)

func ValidateWorkspaceRootPath(value string) error {
    _, err := workspacePathStyleOf(value)
    return err
}

func validateRootPath(value string) error {
    return ValidateWorkspaceRootPath(value)
}

func workspacePathStyleOf(value string) (workspacePathStyle, error) {
    if value == "" || strings.TrimSpace(value) != value ||
       strings.IndexFunc(value, unicode.IsControl) >= 0 {
        return workspacePathInvalid, errors.New("path must be an absolute, clean path")
    }
    if strings.HasPrefix(value, "/") {
        if value == "/" {
            return workspacePathInvalid, errors.New("filesystem root is not an allowed workspace root")
        }
        if strings.Contains(value, "\\") || strings.HasPrefix(value, "//") || !cleanComponents(value[1:], "/") {
            return workspacePathInvalid, errors.New("POSIX path must be clean and contain no traversal")
        }
        return workspacePathPOSIX, nil
    }
    if len(value) >= 3 && isASCIILetter(value[0]) && value[1] == ':' && value[2] == '\\' {
        if len(value) == 3 {
            return workspacePathInvalid, errors.New("drive root is not an allowed workspace root")
        }
        if strings.ContainsAny(value[3:], "/:*?\"<>|") || !cleanComponents(value[3:], "\\") {
            return workspacePathInvalid, errors.New("Windows path must be clean and contain no traversal")
        }
        return workspacePathWindows, nil
    }
    if strings.HasPrefix(value, "\\\\") {
        rest := value[2:]
        parts := strings.Split(rest, "\\")
        // A share root, or an incomplete UNC path, must not be a Workspace.
        if len(parts) < 3 || strings.ContainsAny(rest, "/:*?\"<>|") || !cleanComponents(rest, "\\") {
            return workspacePathInvalid, errors.New("Windows UNC workspace path must contain a share and a subdirectory")
        }
        return workspacePathWindows, nil
    }
    return workspacePathInvalid, errors.New("path must be absolute")
}

func cleanComponents(value, separator string) bool {
    if value == "" {
        return false
    }
    for _, component := range strings.Split(value, separator) {
        if component == "" || component == "." || component == ".." {
            return false
        }
    }
    return true
}

func isASCIILetter(value byte) bool {
    return (value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z')
}

func workspacePathsEqual(left, right string) bool {
    leftStyle, leftErr := workspacePathStyleOf(left)
    rightStyle, rightErr := workspacePathStyleOf(right)
    if leftErr != nil || rightErr != nil || leftStyle != rightStyle {
        return false
    }
    if leftStyle == workspacePathWindows {
        return strings.EqualFold(left, right)
    }
    return left == right
}

// Lexical containment may be applied across Server and Node platforms.
// A prefix must end on a path separator, not merely share some characters.
func pathContained(root, candidate string) bool {
    rootStyle, rootErr := workspacePathStyleOf(root)
    candidateStyle, candidateErr := workspacePathStyleOf(candidate)
    if rootErr != nil || candidateErr != nil || rootStyle != candidateStyle {
        return false
    }
    separator := "/"
    if rootStyle == workspacePathWindows {
        separator = "\\"
        root, candidate = strings.ToLower(root), strings.ToLower(candidate)
    }
    return candidate == root || strings.HasPrefix(candidate, root+separator)
}
