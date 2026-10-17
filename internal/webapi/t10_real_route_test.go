package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/node"
)

type t10InventorySource struct{ snapshot core.HarnessInventorySnapshot }
func (s t10InventorySource) Discover(context.Context) (core.HarnessInventorySnapshot, error) { return s.snapshot, nil }

func TestT10RealWebToSecretaryActionToRemoteDaemonAndConversation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "server.db"))
	if err != nil { t.Fatal(err) }
	defer store.Close()
	api, err := New(ctx, store, "bootstrap")
	if err != nil { t.Fatal(err) }
	personID := api.OwnerID()
	capability, err := store.RotateSecretaryCapability(ctx, personID)
	if err != nil { t.Fatal(err) }
	manager, err := node.NewServerManager(ctx, store, "t10-route-pair", "t10-route-admin")
	if err != nil { t.Fatal(err) }
	manager.SetEventSink(node.NewStoreEventSink(store))
	manager.SetCommandOutcomeSink(node.NewStoreCommandOutcomeSink(store))
	api.AttachNodeService(manager)
	api.AttachSecretaryWorkerTools(ctl.WorkerService{
		Store: store, PersonID: personID, Capability: capability,
		WorkerPolicy: core.HarnessPolicy{DefaultHarness: core.HarnessFX},
		WorkerProfileSource: func() (node.ManagedProfile, error) {
			return node.ManagedProfile{Version:"t10-test", Name:"worker", Content:"Analyze original attached image.", Hash:"t10-test", Runtime:"fx", AllowTools: []string{"read"}}, nil
		},
		Runtime: ctl.NodeRuntime{Manager: manager},
	})
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	identity, err := node.EnrollNode(ctx, server.Client(), server.URL, "t10-route-pair", "t10-route-remote")
	if err != nil { t.Fatal(err) }
	instance := core.HarnessInstance{
		ID:core.HarnessInstanceID(string(identity.Node)+"/fx"), Node:identity.Node, Kind:core.HarnessFX,
		Version:"test", Status:core.HarnessReady, Authentication:core.HarnessAuthentication{Authenticated:true},
		Capabilities:core.HarnessCapabilities{Execution:[]core.ExecutionCapability{core.CapabilityCancel,core.CapabilityImageInput}},
	}
	inventory := core.HarnessInventorySnapshot{Node:identity.Node, ObservedAt:time.Now().UTC(), Instances:[]core.HarnessInstance{instance}}
	image := t10WebPNG(t)
	runtime := &t10ImageRuntime{original:image, seen:make(chan node.StartRequest, 2)}
	localStore, err := node.OpenLocalStore(filepath.Join(t.TempDir(), "node.json"))
	if err != nil { t.Fatal(err) }
	defer localStore.Close()
	daemon := &node.Daemon{
		Identity:identity, Store:localStore, Runtime:runtime, Inventory:t10InventorySource{snapshot:inventory},
		Capacity:2, HeartbeatInterval:20*time.Millisecond, InventoryInterval:time.Hour,
		OutboxPollInterval:10*time.Millisecond, ReconnectMin:10*time.Millisecond, ReconnectMax:40*time.Millisecond,
	}
	done := make(chan error, 1)
	go func() { done <- daemon.Run(ctx) }()
	defer func() {
		cancel()
		select { case err := <-done: if err != nil { t.Errorf("daemon: %v",err) }
		case <-time.After(time.Second): t.Error("daemon did not stop") }
	}()
	for {
		status, err := manager.Status(ctx, identity.Node)
		if err == nil && status.Online && !status.LastHeartbeatAt.IsZero() { break }
		select { case <-ctx.Done(): t.Fatalf("remote Node never became ready: %v",ctx.Err())
		case <-time.After(10*time.Millisecond): }
	}
	jar,err := cookiejar.New(nil)
	if err != nil { t.Fatal(err) }
	client := &http.Client{Jar:jar}
	login(t,client,server.URL)
	post := func(path,key string,body []byte, contentType string) (*http.Response, []byte) {
		t.Helper()
		req,err := http.NewRequestWithContext(ctx,http.MethodPost,server.URL+path,bytes.NewReader(body))
		if err != nil { t.Fatal(err) }
		req.Header.Set("Content-Type",contentType)
		req.Header.Set("Idempotency-Key",key)
		res,err := client.Do(req)
		if err != nil { t.Fatal(err) }
		defer res.Body.Close()
		var output bytes.Buffer
		if _,err := output.ReadFrom(res.Body); err != nil { t.Fatal(err) }
		return res,output.Bytes()
	}
	res,raw := post("/v1/artifacts?name=photo.png","t10-route-original",image,"application/octet-stream")
	if res.StatusCode != http.StatusCreated { t.Fatalf("Web image upload status=%d body=%s",res.StatusCode,raw) }
	var stored struct { Reference string `json:"reference"` }
	if err := json.Unmarshal(raw,&stored); err != nil { t.Fatal(err) }
	caption := "What is in the picture? "+stored.Reference
	message,_ := json.Marshal(map[string]string{"external_message_id":"t10-inbound","body":caption})
	res,raw = post("/v1/messages","t10-web-inbound",message,"application/json")
	if res.StatusCode != http.StatusAccepted { t.Fatalf("Web input status=%d body=%s",res.StatusCode,raw) }
	var ack struct { Entry core.ConversationEntry `json:"entry"` }
	if err := json.Unmarshal(raw,&ack); err != nil { t.Fatal(err) }
	if ack.Entry.Body != caption { t.Fatalf("caption lost: %#v",ack.Entry) }
	conversation,err := store.ConversationForPerson(ctx,personID)
	if err != nil { t.Fatal(err) }
	secretaryID,err := store.EnsureSecretaryIdentity(ctx,personID,conversation.ID)
	if err != nil { t.Fatal(err) }
	if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{Version:"t10", Harness:"fx", Model:"fixture", Reasoning:"high", ProfileVersion:"t10", ProfileName:"secretary", ProfileHash:"t10", ProfileContent:"test"}); err != nil { t.Fatal(err) }
	turn,dup,err := store.EnqueueSecretaryTurnForEntry(ctx,secretaryID.ID,caption,ack.Entry,nil)
	if err != nil || dup { t.Fatalf("enqueue canonical Secretary turn: %v duplicate=%v",err,dup) }
	if _,err := store.StartSecretaryTurn(ctx,turn.ID); err != nil { t.Fatal(err) }
	action := map[string]any{"name":"spawn_worker","arguments":map[string]any{
		"intent":"What is in the picture?", "idempotency_key":"t10-bound-action",
		"preferences":map[string]any{"node_id":identity.Node,"harness_kind":"fx"},
	}}
	requestBody,_ := json.Marshal(action)
	tool := func() secretaryToolCallResponse {
		t.Helper()
		req,err := http.NewRequestWithContext(ctx,http.MethodPost,server.URL+"/v1/internal/secretary/tools/call",bytes.NewReader(requestBody))
		if err != nil { t.Fatal(err) }
		req.Header.Set("Authorization","Bearer "+capability)
		req.Header.Set("Content-Type","application/json")
		res,err := client.Do(req)
		if err != nil { t.Fatal(err) }
		defer res.Body.Close()
		var value secretaryToolCallResponse
		if err := json.NewDecoder(res.Body).Decode(&value); err != nil { t.Fatal(err) }
		if res.StatusCode != http.StatusOK || value.Error != "" { t.Fatalf("Secretary tool action: HTTP %d: %#v",res.StatusCode,value) }
		return value
	}
	first := tool()
	select {
	case received := <-runtime.seen:
		if len(received.Images) != 1 || !bytes.Equal(received.Images[0].Data,image) || !strings.Contains(received.Task,stored.Reference) {
			t.Fatalf("Secretary dropped bound image: %#v",received)
		}
	case <-ctx.Done(): t.Fatal("ServerManager did not deliver image to remote native Worker")
	}
	for {
		req,err := http.NewRequestWithContext(ctx,http.MethodGet,server.URL+"/v1/conversation",nil)
		if err != nil { t.Fatal(err) }
		res,err := client.Do(req)
		if err != nil { t.Fatal(err) }
		var body bytes.Buffer
		_,_ = body.ReadFrom(res.Body)
		res.Body.Close()
		if res.StatusCode == http.StatusOK && strings.Contains(body.String(),"Grounded result: picture contains a colored pixel") { break }
		select { case <-ctx.Done(): t.Fatalf("Worker Result did not reach Personal Conversation: %v last=%s",ctx.Err(),body.String())
		case <-time.After(20*time.Millisecond): }
	}
	// Replay only after the remote terminal event has been committed to the Personal Conversation.
	second := tool()
	firstAction := first.Value.(map[string]any)
	secondAction := second.Value.(map[string]any)
	firstWorker := firstAction["worker"].(map[string]any)["worker_ref"]
	secondWorker := secondAction["worker"].(map[string]any)["worker_ref"]
	if firstWorker == "" || firstWorker != secondWorker || runtime.starts != 1 {
		t.Fatalf("Secretary action replay changed Worker identity: %d, first=%v second=%v",runtime.starts,firstWorker,secondWorker)
	}
}
