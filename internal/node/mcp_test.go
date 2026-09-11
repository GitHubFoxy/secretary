package node

import (
	"encoding/json"
	"testing"
)

func TestSecretaryMCPServerUsesScopedEnvironment(t *testing.T) {
	server := SecretaryMCPServer("secretary-mcp", "/state", "worker", "wcap_1", "wrk_1")
	encoded, err := json.Marshal(server)
	if err != nil {
		t.Fatal(err)
	}
	text := string(encoded)
	for _, want := range []string{"\"name\":\"secretary\"", "SECRETARY_MCP_DATA_DIR", "SECRETARY_MCP_ROLE", "SECRETARY_MCP_CAPABILITY", "SECRETARY_MCP_WORKER_REF", "wcap_1"} {
		if !contains(text, want) {
			t.Fatalf("server=%s missing %q", text, want)
		}
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
