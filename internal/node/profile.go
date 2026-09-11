package node

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ManagedSkill struct {
	Path    string
	Content string
	Hash    string
}

type ManagedProfile struct {
	Version    string
	Name       string
	Content    string
	Skills     []ManagedSkill
	AllowTools []string
	Hash       string
	Runtime    string
	Model      string
	Reasoning  string
	Delivery   string
}

func (p ManagedProfile) EffectivePrompt() string {
	parts := make([]string, 0, 1+len(p.Skills))
	if strings.TrimSpace(p.Content) != "" {
		parts = append(parts, strings.TrimSpace(p.Content))
	}
	for _, skill := range p.Skills {
		if strings.TrimSpace(skill.Content) == "" {
			continue
		}
		parts = append(parts, fmt.Sprintf("\n\n## Managed skill: %s\n\n%s", skill.Path, strings.TrimSpace(skill.Content)))
	}
	return strings.Join(parts, "")
}

func (p ManagedProfile) MaterializeInstructions(workspace string) (string, error) {
	if strings.TrimSpace(p.EffectivePrompt()) == "" {
		return "", fmt.Errorf("node: profile %q has no instructions", p.Name)
	}
	path := filepath.Join(workspace, "AGENTS.md")
	body := "# Managed Secretary profile\n\n" + p.EffectivePrompt() + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return "", fmt.Errorf("write managed AGENTS.md: %w", err)
	}
	return path, nil
}

func (p ManagedProfile) openCodeConfig(workspace string) (string, error) {
	if strings.TrimSpace(p.EffectivePrompt()) == "" {
		return "", fmt.Errorf("node: profile %q has no instructions", p.Name)
	}
	name := "secretary-managed"
	if p.Hash != "" {
		name += "-" + p.Hash[:minInt(12, len(p.Hash))]
	}
	agent := map[string]any{
		"mode":        "primary",
		"description": "Managed Secretary profile. Do not select another agent.",
		"prompt":      p.EffectivePrompt(),
		"permission":  "allow",
	}
	if strings.Contains(p.Model, "/") {
		agent["model"] = p.Model
	}
	if p.Reasoning != "" && p.Reasoning != "default" {
		agent["reasoningEffort"] = p.Reasoning
	}
	if len(p.AllowTools) > 0 {
		tools := make(map[string]bool, len(p.AllowTools))
		for _, tool := range p.AllowTools {
			tools[tool] = true
		}
		agent["tools"] = tools
	}
	config := map[string]any{
		"$schema":       "https://opencode.ai/config.json",
		"default_agent": name,
		"agent":         map[string]any{name: agent},
	}
	if strings.Contains(p.Model, "/") {
		config["model"] = p.Model
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode OpenCode config: %w", err)
	}
	managedDir := filepath.Join(workspace, ".secretary")
	if err := os.MkdirAll(managedDir, 0o700); err != nil {
		return "", fmt.Errorf("create managed OpenCode config directory: %w", err)
	}
	path := filepath.Join(managedDir, "opencode.json")
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		return "", fmt.Errorf("write OpenCode config: %w", err)
	}
	return path, nil
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func HashProfile(content string, skills []ManagedSkill, fields ...string) string {
	h := sha256.New()
	_, _ = h.Write([]byte(content))
	for _, skill := range skills {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(skill.Path))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(skill.Hash))
	}
	for _, field := range fields {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(field))
	}
	return hex.EncodeToString(h.Sum(nil))
}
