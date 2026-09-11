package node

import "path/filepath"

const mcpCapabilityEnv = "SECRETARY_MCP_CAPABILITY"

// SecretaryMCPServer builds the per-session stdio MCP definition. The token is
// passed only to the helper process through ACP's env field, never in prompts.
func SecretaryMCPServer(command, dataDir, role, capability, workerRef string) MCPServer {
	env := []MCPEnv{
		{Name: "SECRETARY_MCP_DATA_DIR", Value: dataDir},
		{Name: "SECRETARY_MCP_ROLE", Value: role},
	}
	if capability != "" {
		env = append(env, MCPEnv{Name: mcpCapabilityEnv, Value: capability})
	}
	if workerRef != "" {
		env = append(env, MCPEnv{Name: "SECRETARY_MCP_WORKER_REF", Value: workerRef})
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
