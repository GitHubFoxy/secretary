package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/app"
	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/node"
	"github.com/beruseruko/secretary/internal/webapi"
	"github.com/coder/websocket"
)

func TestThinVerticalSlice(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "slice.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := webapi.New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(os.Args[0], "-test.run=TestE2EFakeACPProcess")
	local := node.NewLocal(node.ACPRuntime{Command: command.Path, Arguments: command.Args[1:]})
	api.AttachNode(local)
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	loginE2E(t, client, server.URL)

	postE2EMessage(t, client, server.URL, "in-1", "Please investigate")
	conversation := mustConversation(t, client, server.URL)
	if len(conversation) != 1 || conversation[0].Kind != core.EntryUser {
		t.Fatalf("initial conversation=%#v", conversation)
	}

	capability, err := store.RotateSecretaryCapability(ctx, api.OwnerID())
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := &app.Dispatcher{Store: store, Node: local}
	api.AttachWorkerController(&app.WorkerController{Store: store, Node: local})
	service := ctl.Service{Store: store, PersonID: api.OwnerID(), Capability: capability, Dispatcher: dispatcher}
	task, err := service.Create(ctx, "Inspect the local repository")
	if err != nil {
		t.Fatal(err)
	}
	if task.State != core.TaskOpen {
		t.Fatalf("task after dispatch=%#v", task)
	}
	if _, err := store.AppendEntry(ctx, task.ConversationID, core.EntrySecretary, "Worker ready: worker_ref="+task.ID); err != nil {
		t.Fatal(err)
	}

	conversationSocket := dialE2EWebSocket(t, client, server.URL, "/v1/ws?after_seq=2")
	defer conversationSocket.CloseNow()
	workerStatus := getE2E(t, client, server.URL+"/v1/workers/"+task.ID)
	if workerStatus["state"] != "active" {
		t.Fatalf("worker status=%#v", workerStatus)
	}
	activitySocket := dialE2EWebSocket(t, client, server.URL, "/v1/workers/"+task.ID+"/activity")
	defer activitySocket.CloseNow()
	_, activityPayload, err := activitySocket.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var activity node.Activity
	if err := json.Unmarshal(activityPayload, &activity); err != nil || activity.Text != "fake activity" {
		t.Fatalf("activity=%s err=%v", activityPayload, err)
	}

	postE2EJSON(t, client, server.URL+"/v1/workers/"+task.ID+"/steer", `{"text":"keep the scope narrow"}`)
	postE2EJSON(t, client, server.URL+"/v1/workers/"+task.ID+"/stop", ``)
	stopping := getE2E(t, client, server.URL+"/v1/workers/"+task.ID)
	if stopping["state"] != "stopping" {
		t.Fatalf("stopping status=%#v", stopping)
	}

	_, resultPayload, err := conversationSocket.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var resultEntry core.ConversationEntry
	if err := json.Unmarshal(resultPayload, &resultEntry); err != nil {
		t.Fatal(err)
	}
	if resultEntry.Kind != core.EntryWorkerResult || resultEntry.Body != "stopped by owner" {
		t.Fatalf("result entry=%#v", resultEntry)
	}
	entries := mustConversation(t, client, server.URL)
	if len(entries) != 3 || entries[2].Kind != core.EntryWorkerResult {
		t.Fatalf("final conversation=%#v", entries)
	}
	details, err := store.TaskDetails(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(details.Results) != 1 || details.Attempts[0].State != core.AttemptCanceled {
		t.Fatalf("task details=%#v", details)
	}
}

func loginE2E(t *testing.T, client *http.Client, base string) {
	t.Helper()
	response, err := client.Post(base+"/v1/web/session", "application/json", bytes.NewBufferString(`{"bootstrap_token":"bootstrap"}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("login status=%d", response.StatusCode)
	}
}

func postE2EMessage(t *testing.T, client *http.Client, base, id, body string) {
	t.Helper()
	postE2EJSONStatus(t, client, base+"/v1/messages", `{"external_message_id":"`+id+`","body":"`+body+`"}`, http.StatusAccepted)
}

func postE2EJSON(t *testing.T, client *http.Client, path, body string) {
	postE2EJSONStatus(t, client, path, body, http.StatusAccepted)
}
func postE2EJSONStatus(t *testing.T, client *http.Client, path, body string, want int) {
	t.Helper()
	request, _ := http.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != want {
		t.Fatalf("POST %s status=%d", path, response.StatusCode)
	}
}

func getE2E(t *testing.T, client *http.Client, path string) map[string]any {
	t.Helper()
	response, err := client.Get(path)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status=%d", path, response.StatusCode)
	}
	var value map[string]any
	if err := json.NewDecoder(response.Body).Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func mustConversation(t *testing.T, client *http.Client, base string) []core.ConversationEntry {
	t.Helper()
	response, err := client.Get(base + "/v1/conversation?after_seq=0")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var entries []core.ConversationEntry
	if err := json.NewDecoder(response.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	return entries
}

func dialE2EWebSocket(t *testing.T, client *http.Client, base, path string) *websocket.Conn {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Scheme = "ws"
	if index := stringsIndex(path, "?"); index >= 0 {
		parsed.Path = path[:index]
		parsed.RawQuery = path[index+1:]
	} else {
		parsed.Path = path
	}
	header := http.Header{}
	cookieURL, _ := url.Parse(base)
	for _, cookie := range client.Jar.Cookies(cookieURL) {
		header.Add("Cookie", cookie.String())
	}
	connection, response, err := websocket.Dial(context.Background(), parsed.String(), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		if response != nil {
			t.Fatalf("websocket %s status=%d err=%v", path, response.StatusCode, err)
		}
		t.Fatal(err)
	}
	return connection
}

func stringsIndex(value, separator string) int {
	for i := 0; i+len(separator) <= len(value); i++ {
		if value[i:i+len(separator)] == separator {
			return i
		}
	}
	return -1
}

func TestE2EFakeACPProcess(t *testing.T) {
	helper := false
	for _, arg := range os.Args {
		if arg == "-test.run=TestE2EFakeACPProcess" {
			helper = true
			break
		}
	}
	if !helper {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	scanner := bufio.NewScanner(os.Stdin)
	var activePrompt json.RawMessage
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
			activePrompt = request.ID
			_ = encoder.Encode(map[string]any{"method": "session/update", "params": map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "fake activity"}}})
		case "session/cancel":
			if len(activePrompt) > 0 {
				_ = encoder.Encode(map[string]any{"id": activePrompt, "result": map[string]string{"summary": "stopped by owner", "stopReason": "cancelled"}})
				activePrompt = nil
			}
		}
		if len(request.ID) == 0 {
			continue
		}
		result := map[string]any{}
		switch request.Method {
		case "session/new":
			result = map[string]any{"sessionId": "fake-session"}
		case "_session/steering":
			result = map[string]any{"outcome": "injected"}
		case "initialize":
			result = map[string]any{}
		case "session/prompt":
			continue
		}
		_ = encoder.Encode(map[string]any{"id": request.ID, "result": result})
	}
}
