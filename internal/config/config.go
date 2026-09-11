// Package config loads and compiles Secretary's external runtime configuration.
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

var ErrInvalid = errors.New("config: invalid")

type Config struct {
	Profiles  Profiles  `toml:"profiles" json:"profiles"`
	Skills    []string  `toml:"skills" json:"skills"`
	Tools     Tools     `toml:"tools" json:"tools"`
	Models    Models    `toml:"models" json:"models"`
	Runtime   Runtime   `toml:"runtime" json:"runtime"`
	Retention Retention `toml:"retention" json:"retention"`
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
	Fast      string `toml:"fast" json:"fast"`
	Smart     string `toml:"smart" json:"smart"`
	Cheap     string `toml:"cheap" json:"cheap"`
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
	Version  string             `json:"version"`
	Config   Config             `json:"config"`
	Profiles map[string]Profile `json:"profiles"`
}
type Profile struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	Content    string   `json:"content"`
	Skills     []Skill  `json:"skills"`
	AllowTools []string `json:"allow_tools"`
	Runtime    string   `json:"runtime"`
	Model      string   `json:"model"`
	Reasoning  string   `json:"reasoning"`
	Hash       string   `json:"hash"`
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
	var c Config
	if err := toml.Unmarshal(data, &c); err != nil {
		return Snapshot{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return compile(filepath.Dir(path), data, c)
}

func compile(base string, raw []byte, c Config) (Snapshot, error) {
	if err := validateConfig(c); err != nil {
		return Snapshot{}, err
	}
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
		model := c.Models.Secretary
		if name != "secretary" {
			model = c.Models.Smart
		}
		profile := Profile{Name: name, Path: resolved, Content: string(content), Skills: skills, AllowTools: append([]string(nil), c.Tools.Allow...), Runtime: c.Runtime.Harness, Model: model, Reasoning: c.Runtime.Reasoning}
		profile.Hash = digest(profile.Content, profile.Runtime, profile.Model, profile.Reasoning, strings.Join(profile.AllowTools, "\n"), skillDigest(skills))
		profiles[name] = profile
	}
	return Snapshot{Version: digest(string(raw), profiles["secretary"].Hash, profiles["worker"].Hash, profiles["child_worker"].Hash), Config: c, Profiles: profiles}, nil
}

func validateConfig(c Config) error {
	if c.Profiles.Secretary == "" || c.Profiles.Worker == "" || c.Profiles.ChildWorker == "" {
		return fmt.Errorf("%w: profiles.secretary, profiles.worker and profiles.child_worker are required", ErrInvalid)
	}
	if c.Runtime.Harness == "" {
		return fmt.Errorf("%w: runtime.harness is required", ErrInvalid)
	}
	switch c.Runtime.Harness {
	case "opencode", "codex", "fx":
	default:
		return fmt.Errorf("%w: runtime.harness must be opencode, codex or fx", ErrInvalid)
	}
	if c.Models.Secretary == "" || c.Models.Fast == "" || c.Models.Smart == "" || c.Models.Cheap == "" {
		return fmt.Errorf("%w: models.secretary, models.fast, models.smart and models.cheap are required", ErrInvalid)
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
	if previous.Config.Models != next.Config.Models {
		changed["models"] = next.Config.Models
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
