package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestServerRequestDeliveryWaitsForNativeReplyWriter(t *testing.T) {
	writer := &blockingReplyWriter{started: make(chan struct{}), release: make(chan struct{})}
	client := &Client{stdin: writer, events: make(chan Message, 1), done: make(chan struct{})}
	delivered := make(chan error, 1)
	client.SetServerRequestHandler(func(Message) (any, error) {
		return map[string]string{"ok": "yes"}, nil
	})
	client.SetServerRequestDeliveryHandler(func(_ Message, err error) { delivered <- err })
	finished := make(chan error, 1)
	go func() {
		finished <- client.handleServerRequest(Message{ID: json.RawMessage("7"), Method: "session/request_permission"})
	}()
	<-writer.started
	select {
	case err := <-delivered:
		t.Fatalf("delivery confirmed before native writer released: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(writer.release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if err := <-delivered; err != nil {
		t.Fatal(err)
	}
}

type blockingReplyWriter struct {
	started chan struct{}
	release chan struct{}
}

func (w *blockingReplyWriter) Write(payload []byte) (int, error) {
	_ = payload
	select {
	case <-w.started:
	default:
		close(w.started)
	}
	<-w.release
	return len(payload), nil
}
func (*blockingReplyWriter) Close() error { return nil }

type contextBlockingWriter struct {
	closed chan struct{}
	once   sync.Once
}

func (w *contextBlockingWriter) Write([]byte) (int, error) {
	<-w.closed
	return 0, io.ErrClosedPipe
}

func (w *contextBlockingWriter) Close() error {
	w.once.Do(func() { close(w.closed) })
	return nil
}

func TestNotifyContextClosesClientWhenWriteTimesOut(t *testing.T) {
	writer := &contextBlockingWriter{closed: make(chan struct{})}
	client := &Client{stdin: writer, events: make(chan Message, 1), done: make(chan struct{}), stop: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := client.NotifyContext(ctx, "session/cancel", map[string]string{"sessionId": "session-1"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("notify error = %v, want deadline exceeded", err)
	}
	if time.Since(started) > time.Second {
		t.Fatal("notify context did not return after its deadline")
	}
	select {
	case <-writer.closed:
	case <-time.After(time.Second):
		t.Fatal("timed out client writer was not closed")
	}
}

func TestClientRoutesRequestBeforePendingResponseWithSameNumericID(t *testing.T) {
	writer := &recordingWriter{}
	stdout, peer := io.Pipe()
	client := &Client{stdin: writer, wait: func() error { return nil }, events: make(chan Message, 1), done: make(chan struct{}), stop: make(chan struct{})}
	requests := make(chan Message, 1)
	client.SetServerRequestHandler(func(message Message) (any, error) {
		requests <- message
		return map[string]string{"outcome": "denied"}, nil
	})
	go client.read(stdout)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		var result map[string]bool
		finished <- client.Request(ctx, "session/prompt", map[string]any{}, &result)
	}()
	deadline := time.Now().Add(time.Second)
	for !strings.Contains(writer.String(), `"method":"session/prompt"`) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if _, err := io.WriteString(peer, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"session/request_permission\",\"params\":{}}\n{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"ok\":true}}\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case request := <-requests:
		if request.Method != "session/request_permission" || string(request.ID) != "1" {
			t.Fatalf("request=%#v", request)
		}
	case <-ctx.Done():
		t.Fatal("same-ID server request was treated as a pending response")
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for !strings.Contains(writer.String(), `"id":1,"result"`) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(writer.String(), `"id":1,"result"`) {
		t.Fatalf("server response was not written: %q", writer.String())
	}
	_ = peer.Close()
}

func TestCustomHandlerCannotBeBypassedByFilesystemRequest(t *testing.T) {
	writer := &recordingWriter{}
	client := NewClient(writer)
	called := false
	client.SetServerRequestHandler(func(message Message) (any, error) {
		called = true
		if message.Method != "fs/read_text_file" {
			return nil, errors.New("unexpected method")
		}
		return nil, errors.New("filesystem capability is not available")
	})
	if err := client.HandleServerRequest(Message{ID: json.RawMessage("7"), Method: "fs/read_text_file", Params: json.RawMessage(`{"path":"/private/secret"}`)}); err == nil {
		t.Fatal("filesystem request unexpectedly succeeded")
	}
	if !called || strings.Contains(writer.String(), "secret") {
		t.Fatalf("filesystem request bypassed handler or leaked path: called=%v response=%q", called, writer.String())
	}
}

type recordingWriter struct {
	mu sync.Mutex
	strings.Builder
}

func (w *recordingWriter) Write(payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Builder.Write(payload)
}
func (w *recordingWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Builder.String()
}
func (*recordingWriter) Close() error { return nil }

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

func TestClientRoutesStringIDServerRequest(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestACPServerRequestProcess")
	client, err := StartWithLogEnv(context.Background(), nil, []string{"ACP_STRING_SERVER_ID=1"}, command.Path, command.Args[1:]...)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	requests := make(chan Message, 1)
	client.SetServerRequestHandler(func(message Message) (any, error) {
		requests <- message
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
	select {
	case message := <-requests:
		if message.Method != "session/request_permission" || string(message.ID) != `"server-request"` {
			t.Fatalf("server request=%#v", message)
		}
	case <-ctx.Done():
		t.Fatal("string-ID server request was not routed")
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
				serverRequestID := "900"
				if os.Getenv("ACP_STRING_SERVER_ID") == "1" {
					serverRequestID = `"server-request"`
				}
				if string(request.ID) == "900" || string(request.ID) == serverRequestID {
					outcome, _ := request.Result["outcome"].(map[string]any)
					approved = outcome["outcome"] == "selected" && outcome["optionId"] == "allow_always"
					if approved {
						_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 2, "result": map[string]string{"summary": "approved"}})
					}
					continue
				}
				if request.Method == "session/prompt" {
					requestID := any(900)
					if os.Getenv("ACP_STRING_SERVER_ID") == "1" {
						requestID = "server-request"
					}
					_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": requestID, "method": "session/request_permission", "params": map[string]any{"options": []map[string]string{{"optionId": "allow_always", "kind": "allow_always"}}}})
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

func TestRPCErrorPreservesData(t *testing.T) {
	rawObject := []byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"Internal error","data":{"message":"limit reached","codexErrorInfo":"usage_limit_exceeded"}}}`)
	var msgObject Message
	if err := json.Unmarshal(rawObject, &msgObject); err != nil {
		t.Fatal(err)
	}
	if msgObject.Error == nil {
		t.Fatal("expected error to be present")
	}
	if !msgObject.Error.HasData() {
		t.Fatal("expected error to have data")
	}
	var dataObj map[string]string
	if err := json.Unmarshal(msgObject.Error.Data, &dataObj); err != nil {
		t.Fatal(err)
	}
	if dataObj["codexErrorInfo"] != "usage_limit_exceeded" {
		t.Fatalf("unexpected data: %v", dataObj)
	}

	rawArray := []byte(`{"jsonrpc":"2.0","id":2,"error":{"code":-32600,"message":"Invalid Request","data":["item1","item2"]}}`)
	var msgArray Message
	if err := json.Unmarshal(rawArray, &msgArray); err != nil {
		t.Fatal(err)
	}
	if msgArray.Error == nil || !msgArray.Error.HasData() {
		t.Fatal("expected array data to be preserved")
	}
	var dataArr []string
	if err := json.Unmarshal(msgArray.Error.Data, &dataArr); err != nil {
		t.Fatal(err)
	}
	if len(dataArr) != 2 || dataArr[0] != "item1" {
		t.Fatalf("unexpected data array: %v", dataArr)
	}
}
