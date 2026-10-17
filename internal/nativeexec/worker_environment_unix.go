//go:build !windows

package nativeexec

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
)

func workerPlatformEnvironment(ctx context.Context, entries []string) []string {
	policy, ok := workerSandboxFromContext(ctx)
	if !ok || runtime.GOOS != "darwin" { return entries }
	// Do not expose the host's global temporary directory, which could
	// contain a co-located Node/Server credential. Native tools write only
	// into a temp directory created inside their assigned workspace.
	out := make([]string,0,len(entries)+3)
	for _, entry := range entries {
		name, _, _ := strings.Cut(entry,"=")
		if name=="TMPDIR" || name=="TMP" || name=="TEMP" { continue }
		out=append(out, entry)
	}
	temp := filepath.Join(policy.Workspace, ".secretary-worker-tmp")
	return append(out,"TMPDIR="+temp,"TMP="+temp,"TEMP="+temp)
}
