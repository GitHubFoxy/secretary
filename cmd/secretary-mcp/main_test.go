package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExecutableRejectsLegacyWorkerRoles(t *testing.T) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test source")
	}
	packageDir := filepath.Dir(sourceFile)
	binary := filepath.Join(t.TempDir(), "secretary-mcp")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = packageDir
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build secretary-mcp: %v\n%s", err, output)
	}

	for _, role := range []string{"worker", "child_worker"} {
		t.Run(role, func(t *testing.T) {
			dataDir := t.TempDir()
			command := exec.Command(binary, "--role", role)
			command.Dir = packageDir
			command.Env = append(os.Environ(),
				"SECRETARY_MCP_DATA_DIR="+dataDir,
				"SECRETARY_MCP_CAPABILITY=test-capability",
			)
			output, err := command.CombinedOutput()
			if err == nil {
				t.Fatalf("legacy role %q was accepted, output: %s", role, output)
			}
			if !strings.Contains(string(output), "unknown MCP role") {
				t.Fatalf("role %q failed for the wrong reason: %v\n%s", role, err, output)
			}
		})
	}
}
