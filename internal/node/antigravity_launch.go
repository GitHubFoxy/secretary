package node

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	AntigravityDefaultBinary = "agy_acp_server"
	AntigravityCompanionName = "localharness_external"
)

var (
	ErrAntigravityBinaryNotFound   = errors.New("antigravity: executable is unavailable")
	ErrAntigravityCompanionMissing = errors.New("antigravity: companion is missing")
)

var ambientGoogleKeys = map[string]struct{}{
	"GEMINI_API_KEY":                 {},
	"GOOGLE_API_KEY":                 {},
	"GOOGLE_APPLICATION_CREDENTIALS": {},
	"GOOGLE_CLOUD_PROJECT":           {},
	"GOOGLE_CLOUD_LOCATION":          {},
	"GOOGLE_CLOUD_QUOTA_PROJECT":     {},
	"GOOGLE_GENAI_USE_VERTEXAI":      {},
	"GCLOUD_PROJECT":                 {},
	"CLOUDSDK_CORE_PROJECT":          {},
	"AGY_ACP_CCPA_PROJECT":           {},
	"AGY_ACP_ENABLE_OAUTH":           {},
	"GEMINI_HOME":                    {},
	"AGY_ACP_FORCE_FILE_STORAGE":     {},
	"ANTIGRAVITY_HARNESS_PATH":       {},
	"PYTHONUNBUFFERED":               {},
}

func AntigravityBinaryNames() []string {
	if runtime.GOOS == "windows" {
		return []string{"agy_acp_server.exe", "agy_acp_server"}
	}
	return []string{"agy_acp_server", "agy_acp_server.par"}
}

func AntigravityCompanionBinaryName() string {
	if runtime.GOOS == "windows" {
		return "localharness_external.exe"
	}
	return "localharness_external"
}

func ResolveAntigravityExecutable(commandOverride string) (string, string, error) {
	command := strings.TrimSpace(commandOverride)
	if command == "" {
		command = strings.TrimSpace(os.Getenv("SECRETARY_ANTIGRAVITY_COMMAND"))
	}
	var candidatePath string
	if command != "" {
		if filepath.IsAbs(command) || strings.ContainsRune(command, filepath.Separator) || strings.ContainsRune(command, '/') {
			abs, err := filepath.Abs(command)
			if err != nil {
				return "", "", fmt.Errorf("%w: %q: %v", ErrAntigravityBinaryNotFound, command, err)
			}
			candidatePath = abs
		} else {
			looked, err := exec.LookPath(command)
			if err != nil {
				return "", "", fmt.Errorf("%w: %q", ErrAntigravityBinaryNotFound, command)
			}
			candidatePath = looked
		}
	} else {
		for _, name := range AntigravityBinaryNames() {
			looked, err := exec.LookPath(name)
			if err == nil {
				candidatePath = looked
				break
			}
		}
		if candidatePath == "" {
			return "", "", fmt.Errorf("%w: agy_acp_server", ErrAntigravityBinaryNotFound)
		}
	}

	info, err := os.Stat(candidatePath)
	if err != nil || info.IsDir() {
		return "", "", fmt.Errorf("%w: %q", ErrAntigravityBinaryNotFound, candidatePath)
	}

	realExecPath, err := filepath.EvalSymlinks(candidatePath)
	if err != nil {
		return "", "", fmt.Errorf("antigravity: resolve symlink for %q: %w", candidatePath, err)
	}
	realExecPath, err = filepath.Abs(realExecPath)
	if err != nil {
		return "", "", fmt.Errorf("antigravity: resolve path for %q: %w", candidatePath, err)
	}

	companionName := AntigravityCompanionBinaryName()
	companionPath := filepath.Join(filepath.Dir(realExecPath), companionName)
	cinfo, err := os.Stat(companionPath)
	if err != nil || cinfo.IsDir() {
		return "", "", fmt.Errorf("%w: companion %q is missing beside executable %q", ErrAntigravityCompanionMissing, companionName, realExecPath)
	}

	realCompanionPath, err := filepath.EvalSymlinks(companionPath)
	if err == nil {
		companionPath, _ = filepath.Abs(realCompanionPath)
	}

	return realExecPath, companionPath, nil
}

func BuildAntigravityEnvironment(baseEnv []string, companionPath, profileDir, tempDir string) []string {
	var cleaned []string
	for _, entry := range baseEnv {
		key := entry
		if idx := strings.IndexByte(entry, '='); idx >= 0 {
			key = entry[:idx]
		}
		if _, forbidden := ambientGoogleKeys[strings.ToUpper(key)]; forbidden {
			continue
		}
		if strings.EqualFold(key, "TMPDIR") || strings.EqualFold(key, "TEMP") || strings.EqualFold(key, "TMP") {
			continue
		}
		cleaned = append(cleaned, entry)
	}

	cleaned = append(cleaned,
		"ANTIGRAVITY_HARNESS_PATH="+companionPath,
		"GEMINI_HOME="+profileDir,
		"AGY_ACP_FORCE_FILE_STORAGE=1",
		"PYTHONUNBUFFERED=1",
	)

	if tempDir != "" {
		if runtime.GOOS == "windows" {
			cleaned = append(cleaned, "TEMP="+tempDir, "TMP="+tempDir)
		} else {
			cleaned = append(cleaned, "TMPDIR="+tempDir)
		}
	}

	return cleaned
}

func BuildAntigravityPlatformArgs(baseArgs []string) []string {
	args := append([]string(nil), baseArgs...)
	if runtime.GOOS == "linux" {
		hasUID := false
		for _, arg := range args {
			if strings.HasPrefix(arg, "--uid=") {
				hasUID = true
				break
			}
		}
		if !hasUID {
			args = append(args, fmt.Sprintf("--uid=%d", os.Getuid()))
		}
	}
	return args
}
