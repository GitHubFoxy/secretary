package node

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/acp"
)

func TestACPClientCapabilitiesAdvertiseFormOnly(t *testing.T) {
	capabilities := acpClientCapabilities()
	elicitation, ok := capabilities["elicitation"].(map[string]any)
	if !ok {
		t.Fatalf("elicitation capabilities=%#v", capabilities["elicitation"])
	}
	if _, ok := elicitation["form"]; !ok {
		t.Fatalf("form capability missing: %#v", elicitation)
	}
	if _, ok := elicitation["url"]; ok {
		t.Fatalf("URL capability must not be advertised: %#v", elicitation)
	}
	if _, ok := capabilities["fs"]; ok {
		t.Fatalf("filesystem capability must not be advertised: %#v", capabilities)
	}
}

func TestRedactACPLogLineHidesUserInput(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":43,"result":{"action":"accept","content":{"answer":"super-secret"},"input":"also-secret"}}` + "\n")
	redacted := redactACPLogLine(raw)
	if strings.Contains(string(redacted), "super-secret") || strings.Contains(string(redacted), "also-secret") {
		t.Fatalf("ACP log leaked input: %s", redacted)
	}
	var message struct {
		Result map[string]any `json:"result"`
	}
	if err := json.Unmarshal(redacted, &message); err != nil {
		t.Fatal(err)
	}
	if message.Result["redacted"] != true || strings.Contains(string(redacted), `"id"`) {
		t.Fatalf("redacted ACP log=%#v raw=%s", message, redacted)
	}
	update := []byte(`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"text":"raw thought"}}}` + "\n")
	if got := redactACPLogLine(update); strings.Contains(string(got), "raw thought") {
		t.Fatalf("ACP update log leaked runtime content: %s", got)
	}
	unknown := redactACPLogLine([]byte(`{"secret":"credential-value"}`))
	if strings.Contains(string(unknown), "credential-value") || !strings.Contains(string(unknown), `"redacted":true`) {
		t.Fatalf("unknown ACP log field was not redacted: %s", unknown)
	}
	trace := redactACPLogFrame("tx", []byte(`{"jsonrpc":"2.0","method":"initialized"}`))
	if !strings.Contains(string(trace), `"direction":"tx"`) || !strings.Contains(string(trace), `"kind":"notification"`) {
		t.Fatalf("trace lacks safe envelope metadata: %s", trace)
	}
	nonJSON := redactACPLogFrame("rx", []byte("not json"))
	if !strings.Contains(string(nonJSON), `"direction":"rx"`) || strings.Contains(string(nonJSON), "not json") {
		t.Fatalf("non-JSON trace leaked payload or direction: %s", nonJSON)
	}
}

func TestCanonicalElicitationRequestIDPreservesWireNumber(t *testing.T) {
	got, present, valid := canonicalElicitationRequestID(json.RawMessage(`9007199254740993`))
	if !present || !valid || got != "elicitation:number:9007199254740993" {
		t.Fatalf("canonical request ID=%q present=%v valid=%v", got, present, valid)
	}
	got, present, valid = canonicalElicitationRequestID(json.RawMessage(`"9007199254740993"`))
	if !present || !valid || got != "elicitation:string:9007199254740993" {
		t.Fatalf("canonical string request ID=%q present=%v valid=%v", got, present, valid)
	}
	if _, present, valid = canonicalElicitationRequestID(json.RawMessage(`1.5`)); !present || valid {
		t.Fatal("fractional request ID was accepted")
	}
}

func TestACPRuntimeRejectsUnsupportedProtocolVersion(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestFakeACPProcess")
	runtime := ACPRuntime{Command: command.Path, Arguments: command.Args[1:], Environment: []string{"ACP_PROTOCOL_VERSION=2"}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := runtime.Start(ctx, StartRequest{WorkerRef: "unsupported-version", Task: "inspect", Workspace: t.TempDir()}); err == nil {
		t.Fatal("ACP runtime accepted unsupported protocol version")
	}
}

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

func TestACPSessionClosingCancelsPendingInteraction(t *testing.T) {
	client := acp.NewClient(&discardACPWriter{})
	session := newACPSession("session", client, false)
	finished := make(chan error, 1)
	go func() {
		_, err := session.handleServerRequest(acp.Message{
			ID:     json.RawMessage("9"),
			Method: "session/request_permission",
			Params: json.RawMessage(`{"options":[{"optionId":"deny","kind":"deny"}]}`),
		})
		finished <- err
	}()
	select {
	case activity := <-session.Activity():
		if activity.Kind != ActivityPermission {
			t.Fatalf("activity=%#v", activity)
		}
	case <-time.After(time.Second):
		t.Fatal("permission activity was not emitted")
	}
	session.closeActivity()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("pending interaction completed without a cancellation error")
		}
	case <-time.After(time.Second):
		t.Fatal("pending interaction remained blocked after session close")
	}
	session.RebindPendingRequests([]PendingRequest{{RequestID: "rebound", Kind: ActivityPermission}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.Respond(ctx, "rebound", "denied"); err == nil {
		t.Fatal("rebound Respond succeeded after session close")
	}
}

type discardACPWriter struct{}

func (*discardACPWriter) Write(payload []byte) (int, error) { return len(payload), nil }
func (*discardACPWriter) Close() error                      { return nil }

func TestACPRuntimeDefersInitialPrompt(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestFakeACPProcess")
	runtime := ACPRuntime{Command: command.Path, Arguments: command.Args[1:]}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "secretary", Task: "ignored", DeferInitialPrompt: true})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	select {
	case result := <-session.Result():
		t.Fatalf("deferred session produced result=%#v", result)
	case activity := <-session.Activity():
		t.Fatalf("deferred session produced activity=%#v", activity)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestACPRuntimeNormalizesRichActivityWithoutRawThought(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestFakeACPProcess")
	runtime := ACPRuntime{Command: command.Path, Arguments: command.Args[1:], Environment: []string{"ACP_RICH_ACTIVITY=1"}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "worker", Task: "inspect", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	seen := map[ActivityKind]Activity{}
	for len(seen) < 3 {
		select {
		case activity := <-session.Activity():
			seen[activity.Kind] = activity
		case <-ctx.Done():
			t.Fatalf("rich ACP activity=%#v", seen)
		}
	}
	if _, ok := seen[ActivityThinkingSummary]; !ok {
		t.Fatalf("thinking summary missing: %#v", seen)
	}
	if activity := seen[ActivityThinkingSummary]; activity.Summary == "raw internal thought" || strings.Contains(activity.Summary, "internal thought") {
		t.Fatalf("raw thought was exposed: %#v", activity)
	}
	if activity := seen[ActivityToolCall]; activity.Tool != "list_workers" || string(activity.Arguments) != `{"scope":"current"}` {
		t.Fatalf("tool call=%#v", activity)
	}
	if activity := seen[ActivityToolResult]; activity.Tool != "list_workers" || activity.Result == "" {
		t.Fatalf("tool result=%#v", activity)
	}
}

func TestACPRuntimeDeliversEveryActivityInBurst(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=TestFakeACPProcess")
	runtime := ACPRuntime{Command: command.Path, Arguments: command.Args[1:], Environment: []string{"ACP_BURST=128"}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := runtime.Start(ctx, StartRequest{WorkerRef: "burst-worker", Task: "inspect", Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	select {
	case result := <-session.Result():
		if result.Status != "succeeded" {
			t.Fatalf("burst result=%#v", result)
		}
	case <-ctx.Done():
		t.Fatal("burst prompt did not complete")
	}
	for index := 0; index < 128; index++ {
		select {
		case activity := <-session.Activity():
			want := "burst-" + strconv.Itoa(index)
			if activity.Text != want {
				t.Fatalf("activity[%d]=%#v, want text %q", index, activity, want)
			}
		case <-ctx.Done():
			t.Fatalf("activity burst truncated at %d: %v", index, ctx.Err())
		}
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

func TestACPRuntimeResumeRespondsBeforeLateReplayedRequests(t *testing.T) {
	for _, kind := range []string{"permission", "input"} {
		t.Run(kind, func(t *testing.T) {
			responsesPath := filepath.Join(t.TempDir(), "responses.jsonl")
			command := exec.Command(os.Args[0], "-test.run=TestFakeACPReconnectProcess")
			runtime := ACPRuntime{Command: command.Path, Arguments: command.Args[1:], Environment: []string{"ACP_RECONNECT_KIND=" + kind, "ACP_RECONNECT_LATE=1", "ACP_RESPONSES_FILE=" + responsesPath}}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			session, err := runtime.Resume(ctx, StartRequest{WorkerRef: "worker", Workspace: t.TempDir(), PendingRequestIDs: []string{"durable-" + kind}}, "saved-session")
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			responded := make(chan error, 1)
			go func() {
				responded <- session.(Responder).Respond(ctx, "durable-"+kind, map[string]string{"permission": "denied", "input": "answer"}[kind])
			}()
			select {
			case err := <-responded:
				if err != nil {
					t.Fatalf("late %s response failed: %v", kind, err)
				}
			case <-ctx.Done():
				t.Fatalf("late %s response was not delivered: %v", kind, ctx.Err())
			}
			deadline := time.Now().Add(2 * time.Second)
			var data []byte
			for {
				var readErr error
				data, readErr = os.ReadFile(responsesPath)
				if readErr == nil && strings.Count(string(data), "\n") == 1 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("responses=%q", data)
				}
				time.Sleep(10 * time.Millisecond)
			}
			var nativeReply struct {
				Error  json.RawMessage `json:"error"`
				Result struct {
					Input   string `json:"input"`
					Outcome struct {
						OptionID string `json:"optionId"`
					} `json:"outcome"`
				} `json:"result"`
			}
			if err := json.Unmarshal(data, &nativeReply); err != nil {
				t.Fatal(err)
			}
			if len(nativeReply.Error) != 0 {
				t.Fatalf("native reply error=%s", nativeReply.Error)
			}
			if kind == "permission" && nativeReply.Result.Outcome.OptionID != "deny" {
				t.Fatalf("native permission reply=%s", data)
			}
			if kind == "input" && nativeReply.Result.Input != "answer" {
				t.Fatalf("native input reply=%s", data)
			}
		})
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

func TestACPSessionRebindsConcurrentRequestsByKindInsteadOfFIFO(t *testing.T) {
	writer := &recordingNativeReplyWriter{}
	client := acp.NewClient(writer)
	session := newACPSession("reordered-session", client, false)
	session.setRequestHandler()
	// Deliberately place input before permission. Native replay arrives in the
	// opposite order, so a sorted/FIFO durable ID list cross-binds the requests.
	session.RebindPendingRequests([]PendingRequest{{RequestID: "durable-input", Kind: ActivityUserInput}, {RequestID: "durable-permission", Kind: ActivityPermission}})
	permission := acp.Message{ID: json.RawMessage("101"), Method: "session/request_permission", Params: json.RawMessage(`{"options":[{"optionId":"deny","kind":"reject_once"}]}`)}
	input := acp.Message{ID: json.RawMessage("102"), Method: "session/request_input", Params: json.RawMessage(`{"prompt":"version?"}`)}
	finished := make(chan error, 2)
	go func() { finished <- client.HandleServerRequest(permission) }()
	go func() { finished <- client.HandleServerRequest(input) }()

	seen := make(map[ActivityKind]string)
	for len(seen) < 2 {
		select {
		case activity := <-session.Activity():
			seen[activity.Kind] = activity.RequestID
		case <-time.After(time.Second):
			t.Fatalf("requests were not observed: %#v", seen)
		}
	}
	if seen[ActivityPermission] != "durable-permission" || seen[ActivityUserInput] != "durable-input" {
		t.Fatalf("rebound requests crossed: %#v", seen)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.Respond(ctx, "durable-permission", "denied"); err != nil {
		t.Fatal(err)
	}
	if err := session.Respond(ctx, "durable-input", "answer"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
	}
}

func TestACPSessionRebindsSameKindByDurableRequestID(t *testing.T) {
	writer := &recordingNativeReplyWriter{}
	client := acp.NewClient(writer)
	session := newACPSession("same-kind-session", client, false)
	session.setRequestHandler()
	session.RebindPendingRequests([]PendingRequest{{RequestID: "input-a", Kind: ActivityUserInput}, {RequestID: "input-b", Kind: ActivityUserInput}})
	first := acp.Message{ID: json.RawMessage("111"), Method: "session/request_input", Params: json.RawMessage(`{"request_id":"input-b","prompt":"second"}`)}
	second := acp.Message{ID: json.RawMessage("112"), Method: "session/request_input", Params: json.RawMessage(`{"request_id":"input-a","prompt":"first"}`)}
	finished := make(chan error, 2)
	go func() { finished <- client.HandleServerRequest(first) }()
	go func() { finished <- client.HandleServerRequest(second) }()
	seen := make(map[string]struct{})
	for len(seen) < 2 {
		select {
		case activity := <-session.Activity():
			seen[activity.RequestID] = struct{}{}
		case <-time.After(time.Second):
			t.Fatalf("same-kind requests were not observed: %#v", seen)
		}
	}
	if _, ok := seen["input-a"]; !ok {
		t.Fatalf("input-a was not rebound: %#v", seen)
	}
	if _, ok := seen["input-b"]; !ok {
		t.Fatalf("input-b was not rebound: %#v", seen)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.Respond(ctx, "input-a", "one"); err != nil {
		t.Fatal(err)
	}
	if err := session.Respond(ctx, "input-b", "two"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := <-finished; err != nil {
			t.Fatal(err)
		}
	}
}

func TestACPSessionTrustedLocalSelectsOneShotOption(t *testing.T) {
	writer := &recordingNativeReplyWriter{}
	client := acp.NewClient(writer)
	session := newACPSession("trusted-local-session", client, false)
	session.setRequestHandler()
	request := acp.Message{
		ID:     json.RawMessage("88"),
		Method: "session/request_permission",
		Params: json.RawMessage(`{"options":[{"optionId":"allow_once","kind":"allow_once"},{"optionId":"allow_always","kind":"allow_always"}]}`),
	}
	finished := make(chan error, 1)
	go func() { finished <- client.HandleServerRequest(request) }()
	var requestID string
	select {
	case activity := <-session.Activity():
		requestID = activity.RequestID
	case <-time.After(time.Second):
		t.Fatal("permission request was not observed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.Respond(ctx, requestID, "approved:trusted-local"); err != nil {
		t.Fatalf("trusted-local response failed: %v", err)
	}
	if err := <-finished; err != nil {
		t.Fatalf("ACP handler failed: %v", err)
	}
	var reply acp.Message
	if err := json.Unmarshal(writer.last(), &reply); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Outcome struct {
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal(reply.Result, &result); err != nil {
		t.Fatal(err)
	}
	if got := result.Outcome.OptionID; got != "allow_once" {
		t.Fatalf("trusted-local option=%q, want allow_once", got)
	}
}

func TestACPSessionHandlerErrorIsNotSuccessfulWithOnlyAllowOnce(t *testing.T) {
	writer := &recordingNativeReplyWriter{}
	client := acp.NewClient(writer)
	session := newACPSession("handler-error-session", client, false)
	session.setRequestHandler()
	request := acp.Message{
		ID:     json.RawMessage("89"),
		Method: "session/request_permission",
		Params: json.RawMessage(`{"options":[{"optionId":"allow_once","kind":"allow_once"}]}`),
	}
	finished := make(chan error, 1)
	go func() { finished <- client.HandleServerRequest(request) }()
	var requestID string
	select {
	case activity := <-session.Activity():
		requestID = activity.RequestID
	case <-time.After(time.Second):
		t.Fatal("permission request was not observed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.Respond(ctx, requestID, "denied"); err == nil {
		t.Fatal("handler error was reported as successful native delivery")
	}
	if err := <-finished; err == nil {
		t.Fatal("ACP handler error was lost")
	}
	var reply acp.Message
	if err := json.Unmarshal(writer.last(), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Error == nil || len(reply.Result) != 0 {
		t.Fatalf("handler error reply=%#v", reply)
	}
}

func TestACPSessionHandlesFormElicitation(t *testing.T) {
	writer := &recordingNativeReplyWriter{}
	client := acp.NewClient(writer)
	session := newACPSession("elicitation-session", client, false)
	session.setRequestHandler()
	request := acp.Message{
		ID:     json.RawMessage("43"),
		Method: "elicitation/create",
		Params: json.RawMessage(`{"requestId":42,"mode":"form","message":"Which command should I run?","requestedSchema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}}`),
	}
	finished := make(chan error, 1)
	go func() { finished <- client.HandleServerRequest(request) }()
	var activity Activity
	select {
	case activity = <-session.Activity():
	case err := <-finished:
		t.Fatalf("elicitation handler returned before request observation: %v", err)
	case <-time.After(time.Second):
		t.Fatal("elicitation request was not observed")
	}
	if activity.Kind != ActivityUserInput || activity.RequestID != "elicitation:number:42" || activity.Summary != "Which command should I run?" || !strings.Contains(string(activity.RequestSchema), `"answer"`) {
		t.Fatalf("elicitation activity=%#v", activity)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.Respond(ctx, activity.RequestID, "git status"); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	var reply acp.Message
	if err := json.Unmarshal(writer.last(), &reply); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Action  string            `json:"action"`
		Content map[string]string `json:"content"`
	}
	if err := json.Unmarshal(reply.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.Action != "accept" || result.Content["answer"] != "git status" {
		t.Fatalf("elicitation response=%#v", result)
	}
}

func TestElicitationResponseValidatesSchemaConstraints(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"answer":{"type":"string","enum":["yes","no"],"minLength":2},"count":{"type":"integer","minimum":1,"maximum":3}},"required":["answer","count"]}`)
	if _, err := elicitationResponse(schema, `{"answer":"yes","count":2}`); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{`{"answer":"maybe","count":2}`, `{"answer":"yes","count":4}`} {
		if _, err := elicitationResponse(schema, invalid); err == nil {
			t.Fatalf("invalid elicitation response accepted: %s", invalid)
		}
	}
}

func TestACPSessionRejectsSecretFormElicitation(t *testing.T) {
	writer := &recordingNativeReplyWriter{}
	client := acp.NewClient(writer)
	session := newACPSession("elicitation-secret-session", client, false)
	session.setRequestHandler()
	request := acp.Message{ID: json.RawMessage("46"), Method: "elicitation/create", Params: json.RawMessage(`{"mode":"form","message":"Enter your password","requestedSchema":{"type":"object","properties":{"password":{"type":"string"}},"required":["password"]}}`)}
	if err := client.HandleServerRequest(request); err == nil {
		t.Fatal("secret form elicitation was accepted")
	}
	select {
	case activity := <-session.Activity():
		t.Fatalf("secret elicitation emitted activity=%#v", activity)
	default:
	}
}

func TestACPSessionRebindsFormElicitationByRequestID(t *testing.T) {
	writer := &recordingNativeReplyWriter{}
	client := acp.NewClient(writer)
	session := newACPSession("elicitation-rebind-session", client, false)
	session.setRequestHandler()
	session.RebindPendingRequests([]PendingRequest{{RequestID: "elicitation:number:42", Kind: ActivityUserInput}})
	request := acp.Message{ID: json.RawMessage("45"), Method: "elicitation/create", Params: json.RawMessage(`{"requestId":42,"mode":"form","message":"question","requestedSchema":{"type":"object","properties":{"answer":{"type":"string"}},"required":["answer"]}}`)}
	finished := make(chan error, 1)
	go func() { finished <- client.HandleServerRequest(request) }()
	var activity Activity
	select {
	case activity = <-session.Activity():
	case <-time.After(time.Second):
		t.Fatal("rebound elicitation was not observed")
	}
	if activity.RequestID != "elicitation:number:42" {
		t.Fatalf("rebound request ID=%q", activity.RequestID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.Respond(ctx, activity.RequestID, "answer"); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestACPSessionRejectsUnsupportedElicitationMode(t *testing.T) {
	writer := &recordingNativeReplyWriter{}
	client := acp.NewClient(writer)
	session := newACPSession("elicitation-url-session", client, false)
	session.setRequestHandler()
	request := acp.Message{ID: json.RawMessage("44"), Method: "elicitation/create", Params: json.RawMessage(`{"mode":"url","message":"authorize","elicitationId":"id","url":"https://example.invalid"}`)}
	if err := client.HandleServerRequest(request); err == nil {
		t.Fatal("URL elicitation was accepted without URL capability")
	}
	select {
	case activity := <-session.Activity():
		t.Fatalf("unsupported elicitation emitted activity=%#v", activity)
	default:
	}
	var reply acp.Message
	if err := json.Unmarshal(writer.last(), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Error == nil || reply.Error.Code != -32602 {
		t.Fatalf("unsupported elicitation error=%#v", reply.Error)
	}
}

type recordingNativeReplyWriter struct {
	mu      sync.Mutex
	payload []byte
}

func (w *recordingNativeReplyWriter) Write(payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.payload = append([]byte(nil), payload...)
	return len(payload), nil
}

func (*recordingNativeReplyWriter) Close() error { return nil }

func (w *recordingNativeReplyWriter) last() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.payload...)
}

func TestACPSessionRespondRetriesFailedNativeReplyOnSameSession(t *testing.T) {
	writer := &failOnceNativeReplyWriter{}
	client := acp.NewClient(writer)
	session := newACPSession("saved-session", client, false)
	session.setRequestHandler()
	request := acp.Message{
		ID:     json.RawMessage("77"),
		Method: "session/request_permission",
		Params: json.RawMessage(`{"options":[{"optionId":"allow_once","kind":"allow_once"}]}`),
	}
	finished := make(chan error, 1)
	go func() { finished <- client.HandleServerRequest(request) }()
	var requestID string
	select {
	case activity := <-session.Activity():
		requestID = activity.RequestID
	case <-time.After(time.Second):
		t.Fatal("permission request was not observed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := session.Respond(ctx, requestID, "approved"); err == nil {
		t.Fatal("first Respond unexpectedly succeeded")
	}
	if err := session.Respond(ctx, requestID, "approved"); err != nil {
		t.Fatalf("second Respond did not retry native delivery: %v", err)
	}
	if err := <-finished; err == nil || err.Error() != "injected native reply write failure" {
		t.Fatalf("first server request delivery error=%v", err)
	}
	if attempts, successful := writer.counts(); attempts != 2 || successful != 1 {
		t.Fatalf("native reply writes=%d successful=%d, want two attempts and one success", attempts, successful)
	}
}

type failOnceNativeReplyWriter struct {
	mu         sync.Mutex
	attempts   int
	successful int
}

func (w *failOnceNativeReplyWriter) Write(payload []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.attempts++
	if w.attempts == 1 {
		return 0, errors.New("injected native reply write failure")
	}
	w.successful++
	return len(payload), nil
}

func (*failOnceNativeReplyWriter) Close() error { return nil }

func (w *failOnceNativeReplyWriter) counts() (int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.attempts, w.successful
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
				_ = encoder.Encode(map[string]any{"id": json.RawMessage(request.ID), "result": map[string]any{"protocolVersion": 1}})
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
			_ = encoder.Encode(map[string]any{"id": request.ID, "result": map[string]any{"protocolVersion": 1}})
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
			_ = encoder.Encode(map[string]any{"id": json.RawMessage(request.ID), "result": map[string]any{"protocolVersion": 1}})
			continue
		}
		if request.Method == "session/load" {
			method := "session/request_permission"
			params := map[string]any{"options": []map[string]string{{"optionId": "deny", "kind": "reject_once"}}}
			if os.Getenv("ACP_RECONNECT_KIND") == "input" {
				method = "session/request_input"
				params = map[string]any{"question": "which file?"}
			}
			if os.Getenv("ACP_RECONNECT_LATE") == "1" {
				_ = encoder.Encode(map[string]any{"id": json.RawMessage(request.ID), "result": map[string]any{}})
				time.Sleep(50 * time.Millisecond)
			}
			_ = encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": 77, "method": method, "params": params})
			if os.Getenv("ACP_RECONNECT_LATE") != "1" {
				_ = encoder.Encode(map[string]any{"id": json.RawMessage(request.ID), "result": map[string]any{}})
			}
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
			if os.Getenv("ACP_BURST") != "" {
				count, _ := strconv.Atoi(os.Getenv("ACP_BURST"))
				for index := 0; index < count; index++ {
					_ = encoder.Encode(map[string]any{"method": "session/update", "params": map[string]any{"update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "burst-" + strconv.Itoa(index)}}}})
				}
			} else if os.Getenv("ACP_RICH_ACTIVITY") == "1" {
				_ = encoder.Encode(map[string]any{"method": "session/update", "params": map[string]any{"update": map[string]any{"sessionUpdate": "agent_thought_chunk", "content": map[string]string{"type": "text", "text": "raw internal thought"}}}})
				_ = encoder.Encode(map[string]any{"method": "session/update", "params": map[string]any{"update": map[string]any{"sessionUpdate": "tool_call", "title": "list_workers", "rawInput": map[string]string{"scope": "current"}}}})
				_ = encoder.Encode(map[string]any{"method": "session/update", "params": map[string]any{"update": map[string]any{"sessionUpdate": "tool_call_update", "title": "list_workers", "status": "completed", "rawOutput": map[string]string{"status": "ok"}}}})
			} else {
				_ = encoder.Encode(map[string]any{"method": "session/update", "params": map[string]any{"sessionId": "fake-session", "update": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "fake activity"}}}})
			}
		}
		if len(request.ID) == 0 {
			continue
		}
		result := map[string]any{}
		switch request.Method {
		case "initialize":
			version := 1
			if os.Getenv("ACP_PROTOCOL_VERSION") == "2" {
				version = 2
			}
			result = map[string]any{"protocolVersion": version}
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
