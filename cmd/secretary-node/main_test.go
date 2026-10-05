package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

func TestOpenCodeProbeAndRuntimeUseSameExplicitBinary(t *testing.T) {
	root := t.TempDir()
	pathBinary, explicitBinary := filepath.Join(root, "opencode"), filepath.Join(root, "opencode-v2")
	for _, binary := range []string{pathBinary, explicitBinary} {
		if err := os.WriteFile(binary, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", root)
	t.Setenv("SECRETARY_OPENCODE_COMMAND", explicitBinary)
	runtime := configuredNodeRuntime(root).(node.RuntimeRouter).OpenCode.(node.OpenCodeRuntime)
	if runtime.Command != explicitBinary || installedHarnesses()[core.HarnessOpenCode] != explicitBinary {
		t.Fatal("OpenCode runtime and inventory selected different binaries")
	}
	missing := filepath.Join(root, "missing-opencode-v2")
	t.Setenv("SECRETARY_OPENCODE_COMMAND", missing)
	if installedHarnesses()[core.HarnessOpenCode] != missing {
		t.Fatal("missing explicit OpenCode silently fell back to PATH")
	}
}

func TestConfiguredNodeRuntimeUsesStableWorkerNativeStore(t *testing.T) {
	root := t.TempDir()
	runtime := configuredNodeRuntime(root).(node.RuntimeRouter)
	openCode := runtime.OpenCode.(node.OpenCodeRuntime)
	if openCode.DataHome != node.OpenCodeNativeDataHome(root) {
		t.Fatal("Node OpenCode runtime does not use its stable private native store")
	}
}

func TestConfiguredNodeRuntimeAndInventoryShareNativeStore(t *testing.T) {
	for _, store := range []node.OpenCodeNativeStore{
		{DataHome: filepath.Join(t.TempDir(), "private-native")},
		{DataHome: filepath.Join(t.TempDir(), "legacy-xdg"), Legacy: true, MigrationRequired: true},
	} {
		runtime := configuredNodeRuntimeWithStore(t.TempDir(), store).(node.RuntimeRouter).OpenCode.(node.OpenCodeRuntime)
		inventory := configuredNodeDiscovery("node-a", true, store)
		if runtime.DataHome != inventory.OpenCodeDataHome || runtime.LegacyDataHome != inventory.LegacyOpenCodeDataHome {
			t.Fatal("Node runtime and actual inventory selected different native stores")
		}
	}
}

func TestConfiguredNodeRuntimeDefaultsOpenCodeAndRetainsSelectableAdapters(t *testing.T) {
	for _, key := range []string{
		"SECRETARY_ACP_COMMAND", "SECRETARY_ACP_ARGS", "SECRETARY_CLAUDE_COMMAND", "SECRETARY_CLAUDE_ARGS",
		"SECRETARY_FX_COMMAND", "SECRETARY_FX_ARGS", "SECRETARY_OPENCODE_COMMAND", "SECRETARY_OPENCODE_ARGS",
	} {
		t.Setenv(key, "")
	}
	runtime, ok := configuredNodeRuntime(t.TempDir()).(node.RuntimeRouter)
	if !ok {
		t.Fatal("Node runtime is not a harness router")
	}
	if runtime.DefaultHarness != "opencode" || runtime.OpenCode == nil || runtime.FX == nil || runtime.Claude == nil || runtime.ACP == nil {
		t.Fatal("Node runtime defaults or selectable adapters are incomplete")
	}
}
