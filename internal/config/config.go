// Package config loads and compiles Secretary's external runtime configuration.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/pelletier/go-toml/v2"
)

var ErrInvalid = errors.New("config: invalid")

const (
	DefaultTitleHarness               = "opencode"
	DefaultTitlePrompt                = "title-generation-prompt.md"
	DefaultTitleModel                 = "gpt-6-luna"
	DefaultTitleModelReasoning        = "minimal"
	SecretaryReplyContractAddressedV1 = core.SecretaryReplyContractAddressedV1
)

type Config struct {
	Profiles     Profiles        `toml:"profiles" json:"profiles"`
	Skills       []string        `toml:"skills" json:"skills"`
	Tools        Tools           `toml:"tools" json:"tools"`
	Models       Models          `toml:"models" json:"models"`
	Runtime      Runtime         `toml:"runtime" json:"-"` // legacy compatibility
	Secretary    SecretaryPolicy `toml:"secretary" json:"secretary"`
	WorkerPolicy WorkerPolicy    `toml:"worker_policy" json:"worker_policy"`
	Telegram     TelegramPolicy  `toml:"telegram" json:"telegram"`
	Retention    Retention       `toml:"retention" json:"retention"`
}

type SecretaryPolicy struct {
	Harness       string `toml:"harness" json:"harness"`
	Model         string `toml:"model" json:"model"`
	Reasoning     string `toml:"reasoning" json:"reasoning"`
	ReplyContract string `toml:"reply_contract" json:"reply_contract,omitempty"`
}

type WorkerPolicy struct {
	DefaultHarness     string   `toml:"default_harness" json:"default_harness"`
	Model              string   `toml:"model" json:"model"`
	Reasoning          string   `toml:"reasoning" json:"reasoning"`
	FallbackModels     []string `toml:"fallback_models" json:"fallback_models"`
	PreferredHarnesses []string `toml:"preferred_harnesses" json:"preferred_harnesses"`
	ActiveAttempts     int      `toml:"active_attempts" json:"active_attempts"`
}

// TelegramPolicy задаёт только настройки генерации названий Worker Topics.
type TelegramPolicy struct {
	TitleHarness        string `toml:"title_harness" json:"title_harness"`
	TitlePrompt         string `toml:"title_prompt" json:"title_prompt"`
	TitleModel          string `toml:"title_model" json:"title_model"`
	TitleModelReasoning string `toml:"title_model_reasoning" json:"title_model_reasoning"`
}

type Profiles struct {
	Secretary   string `toml:"secretary" json:"secretary"`
	Worker      string `toml:"worker" json:"worker"`
	ChildWorker string `toml:"child_worker" json:"child_worker"`
}
type Tools struct {
	Allow []string `toml:"allow_tools" json:"allow_tools"`
}
type Models struct {
	Secretary string `toml:"secretary" json:"secretary"`
	Fast      string `toml:"fast" json:"-"`  // legacy compatibility
	Smart     string `toml:"smart" json:"-"` // legacy compatibility
	Cheap     string `toml:"cheap" json:"-"` // legacy compatibility
}
type Runtime struct {
	Harness   string `toml:"harness" json:"harness"`
	Reasoning string `toml:"reasoning" json:"reasoning"`
}
type Retention struct {
	RawLogs        string `toml:"raw_logs" json:"raw_logs"`
	RawLogMaxBytes int64  `toml:"raw_log_max_bytes" json:"raw_log_max_bytes"`
}

func (r Retention) RawLogPolicy() (time.Duration, int64, bool, error) {
	if r.RawLogs == "forever" {
		return 0, r.RawLogMaxBytes, true, nil
	}
	if r.RawLogs == "" {
		return 30 * 24 * time.Hour, r.RawLogMaxBytes, false, nil
	}
	age, err := time.ParseDuration(r.RawLogs)
	if err != nil || age <= 0 {
		return 0, 0, false, fmt.Errorf("%w: retention.raw_logs must be a positive duration or forever", ErrInvalid)
	}
	return age, r.RawLogMaxBytes, false, nil
}

type Snapshot struct {
	Version     string             `json:"version"`
	Config      Config             `json:"config"`
	Profiles    map[string]Profile `json:"profiles"`
	TitlePrompt Prompt             `json:"title_prompt"`
}

type Prompt struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Hash    string `json:"hash"`
}

type Profile struct {
	Name                 string   `json:"name"`
	Path                 string   `json:"path"`
	Content              string   `json:"content"`
	Skills               []Skill  `json:"skills"`
	AllowTools           []string `json:"allow_tools"`
	Runtime              string   `json:"runtime"`
	Model                string   `json:"model"`
	Reasoning            string   `json:"reasoning"`
	ReplyContractVersion string   `json:"reply_contract_version,omitempty"`
	Hash                 string   `json:"hash"`
}
type Skill struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Hash    string `json:"hash"`
}

func Load(path string) (Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read config %s: %w", path, err)
	}
	c := Config{Telegram: TelegramPolicy{
		TitleHarness: DefaultTitleHarness, TitlePrompt: DefaultTitlePrompt,
		TitleModel: DefaultTitleModel, TitleModelReasoning: DefaultTitleModelReasoning,
	}}
	if err := toml.Unmarshal(data, &c); err != nil {
		return Snapshot{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	var explicit struct {
		Telegram struct {
			TitlePrompt *string `toml:"title_prompt"`
		} `toml:"telegram"`
	}
	if err := toml.Unmarshal(data, &explicit); err != nil {
		return Snapshot{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return compile(filepath.Dir(path), data, c, explicit.Telegram.TitlePrompt == nil)
}

func compile(base string, raw []byte, c Config, allowEmbeddedTitlePrompt bool) (Snapshot, error) {
	if err := validateConfig(c); err != nil {
		return Snapshot{}, err
	}
	titlePrompt, err := loadTitlePrompt(base, c.Telegram.TitlePrompt, allowEmbeddedTitlePrompt)
	if err != nil {
		return Snapshot{}, err
	}
	secretary := c.EffectiveSecretaryPolicy()
	workerPolicy := c.EffectiveWorkerPolicy()
	skills, err := loadSkills(c.Skills)
	if err != nil {
		return Snapshot{}, err
	}
	paths := map[string]string{"secretary": c.Profiles.Secretary, "worker": c.Profiles.Worker, "child_worker": c.Profiles.ChildWorker}
	profiles := make(map[string]Profile, len(paths))
	for name, path := range paths {
		resolved := path
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(base, resolved)
		}
		content, err := os.ReadFile(resolved)
		if err != nil {
			return Snapshot{}, fmt.Errorf("read %s profile %s: %w", name, resolved, err)
		}
		if strings.TrimSpace(string(content)) == "" {
			return Snapshot{}, fmt.Errorf("%w: %s profile %s is empty", ErrInvalid, name, resolved)
		}
		runtime := secretary.Harness
		model := secretary.Model
		reasoning := secretary.Reasoning
		if name != "secretary" {
			runtime = workerPolicy.DefaultHarness
			model = workerPolicy.Model
			if workerPolicy.Reasoning != "" {
				reasoning = workerPolicy.Reasoning
			}
			if model == "" {
				model = c.Models.Smart
			}
			if model == "" {
				model = "default"
			}
		}
		replyContractVersion := ""
		if name == "secretary" {
			replyContractVersion = c.Secretary.ReplyContract
		}
		profile := Profile{Name: name, Path: resolved, Content: string(content), Skills: skills, AllowTools: append([]string(nil), c.Tools.Allow...), Runtime: runtime, Model: model, Reasoning: reasoning, ReplyContractVersion: replyContractVersion}
		profile.Hash = digest(profile.Content, profile.Runtime, profile.Model, profile.Reasoning, strings.Join(profile.AllowTools, "\n"), skillDigest(skills))
		if replyContractVersion != "" {
			profile.Hash = digest(profile.Hash, replyContractVersion)
		}
		profiles[name] = profile
	}
	return Snapshot{Version: digest(string(raw), profiles["secretary"].Hash, profiles["worker"].Hash, profiles["child_worker"].Hash, titlePrompt.Hash), Config: c, Profiles: profiles, TitlePrompt: titlePrompt}, nil
}

func loadTitlePrompt(base, path string, allowEmbedded bool) (Prompt, error) {
	resolved := path
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(base, resolved)
	}
	file, err := os.Open(resolved)
	var content []byte
	if err == nil {
		content, err = io.ReadAll(io.LimitReader(file, 64*1024+1))
		_ = file.Close()
	} else if errors.Is(err, os.ErrNotExist) && allowEmbedded && path == DefaultTitlePrompt {
		content, err = defaults.ReadFile("defaults/" + DefaultTitlePrompt)
	}
	if err != nil {
		return Prompt{}, fmt.Errorf("%w: telegram.title_prompt cannot be read", ErrInvalid)
	}
	if strings.TrimSpace(string(content)) == "" || len(content) > 64*1024 || !utf8.Valid(content) {
		return Prompt{}, fmt.Errorf("%w: telegram.title_prompt must be non-empty UTF-8 and at most 64 KiB", ErrInvalid)
	}
	return Prompt{Path: resolved, Content: string(content), Hash: digest(string(content))}, nil
}

func (c Config) EffectiveSecretaryPolicy() SecretaryPolicy {
	policy := c.Secretary
	if policy.Harness == "" {
		policy.Harness = c.Runtime.Harness
	}
	if policy.Model == "" {
		policy.Model = c.Models.Secretary
	}
	if policy.Reasoning == "" {
		policy.Reasoning = c.Runtime.Reasoning
	}
	return policy
}

func (c Config) EffectiveWorkerPolicy() WorkerPolicy {
	policy := c.WorkerPolicy
	if policy.DefaultHarness == "" {
		if c.Secretary.Harness == "" {
			policy.DefaultHarness = c.Runtime.Harness
		}
		if policy.DefaultHarness == "" {
			policy.DefaultHarness = "codex"
		}
	}
	return policy
}

func validateHarness(field, value string) error {
	switch value {
	case "opencode", "codex", "fx", "claude_code":
		return nil
	default:
		return fmt.Errorf("%w: %s must be opencode, codex, fx or claude_code", ErrInvalid, field)
	}
}

func validateTelegramPolicy(policy TelegramPolicy) error {
	if policy.TitleHarness != "opencode" {
		return fmt.Errorf("%w: telegram.title_harness must be opencode", ErrInvalid)
	}
	if strings.TrimSpace(policy.TitlePrompt) == "" || strings.IndexFunc(policy.TitlePrompt, func(r rune) bool {
		return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	}) >= 0 {
		return fmt.Errorf("%w: telegram.title_prompt must be a non-empty file path without control characters", ErrInvalid)
	}
	if strings.Contains(policy.TitleModel, "#") {
		return fmt.Errorf("%w: telegram.title_model must not include a variant; use title_model_reasoning", ErrInvalid)
	}
	if policy.TitleModel == "" || strings.IndexFunc(policy.TitleModel, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
	}) >= 0 {
		return fmt.Errorf("%w: telegram.title_model must be a non-empty model ID without whitespace or control characters", ErrInvalid)
	}
	for _, part := range strings.Split(policy.TitleModel, "/") {
		if part == "" {
			return fmt.Errorf("%w: telegram.title_model must not contain empty slash-separated components", ErrInvalid)
		}
	}
	switch policy.TitleModelReasoning {
	case "none", "minimal", "low", "medium", "high", "xhigh":
		return nil
	default:
		return fmt.Errorf("%w: telegram.title_model_reasoning must be none, minimal, low, medium, high or xhigh", ErrInvalid)
	}
}

func validateConfig(c Config) error {
	if c.Secretary.Harness == "" && c.Runtime.Harness != "" {
		if err := validateHarness("runtime.harness", c.Runtime.Harness); err != nil {
			return err
		}
	}
	secretary := c.EffectiveSecretaryPolicy()
	if secretary.Harness == "" {
		return fmt.Errorf("%w: secretary.harness is required", ErrInvalid)
	}
	if err := validateHarness("secretary.harness", secretary.Harness); err != nil {
		return err
	}
	if secretary.Model == "" {
		return fmt.Errorf("%w: secretary.model is required", ErrInvalid)
	}
	if secretary.Reasoning == "" {
		return fmt.Errorf("%w: secretary.reasoning is required", ErrInvalid)
	}
	switch c.Secretary.ReplyContract {
	case "", SecretaryReplyContractAddressedV1:
	default:
		return fmt.Errorf("%w: secretary.reply_contract must be empty or %s", ErrInvalid, SecretaryReplyContractAddressedV1)
	}
	workerPolicy := c.EffectiveWorkerPolicy()
	if err := validateHarness("worker_policy.default_harness", workerPolicy.DefaultHarness); err != nil {
		return err
	}
	if c.Models.Fast == "" {
		c.Models.Fast = "fast"
	}
	if c.Models.Smart == "" {
		c.Models.Smart = "smart"
	}
	if c.Models.Cheap == "" {
		c.Models.Cheap = "cheap"
	}
	if len(c.Tools.Allow) == 0 {
		return fmt.Errorf("%w: tools.allow_tools cannot be empty", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, tool := range c.Tools.Allow {
		if tool == "" || tool != strings.ToLower(tool) || seen[tool] {
			return fmt.Errorf("%w: tools.allow_tools must be unique lowercase names", ErrInvalid)
		}
		seen[tool] = true
	}
	if !sort.StringsAreSorted(c.Tools.Allow) {
		return fmt.Errorf("%w: tools.allow_tools must be sorted", ErrInvalid)
	}
	switch workerPolicy.Reasoning {
	case "", "default", "none", "minimal", "low", "medium", "high", "xhigh":
	default:
		return fmt.Errorf("%w: worker_policy.reasoning is unsupported", ErrInvalid)
	}
	if workerPolicy.ActiveAttempts < 0 {
		return fmt.Errorf("%w: worker_policy.active_attempts cannot be negative", ErrInvalid)
	}
	if c.Profiles.Secretary == "" || c.Profiles.Worker == "" || c.Profiles.ChildWorker == "" {
		return fmt.Errorf("%w: profiles.secretary, profiles.worker and profiles.child_worker are required", ErrInvalid)
	}
	if err := validateTelegramPolicy(c.Telegram); err != nil {
		return err
	}
	if _, _, _, err := c.Retention.RawLogPolicy(); err != nil {
		return err
	}
	return nil
}

func loadSkills(paths []string) ([]Skill, error) {
	skills := make([]Skill, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%w: skill path must be absolute: %s", ErrInvalid, path)
		}
		if seen[path] {
			return nil, fmt.Errorf("%w: duplicate skill path: %s", ErrInvalid, path)
		}
		seen[path] = true
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read skill %s: %w", path, err)
		}
		skills = append(skills, Skill{Path: path, Content: string(content), Hash: digest(string(content))})
	}
	return skills, nil
}

// Diff returns the observable changes between two immutable config versions.
func Diff(previous, next Snapshot) map[string]any {
	changed := make(map[string]any)
	if previous.Config.Runtime != next.Config.Runtime {
		changed["runtime"] = next.Config.Runtime
	}
	if previous.Config.Secretary != next.Config.Secretary {
		changed["secretary"] = next.Config.Secretary
	}
	if previous.Config.WorkerPolicy.DefaultHarness != next.Config.WorkerPolicy.DefaultHarness || previous.Config.WorkerPolicy.Model != next.Config.WorkerPolicy.Model || previous.Config.WorkerPolicy.Reasoning != next.Config.WorkerPolicy.Reasoning || strings.Join(previous.Config.WorkerPolicy.FallbackModels, ",") != strings.Join(next.Config.WorkerPolicy.FallbackModels, ",") || previous.Config.WorkerPolicy.ActiveAttempts != next.Config.WorkerPolicy.ActiveAttempts || strings.Join(previous.Config.WorkerPolicy.PreferredHarnesses, ",") != strings.Join(next.Config.WorkerPolicy.PreferredHarnesses, ",") {
		changed["worker_policy"] = next.Config.WorkerPolicy
	}
	if previous.Config.Models != next.Config.Models {
		changed["models"] = next.Config.Models
	}
	if previous.Config.Telegram != next.Config.Telegram {
		changed["telegram"] = next.Config.Telegram
	}
	if previous.TitlePrompt.Hash != next.TitlePrompt.Hash {
		changed["telegram.title_prompt"] = next.TitlePrompt.Hash
	}
	if strings.Join(previous.Config.Tools.Allow, ",") != strings.Join(next.Config.Tools.Allow, ",") {
		changed["allow_tools"] = next.Config.Tools.Allow
	}
	for _, name := range []string{"secretary", "worker", "child_worker"} {
		if previous.Profiles[name].Hash != next.Profiles[name].Hash {
			changed["profile."+name] = next.Profiles[name].Hash
		}
	}
	return changed
}

func skillDigest(skills []Skill) string {
	values := make([]string, 0, len(skills))
	for _, s := range skills {
		values = append(values, s.Path+":"+s.Hash)
	}
	return digest(values...)
}
func digest(values ...string) string {
	h := sha256.New()
	for _, value := range values {
		_, _ = h.Write([]byte(value))
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
