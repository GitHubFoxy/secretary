package node

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestACPRuntimeMCPArraysOnStartAndResume(t *testing.T) {
	for _, resume := range []bool{false, true} {
		for _, servers := range [][]MCPServer{
			nil,
			{{Name: "omitted", Command: "mcp"}},
			{{Name: "empty", Command: "mcp", Args: []string{}, Env: []MCPEnv{}}},
			{{Name: "configured", Command: "mcp", Args: []string{"--read"}, Env: []MCPEnv{{Name: "SCOPE", Value: "worker"}}}},
		} {
			name := "Start"
			if resume {
				name = "Resume"
			}
			if len(servers) != 0 {
				name += "/" + servers[0].Name
			}
			t.Run(name, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				runtime := ACPRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestFakeACPMCPArraysProcess$"}, Environment: []string{"ACP_MCP_ARRAY_FIXTURE=1"}}
				before, err := json.Marshal(servers)
				if err != nil {
					t.Fatal(err)
				}
				request := StartRequest{WorkerRef: "worker", Workspace: t.TempDir(), DeferInitialPrompt: true, MCPServers: servers}
				var session Session
				if resume {
					session, err = runtime.Resume(ctx, request, "mcp-wire-session")
				} else {
					session, err = runtime.Start(ctx, request)
				}
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				if session.ID() != "mcp-wire-session" {
					t.Fatalf("session identity=%q", session.ID())
				}
				after, err := json.Marshal(request.MCPServers)
				if err != nil || string(before) != string(after) {
					t.Fatalf("request mutated: before=%s after=%s err=%v", before, after, err)
				}
			})
		}
	}
}

func TestFakeACPMCPArraysProcess(t *testing.T) {
	if os.Getenv("ACP_MCP_ARRAY_FIXTURE") != "1" {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				MCPServers json.RawMessage `json:"mcpServers"`
			} `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		result := map[string]any{}
		switch request.Method {
		case "initialize":
			result["protocolVersion"] = 1
		case "session/new", "session/load":
			var servers []map[string]json.RawMessage
			valid := len(request.Params.MCPServers) > 0 && request.Params.MCPServers[0] == '[' && json.Unmarshal(request.Params.MCPServers, &servers) == nil
			for _, server := range servers {
				if string(server["name"]) == `"configured"` {
					valid = valid && string(server["args"]) == `["--read"]` && string(server["env"]) == `[{"name":"SCOPE","value":"worker"}]`
				}
				for _, field := range []string{"args", "env"} {
					value := server[field]
					valid = valid && len(value) > 0 && value[0] == '['
				}
			}
			if !valid {
				_ = encoder.Encode(map[string]any{"id": request.ID, "error": map[string]any{"code": -32602, "message": "MCP servers, args, env must be arrays"}})
				continue
			}
			result["sessionId"] = "mcp-wire-session"
		}
		_ = encoder.Encode(map[string]any{"id": request.ID, "result": result})
	}
	os.Exit(0)
}
