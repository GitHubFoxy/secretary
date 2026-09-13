package node

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
)

// WorkspaceMapping is the local path for a server-owned Project on one Node.
// Paths are operator configuration, never values supplied by a Worker.
type WorkspaceMapping struct {
	ProjectID string `json:"project_id"`
	Path      string `json:"path"`
}

// DeploymentConfig is the non-secret configuration used to install a Node.
// A Node has no listener. It only dials the Secretary server outbound.
type DeploymentConfig struct {
	ServerURL       string             `json:"server_url"`
	Node            core.NodeReference `json:"node"`
	DataDir         string             `json:"data_dir"`
	Workspaces      []WorkspaceMapping `json:"workspaces,omitempty"`
	Capacity        int                `json:"capacity,omitempty"`
	ListenAddress   string             `json:"listen_address,omitempty"`
	IncludeOpenCode bool               `json:"include_opencode,omitempty"`
}

func LoadDeploymentConfig(path string) (DeploymentConfig, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return DeploymentConfig{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var config DeploymentConfig
	if err := decoder.Decode(&config); err != nil {
		return DeploymentConfig{}, fmt.Errorf("node deployment: decode config: %w", err)
	}
	if err := config.Validate(); err != nil {
		return DeploymentConfig{}, err
	}
	return config, nil
}

func SaveDeploymentConfig(path string, config DeploymentConfig) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return errors.New("node deployment: config path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, append(encoded, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return os.Chmod(path, 0o600)
}

func (c DeploymentConfig) Validate() error {
	serverURL := strings.TrimSpace(c.ServerURL)
	parsed, err := url.Parse(serverURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || strings.TrimSpace(parsed.Host) == "" || parsed.User != nil {
		return errors.New("node deployment: server URL must be an http(s) URL without credentials")
	}
	if !validNodeReference(c.Node) {
		return errors.New("node deployment: valid Node identity is required")
	}
	if strings.TrimSpace(c.DataDir) == "" || !filepath.IsAbs(c.DataDir) {
		return errors.New("node deployment: absolute data directory is required")
	}
	if strings.TrimSpace(c.ListenAddress) != "" {
		return errors.New("node deployment: inbound Node listener is forbidden")
	}
	if c.Capacity < 0 {
		return errors.New("node deployment: capacity cannot be negative")
	}
	seen := make(map[string]struct{}, len(c.Workspaces))
	for _, mapping := range c.Workspaces {
		project := strings.TrimSpace(mapping.ProjectID)
		if project == "" {
			return errors.New("node deployment: workspace Project ID is required")
		}
		if _, exists := seen[project]; exists {
			return fmt.Errorf("node deployment: duplicate workspace mapping for Project %q", project)
		}
		seen[project] = struct{}{}
		if strings.TrimSpace(mapping.Path) == "" || !filepath.IsAbs(mapping.Path) {
			return fmt.Errorf("node deployment: workspace mapping for Project %q must use an absolute path", project)
		}
	}
	return nil
}

func (c DeploymentConfig) ProtocolWorkspaces() ([]Workspace, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	workspaces := make([]Workspace, 0, len(c.Workspaces))
	for _, mapping := range c.Workspaces {
		workspaces = append(workspaces, Workspace{ProjectID: mapping.ProjectID, Path: filepath.Clean(mapping.Path)})
	}
	sort.SliceStable(workspaces, func(i, j int) bool { return workspaces[i].ProjectID < workspaces[j].ProjectID })
	return workspaces, nil
}

// CredentialSet names the four independent credential domains. The raw values
// are accepted only at process boundaries and are never suitable for export.
type CredentialSet struct {
	Node             string `json:"-"`
	Client           string `json:"-"`
	SecretaryRuntime string `json:"-"`
	Telegram         string `json:"-"`
}

func (c CredentialSet) Validate() error {
	values := []struct {
		name  string
		value string
	}{
		{"Node", strings.TrimSpace(c.Node)},
		{"Client", strings.TrimSpace(c.Client)},
		{"Secretary runtime", strings.TrimSpace(c.SecretaryRuntime)},
		{"Telegram", strings.TrimSpace(c.Telegram)},
	}
	seen := make(map[string]string, len(values))
	for _, item := range values {
		if item.value == "" {
			continue
		}
		if previous, exists := seen[item.value]; exists {
			return fmt.Errorf("node credentials: %s credential must differ from %s credential", item.name, previous)
		}
		seen[item.value] = item.name
	}
	return nil
}

type RedactedCredentialSet struct {
	Node             string `json:"node_credential,omitempty"`
	Client           string `json:"client_credential,omitempty"`
	SecretaryRuntime string `json:"secretary_runtime_credential,omitempty"`
	Telegram         string `json:"telegram_token,omitempty"`
}

func (c CredentialSet) Redacted() RedactedCredentialSet {
	redact := func(value string) string {
		if strings.TrimSpace(value) == "" {
			return ""
		}
		return "[redacted]"
	}
	return RedactedCredentialSet{Node: redact(c.Node), Client: redact(c.Client), SecretaryRuntime: redact(c.SecretaryRuntime), Telegram: redact(c.Telegram)}
}

// RedactSecrets is used at log and diagnostic boundaries. It replaces longer
// secrets first, so a shared prefix cannot reveal a suffix of a credential.
func RedactSecrets(value string, secrets ...string) string {
	unique := make([]string, 0, len(secrets))
	seen := make(map[string]struct{}, len(secrets))
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if secret == "" {
			continue
		}
		if _, exists := seen[secret]; exists {
			continue
		}
		seen[secret] = struct{}{}
		unique = append(unique, secret)
	}
	sort.SliceStable(unique, func(i, j int) bool { return len(unique[i]) > len(unique[j]) })
	for _, secret := range unique {
		value = strings.ReplaceAll(value, secret, "[redacted]")
	}
	return value
}
