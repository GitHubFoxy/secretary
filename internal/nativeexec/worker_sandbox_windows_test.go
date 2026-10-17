//go:build windows

package nativeexec

import (
	"context"
	"encoding/base64"
	"unicode/utf16"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This runs on windows-latest using a separately provisioned non-admin Windows
// account. It asserts both denied Node secret reads and persistent native CLI
// session state across two independently launched Worker processes.
func TestT09WindowsSeparateAccountStateAndResume(t *testing.T) {
	workspace := os.Getenv("SECRETARY_WORKER_TEST_WORKSPACE")
	protected := os.Getenv("SECRETARY_WORKER_TEST_PROTECTED_DIR")
	home := os.Getenv("SECRETARY_WORKER_HOME")
	if workspace == "" || protected == "" || home == "" {
		t.Skip("cross-SID Windows Worker integration requires provisioned account")
	}
	if err := os.WriteFile(filepath.Join(protected,"identity.json"),[]byte("NODE-CREDENTIAL-SECRET"),0600); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(protected,"node-state.json"),[]byte("NODE-ATTEMPT-METADATA"),0600); err != nil { t.Fatal(err) }
	ctx := WithWorkerSandbox(context.Background(), WorkerSandbox{ProtectedDir:protected, Workspace:workspace})
	tests := []struct{name, dir string}{
		{"Codex",filepath.Join(home,".codex")},
		{"Claude",filepath.Join(home,".claude")},
		{"OpenCode",filepath.Join(home,"opencode-native")},
	}
	for _, tc := range tests {
		t.Run(tc.name,func(t *testing.T) {
			state := filepath.Join(tc.dir,"resume.txt")
			for _, script := range []string{
				`Set-Content -NoNewline -LiteralPath '`+state+`' -Value 'SESSION-READY'`,
				`Get-Content -LiteralPath '`+state+`'`,
			} {
				encoded := utf16.Encode([]rune(script))
				command := make([]byte, 0, len(encoded)*2)
				for _, unit := range encoded { command = append(command, byte(unit), byte(unit>>8)) }
				cmd, err := CommandContext(ctx,"powershell.exe","-NoProfile","-NonInteractive","-EncodedCommand",base64.StdEncoding.EncodeToString(command))
				if err != nil { t.Fatal(err) }
				cmd.Env = SecureWorkerEnvironment(ctx,cmd.Env)
				out, err := cmd.CombinedOutput()
				if err != nil { t.Fatalf("worker Start/Resume failed: %v: %s",err,out) }
				if strings.Contains(script,"Get-Content") && !strings.Contains(string(out),"SESSION-READY") {
					t.Fatalf("persistent Worker state lost across process restart: %s",out)
				}
			}
		})
	}
}
