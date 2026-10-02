package telegram

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const titleAgent = "secretary-topic-title"
const titleVariant = "secretary-title"

// OpenCodeTitleGenerator запускает отдельный OpenCode v2 с одним model step,
// внешним system prompt и запретом всех tools. Worker runtime не используется.
type OpenCodeTitleGenerator struct {
	Model     string
	Reasoning string
	Prompt    string
	run       func(context.Context, titleCommand) ([]byte, error)
}

type titleCommand struct {
	Directory   string
	Environment []string
	Arguments   []string
	Input       string
}

func (g OpenCodeTitleGenerator) Generate(ctx context.Context, task string) (string, error) {
	if strings.TrimSpace(g.Prompt) == "" || strings.TrimSpace(task) == "" || g.Model == "" || g.Reasoning == "" {
		return "", errors.New("telegram: incomplete title request")
	}
	provider, model, ok := titleModelParts(g.Model)
	if !ok {
		return "", errors.New("telegram: invalid title model")
	}
	root, err := os.MkdirTemp("", "secretary-topic-title-")
	if err != nil {
		return "", errors.New("telegram: title workspace unavailable")
	}
	defer os.RemoveAll(root)
	configDir := filepath.Join(root, "config", "opencode")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return "", errors.New("telegram: title configuration unavailable")
	}
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", errors.New("telegram: title configuration unavailable")
	}
	encoded, err := json.Marshal(g.openCodeConfig(provider, model))
	if err != nil {
		return "", errors.New("telegram: title configuration unavailable")
	}
	configPath := filepath.Join(configDir, "opencode.json")
	if err := os.WriteFile(configPath, encoded, 0o600); err != nil {
		return "", errors.New("telegram: title configuration unavailable")
	}
	environment, err := titleEnvironment(root, home, configDir, configPath)
	if err != nil {
		return "", errors.New("telegram: title configuration unavailable")
	}
	run := g.run
	if run == nil {
		run = runOpenCodeTitleCommand
	}
	command := titleCommand{Directory: root, Environment: environment, Arguments: []string{"--version"}}
	version, err := run(ctx, command)
	if err != nil || !strings.HasPrefix(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(string(version)), "opencode "), "v"), "2.") {
		return "", errors.New("telegram: OpenCode v2 is required for title generation")
	}
	input, _ := json.Marshal(map[string]string{"task_prompt": task})
	command.Input = string(input) + "\n"
	command.Arguments = []string{
		"run", "--standalone", "--agent", titleAgent,
		"--model", provider + "/" + model + "#" + titleVariant,
		"--format", "json", "--title", "Secretary topic title", "--log-level", "none",
	}
	output, err := run(ctx, command)
	if err != nil || ctx.Err() != nil {
		return "", errors.New("telegram: title generation failed")
	}
	return parseOpenCodeTitle(output)
}

func titleModelParts(value string) (provider, model string, ok bool) {
	if strings.Contains(value, "#") {
		return "", "", false
	}
	provider, model, found := strings.Cut(value, "/")
	if !found {
		provider, model = "openai", value
	}
	return provider, model, provider != "" && model != ""
}

func (g OpenCodeTitleGenerator) openCodeConfig(provider, model string) map[string]any {
	deny := []map[string]string{{"action": "*", "resource": "*", "effect": "deny"}}
	return map[string]any{
		"model":         provider + "/" + model,
		"default_agent": titleAgent,
		"update":        "disable",
		"snapshots":     false,
		"warming":       false,
		"compaction":    map[string]bool{"auto": false},
		"permissions":   deny,
		"agents": map[string]any{titleAgent: map[string]any{
			"mode": "primary", "system": g.Prompt, "steps": 1, "permissions": deny,
		}},
		"providers": map[string]any{provider: map[string]any{
			"models": map[string]any{model: map[string]any{
				"capabilities": map[string]any{"tools": false, "input": []string{"text"}, "output": []string{"text"}},
				"variants":     []map[string]any{{"id": titleVariant, "settings": map[string]string{"reasoningEffort": g.Reasoning}}},
			}},
		}},
	}
}

// HOME и config/state отделены от пользовательских instructions, skills,
// plugins и background service. Data/cache остаются прежними для provider
// authentication и встроенных provider packages; credentials не копируются.
func titleEnvironment(root, home, configDir, path string) ([]string, error) {
	originalHome, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(key, "SECRETARY_") && key != "CODEX_CONFIG" {
			values[key] = value
		}
	}
	if values["XDG_DATA_HOME"] == "" {
		values["XDG_DATA_HOME"] = filepath.Join(originalHome, ".local", "share")
	}
	if values["XDG_CACHE_HOME"] == "" {
		values["XDG_CACHE_HOME"] = filepath.Join(originalHome, ".cache")
	}
	values["HOME"] = home
	values["XDG_CONFIG_HOME"] = filepath.Dir(configDir)
	values["XDG_STATE_HOME"] = filepath.Join(root, "state")
	values["OPENCODE_CONFIG_DIR"] = configDir
	values["OPENCODE_CONFIG"] = path
	values["OPENCODE_CONFIG_CONTENT"] = ""
	values["OPENCODE_DISABLE_PROJECT_CONFIG"] = "1"
	values["OPENCODE_SERVER_URL"] = ""
	values["OPENCODE_SERVER_PASSWORD"] = ""
	values["OPENCODE_SERVER_USERNAME"] = ""
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	environment := make([]string, 0, len(values))
	for _, key := range keys {
		environment = append(environment, key+"="+values[key])
	}
	return environment, nil
}

func parseOpenCodeTitle(output []byte) (string, error) {
	fail := errors.New("telegram: invalid title response")
	scanner := bufio.NewScanner(bytes.NewReader(output))
	message := ""
	var parts []string
	indices := map[string]int{}
	for scanner.Scan() {
		var event struct {
			Type string `json:"type"`
			Part struct {
				ID        string `json:"id"`
				MessageID string `json:"messageID"`
				Type      string `json:"type"`
				Text      string `json:"text"`
				Time      struct {
					End int64 `json:"end"`
				} `json:"time"`
			} `json:"part"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			return "", fail
		}
		if event.Type == "error" || strings.HasPrefix(event.Type, "tool") || event.Part.Type == "tool" {
			return "", fail
		}
		if event.Type != "text" || event.Part.Type != "text" || event.Part.Time.End == 0 {
			continue
		}
		if event.Part.ID == "" || event.Part.MessageID == "" {
			return "", fail
		}
		if message != event.Part.MessageID {
			message = event.Part.MessageID
			parts = nil
			indices = map[string]int{}
		}
		index, found := indices[event.Part.ID]
		if !found {
			index = len(parts)
			indices[event.Part.ID] = index
			parts = append(parts, "")
		}
		parts[index] = event.Part.Text
	}
	text := strings.Join(parts, "")
	if scanner.Err() != nil || text == "" || len(text) > 4096 {
		return "", fail
	}
	return text, nil
}
