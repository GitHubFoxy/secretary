package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestASRConfig_DefaultsAndYAML(t *testing.T) {
	dir := t.TempDir()
	tomlPath := filepath.Join(dir, "config.toml")
	yamlPath := filepath.Join(dir, "config.yaml")

	if err := ensureDefault(tomlPath); err != nil {
		t.Fatal(err)
	}
	if err := ensureDefaultTitlePrompt(dir); err != nil {
		t.Fatal(err)
	}

	yamlContent := `
# ASR
asr:
  mode: local
  show_asr_in_chat: false
  engine: nemo-speech
  model: /custom/parakeet-tdt-0.6b-v3.q8_0.gguf
  max_audio_seconds: 120
  max_file_bytes: 10000000
  timeout_seconds: 90

  formatter:
    enabled: true
    mode: online
    provider: openai
    api_key_env: OPENAI_API_KEY
    model: gpt-6-luna
    reasoning: none
    timeout_seconds: 20
    prompt: formatter.md
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "formatter.md"), []byte("Ты форматируешь текст."), 0o600); err != nil {
		t.Fatal(err)
	}

	snap, err := Load(tomlPath)
	if err != nil {
		t.Fatalf("Load with config.yaml failed: %v", err)
	}

	if snap.Config.ASR.Mode != "local" {
		t.Errorf("got mode %q, want 'local'", snap.Config.ASR.Mode)
	}
	if snap.Config.ASR.Engine != "nemo-speech" {
		t.Errorf("got engine %q, want 'nemo-speech'", snap.Config.ASR.Engine)
	}
	if snap.Config.ASR.Model != "/custom/parakeet-tdt-0.6b-v3.q8_0.gguf" {
		t.Errorf("got model %q, want '/custom/parakeet-tdt-0.6b-v3.q8_0.gguf'", snap.Config.ASR.Model)
	}
	if snap.Config.ASR.MaxAudioSeconds != 120 {
		t.Errorf("got MaxAudioSeconds %d, want 120", snap.Config.ASR.MaxAudioSeconds)
	}
	if snap.Config.ASR.MaxFileBytes != 10000000 {
		t.Errorf("got MaxFileBytes %d, want 10000000", snap.Config.ASR.MaxFileBytes)
	}
	if snap.Config.ASR.TimeoutSeconds != 90 {
		t.Errorf("got TimeoutSeconds %d, want 90", snap.Config.ASR.TimeoutSeconds)
	}
	if snap.Config.ASR.ShowASRInChat {
		t.Errorf("got ShowASRInChat true, want false")
	}
	if !snap.Config.ASR.Formatter.Enabled {
		t.Errorf("got Formatter.Enabled false, want true")
	}
	if snap.Config.ASR.Formatter.Model != "gpt-6-luna" {
		t.Errorf("got Formatter.Model %q, want 'gpt-6-luna'", snap.Config.ASR.Formatter.Model)
	}
	if snap.ASRPrompt.Content != "Ты форматируешь текст." {
		t.Errorf("got prompt %q, want 'Ты форматируешь текст.'", snap.ASRPrompt.Content)
	}
}

func TestASRConfig_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		config  ASRConfig
		wantErr string
	}{
		{
			name: "unsupported mode",
			config: ASRConfig{
				Mode:   "cloud",
				Engine: "nemo-speech",
				Model:  "/some/model",
			},
			wantErr: "asr.mode must be local",
		},
		{
			name: "unsupported engine",
			config: ASRConfig{
				Mode:   "local",
				Engine: "whisper",
				Model:  "/some/model",
			},
			wantErr: "asr.engine must be nemo-speech",
		},
		{
			name: "empty model",
			config: ASRConfig{
				Mode:   "local",
				Engine: "nemo-speech",
				Model:  "",
			},
			wantErr: "asr.model must be a non-empty path",
		},
		{
			name: "negative max audio",
			config: ASRConfig{
				Mode:            "local",
				Engine:          "nemo-speech",
				Model:           "/some/model",
				MaxAudioSeconds: -10,
			},
			wantErr: "asr.max_audio_seconds must be positive",
		},
		{
			name: "unsupported formatter provider",
			config: ASRConfig{
				Mode:            "local",
				Engine:          "nemo-speech",
				Model:           "/some/model",
				MaxAudioSeconds: 120,
				MaxFileBytes:    10000000,
				TimeoutSeconds:  90,
				Formatter: FormatterConfig{
					Enabled:        true,
					Mode:           "online",
					Provider:       "anthropic",
					APIKeyEnv:      "KEY",
					Model:          "claude",
					TimeoutSeconds: 20,
					Prompt:         "prompt.md",
				},
			},
			wantErr: "asr.formatter.provider must be openai",
		},
		{
			name: "unsupported formatter mode",
			config: ASRConfig{
				Mode:            "local",
				Engine:          "nemo-speech",
				Model:           "/some/model",
				MaxAudioSeconds: 120,
				MaxFileBytes:    10000000,
				TimeoutSeconds:  90,
				Formatter: FormatterConfig{
					Enabled:        true,
					Mode:           "local",
					Provider:       "openai",
					APIKeyEnv:      "KEY",
					Model:          "gpt-6-luna",
					TimeoutSeconds: 20,
					Prompt:         "prompt.md",
				},
			},
			wantErr: "asr.formatter.mode must be online",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateASRConfig(tt.config)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error %q to contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestASRSidecarReloadAndRelativePaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	m, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	body := "asr:\n  mode: local\n  engine: nemo-speech\n  model: weights.gguf\n  formatter:\n    enabled: true\n    mode: online\n    provider: openai\n    prompt: formatter.md\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	snap, err := m.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Config.ASR.Model != filepath.Join(dir, "weights.gguf") || snap.ASRPrompt.Path != filepath.Join(dir, "formatter.md") {
		t.Fatalf("paths=%+v", snap)
	}
	version := snap.Version
	body = strings.Replace(body, "  model: weights.gguf", "  show_asr_in_chat: true\n  model: weights.gguf", 1)
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	snap, err = m.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if snap.Version == version || !snap.Config.ASR.ShowASRInChat {
		t.Fatal("sidecar change ignored")
	}
	if err := os.Remove(filepath.Join(dir, "formatter.md")); err != nil {
		t.Fatal(err)
	}
	if _, err = m.Reload(); err == nil {
		t.Fatal("missing explicit prompt silently ignored")
	}
}
