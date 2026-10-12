package node

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func evalPath(t *testing.T, p string) string {
	t.Helper()
	eval, err := filepath.EvalSymlinks(p)
	if err == nil {
		return eval
	}
	return p
}

func TestResolveAntigravityExecutable_Success(t *testing.T) {
	dir := t.TempDir()
	companionName := "localharness_external"
	execName := "agy_acp_server"
	if runtime.GOOS == "windows" {
		companionName = "localharness_external.exe"
		execName = "agy_acp_server.exe"
	}
	execPath := filepath.Join(dir, execName)
	companionPath := filepath.Join(dir, companionName)

	if err := os.WriteFile(execPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(companionPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	resolvedExec, resolvedCompanion, err := ResolveAntigravityExecutable(execPath)
	if err != nil {
		t.Fatalf("unexpected error resolving executable: %v", err)
	}
	if evalPath(t, resolvedExec) != evalPath(t, execPath) {
		t.Fatalf("got executable %q; want %q", resolvedExec, execPath)
	}
	if evalPath(t, resolvedCompanion) != evalPath(t, companionPath) {
		t.Fatalf("got companion %q; want %q", resolvedCompanion, companionPath)
	}
}

func TestResolveAntigravityExecutable_Symlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test skipped on windows")
	}
	targetDir := t.TempDir()
	symlinkDir := t.TempDir()

	execPath := filepath.Join(targetDir, "agy_acp_server")
	companionPath := filepath.Join(targetDir, "localharness_external")
	linkPath := filepath.Join(symlinkDir, "agy_acp_server")

	if err := os.WriteFile(execPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(companionPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(execPath, linkPath); err != nil {
		t.Fatal(err)
	}

	resolvedExec, resolvedCompanion, err := ResolveAntigravityExecutable(linkPath)
	if err != nil {
		t.Fatalf("unexpected error resolving symlink executable: %v", err)
	}
	if evalPath(t, resolvedExec) != evalPath(t, execPath) {
		t.Fatalf("got executable %q; want %q", resolvedExec, execPath)
	}
	if evalPath(t, resolvedCompanion) != evalPath(t, companionPath) {
		t.Fatalf("got companion %q; want %q", resolvedCompanion, companionPath)
	}
}

func TestResolveAntigravityExecutable_MissingExecutable(t *testing.T) {
	_, _, err := ResolveAntigravityExecutable("/nonexistent/agy_acp_server")
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("expected unavailable error, got: %v", err)
	}
}

func TestResolveAntigravityExecutable_MissingCompanion(t *testing.T) {
	dir := t.TempDir()
	execName := "agy_acp_server"
	if runtime.GOOS == "windows" {
		execName = "agy_acp_server.exe"
	}
	execPath := filepath.Join(dir, execName)
	if err := os.WriteFile(execPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, _, err := ResolveAntigravityExecutable(execPath)
	if err == nil || !strings.Contains(err.Error(), "localharness_external") || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing companion error, got: %v", err)
	}
}

func TestAntigravityEnvironment_CleansAmbientCredentials(t *testing.T) {
	baseEnv := []string{
		"PATH=/usr/bin:/bin",
		"USER=testuser",
		"GOOGLE_API_KEY=secret_google_key",
		"GEMINI_API_KEY=secret_gemini_key",
		"GOOGLE_APPLICATION_CREDENTIALS=/path/to/creds.json",
		"GCLOUD_PROJECT=my-project",
		"ANTIGRAVITY_HARNESS_PATH=/stale/harness",
		"GEMINI_HOME=/stale/home",
	}

	profileDir := "/test/profile"
	tempDir := "/test/tmp"
	companionPath := "/test/bin/localharness_external"

	cleaned := BuildAntigravityEnvironment(baseEnv, companionPath, profileDir, tempDir)
	envMap := make(map[string]string)
	for _, entry := range cleaned {
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	for _, forbidden := range []string{
		"GOOGLE_API_KEY",
		"GEMINI_API_KEY",
		"GOOGLE_APPLICATION_CREDENTIALS",
		"GCLOUD_PROJECT",
	} {
		if val, exists := envMap[forbidden]; exists {
			t.Fatalf("forbidden ambient key leaked: %s=%s", forbidden, val)
		}
	}

	if envMap["ANTIGRAVITY_HARNESS_PATH"] != companionPath {
		t.Fatalf("ANTIGRAVITY_HARNESS_PATH = %q; want %q", envMap["ANTIGRAVITY_HARNESS_PATH"], companionPath)
	}
	if envMap["GEMINI_HOME"] != profileDir {
		t.Fatalf("GEMINI_HOME = %q; want %q", envMap["GEMINI_HOME"], profileDir)
	}
	if envMap["AGY_ACP_FORCE_FILE_STORAGE"] != "1" {
		t.Fatalf("AGY_ACP_FORCE_FILE_STORAGE = %q; want 1", envMap["AGY_ACP_FORCE_FILE_STORAGE"])
	}
	if envMap["PYTHONUNBUFFERED"] != "1" {
		t.Fatalf("PYTHONUNBUFFERED = %q; want 1", envMap["PYTHONUNBUFFERED"])
	}
	if runtime.GOOS == "windows" {
		if envMap["TEMP"] != tempDir || envMap["TMP"] != tempDir {
			t.Fatalf("TEMP/TMP not set to %q: %v", tempDir, envMap)
		}
	} else {
		if envMap["TMPDIR"] != tempDir {
			t.Fatalf("TMPDIR = %q; want %q", envMap["TMPDIR"], tempDir)
		}
	}
	if envMap["USER"] != "testuser" || envMap["PATH"] != "/usr/bin:/bin" {
		t.Fatalf("safe base environment lost: %v", envMap)
	}
}

func TestAntigravityPlatformArgs(t *testing.T) {
	args := BuildAntigravityPlatformArgs([]string{"--custom-flag"})
	if runtime.GOOS == "linux" {
		found := false
		for _, arg := range args {
			if arg == "--uid=" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected --uid= in platform args on linux: %v", args)
		}
	} else {
		for _, arg := range args {
			if arg == "--uid=" {
				t.Fatalf("unexpected --uid= on non-linux platform %s: %v", runtime.GOOS, args)
			}
		}
	}
}

func TestAntigravityPlatformArgs_PreservesUserUID(t *testing.T) {
	args := BuildAntigravityPlatformArgs([]string{"--uid=1234", "--verbose"})
	uidCount := 0
	for _, arg := range args {
		if strings.HasPrefix(arg, "--uid") {
			uidCount++
		}
	}
	if uidCount != 1 {
		t.Fatalf("expected exactly 1 --uid argument, got %d in %v", uidCount, args)
	}
	if args[0] != "--uid=1234" {
		t.Fatalf("expected preserved --uid=1234, got %q", args[0])
	}
}

func TestDefaultAntigravityProfileDir(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom-profile")
	t.Setenv("SECRETARY_ANTIGRAVITY_PROFILE_DIR", custom)
	if got := DefaultAntigravityProfileDir(); got != custom {
		t.Fatalf("got %q; want %q", got, custom)
	}
}
