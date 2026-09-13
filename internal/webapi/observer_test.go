package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
	"github.com/coder/websocket"
)

type observerRuntime struct{ session *observerSession }
type observerSession struct {
	activity chan node.Activity
	result   chan node.Result
	queued   chan string
	steered  string
}

func (r *observerRuntime) Start(context.Context, node.StartRequest) (node.Session, error) {
	r.session = &observerSession{activity: make(chan node.Activity, 2), result: make(chan node.Result, 1), queued: make(chan string, 2)}
	return r.session, nil
}
func (s *observerSession) ID() string                                 { return "observer-session" }
func (s *observerSession) Prompt(context.Context, string) error       { return nil }
func (s *observerSession) Queue(_ context.Context, text string) error { s.queued <- text; return nil }
func (s *observerSession) Steer(_ context.Context, text string) (bool, error) {
	s.steered = text
	return true, nil
}
func (s *observerSession) Cancel(context.Context) error {
	s.result <- node.Result{Status: "canceled", Summary: "stopped"}
	return nil
}
func (s *observerSession) Activity() <-chan node.Activity { return s.activity }
func (s *observerSession) Result() <-chan node.Result     { return s.result }
func (s *observerSession) Close() error                   { return nil }

func observerTestServer(t *testing.T, local *node.LocalNode) (*httptest.Server, *http.Client) {
	t.Helper()
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "observer.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.AttachNode(local)
	server := httptest.NewServer(api.Handler())
	jar, _ := cookiejar.New(nil)
	return server, &http.Client{Jar: jar}
}

func TestPublicClientResponsesRedactNativeRuntimeSessionID(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "public-redaction.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := store.CreateTask(ctx, conversation.ID, "inspect")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := store.AcceptDispatch(ctx, task.ID, "public-worker", "local", "native-secret-session", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEntry(ctx, conversation.ID, core.EntryUser, "Plain Markdown contains xoxb_ABC"); err != nil {
		t.Fatal(err)
	}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &http.Client{Jar: mustWebCookieJar(t)}
	login(t, client, server.URL)
	for _, path := range []string{"/v1/bootstrap", "/v1/workers", "/v1/workers/public-worker/thread", "/v1/conversation"} {
		response, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		body, readErr := io.ReadAll(response.Body)
		response.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("path=%s status=%d body=%s", path, response.StatusCode, body)
		}
		if strings.Contains(string(body), "runtime_session_id") || strings.Contains(string(body), "native-secret-session") || strings.Contains(string(body), "xoxb_ABC") {
			t.Fatalf("path=%s leaked native runtime ID: %s", path, body)
		}
	}
}

func TestWorkerDiagnosticViewIncludesOutcomesAndRedactedHarnessDetails(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "diagnostics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, _, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "diagnostic-worker", Intent: "inspect", ProjectID: "project", NodeID: "local", HarnessInstanceID: "local/fx", PolicySnapshot: "safe"}, core.TurnSpec{Input: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	details, err := store.WorkerDetailsForConversation(ctx, conversation.ID, "diagnostic-worker")
	if err != nil || len(details.Attempts) != 1 {
		t.Fatalf("details=%#v err=%v", details, err)
	}
	if _, _, _, err := store.RecordAttemptOutcome(ctx, details.Attempts[0].ID, core.AttemptOutcomeInput{Status: core.OutcomeFailed, Classification: core.OutcomeFinal, ErrorCode: "harness_failed", ErrorMessage: "failed", Diagnostics: "diagnostic detail", Summary: "failed"}); err != nil {
		t.Fatal(err)
	}
	logDir := t.TempDir()
	log := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"native-runtime-session","update":{"sessionUpdate":"tool_call","title":"shell","rawInput":{"command":"printf safe","token":"top-secret-token"}}}}` + "\n" +
		`{"jsonrpc":"2.0","method":"session/update","params":{"update":{"sessionUpdate":"agent_thought_chunk","content":{"type":"text","text":"raw chain-of-thought must not escape"}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(logDir, "diagnostic-worker.jsonl"), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.diagnosticLogDir = logDir
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &http.Client{Jar: mustWebCookieJar(t)}
	login(t, client, server.URL)
	response, err := client.Get(server.URL + "/v1/workers/diagnostic-worker/diagnostics")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	text := string(body)
	for _, want := range []string{"attempt_outcomes", "raw_harness_details", "harness_failed", "shell"} {
		if !strings.Contains(text, want) {
			t.Fatalf("diagnostics missing %q: %s", want, text)
		}
	}
	for _, forbidden := range []string{"runtime_session_id", "native-runtime-session", "top-secret-token", "raw chain-of-thought"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("diagnostics leaked %q: %s", forbidden, text)
		}
	}
	conversationResponse, err := client.Get(server.URL + "/v1/workers/diagnostic-worker")
	if err != nil {
		t.Fatal(err)
	}
	conversationBody, _ := io.ReadAll(conversationResponse.Body)
	conversationResponse.Body.Close()
	if strings.Contains(string(conversationBody), "shell") || strings.Contains(string(conversationBody), "harness_details") {
		t.Fatalf("normal Worker contract contains diagnostic details: %s", conversationBody)
	}
}

func TestWorkerObserverReplaysBurstActivityFromDurableStore(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "burst-observer.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, conversation, err := store.CreatePersonWithConversation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, turn, attempt, err := store.CreateWorker(ctx, conversation.ID, core.WorkerSpec{WorkerRef: "burst-observer", Intent: "inspect", ProjectID: "project", NodeID: "local", HarnessInstanceID: "local/fx", PolicySnapshot: "safe"}, core.TurnSpec{Input: "inspect"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetPhase4AttemptActive(ctx, attempt.ID); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 128; index++ {
		activity := core.Activity{
			Metadata: core.ActivityMetadata{EventID: "burst-event-" + strconv.Itoa(index), Node: "local", HarnessInstanceID: "local/fx", WorkerRef: "burst-observer", TurnID: turn.ID, AttemptID: attempt.ID, Sequence: uint64(index + 1), ObservedAt: time.Now().UTC()},
			Kind:     core.ActivityKindAssistantTextDelta,
			Text:     "activity-" + strconv.Itoa(index),
		}
		if _, err := store.RecordNodeActivityReplay(ctx, activity); err != nil {
			t.Fatalf("activity %d: %v", index, err)
		}
	}
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	client := &http.Client{Jar: mustWebCookieJar(t)}
	login(t, client, server.URL)
	response, err := client.Get(server.URL + "/v1/workers/burst-observer/activity?after_seq=0")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var events []core.Event
	if err := json.NewDecoder(response.Body).Decode(&events); err != nil {
		t.Fatal(err)
	}
	activityCount := 0
	for _, event := range events {
		if event.Kind == "attempt.activity" {
			activityCount++
		}
	}
	if response.StatusCode != http.StatusOK || activityCount != 128 {
		t.Fatalf("status=%d events=%d activity=%d", response.StatusCode, len(events), activityCount)
	}
}

func TestWorkerObserverStatusSteerStopAndActivity(t *testing.T) {
	runtime := &observerRuntime{}
	local := node.NewLocal(runtime)
	storeServer, client := observerTestServer(t, local)
	defer storeServer.Close()
	login(t, client, storeServer.URL)
	if _, err := local.Dispatch(context.Background(), node.StartRequest{WorkerRef: "worker-1"}); err != nil {
		t.Fatal(err)
	}

	response, err := client.Get(storeServer.URL + "/v1/workers/worker-1")
	if err != nil {
		t.Fatal(err)
	}
	var status map[string]any
	if err := json.NewDecoder(response.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	if _, leaked := status["session_id"]; leaked {
		t.Fatalf("public Worker response leaked native session ID: %#v", status)
	}
	request, _ := http.NewRequest(http.MethodPost, storeServer.URL+"/v1/workers/worker-1/steer", bytes.NewBufferString(`{"text":"change"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "observer-steer")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if runtime.session.steered != "change" {
		t.Fatalf("steered=%q", runtime.session.steered)
	}

	parsed, _ := url.Parse(storeServer.URL)
	header := http.Header{}
	for _, cookie := range client.Jar.Cookies(parsed) {
		header.Add("Cookie", cookie.String())
	}
	parsed.Scheme = "ws"
	parsed.Path = "/v1/workers/worker-1/activity"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, parsed.String(), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	runtime.session.activity <- node.Activity{Kind: node.ActivityTool, Text: "shell"}
	_, payload, err := connection.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var activity node.Activity
	if err := json.Unmarshal(payload, &activity); err != nil || activity.Text != "shell" {
		t.Fatalf("activity=%#v err=%v", activity, err)
	}
	request, _ = http.NewRequest(http.MethodPost, storeServer.URL+"/v1/workers/worker-1/queue", bytes.NewBufferString(`{"text":"after idle"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "observer-queue")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("queue status=%d", response.StatusCode)
	}
	if got := <-runtime.session.queued; got != "after idle" {
		t.Fatalf("queued=%q", got)
	}

	request, _ = http.NewRequest(http.MethodPost, storeServer.URL+"/v1/workers/worker-1/stop", nil)
	request.Header.Set("Idempotency-Key", "observer-stop")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("stop status=%d", response.StatusCode)
	}
}
