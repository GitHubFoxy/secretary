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
		{DataHome: filepath.Join(t.TempDir(), "legacy-xdg"), Mode: node.OpenCodeNativeStoreModeLegacy},
	} {
		runtime := configuredNodeRuntimeWithStore(t.TempDir(), store).(node.RuntimeRouter).OpenCode.(node.OpenCodeRuntime)
		inventory := configuredNodeDiscovery("node-a", true, store)
		if runtime.DataHome != inventory.OpenCodeDataHome || runtime.LegacyDataHome != inventory.LegacyOpenCodeDataHome {
			t.Fatal("Node runtime and actual inventory selected different native stores")
		}
	}
}

func TestSelectNodeNativeStoreValidatesSharedPathAndConfigOnlyLegacyState(t *testing.T) {
	serverRoot := t.TempDir()
	nodeDataDir := filepath.Join(serverRoot, "node", "data")
	_, _, err := node.SelectSharedOpenCodeNativeStores(serverRoot, nodeDataDir)
	if err != nil {
		t.Fatal("could not prepare co-located shared store")
	}
	selected, err := selectNodeNativeStore(nodeDataDir, false)
	if err != nil || selected.Mode != node.OpenCodeNativeStoreModeShared || selected.DataHome != node.OpenCodeNativeDataHome(serverRoot) {
		t.Fatal("local Node did not validate and retain the co-located shared store")
	}

	wrongServerRoot := t.TempDir()
	wrongNodeData := filepath.Join(wrongServerRoot, "node", "data")
	wrongHome := filepath.Join(t.TempDir(), "opencode-native")
	if err := os.MkdirAll(wrongHome, 0o700); err != nil {
		t.Fatal("foreign store fixture unavailable")
	}
	selection := []byte(`{"version":1,"mode":"shared","data_home":"` + wrongHome + `"}`)
	if err := os.MkdirAll(wrongNodeData, 0o700); err != nil {
		t.Fatal("foreign Node fixture unavailable")
	}
	if err := os.WriteFile(filepath.Join(wrongNodeData, "opencode-native-selection.json"), selection, 0o600); err != nil {
		t.Fatal("foreign Node selection fixture unavailable")
	}
	if _, err := selectNodeNativeStore(wrongNodeData, false); err == nil {
		t.Fatal("Node accepted a shared store outside the co-located Secretary installation")
	}

	if _, err := selectNodeNativeStore(nodeDataDir, true); err == nil {
		t.Fatal("standalone Node accepted the co-located Secretary shared store")
	}

	legacyDataDir := filepath.Join(t.TempDir(), "node", "data")
	if err := os.MkdirAll(filepath.Dir(legacyDataDir), 0o700); err != nil {
		t.Fatal("legacy Node config directory unavailable")
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(legacyDataDir), "config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal("legacy Node config fixture unavailable")
	}
	legacyHome := filepath.Join(t.TempDir(), "legacy-xdg")
	t.Setenv("XDG_DATA_HOME", legacyHome)
	legacy, err := selectNodeNativeStore(legacyDataDir, false)
	if err != nil || legacy.Mode != node.OpenCodeNativeStoreModeLegacy || legacy.DataHome != legacyHome {
		t.Fatal("existing Node config without state was silently assigned an isolated store")
	}
}

func TestConfiguredNodeRuntimeDefaultsCodexAndRetainsHistoricalAdapters(t *testing.T) {
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
	if runtime.DefaultHarness != "codex" || runtime.OpenCode == nil || runtime.FX == nil || runtime.Claude == nil || runtime.ACP == nil {
		t.Fatal("Node runtime defaults or selectable adapters are incomplete")
	}
}
