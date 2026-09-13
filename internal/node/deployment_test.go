package node

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestDeploymentConfigSupportsPrivateServerWorkspaceMappingsAndNoNodeListener(t *testing.T) {
	config := DeploymentConfig{
		ServerURL: "https://secretary.tailnet.ts.net",
		Node:      "macbook",
		DataDir:   "/Users/alice/.secretary/node",
		Workspaces: []WorkspaceMapping{
			{ProjectID: "frontend", Path: "/Users/alice/src/frontend"},
			{ProjectID: "infra", Path: "/Volumes/work/infra"},
		},
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("valid private deployment rejected: %v", err)
	}
	workspaces, err := config.ProtocolWorkspaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(workspaces) != 2 || workspaces[0].ProjectID != "frontend" || workspaces[1].Path != "/Volumes/work/infra" {
		t.Fatalf("unexpected protocol workspaces: %#v", workspaces)
	}

	for _, listener := range []string{"127.0.0.1:0", ":8082", "0.0.0.0:8082"} {
		config.ListenAddress = listener
		if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "inbound") {
			t.Fatalf("listener %q was accepted: %v", listener, err)
		}
	}
}

func TestDeploymentConfigPersistsWithPrivateFilePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	config := DeploymentConfig{ServerURL: "http://127.0.0.1:8081", Node: "local", DataDir: filepath.Join(t.TempDir(), "node")}
	if err := SaveDeploymentConfig(path, config); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadDeploymentConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Node != config.Node || loaded.ServerURL != config.ServerURL {
		t.Fatalf("loaded config=%#v, want %#v", loaded, config)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config permissions=%#o, want 0600", got)
	}
}

func TestHarnessDiscoveryUsesInstalledBinaryOverrides(t *testing.T) {
	var calls []string
	runner := CommandRunnerFunc(func(_ context.Context, name string, args ...string) (CommandResult, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return CommandResult{Stdout: "1.2.3\nmodels: test-model\nreasoning: medium\n"}, nil
	})
	inventory, err := (HarnessDiscovery{Node: "macbook", Runner: runner, BinaryOverrides: map[core.HarnessKind]string{core.HarnessFX: "/opt/fx"}}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Instances) == 0 || !strings.HasPrefix(calls[0], "/opt/fx ") {
		t.Fatalf("discovery did not use installed FX path: calls=%v inventory=%#v", calls, inventory)
	}
}

func TestCredentialSetSeparatesRolesAndRedactsExport(t *testing.T) {
	credentials := CredentialSet{
		Node:             "node-credential",
		Client:           "client-credential",
		SecretaryRuntime: "secretary-capability",
		Telegram:         "telegram-bot-token",
	}
	if err := credentials.Validate(); err != nil {
		t.Fatal(err)
	}
	rawEncoded, err := json.Marshal(credentials)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"node-credential", "client-credential", "secretary-capability", "telegram-bot-token"} {
		if strings.Contains(string(rawEncoded), secret) {
			t.Fatalf("raw credential set leaked %q: %s", secret, rawEncoded)
		}
	}
	redacted := credentials.Redacted()
	encoded, err := json.Marshal(redacted)
	if err != nil {
		t.Fatal(err)
	}
	output := string(encoded)
	for _, secret := range []string{"node-credential", "client-credential", "secretary-capability", "telegram-bot-token"} {
		if strings.Contains(output, secret) {
			t.Fatalf("credential leaked in redacted export: %q in %s", secret, output)
		}
	}
	if !strings.Contains(output, "[redacted]") {
		t.Fatalf("redacted export did not mark secret fields: %s", output)
	}

	credentials.Client = credentials.Node
	if err := credentials.Validate(); err == nil || !strings.Contains(err.Error(), "Client") {
		t.Fatalf("shared Node and Client credential was accepted: %v", err)
	}
}

func TestCredentialSetRejectsClientCredentialAsNodeCredential(t *testing.T) {
	credentials := CredentialSet{Node: "same", Client: "same", SecretaryRuntime: "secretary", Telegram: "telegram"}
	if err := credentials.Validate(); err == nil {
		t.Fatal("Client credential was accepted as Node credential")
	}
}

func TestDeploymentConfigRejectsPublicAndInvalidServerURLs(t *testing.T) {
	base := DeploymentConfig{ServerURL: "https://secretary.tailnet.ts.net", Node: core.NodeReference("home-server"), DataDir: "/srv/secretary/node"}
	for _, serverURL := range []string{"", "localhost", "ftp://secretary.example", "https://"} {
		base.ServerURL = serverURL
		if err := base.Validate(); err == nil {
			t.Fatalf("invalid server URL %q was accepted", serverURL)
		}
	}
}
