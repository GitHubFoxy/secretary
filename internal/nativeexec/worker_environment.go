package nativeexec

import (
	"context"
	"os"
	"runtime"
	"strings"
)

// SecureWorkerEnvironment constructs an explicit list of non-authority
// variables. A deny-by-prefix policy misses arbitrary Node-only secrets such
// as DATABASE_URL, AWS_SECRET_ACCESS_KEY and CUSTOM_PRIVATE_TOKEN.
func SecureWorkerEnvironment(ctx context.Context, entries []string) []string {
	if _, ok := workerSandboxFromContext(ctx); !ok { return entries }
	if entries == nil { entries = os.Environ() }
	result := make([]string,0,len(entries))
	for _, entry := range entries {
		name, _, found := strings.Cut(entry,"=")
		if !found || name == "" { continue }
		if !workerEnvironmentAllowed(name) { continue }
		result = append(result,entry)
	}
	return workerPlatformEnvironment(ctx, result)
}

func workerEnvironmentAllowed(name string) bool {
	// The Windows password/configuration never reaches its native child. These
	// are passed only to a trusted logon broker, which drops them before spawn.
	if runtime.GOOS == "windows" && strings.HasPrefix(name,"SECRETARY_WORKER_") { return true }
	if name == "SECRETARY_ARTIFACT_BROKER_URL" || strings.HasPrefix(name,"SECRETARY_CLAUDE_MCP_") { return true }
	if strings.HasPrefix(name,"LC_") { return true }
	switch name {
	case "PATH", "HOME", "USER", "USERNAME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH",
		"SHELL", "TERM", "LANG", "LANGUAGE", "TZ", "TMPDIR", "TMP", "TEMP",
		"APPDATA", "LOCALAPPDATA", "SystemRoot", "SYSTEMROOT", "WINDIR", "ComSpec",
		"COMSPEC", "PATHEXT", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432",
		"NUMBER_OF_PROCESSORS", "PROCESSOR_ARCHITECTURE", "CI", "NO_COLOR",
		"CLICOLOR", "CLICOLOR_FORCE", "COLORTERM",
		"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
		"CODEX_HOME", "CLAUDE_CONFIG_DIR",
		"OPENCODE_CONFIG_DIR", "OPENCODE_CONFIG", "OPENCODE_CONFIG_CONTENT",
		"OPENCODE_DISABLE_PROJECT_CONFIG":
		return true
	}
	return false
}
