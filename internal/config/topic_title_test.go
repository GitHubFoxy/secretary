package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func titleConfigPath(t *testing.T, settings string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if _, err := Open(path); err != nil {
		t.Fatal(err)
	}
	body, err := defaults.ReadFile("defaults/config.toml")
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(body), "[telegram]\n")
	if start < 0 {
		t.Fatal("default config must contain telegram section")
	}
	end := strings.Index(string(body)[start:], "[retention]\n")
	if end < 0 {
		t.Fatal("default config must contain retention section")
	}
	text := string(body[:start]) + settings + "\n" + string(body[start+end:])
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTopicTitleDefaultsAndPartialConfiguration(t *testing.T) {
	for _, test := range []struct {
		name, settings, model, reasoning string
	}{
		{"legacy without section", "", "gpt-6-luna", "minimal"},
		{"empty section", "[telegram]\n", "gpt-6-luna", "minimal"},
		{"model only", "[telegram]\ntitle_model = \"provider/custom-model\"\n", "provider/custom-model", "minimal"},
		{"reasoning only", "[telegram]\ntitle_model_reasoning = \"none\"\n", "gpt-6-luna", "none"},
		{"explicit", "[telegram]\ntitle_model = \"provider/custom-model\"\ntitle_model_reasoning = \"low\"\n", "provider/custom-model", "low"},
	} {
		t.Run(test.name, func(t *testing.T) {
			snapshot, err := Load(titleConfigPath(t, test.settings))
			if err != nil {
				t.Fatal(err)
			}
			if got := snapshot.Config.Telegram; got.TitleModel != test.model || got.TitleModelReasoning != test.reasoning || got.TitleHarness != DefaultTitleHarness || got.TitlePrompt != DefaultTitlePrompt {
				t.Fatalf("telegram=%#v", got)
			}
		})
	}
}

func TestTopicTitleLegacyRuntimeConfig(t *testing.T) {
	path := titleConfigPath(t, "")
	legacy := `[profiles]
secretary = "profiles/secretary.md"
worker = "profiles/worker.md"
child_worker = "profiles/child-worker.md"
[tools]
allow_tools = ["read"]
[models]
secretary = "legacy-secretary"
smart = "legacy-worker"
[runtime]
harness = "codex"
reasoning = "high"
`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Config.Telegram.TitleModel != "gpt-6-luna" || snapshot.Config.Telegram.TitleModelReasoning != "minimal" {
		t.Fatalf("legacy title defaults=%#v", snapshot.Config.Telegram)
	}
	if secretary, worker := snapshot.Profiles["secretary"], snapshot.Profiles["worker"]; secretary.Model != "legacy-secretary" || worker.Model != "legacy-worker" || secretary.Runtime != "codex" || worker.Runtime != "codex" || secretary.Reasoning != "high" || worker.Reasoning != "high" {
		t.Fatalf("legacy runtime policy changed: %#v", snapshot.Profiles)
	}
}

func TestTopicTitleDefaultExampleAndSnapshotJSON(t *testing.T) {
	manager, err := Open(filepath.Join(t.TempDir(), "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	before := manager.Snapshot()
	if got := before.Config.Telegram; got.TitleModel != DefaultTitleModel || got.TitleModelReasoning != DefaultTitleModelReasoning || got.TitleHarness != DefaultTitleHarness || got.TitlePrompt != DefaultTitlePrompt {
		t.Fatalf("default telegram=%#v", got)
	}
	encoded, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	var after Snapshot
	if err := json.Unmarshal(encoded, &after); err != nil {
		t.Fatal(err)
	}
	if after.Config.Telegram != before.Config.Telegram || after.TitlePrompt != before.TitlePrompt || after.TitlePrompt.Content == "" || after.TitlePrompt.Hash == "" {
		t.Fatalf("title settings or prompt lost in JSON")
	}
}

func TestTopicTitleReasoningValues(t *testing.T) {
	for _, value := range []string{"none", "minimal", "low", "medium", "high", "xhigh"} {
		t.Run(value, func(t *testing.T) {
			snapshot, err := Load(titleConfigPath(t, "[telegram]\ntitle_model_reasoning = \""+value+"\"\n"))
			if err != nil {
				t.Fatal(err)
			}
			if snapshot.Config.Telegram.TitleModelReasoning != value {
				t.Fatalf("reasoning=%q", snapshot.Config.Telegram.TitleModelReasoning)
			}
		})
	}
}

func TestTopicTitleValidation(t *testing.T) {
	for _, test := range []struct {
		name, setting, field string
	}{
		{"empty harness", `title_harness = ""`, "telegram.title_harness"},
		{"unsupported harness", `title_harness = "fx"`, "telegram.title_harness"},
		{"harness case", `title_harness = "OpenCode"`, "telegram.title_harness"},
		{"empty prompt path", `title_prompt = ""`, "telegram.title_prompt"},
		{"missing prompt", `title_prompt = "missing-title.md"`, "telegram.title_prompt"},
		{"control prompt path", `title_prompt = "title\u0000.md"`, "telegram.title_prompt"},
		{"model variant conflicts with reasoning", `title_model = "openai/model#high"`, "telegram.title_model"},
		{"empty model", `title_model = ""`, "telegram.title_model"},
		{"blank model", `title_model = " "`, "telegram.title_model"},
		{"model whitespace", `title_model = "gpt 6"`, "telegram.title_model"},
		{"model newline", `title_model = "gpt\n6"`, "telegram.title_model"},
		{"model control", `title_model = "gpt\u0000"`, "telegram.title_model"},
		{"model invisible formatting", `title_model = "gpt\u200b6"`, "telegram.title_model"},
		{"missing provider", `title_model = "/model"`, "telegram.title_model"},
		{"missing model", `title_model = "provider/"`, "telegram.title_model"},
		{"empty component", `title_model = "provider//model"`, "telegram.title_model"},
		{"empty reasoning", `title_model_reasoning = ""`, "telegram.title_model_reasoning"},
		{"blank reasoning", `title_model_reasoning = " "`, "telegram.title_model_reasoning"},
		{"unknown reasoning", `title_model_reasoning = "ultra"`, "telegram.title_model_reasoning"},
		{"runtime default is not title reasoning", `title_model_reasoning = "default"`, "telegram.title_model_reasoning"},
		{"case sensitive reasoning", `title_model_reasoning = "MINIMAL"`, "telegram.title_model_reasoning"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(titleConfigPath(t, "[telegram]\n"+test.setting+"\n"))
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), test.field) {
				t.Fatalf("error=%v, want ErrInvalid for %s", err, test.field)
			}
		})
	}
}

func TestTopicTitleChangeDoesNotChangeRuntimeProfiles(t *testing.T) {
	path := titleConfigPath(t, "")
	before, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, []byte("\n[telegram]\ntitle_model = \"provider/title-model\"\ntitle_model_reasoning = \"none\"\n")...)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.Version == after.Version {
		t.Fatal("title config change must change snapshot version")
	}
	if !reflect.DeepEqual(before.Profiles, after.Profiles) || before.Config.Secretary != after.Config.Secretary || !reflect.DeepEqual(before.Config.WorkerPolicy, after.Config.WorkerPolicy) || before.Config.Runtime != after.Config.Runtime || before.Config.Models != after.Config.Models {
		t.Fatal("title config changed Secretary or Worker runtime policy/profile")
	}
	if got, want := Diff(before, after), (map[string]any{"telegram": after.Config.Telegram}); !reflect.DeepEqual(got, want) {
		t.Fatalf("diff=%#v, want %#v", got, want)
	}
}

func TestTopicTitleReloadRecordsChangeAndRejectsInvalidValues(t *testing.T) {
	path := titleConfigPath(t, "")
	manager, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var recorded map[string]any
	manager.SetChangeRecorder(func(before, after Snapshot) error {
		recorded = Diff(before, after)
		return nil
	})
	valid := append(append([]byte(nil), body...), []byte("\n[telegram]\ntitle_model = \"custom-title\"\ntitle_model_reasoning = \"none\"\n")...)
	if err := os.WriteFile(path, valid, 0o600); err != nil {
		t.Fatal(err)
	}
	active, err := manager.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if active.Config.Telegram.TitleModel != "custom-title" || !reflect.DeepEqual(recorded, map[string]any{"telegram": active.Config.Telegram}) {
		t.Fatalf("active=%#v, recorded=%#v", active.Config.Telegram, recorded)
	}
	invalid := strings.Replace(string(valid), `title_model_reasoning = "none"`, `title_model_reasoning = "unsupported"`, 1)
	if err := os.WriteFile(path, []byte(invalid), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Reload(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid reload error=%v", err)
	}
	if got := manager.Snapshot(); got.Version != active.Version || got.Config.Telegram != active.Config.Telegram {
		t.Fatal("invalid title configuration replaced active snapshot")
	}
	if !reflect.DeepEqual(recorded, map[string]any{"telegram": active.Config.Telegram}) {
		t.Fatal("invalid reload called change recorder")
	}
}
