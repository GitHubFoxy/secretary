package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	DefaultFormatterPrompt    = "formatter.md"
	DefaultASRMaxAudio        = 120
	DefaultASRMaxBytes        = 10000000
	DefaultASRTimeout         = 90
	DefaultFormatterTimeout   = 20
	DefaultFormatterModel     = "gpt-6-luna"
	DefaultFormatterReasoning = "none"
	DefaultFormatterAPIKeyEnv = "OPENAI_API_KEY"
)

type ASRConfig struct {
	Mode            string          `toml:"mode" yaml:"mode" json:"mode"`
	ShowASRInChat   bool            `toml:"show_asr_in_chat" yaml:"show_asr_in_chat" json:"show_asr_in_chat"`
	Engine          string          `toml:"engine" yaml:"engine" json:"engine"`
	Model           string          `toml:"model" yaml:"model" json:"model"`
	MaxAudioSeconds int             `toml:"max_audio_seconds" yaml:"max_audio_seconds" json:"max_audio_seconds"`
	MaxFileBytes    int64           `toml:"max_file_bytes" yaml:"max_file_bytes" json:"max_file_bytes"`
	TimeoutSeconds  int             `toml:"timeout_seconds" yaml:"timeout_seconds" json:"timeout_seconds"`
	Formatter       FormatterConfig `toml:"formatter" yaml:"formatter" json:"formatter"`
}

type FormatterConfig struct {
	Enabled        bool   `toml:"enabled" yaml:"enabled" json:"enabled"`
	Mode           string `toml:"mode" yaml:"mode" json:"mode"`
	Provider       string `toml:"provider" yaml:"provider" json:"provider"`
	APIKeyEnv      string `toml:"api_key_env" yaml:"api_key_env" json:"api_key_env"`
	Model          string `toml:"model" yaml:"model" json:"model"`
	Reasoning      string `toml:"reasoning" yaml:"reasoning" json:"reasoning"`
	TimeoutSeconds int    `toml:"timeout_seconds" yaml:"timeout_seconds" json:"timeout_seconds"`
	Prompt         string `toml:"prompt" yaml:"prompt" json:"prompt"`
}

func (c ASRConfig) IsConfigured() bool {
	return c.Mode != "" || c.Engine != "" || c.Model != "" || c.Formatter.Enabled
}

func validateASRConfig(c ASRConfig) error {
	if !c.IsConfigured() {
		return nil
	}
	if c.Mode != "local" {
		return fmt.Errorf("%w: asr.mode must be local", ErrInvalid)
	}
	if c.Engine != "nemo-speech" {
		return fmt.Errorf("%w: asr.engine must be nemo-speech", ErrInvalid)
	}
	if strings.TrimSpace(c.Model) == "" {
		return fmt.Errorf("%w: asr.model must be a non-empty path to model weights", ErrInvalid)
	}
	if c.MaxAudioSeconds <= 0 {
		return fmt.Errorf("%w: asr.max_audio_seconds must be positive", ErrInvalid)
	}
	if c.MaxFileBytes <= 0 {
		return fmt.Errorf("%w: asr.max_file_bytes must be positive", ErrInvalid)
	}
	if c.TimeoutSeconds <= 0 {
		return fmt.Errorf("%w: asr.timeout_seconds must be positive", ErrInvalid)
	}
	if c.Formatter.Enabled {
		if c.Formatter.Mode != "online" {
			return fmt.Errorf("%w: asr.formatter.mode must be online", ErrInvalid)
		}
		if c.Formatter.Provider != "openai" {
			return fmt.Errorf("%w: asr.formatter.provider must be openai", ErrInvalid)
		}
		if strings.TrimSpace(c.Formatter.APIKeyEnv) == "" {
			return fmt.Errorf("%w: asr.formatter.api_key_env is required", ErrInvalid)
		}
		if strings.TrimSpace(c.Formatter.Model) == "" {
			return fmt.Errorf("%w: asr.formatter.model is required", ErrInvalid)
		}
		if c.Formatter.Reasoning != "none" {
			return fmt.Errorf("%w: asr.formatter.reasoning must be none", ErrInvalid)
		}
		if c.Formatter.TimeoutSeconds <= 0 {
			return fmt.Errorf("%w: asr.formatter.timeout_seconds must be positive", ErrInvalid)
		}
		if strings.TrimSpace(c.Formatter.Prompt) == "" {
			return fmt.Errorf("%w: asr.formatter.prompt is required", ErrInvalid)
		}
	}
	return nil
}

func ensureDefaultFormatterPrompt(base string) error {
	body, err := defaults.ReadFile("defaults/" + DefaultFormatterPrompt)
	if err != nil {
		return err
	}
	path := filepath.Join(base, DefaultFormatterPrompt)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create formatter.md: %w", err)
	}
	_, writeErr := file.Write(body)
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("write formatter.md: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close formatter.md: %w", closeErr)
	}
	return nil
}

func loadFormatterPrompt(base, path string, allowEmbedded bool) (Prompt, error) {
	resolved := path
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(base, resolved)
	}
	file, err := os.Open(resolved)
	var content []byte
	if err == nil {
		content, err = io.ReadAll(io.LimitReader(file, 64*1024+1))
		_ = file.Close()
	} else if errors.Is(err, os.ErrNotExist) && allowEmbedded && path == DefaultFormatterPrompt {
		content, err = defaults.ReadFile("defaults/" + DefaultFormatterPrompt)
	}
	if err != nil {
		return Prompt{}, fmt.Errorf("%w: asr.formatter.prompt cannot be read", ErrInvalid)
	}
	if strings.TrimSpace(string(content)) == "" || len(content) > 64*1024 || !utf8.Valid(content) {
		return Prompt{}, fmt.Errorf("%w: asr.formatter.prompt must be non-empty UTF-8 and at most 64 KiB", ErrInvalid)
	}
	return Prompt{Path: resolved, Content: string(content), Hash: digest(string(content))}, nil
}

func loadASRFromYAML(yamlPath string) (ASRConfig, Prompt, bool, error) {
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return ASRConfig{}, Prompt{}, false, fmt.Errorf("read %s: %w", yamlPath, err)
	}
	var root struct {
		ASR *ASRConfig `yaml:"asr"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&root); err != nil {
		return ASRConfig{}, Prompt{}, false, fmt.Errorf("%w: parse yaml %s: %v", ErrInvalid, yamlPath, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return ASRConfig{}, Prompt{}, false, fmt.Errorf("%w: ASR YAML must contain one document", ErrInvalid)
	}
	if root.ASR == nil {
		return ASRConfig{}, Prompt{}, false, nil
	}
	if !root.ASR.IsConfigured() {
		return ASRConfig{}, Prompt{}, true, fmt.Errorf("%w: asr requires mode, engine and model", ErrInvalid)
	}
	asr := *root.ASR
	if asr.Model != "" && !filepath.IsAbs(asr.Model) {
		asr.Model = filepath.Join(filepath.Dir(yamlPath), asr.Model)
	}
	if asr.MaxAudioSeconds == 0 {
		asr.MaxAudioSeconds = DefaultASRMaxAudio
	}
	if asr.MaxFileBytes == 0 {
		asr.MaxFileBytes = DefaultASRMaxBytes
	}
	if asr.TimeoutSeconds == 0 {
		asr.TimeoutSeconds = DefaultASRTimeout
	}
	var prompt Prompt
	if asr.Formatter.Enabled {
		if asr.Formatter.APIKeyEnv == "" {
			asr.Formatter.APIKeyEnv = DefaultFormatterAPIKeyEnv
		}
		if asr.Formatter.Model == "" {
			asr.Formatter.Model = DefaultFormatterModel
		}
		if asr.Formatter.Reasoning == "" {
			asr.Formatter.Reasoning = DefaultFormatterReasoning
		}
		if asr.Formatter.TimeoutSeconds == 0 {
			asr.Formatter.TimeoutSeconds = DefaultFormatterTimeout
		}
		if asr.Formatter.Prompt == "" {
			asr.Formatter.Prompt = DefaultFormatterPrompt
		}
		var promptErr error
		prompt, promptErr = loadFormatterPrompt(filepath.Dir(yamlPath), asr.Formatter.Prompt, false)
		if promptErr != nil {
			return ASRConfig{}, Prompt{}, true, promptErr
		}
	}
	if err := validateASRConfig(asr); err != nil {
		return ASRConfig{}, Prompt{}, true, err
	}
	return asr, prompt, true, nil
}
