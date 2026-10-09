package node

import (
	"errors"
	"path/filepath"
	"strings"
	"unicode"

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

func validateWorkerMCPServers(servers []MCPServer) error {
	names := map[string]bool{}
	for _, server := range servers {
		if server.Name == "" || strings.EqualFold(server.Name, "secretary") {
			return errors.New("node: invalid or reserved Worker MCP name")
		}
		for _, character := range server.Name {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
				return errors.New("node: invalid Worker MCP name")
			}
		}
		if names[server.Name] {
			return errors.New("node: duplicate Worker MCP name")
		}
		names[server.Name] = true
		if strings.TrimSpace(server.Command) == "" || strings.TrimSpace(server.Command) != server.Command || strings.IndexFunc(server.Command, unicode.IsControl) >= 0 {
			return errors.New("node: invalid Worker MCP command")
		}
		for _, arg := range server.Args {
			if strings.ContainsRune(arg, 0) {
				return errors.New("node: invalid Worker MCP argument")
			}
		}
		environment := map[string]bool{}
		for _, variable := range server.Env {
			if !validEnvironmentName(variable.Name) || strings.HasPrefix(variable.Name, "SECRETARY_MCP_") || variable.Name == "SECRETARY_CAPABILITY" || strings.ContainsRune(variable.Value, 0) {
				return errors.New("node: invalid or reserved Worker MCP environment")
			}
			if environment[variable.Name] {
				return errors.New("node: duplicate Worker MCP environment name")
			}
			environment[variable.Name] = true
		}
	}
	return nil
}
func cloneMCPServers(servers []MCPServer) []MCPServer {
	if servers == nil {
		return nil
	}
	cloned := append([]MCPServer{}, servers...)
	for i, server := range servers {
		cloned[i].Args = append([]string(nil), server.Args...)
		cloned[i].Env = append([]MCPEnv(nil), server.Env...)
	}
	return cloned
}
