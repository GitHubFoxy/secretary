package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
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
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	request, _ := http.NewRequest(http.MethodPost, storeServer.URL+"/v1/workers/worker-1/steer", bytes.NewBufferString(`{"text":"change"}`))
	request.Header.Set("Content-Type", "application/json")
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
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("stop status=%d", response.StatusCode)
	}
}
