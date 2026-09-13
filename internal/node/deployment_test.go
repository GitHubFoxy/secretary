package node

import (
	"encoding/json"
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
