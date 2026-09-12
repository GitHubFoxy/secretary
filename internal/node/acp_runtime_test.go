package node

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestACPRuntimeResumeRebindsOutstandingRequestsBeforeSessionLoad(t *testing.T) {
	for _, kind := range []string{"permission", "input"} {
		t.Run(kind, func(t *testing.T) {
			responsesPath := filepath.Join(t.TempDir(), "responses.jsonl")
			command := exec.Command(os.Args[0], "-test.run=TestFakeACPReconnectProcess")
			runtime := ACPRuntime{Command: command.Path, Arguments: command.Args[1:], Environment: []string{"ACP_RECONNECT_KIND=" + kind, "ACP_RESPONSES_FILE=" + responsesPath}}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session, err := runtime.Resume(ctx, StartRequest{WorkerRef: "worker", Workspace: t.TempDir(), PendingRequestIDs: []string{"durable-" + kind}}, "saved-session")
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			select {
			case activity := <-session.Activity():
				if activity.RequestID != "durable-"+kind {
					t.Fatalf("request id=%q, want durable id", activity.RequestID)
				}
				if kind == "permission" && activity.Kind != ActivityPermission {
					t.Fatalf("activity=%#v", activity)
				}
				if kind == "input" && activity.Kind != ActivityUserInput {
					t.Fatalf("activity=%#v", activity)
				}
			case <-ctx.Done():
				t.Fatal("ACP request was not replayed")
			}
			if err := session.(Responder).Respond(ctx, "durable-"+kind, map[string]string{"permission": "denied", "input": "answer"}[kind]); err != nil {
				t.Fatal(err)
			}
			if err := session.(Responder).Respond(ctx, "durable-"+kind, "duplicate"); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(2 * time.Second)
			for {
				data, readErr := os.ReadFile(responsesPath)
				if readErr == nil && strings.TrimSpace(string(data)) != "" && strings.Count(string(data), "\n") == 1 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("responses=%q", data)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}

func TestACPRuntimeRespondFailsWhenNativeReplyWriterIsUnavailable(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestFakeACPReplyWriterFailureProcess")
	runtime := ACPRuntime{Command: command.Path, Arguments: command.Args[1:]}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := runtime.Resume(ctx, StartRequest{WorkerRef: "worker", Workspace: t.TempDir(), PendingRequestIDs: []string{"durable-permission"}}, "saved-session")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	select {
	case activity := <-session.Activity():
		if activity.RequestID != "durable-permission" {
			t.Fatalf("request id=%q", activity.RequestID)
		}
	case <-ctx.Done():
		t.Fatal("ACP request was not replayed")
	}
	select {
	case <-time.After(100 * time.Millisecond):
	}
	if err := session.(Responder).Respond(ctx, "durable-permission", "denied"); err == nil {
		t.Fatal("Respond reported success after native ACP process stopped")
	}
}

func TestFakeACPReplyWriterFailureProcess(t *testing.T) {
	for _, arg := range os.Args {
		if arg != "-test.run=TestFakeACPReplyWriterFailureProcess" {
			continue
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
			case "initialize":
				_ = encoder.Encode(map[string]any{"id": json.RawMessage(request.ID), "result": map[string]any{}})
			case "session/load":
				_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 77, "method": "session/request_permission", "params": map[string]any{"options": []map[string]string{{"optionId": "deny", "kind": "reject_once"}}}})
				_ = encoder.Encode(map[string]any{"id": json.RawMessage(request.ID), "result": map[string]any{}})
				_ = os.Stdin.Close()
				time.Sleep(500 * time.Millisecond)
				return
			}
		}
		return
	}
}

func TestACPRuntimeApprovalPrefersOneShotOption(t *testing.T) {
	responsePath := filepath.Join(t.TempDir(), "selected-option")
	command := exec.Command(os.Args[0], "-test.run=TestFakeACPAllowOnceProcess")
	runtime := ACPRuntime{Command: command.Path, Arguments: command.Args[1:], Environment: []string{"ACP_SELECTED_OPTION_FILE=" + responsePath}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "worker", Task: "inspect", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	var requestID string
	select {
	case activity := <-session.Activity():
		if activity.Kind != ActivityPermission {
			t.Fatalf("activity=%#v", activity)
		}
		requestID = activity.RequestID
	case <-ctx.Done():
		t.Fatal("permission request was not observed")
	}
	if err := session.(Responder).Respond(ctx, requestID, "approved"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		selected, readErr := os.ReadFile(responsePath)
		if readErr == nil && strings.TrimSpace(string(selected)) != "" {
			if strings.TrimSpace(string(selected)) != "allow_once" {
				t.Fatalf("selected option=%q", selected)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("fake ACP did not record selected option")
}

func TestFakeACPAllowOnceProcess(t *testing.T) {
	if os.Getenv("ACP_SELECTED_OPTION_FILE") == "" {
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
		case "initialize":
			_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{}})
		case "session/new":
			_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]string{"sessionId": "allow-once-session"}})
		case "session/prompt":
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 91, "method": "session/request_permission", "params": map[string]any{"options": []map[string]string{{"optionId": "allow_once", "kind": "allow_once"}, {"optionId": "allow_always", "kind": "allow_always"}}}})
		default:
			if len(request.ID) > 0 {
				var response struct {
					ID     json.RawMessage `json:"id"`
					Result struct {
						Outcome struct {
							OptionID string `json:"optionId"`
						} `json:"outcome"`
					} `json:"result"`
				}
				if json.Unmarshal(scanner.Bytes(), &response) == nil && response.Result.Outcome.OptionID != "" {
					_ = os.WriteFile(os.Getenv("ACP_SELECTED_OPTION_FILE"), []byte(response.Result.Outcome.OptionID), 0o600)
					_ = encoder.Encode(map[string]any{"id": 92, "result": map[string]string{"summary": "approved"}})
				}
			}
		}
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

func TestFakeACPReconnectProcess(t *testing.T) {
	if os.Getenv("ACP_RECONNECT_KIND") == "" {
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
		if request.Method == "initialize" {
			_ = encoder.Encode(map[string]any{"id": json.RawMessage(request.ID), "result": map[string]any{}})
			continue
		}
		if request.Method == "session/load" {
			method := "session/request_permission"
			params := map[string]any{"options": []map[string]string{{"optionId": "deny", "kind": "reject_once"}}}
			if os.Getenv("ACP_RECONNECT_KIND") == "input" {
				method = "session/request_input"
				params = map[string]any{"question": "which file?"}
			}
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 77, "method": method, "params": params})
			_ = encoder.Encode(map[string]any{"id": json.RawMessage(request.ID), "result": map[string]any{}})
			continue
		}
		if len(request.ID) > 0 {
			var response struct {
				ID     json.RawMessage `json:"id"`
				Result json.RawMessage `json:"result"`
			}
			if request.Method == "" && json.Unmarshal(scanner.Bytes(), &response) == nil && string(response.ID) == "77" {
				if path := os.Getenv("ACP_RESPONSES_FILE"); path != "" {
					file, _ := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
					if file != nil {
						_, _ = file.Write(append(scanner.Bytes(), '\n'))
						_ = file.Close()
					}
				}
			}
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
