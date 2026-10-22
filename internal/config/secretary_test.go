package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCleanInstallDefaultsSecretaryAndWorkersToCodex(t *testing.T) {
	manager, err := Open(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	config := manager.Snapshot().Config
	if config.Secretary.Harness != "codex" || config.Secretary.Model != "default" || config.Secretary.Reasoning != "default" {
		t.Fatalf("Secretary default policy=%#v", config.Secretary)
	}
	if config.WorkerPolicy.DefaultHarness != "codex" || config.WorkerPolicy.Model != "default" || config.WorkerPolicy.Reasoning != "default" || !reflect.DeepEqual(config.WorkerPolicy.PreferredHarnesses, []string{"codex", "claude_code"}) {
		t.Fatalf("Worker default policy=%#v", config.WorkerPolicy)
	}
	if config.Profiles.Secretary != "profiles/secretary.md" {
		t.Fatalf("external profile path changed: %#v", config.Profiles)
	}
	if got := (Config{}).EffectiveWorkerPolicy().DefaultHarness; got != "codex" {
		t.Fatalf("empty Worker policy fallback=%q", got)
	}
}

func TestCodexDefaultAddressedReplyAndExplicitVersionedOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	manager, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	legacy := manager.Snapshot()
	if legacy.Config.Secretary.ReplyContract != SecretaryReplyContractAddressedV1 || legacy.Profiles["secretary"].ReplyContractVersion != SecretaryReplyContractAddressedV1 || legacy.Profiles["secretary"].Runtime != "codex" || strings.Contains(legacy.Profiles["secretary"].Content, "## Addressed reply v1") {
		t.Fatalf("new official Codex setup must enable addressed lifecycle without rewriting its editable profile: %#v", legacy.Profiles["secretary"])
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(content), `reasoning = "default"`, `reasoning = "xhigh"`, 1)
	if updated == string(content) {
		t.Fatal("test could not add the explicit addressed-reply setting")
	}
	if err := os.WriteFile(path, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	optedIn, err := manager.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if optedIn.Config.Secretary.ReplyContract != "addressed-reply-v1" || optedIn.Profiles["secretary"].ReplyContractVersion != SecretaryReplyContractAddressedV1 || optedIn.Version == legacy.Version || optedIn.Profiles["secretary"].Hash == legacy.Profiles["secretary"].Hash {
		t.Fatalf("explicit setting did not version the managed Secretary profile: %#v", optedIn)
	}
	contractDisabled := strings.Replace(string(content), `reply_contract = "addressed-reply-v1"`, `# reply_contract intentionally absent`, 1)
	if contractDisabled == string(content) { t.Fatal("could not disable versioned reply contract") }
	if err := os.WriteFile(path, []byte(contractDisabled), 0o600); err != nil { t.Fatal(err) }
	withoutContract, err := manager.Reload()
	if err != nil { t.Fatal(err) }
	if withoutContract.Config.Secretary.ReplyContract != "" || withoutContract.Profiles["secretary"].ReplyContractVersion != "" || withoutContract.Profiles["secretary"].Hash == legacy.Profiles["secretary"].Hash || withoutContract.Version == legacy.Version {
		t.Fatalf("reply-contract removal did not independently version profile: %#v", withoutContract)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { t.Fatal(err) }
	reenabled, err := manager.Reload()
	if err != nil { t.Fatal(err) }
	if reenabled.Config.Secretary.ReplyContract != SecretaryReplyContractAddressedV1 || reenabled.Profiles["secretary"].ReplyContractVersion != SecretaryReplyContractAddressedV1 || reenabled.Profiles["secretary"].Hash != legacy.Profiles["secretary"].Hash || reenabled.Version != legacy.Version {
		t.Fatalf("reply-contract restoration did not restore exact profile identity: %#v", reenabled)
	}
	invalid := strings.Replace(updated, "addressed-reply-v1", "addressed-reply-v2", 1)
	if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Reload(); err == nil {
		t.Fatal("unknown addressed-reply version was accepted")
	}
	if manager.Snapshot().Version != optedIn.Version {
		t.Fatal("invalid reply contract replaced the active config")
	}
}

func TestSecretaryAndWorkerHarnessPoliciesAreIndependent(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"secretary.md", "worker.md", "child.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "config.toml")
	content := `[profiles]
secretary = "secretary.md"
worker = "worker.md"
child_worker = "child.md"
[tools]
allow_tools = ["read"]
[models]
secretary = "secretary-model"
fast = "fast"
smart = "smart"
cheap = "cheap"
[secretary]
harness = "codex"
model = "secretary-model"
reasoning = "high"
[worker_policy]
default_harness = "fx"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Config.Secretary.Harness != "codex" || snapshot.Config.Secretary.Model != "secretary-model" || snapshot.Config.Secretary.Reasoning != "high" {
		t.Fatalf("secretary policy=%#v", snapshot.Config.Secretary)
	}
	if snapshot.Config.WorkerPolicy.DefaultHarness != "fx" {
		t.Fatalf("worker policy=%#v", snapshot.Config.WorkerPolicy)
	}
	if snapshot.Profiles["secretary"].Runtime != "codex" || snapshot.Profiles["worker"].Runtime != "fx" {
		t.Fatalf("profiles=%#v", snapshot.Profiles)
	}
}

func TestInvalidReloadPreservesActiveConfigSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	manager, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	before := manager.Snapshot()
	if err := os.WriteFile(path, []byte("[secretary]\nharness = \"unknown\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Reload(); err == nil || !strings.Contains(err.Error(), "harness") {
		t.Fatalf("reload error=%v", err)
	}
	if manager.Snapshot().Version != before.Version {
		t.Fatal("invalid config replaced active snapshot")
	}
}

func TestAntigravityHarnessConfigValidation(t *testing.T) {
	if err := validateHarness("secretary.harness", "antigravity"); err != nil {
		t.Fatalf("antigravity should be accepted: %v", err)
	}
	if err := validateHarness("worker_policy.default_harness", "antigravity"); err != nil {
		t.Fatalf("antigravity should be accepted: %v", err)
	}
	if err := validateHarness("secretary.harness", "invalid_harness"); err == nil || !strings.Contains(err.Error(), "antigravity") {
		t.Fatalf("invalid harness error should mention antigravity: %v", err)
	}
}
