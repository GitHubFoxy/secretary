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

func TestACPRuntimeUsesFakeACPProcess(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestFakeACPProcess")
	runtime := ACPRuntime{Command: command.Path, Arguments: command.Args[1:]}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "worker", Task: "inspect", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	select {
	case activity := <-session.Activity():
		if activity.Kind != ActivityText || activity.Text != "fake activity" {
			t.Fatalf("activity=%#v", activity)
		}
	case <-ctx.Done():
		t.Fatal("fake ACP did not publish activity")
	}
	select {
	case result := <-session.Result():
		if result.Status != "succeeded" || result.Summary != "fake task completed" {
			t.Fatalf("result=%#v", result)
		}
	case <-ctx.Done():
		t.Fatal("fake ACP did not complete prompt")
	}
	injected, err := session.Steer(ctx, "keep going")
	if err != nil || !injected {
		t.Fatalf("steer injected=%v err=%v", injected, err)
	}
}

func TestACPRuntimeResumesExistingSession(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestFakeACPProcess")
	runtime := ACPRuntime{Command: command.Path, Arguments: command.Args[1:]}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := runtime.Resume(ctx, StartRequest{WorkerRef: "worker", Workspace: t.TempDir()}, "saved-session")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	queue := session.(Queueer)
	if err := queue.Queue(ctx, "continue"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-session.Result():
		if result.Status != "succeeded" {
			t.Fatalf("result=%#v", result)
		}
	case <-ctx.Done():
		t.Fatal("resumed ACP prompt did not complete")
	}
}

func TestACPRuntimeQueuesPromptUntilCurrentTurnCompletes(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestFakeACPProcess")
	runtime := ACPRuntime{Command: command.Path, Arguments: command.Args[1:]}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "worker", Task: "initial", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	queue, ok := session.(Queueer)
	if !ok {
		t.Fatal("ACP session does not implement Queueer")
	}
	if err := queue.Queue(ctx, "follow-up"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		select {
		case result := <-session.Result():
			if result.Status != "succeeded" {
				t.Fatalf("result=%#v", result)
			}
		case <-ctx.Done():
			t.Fatal("queued ACP prompt did not complete")
		}
	}
}

func TestFakeACPProcess(t *testing.T) {
	helper := false
	for _, arg := range os.Args {
		if arg == "-test.run=TestFakeACPProcess" {
			helper = true
			break
		}
	}
	if !helper {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     json.RawMessage `json:"id,omitempty"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			continue
		}
		switch request.Method {
		case "session/prompt":
			_ = encoder.Encode(map[string]any{"method": "session/update", "params": map[string]any{"sessionId": "fake-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "fake activity"}}}})
		}
		if len(request.ID) == 0 {
			continue
		}
		result := map[string]any{}
		switch request.Method {
		case "session/new":
			result = map[string]any{"sessionId": "fake-session"}
		case "session/prompt":
			result = map[string]any{"summary": "fake task completed"}
		case "_session/steering":
			result = map[string]any{"outcome": "injected"}
		}
		_ = encoder.Encode(map[string]any{"id": request.ID, "result": result})
	}
}
