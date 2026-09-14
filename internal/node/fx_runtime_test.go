package node

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestFXRuntimeInterruptAndContinue(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestFakeFXACPProcess")
	runtime := FXRuntime{ACPRuntime: ACPRuntime{Command: command.Path, Arguments: command.Args[1:]}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "fx-worker", Task: "initial", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	time.Sleep(50 * time.Millisecond)
	injected, err := session.Steer(ctx, "replacement")
	if err != nil || !injected {
		t.Fatalf("injected=%v err=%v", injected, err)
	}
	statuses := make([]string, 0, 2)
	for len(statuses) < 2 {
		select {
		case result := <-session.Result():
			statuses = append(statuses, result.Status)
		case <-ctx.Done():
			t.Fatal("fx interrupt-and-continue did not finish")
		}
	}
	if statuses[0] != "canceled" || statuses[1] != "succeeded" {
		t.Fatalf("statuses=%v", statuses)
	}
}

func TestFakeFXACPProcess(t *testing.T) {
	for _, arg := range os.Args {
		if arg != "-test.run=TestFakeFXACPProcess" {
			continue
		}
		encoder := json.NewEncoder(os.Stdout)
		scanner := bufio.NewScanner(os.Stdin)
		var promptID json.RawMessage
		promptCount := 0
		for scanner.Scan() {
			var request struct {
				ID     json.RawMessage `json:"id,omitempty"`
				Method string          `json:"method"`
			}
			if json.Unmarshal(scanner.Bytes(), &request) != nil {
				continue
			}
			switch request.Method {
			case "session/new":
				_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]string{"sessionId": "fx-session"}})
			case "session/prompt":
				promptCount++
				if promptCount == 1 {
					promptID = append(json.RawMessage(nil), request.ID...)
				} else {
					_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]string{"summary": "replacement complete"}})
				}
			case "session/cancel":
				if len(promptID) > 0 {
					_ = encoder.Encode(map[string]any{"id": promptID, "result": map[string]string{"stopReason": "cancelled"}})
				}
			case "initialize":
				_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"protocolVersion": 1}})
			}
		}
		return
	}
}
