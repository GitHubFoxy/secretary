package node

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
)

var (
	ErrManagedProfileRequired = errors.New("node: managed Worker Profile is required")
	ErrManagedProfileInvalid  = errors.New("node: managed Worker Profile is invalid")
)

type ManagedSkill struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Hash    string `json:"hash"`
}

type ManagedProfile struct {
	Version string `json:"version,omitempty"`
	Name    string `json:"name,omitempty"`
	Content string `json:"content,omitempty"`
	// Hash-significant collections must retain null versus [] across storage/wire.
	Skills               []ManagedSkill `json:"skills"`
	AllowTools           []string       `json:"allow_tools"`
	Hash                 string         `json:"hash,omitempty"`
	SourceHash           string         `json:"source_hash,omitempty"`
	Runtime              string         `json:"runtime,omitempty"`
	Model                string         `json:"model,omitempty"`
	Reasoning            string         `json:"reasoning,omitempty"`
	Delivery             string         `json:"delivery,omitempty"`
	ReplyContractVersion string         `json:"reply_contract_version,omitempty"`
}

// SnapshotHash binds exact instructions, skills, permissions and resolved
// execution pins to the source Profile identity.
func (p ManagedProfile) SnapshotHash() string {
	encoded, _ := json.Marshal(struct {
		Version              string         `json:"version"`
		Name                 string         `json:"name"`
		Content              string         `json:"content"`
		Skills               []ManagedSkill `json:"skills"`
		AllowTools           []string       `json:"allow_tools"`
		SourceHash           string         `json:"source_hash"`
		Runtime              string         `json:"runtime"`
		Model                string         `json:"model"`
		Reasoning            string         `json:"reasoning"`
		Delivery             string         `json:"delivery"`
		ReplyContractVersion string         `json:"reply_contract_version"`
	}{p.Version, p.Name, p.Content, p.Skills, p.AllowTools, p.SourceHash, p.Runtime, p.Model, p.Reasoning, p.Delivery, p.ReplyContractVersion})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func (p ManagedProfile) ValidateWorkerBinding(runtime, model, reasoning string, policy core.ProjectPolicy) error {
	if p.Version == "" || p.Name != "worker" || strings.TrimSpace(p.Content) == "" || p.SourceHash == "" ||
		p.Runtime != runtime || p.Model != model || p.Reasoning != reasoning || p.Hash == "" || p.Hash != p.SnapshotHash() {
		return ErrManagedProfileInvalid
	}
	expectedDelivery := "workspace_instructions"
	if runtime == string(core.HarnessOpenCode) || runtime == string(core.HarnessCodex) || runtime == string(core.HarnessClaudeCode) {
		expectedDelivery = "native"
	}
	if p.Delivery != expectedDelivery && !((runtime == string(core.HarnessCodex) || runtime == string(core.HarnessClaudeCode)) && p.Delivery == "workspace_instructions") {
		return ErrManagedProfileInvalid
	}
	if runtime == string(core.HarnessOpenCode) {
		if _, err := p.openCodePermissions(nil); err != nil {
			return ErrManagedProfileInvalid
		}
	}
	execution := policy.EffectiveExecution()
	for _, tool := range p.AllowTools {
		var capability core.ExecutionCapability
		switch tool {
		case "bash", "shell":
			capability = core.CapabilityShell
		case "edit", "write", "patch":
			capability = core.CapabilityEdit
		}
		if capability == "" {
			continue
		}
		for _, denied := range execution.DeniedCapabilities {
			if denied == capability {
				return ErrManagedProfileInvalid
			}
		}
		if len(execution.AllowedCapabilities) > 0 {
			allowed := false
			for _, value := range execution.AllowedCapabilities {
				allowed = allowed || value == capability
			}
			if !allowed {
				return ErrManagedProfileInvalid
			}
		}
	}
	return nil
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

func (p ManagedProfile) openCodeDeliveryMarker(permissions []map[string]string) string {
	// Hash the actual delivered content and ordered policy, including profiles
	// whose domain hash is absent or predates an adapter/schema change.
	encoded, _ := json.Marshal(struct {
		Prompt               string
		Name                 string
		Tools                []string
		Model                string
		Reasoning            string
		ReplyContractVersion string
		Permissions          []map[string]string
	}{p.EffectivePrompt(), p.Name, p.AllowTools, p.Model, p.Reasoning, p.ReplyContractVersion, permissions})
	digest := sha256.Sum256(encoded)
	return "secretary-profile-v2-" + hex.EncodeToString(digest[:])
}

func (p ManagedProfile) openCodeAgentName(permissions []map[string]string) string {
	return "secretary-managed-" + strings.TrimPrefix(p.openCodeDeliveryMarker(permissions), "secretary-profile-v2-")
}

func (p ManagedProfile) openCodeConfig(workspace string, mcpServers []MCPServer) (string, error) {
	if strings.TrimSpace(p.EffectivePrompt()) == "" {
		return "", fmt.Errorf("node: profile %q has no instructions", p.Name)
	}
	permissions, err := p.openCodePermissions(mcpServers)
	if err != nil {
		return "", err
	}
	name := p.openCodeAgentName(permissions)
	agent := map[string]any{
		"mode":        "primary",
		"description": p.openCodeDeliveryMarker(permissions),
		"system":      p.EffectivePrompt(),
		"permissions": permissions,
	}
	config := map[string]any{
		"$schema":       "https://opencode.ai/config.json",
		"default_agent": name,
		"permissions":   []map[string]string{{"action": "*", "resource": "*", "effect": "deny"}},
		"agents":        map[string]any{name: agent},
	}
	if len(mcpServers) > 0 {
		servers := make(map[string]any, len(mcpServers))
		for _, server := range mcpServers {
			if strings.TrimSpace(server.Command) == "" {
				return "", fmt.Errorf("opencode: MCP server %q has no command", server.Name)
			}
			command := append([]string{server.Command}, server.Args...)
			environment := make(map[string]string, len(server.Env))
			for _, variable := range server.Env {
				if !validEnvironmentName(variable.Name) {
					return "", fmt.Errorf("opencode: MCP server %q has invalid environment name", server.Name)
				}
				if _, duplicate := environment[variable.Name]; duplicate {
					return "", fmt.Errorf("opencode: MCP server %q repeats an environment name", server.Name)
				}
				environment[variable.Name] = "{env:" + variable.Name + "}"
			}
			servers[server.Name] = map[string]any{
				"type": "local", "command": command, "environment": environment, "codemode": false,
			}
		}
		config["mcp"] = map[string]any{"servers": servers}
	}
	model := strings.TrimSpace(p.Model)
	if model != "" && !isModelAlias(model) {
		provider, modelID, qualified := strings.Cut(model, "/")
		if !qualified || provider == "" || modelID == "" {
			return "", fmt.Errorf("opencode: model %q must be provider-qualified", model)
		}
		config["model"] = model
		agent["model"] = model
		if p.Reasoning != "" && p.Reasoning != "default" {
			variant := p.Reasoning
			config["providers"] = map[string]any{provider: map[string]any{
				"models": map[string]any{modelID: map[string]any{
					"capabilities": map[string]any{"tools": len(p.AllowTools) > 0 || len(mcpServers) > 0},
					"variants":     []map[string]any{{"id": variant, "settings": map[string]string{"reasoningEffort": p.Reasoning}}},
				}},
			}}
		}
	} else if p.Reasoning != "" && p.Reasoning != "default" {
		return "", fmt.Errorf("opencode: reasoning %q requires a provider-qualified model", p.Reasoning)
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

func (p ManagedProfile) openCodePermissions(mcpServers []MCPServer) ([]map[string]string, error) {
	permissions := make([]map[string]string, 0)
	if p.ReplyContractVersion != "" && (p.Name != "secretary" || p.ReplyContractVersion != core.SecretaryReplyContractAddressedV1) {
		return nil, fmt.Errorf("opencode: unsupported managed Secretary reply contract %q", p.ReplyContractVersion)
	}
	allow := func(action string) {
		permissions = append(permissions, map[string]string{"action": action, "resource": "*", "effect": "allow"})
	}
	if p.Name == "secretary" {
		for _, server := range mcpServers {
			if server.Name != "secretary" {
				return nil, fmt.Errorf("opencode: Secretary profile cannot attach MCP server %q", server.Name)
			}
		}
		if len(mcpServers) > 1 {
			return nil, fmt.Errorf("opencode: Secretary profile accepts only one server-owned MCP instance")
		}
		if len(mcpServers) == 1 {
			allow("secretary_*")
		}
		return permissions, nil
	}
	if len(mcpServers) > 0 {
		return nil, fmt.Errorf("opencode: managed profile %q cannot attach MCP servers", p.Name)
	}
	actions := map[string][]string{
		"bash":               {"shell"},
		"shell":              {"shell"},
		"edit":               {"edit"},
		"write":              {"edit"},
		"patch":              {"edit"},
		"find":               {"glob"},
		"glob":               {"glob"},
		"grep":               {"grep"},
		"read":               {"read"},
		"webfetch":           {"webfetch"},
		"websearch":          {"websearch"},
		"question":           {"question"},
		"skill":              {"skill"},
		"task":               {"subagent"},
		"subagent":           {"subagent"},
		"external_directory": {"external_directory"},
	}
	seen := make(map[string]struct{}, len(p.AllowTools))
	for _, tool := range p.AllowTools {
		mapped, ok := actions[tool]
		if !ok {
			return nil, fmt.Errorf("opencode: unsupported managed tool %q", tool)
		}
		for _, action := range mapped {
			if _, ok := seen[action]; ok {
				continue
			}
			seen[action] = struct{}{}
			allow(action)
		}
	}
	return permissions, nil
}

func validEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for index, char := range name {
		if (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || char == '_' || index > 0 && char >= '0' && char <= '9' {
			continue
		}
		return false
	}
	return true
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
