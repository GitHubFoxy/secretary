//go:build linux || darwin

package nativeexec

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func nativeSandboxPlatform() string { return runtime.GOOS }

func TestT09SandboxedNativeProviderHistorySurvivesProcessRestart(t *testing.T) {
	if !testing.Short() {
		switch {
		case nativeSandboxPlatform() == "linux":
			if _, err := exec.LookPath("bwrap"); err != nil { t.Skip("bubblewrap unavailable") }
		case nativeSandboxPlatform() == "darwin":
			if _, err := os.Stat("/usr/bin/sandbox-exec"); err != nil { t.Skip("Seatbelt unavailable") }
		}
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	nodeDir := filepath.Join(root, "node")
	workspace := filepath.Join(root, "workspace")
	for _, name := range []string{home, nodeDir, workspace, filepath.Join(home,".codex"), filepath.Join(home,".claude")} {
		if err := os.MkdirAll(name, 0700); err != nil { t.Fatal(err) }
	}
	t.Setenv("HOME",home)
	if err := os.WriteFile(filepath.Join(nodeDir,"identity.json"),[]byte("signing-secret"),0600); err != nil { t.Fatal(err) }
	opencode := filepath.Join(nodeDir, "opencode-native")
	if err := os.MkdirAll(opencode,0700); err != nil { t.Fatal(err) }
	for _, tc := range []struct{name, dir string; provider bool}{
		{"codex",filepath.Join(home,".codex"),false},
		{"claude_code",filepath.Join(home,".claude"),false},
		{"opencode",opencode,true},
	} {
		t.Run(tc.name,func(t *testing.T) {
			ctx := WithWorkerSandbox(context.Background(),WorkerSandbox{ProtectedDir:nodeDir,Workspace:workspace})
			if tc.provider { ctx = WithWorkerWritableDataHome(ctx,tc.dir) }
			checkpoint := filepath.Join(tc.dir,"native-session.checkpoint")
			// Start and finish one sandboxed CLI process. A separately
			// constructed process performs Resume using the same host state.
			for _, script := range []string{
				fmt.Sprintf("printf '%s' > '%s'",tc.name,checkpoint),
				fmt.Sprintf("test \"$(cat '%s')\" = '%s'",checkpoint,tc.name),
			} {
				cmd, err := CommandContext(ctx,"/bin/sh","-c",script)
				if err != nil { t.Fatal(err) }
				out, err := cmd.CombinedOutput()
				if err != nil {
					if runtime.GOOS == "linux" && strings.Contains(string(out),"Creating new namespace failed") {
						t.Skipf("host kernel denied unprivileged namespaces: %s",out)
					}
					t.Fatalf("native session state inaccessible on restart: %v: %s",err,out)
				}
			}
		})
	}
}
