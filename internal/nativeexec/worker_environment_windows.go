//go:build windows

package nativeexec

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// Native providers need the *Worker account's* persistent HOME and auth
// profiles, never the trusted Node account's user profile paths.
func workerPlatformEnvironment(_ context.Context, entries []string) []string {
	home := strings.TrimSpace(os.Getenv("SECRETARY_WORKER_HOME"))
	if home == "" { return entries } // Startup validates account-specific HOME.
	out := make([]string, 0, len(entries)+6)
	for _, entry := range entries {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "HOME", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "APPDATA", "LOCALAPPDATA", "CODEX_HOME", "CLAUDE_CONFIG_DIR":
			continue
		}
		out = append(out, entry)
	}
	out = append(out,
		"HOME="+home,
		"USERPROFILE="+home,
		"APPDATA="+filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA="+filepath.Join(home, "AppData", "Local"),
		"CODEX_HOME="+filepath.Join(home, ".codex"),
		"CLAUDE_CONFIG_DIR="+filepath.Join(home, ".claude"))
	return out
}
