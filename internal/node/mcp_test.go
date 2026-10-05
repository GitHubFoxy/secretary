package node

import (
	"encoding/json"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
)

func TestSecretaryMCPServerUsesScopedEnvironment(t *testing.T) {
	server := SecretaryMCPServer("secretary-mcp", "/state", "secretary-capability")
	encoded, err := json.Marshal(server)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, want := range []string{"\"name\":\"secretary\"", "SECRETARY_MCP_DATA_DIR", "SECRETARY_MCP_CAPABILITY", "secretary-capability"} {
		if !contains(text, want) {
			t.Fatalf("server=%s missing %q", text, want)
		}
	}
	for _, forbidden := range []string{"SECRETARY_MCP_ROLE", "SECRETARY_MCP_WORKER_REF", "worker"} {
		if contains(text, forbidden) {
			t.Fatalf("server=%s contains legacy %q", text, forbidden)
		}
	}
}

func TestSecretaryMCPServerAtCarriesOnlyScopedServerURL(t *testing.T) {
	server := SecretaryMCPServerAt("secretary-mcp", "/state", "capability", "http://127.0.0.1:8081")
	if len(server.Args) != 0 {
		t.Fatalf("server args=%v", server.Args)
	}
	encoded, err := json.Marshal(server)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, want := range []string{"SECRETARY_MCP_SERVER_URL", "http://127.0.0.1:8081"} {
		if !contains(text, want) {
			t.Fatalf("server=%s missing %q", text, want)
		}
	}
}

func TestSecretaryMCPReplyContractIsScopedAndOptIn(t *testing.T) {
	legacy := SecretaryMCPServerAtWithReplyContract("secretary-mcp", "/state", "capability", "http://127.0.0.1:8081", "")
	for _, entry := range legacy.Env {
		if entry.Name == mcpReplyContractEnv {
			t.Fatal("legacy MCP server received addressed reply contract")
		}
	}
	versioned := SecretaryMCPServerAtWithReplyContract("secretary-mcp", "/state", "capability", "http://127.0.0.1:8081", core.SecretaryReplyContractAddressedV1)
	found := false
	for _, entry := range versioned.Env {
		if entry.Name == mcpReplyContractEnv && entry.Value == core.SecretaryReplyContractAddressedV1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("versioned MCP env=%#v", versioned.Env)
	}
}

func contains(value, part string) bool {
	for i := 0; i+len(part) <= len(value); i++ {
		if value[i:i+len(part)] == part {
			return true
		}
	}
	return false
}
