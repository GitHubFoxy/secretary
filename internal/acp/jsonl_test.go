package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestClientAutomaticallyApprovesPermissionRequest(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestACPServerRequestProcess")
	client, err := Start(context.Background(), command.Path, command.Args[1:]...)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetServerRequestHandler(func(message Message) (any, error) {
		if message.Method != "session/request_permission" {
			return nil, errors.New("unexpected server request")
		}
		return map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": "allow_always"}}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var ignored map[string]any
	if err := client.Request(ctx, "initialize", map[string]any{}, &ignored); err != nil {
		t.Fatal(err)
	}
	if err := client.Request(ctx, "session/prompt", map[string]any{}, &ignored); err != nil {
		t.Fatal(err)
	}
}

func TestACPServerRequestProcess(t *testing.T) {
	for _, arg := range os.Args {
		if arg == "-test.run=TestACPServerRequestProcess" {
			encoder := json.NewEncoder(os.Stdout)
			scanner := bufio.NewScanner(os.Stdin)
			approved := false
			for scanner.Scan() {
				var request struct {
					ID     json.RawMessage `json:"id,omitempty"`
					Method string          `json:"method"`
					Result map[string]any  `json:"result,omitempty"`
				}
				if json.Unmarshal(scanner.Bytes(), &request) != nil {
					continue
				}
				if string(request.ID) == "900" {
					outcome, _ := request.Result["outcome"].(map[string]any)
					approved = outcome["outcome"] == "selected" && outcome["optionId"] == "allow_always"
					if approved {
						_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 2, "result": map[string]string{"summary": "approved"}})
					}
					continue
				}
				if request.Method == "session/prompt" {
					_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 900, "method": "session/request_permission", "params": map[string]any{"options": []map[string]string{{"optionId": "allow_always", "kind": "allow_always"}}}})
					continue
				}
				if len(request.ID) == 0 {
					continue
				}
				result := map[string]any{}
				if approved {
					result = map[string]any{"summary": "approved"}
				}
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
			}
			return
		}
	}
}
