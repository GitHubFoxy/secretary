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

func main() {
	out := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request message
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		if request.Method == "session/prompt" {
			_ = out.Encode(message{Method: "session/update", Params: json.RawMessage(`{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"fake activity"}}`)})
		}
		if len(request.ID) > 0 {
			result := any(map[string]any{})
			if request.Method == "session/new" {
				result = map[string]any{"sessionId": "fake-session"}
			}
			if request.Method == "session/prompt" {
				result = map[string]any{"summary": "fake task completed"}
			}
			_ = out.Encode(message{ID: request.ID, Result: result})
		}
	}
}
