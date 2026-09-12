package node

import "path/filepath"

const mcpCapabilityEnv = "SECRETARY_MCP_CAPABILITY"

// SecretaryMCPServer builds the Secretary-only per-session stdio MCP
// definition. The token is passed only through ACP's env field, never in prompts.
func SecretaryMCPServer(command, dataDir, capability string) MCPServer {
	env := []MCPEnv{{Name: "SECRETARY_MCP_DATA_DIR", Value: dataDir}}
	if capability != "" {
		env = append(env, MCPEnv{Name: mcpCapabilityEnv, Value: capability})
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
