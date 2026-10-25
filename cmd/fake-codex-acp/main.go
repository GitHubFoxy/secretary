package main

import (
	"bufio"
	"encoding/json"
	"os"
)

type message struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result any             `json:"result,omitempty"`
}

var packagedACPVersion string

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" && packagedACPVersion != "" {
		_, _ = os.Stdout.WriteString("@agentclientprotocol/codex-acp " + packagedACPVersion + "\n")
		return
	}
	out := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request message
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		if request.Method == "session/prompt" {
			_ = out.Encode(message{Method: "session/update", Params: json.RawMessage(`{"sessionId":"fake-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"fake activity"}}}`)})
		}
		if len(request.ID) > 0 {
			result := any(map[string]any{})
			if request.Method == "session/set_config_option" && os.Getenv("OPENCODE_CONFIG") != "" {
				var ok bool
				result, ok = openCodeFixtureSelection(request.Params)
				if !ok {
					_ = out.Encode(map[string]any{"id": request.ID, "error": map[string]any{"code": -32602, "message": "fixture selection absent from config"}})
					continue
				}
			}
			if request.Method == "initialize" {
				result = map[string]any{
					"protocolVersion": 1,
					"agentInfo":       map[string]any{"version": "1.0.0"},
					"agentCapabilities": map[string]any{
						"loadSession": true,
					},
					"_meta": map[string]any{
						"steering": map[string]any{"supported": true},
					},
				}
			}
			if request.Method == "session/new" {
				result = map[string]any{
					"sessionId": "fake-session",
					"models": map[string]any{
						"availableModels": []map[string]string{{"modelId": "gpt-5-codex"}},
					},
				}
			}
			if request.Method == "session/prompt" {
				result = map[string]any{"summary": "fake task completed"}
			}
			_ = out.Encode(message{ID: request.ID, Result: result})
		}
	}
}
