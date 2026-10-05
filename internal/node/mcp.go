package node

import (
	"path/filepath"

	"github.com/beruseruko/secretary/internal/core"
)

const (
	mcpCapabilityEnv    = "SECRETARY_MCP_CAPABILITY"
	mcpServerURLEnv     = "SECRETARY_MCP_SERVER_URL"
	mcpReplyContractEnv = "SECRETARY_MCP_REPLY_CONTRACT"
)

// SecretaryMCPServer builds the Secretary-only per-session stdio MCP
// definition. The token is passed only through ACP's env field, never in prompts.
func SecretaryMCPServer(command, dataDir, capability string) MCPServer {
	return SecretaryMCPServerAt(command, dataDir, capability, "")
}

// SecretaryMCPServerAt additionally points the MCP process at the in-process
// server runtime. Keeping the URL in scoped environment avoids putting it or
// the capability in the model prompt.
func SecretaryMCPServerAt(command, dataDir, capability, serverURL string) MCPServer {
	return SecretaryMCPServerAtWithReplyContract(command, dataDir, capability, serverURL, "")
}

func SecretaryMCPServerAtWithReplyContract(command, dataDir, capability, serverURL, replyContract string) MCPServer {
	env := []MCPEnv{{Name: "SECRETARY_MCP_DATA_DIR", Value: dataDir}}
	if capability != "" {
		env = append(env, MCPEnv{Name: mcpCapabilityEnv, Value: capability})
	}
	if serverURL != "" {
		env = append(env, MCPEnv{Name: mcpServerURLEnv, Value: serverURL})
	}
	if replyContract == core.SecretaryReplyContractAddressedV1 {
		env = append(env, MCPEnv{Name: mcpReplyContractEnv, Value: replyContract})
	}
	return MCPServer{
		Name:    "secretary",
		Command: command,
		Args:    []string{},
		Env:     env,
	}
}

func RawACPLogPath(dir, workerRef string) string {
	if dir == "" || workerRef == "" {
		return ""
	}
	return filepath.Join(dir, workerRef+".jsonl")
}
