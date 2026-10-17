package node

import "testing"

// Server-side wire validation must not apply its own filepath.IsAbs to a
// remote Windows Node mapping.
func TestProtocolWorkspaceAcceptsRemoteWindowsPath(t *testing.T) {
    for _, value := range []string{"C:\\src\\project", "\\\\server\\share\\repo"} {
        if err := (Workspace{ProjectID: "project-id", Path: value}).Validate(); err != nil {
            t.Fatalf("remote Windows path %q rejected by Server protocol: %v", value, err)
        }
    }
    if err := (Workspace{ProjectID: "project-id", Path: "C:\\src\\project\\..\\private"}).Validate(); err == nil {
        t.Fatal("protocol accepted Windows traversal")
    }
}
