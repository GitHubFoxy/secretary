package webapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

	"github.com/coder/websocket"

	"github.com/beruseruko/secretary/internal/core"
)

func TestClientPairingRejectsMissingAndMalformedBootstrapAuth(t *testing.T) {
	server, _ := testServer(t)
	for _, payload := range []string{
		`{"device_id":"missing-token","display_name":"Missing","platform":"test"}`,
		`{"bootstrap_token":"wrong","device_id":"wrong-token","display_name":"Wrong","platform":"test"}`,
	} {
		response := postJSON(t, server.Client(), server.URL+"/v1/clients/pair", payload)
		if response.status != http.StatusUnauthorized {
			t.Fatalf("malformed pairing auth status=%d body=%#v", response.status, response.body)
		}
	}
}

func TestClientPairingScopesCredentialIsolationAndRevoke(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "clients.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()

	pair := postJSON(t, server.Client(), server.URL+"/v1/clients/pair", `{"bootstrap_token":"bootstrap","device_id":"pi-1","display_name":"Pi","platform":"pi","scopes":["conversation:read"]}`)
	if pair.status != http.StatusCreated || pair.body["status"] != "pending" || pair.body["client_id"] == nil {
		t.Fatalf("pair=%d %#v", pair.status, pair.body)
	}

	ownerJar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: ownerJar}
	login(t, owner, server.URL)
	clientID := pair.body["client_id"].(string)
	approve := postJSON(t, owner, server.URL+"/v1/clients/"+clientID+"/approve", `{}`)
	if approve.status != http.StatusOK || approve.body["credential"] == "" || approve.body["status"] != "active" {
		t.Fatalf("approve=%d %#v", approve.status, approve.body)
	}
	credential := approve.body["credential"].(string)

	client := &http.Client{}
	request, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/conversation", nil)
	request.Header.Set("Authorization", "Bearer "+credential)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("client conversation status=%d", response.StatusCode)
	}
	response.Body.Close()

	request, _ = http.NewRequest(http.MethodGet, server.URL+"/v1/conversation", nil)
	request.Header.Set("Authorization", "Bearer bootstrap")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bootstrap accepted as Client status=%d", response.StatusCode)
	}
	response.Body.Close()

	revoke := postJSON(t, owner, server.URL+"/v1/clients/"+clientID+"/revoke", `{}`)
	if revoke.status != http.StatusOK || revoke.body["status"] != "revoked" {
		t.Fatalf("revoke=%d %#v", revoke.status, revoke.body)
	}
	request, _ = http.NewRequest(http.MethodGet, server.URL+"/v1/conversation", nil)
	request.Header.Set("Authorization", "Bearer "+credential)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("revoked Client status=%d", response.StatusCode)
	}
	response.Body.Close()
}

func TestRegisterStreamRejectsRevokedAndStaleGeneration(t *testing.T) {
	ctx := context.Background()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "register-stream.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := store.PairClientWithToken(ctx, api.OwnerID(), "stream-generation", "Stream", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	client, credential, err := store.ApproveClient(ctx, pairing.ID)
	if err != nil {
		t.Fatal(err)
	}
	stream, accepted := api.registerStream(ctx, client.ID, credential, func() {})
	if !accepted || stream == nil {
		t.Fatal("setup registration unexpectedly rejected")
	}
	if _, err := store.RevokeClient(ctx, client.ID); err != nil {
		t.Fatal(err)
	}
	api.unregisterStream(client.ID, stream)
	if stale, accepted := api.registerStream(ctx, client.ID, credential, func() {}); accepted || stale != nil {
		t.Fatal("revoked stream registration accepted")
	}
	newPairing, err := store.PairClientWithToken(ctx, api.OwnerID(), "stream-generation", "Stream 2", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApproveClient(ctx, newPairing.ID); err != nil {
		t.Fatal(err)
	}
	if stream, accepted := api.registerStream(ctx, client.ID, credential, func() {}); accepted || stream != nil {
		t.Fatal("stale generation stream registration accepted")
	}
}

func TestRevokeClientTerminatesAlreadyConnectedConversationStream(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "revoke-stream.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	ownerJar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: ownerJar}
	login(t, owner, server.URL)
	pair := postJSON(t, server.Client(), server.URL+"/v1/clients/pair", `{"bootstrap_token":"bootstrap","device_id":"revoke-device","display_name":"Revoke","platform":"test"}`)
	clientID := pair.body["client_id"].(string)
	approve := postJSON(t, owner, server.URL+"/v1/clients/"+clientID+"/approve", `{}`)
	credential := approve.body["credential"].(string)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Scheme = "ws"
	parsed.Path = "/v1/ws"
	parsed.RawQuery = "after_seq=0"
	header := http.Header{"Authorization": []string{"Bearer " + credential}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	connection, _, err := websocket.Dial(ctx, parsed.String(), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	if revoke := postJSON(t, owner, server.URL+"/v1/clients/"+clientID+"/revoke", `{}`); revoke.status != http.StatusOK {
		t.Fatalf("revoke=%d %#v", revoke.status, revoke.body)
	}
	readCtx, readCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer readCancel()
	started := time.Now()
	if _, _, err := connection.Read(readCtx); err == nil {
		t.Fatal("revoked Client websocket remained readable")
	} else if time.Since(started) > time.Second {
		t.Fatalf("revoked Client websocket was not terminated promptly: %v", time.Since(started))
	}
}

func TestConversationReplayBoundaryDeduplicatesDelayedNotify(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "replay-race.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a committed SQLite entry whose post-commit observer callback is delayed.
	store.SetEntryObserver(nil)
	conversation, err := store.ConversationForPerson(context.Background(), api.OwnerID())
	if err != nil {
		t.Fatal(err)
	}
	entry, duplicate, err := store.AppendInbound(context.Background(), conversation.ID, "web", "delayed-notify", "one entry")
	if err != nil || duplicate {
		t.Fatalf("append entry=%#v duplicate=%v err=%v", entry, duplicate, err)
	}
	httpServer := httptest.NewServer(api.Handler())
	defer httpServer.Close()
	clientJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: clientJar}
	login(t, client, httpServer.URL)
	parsed, err := url.Parse(httpServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	header := http.Header{}
	for _, cookie := range client.Jar.Cookies(parsed) {
		header.Add("Cookie", cookie.String())
	}
	parsed.Scheme = "ws"
	parsed.Path = "/v1/ws"
	parsed.RawQuery = "after_seq=0"
	connection, _, err := websocket.Dial(context.Background(), parsed.String(), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	readCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	_, payload, err := connection.Read(readCtx)
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	var replayed core.ConversationEntry
	if err := json.Unmarshal(payload, &replayed); err != nil {
		t.Fatal(err)
	}
	if replayed.ID != entry.ID {
		t.Fatalf("replay entry=%#v want=%#v", replayed, entry)
	}

	// This is the delayed notify from the same commit. It must be recognized as already replayed.
	api.publishEntry(entry)
	duplicateCtx, duplicateCancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer duplicateCancel()
	if _, _, err := connection.Read(duplicateCtx); err == nil {
		t.Fatal("delayed notify delivered the replayed entry twice")
	}
}

func TestSlowPublisherNeverSilentlyDropsEntry(t *testing.T) {
	entries := make(chan core.ConversationEntry, 1)
	api := &Server{subscribers: map[*subscription]struct{}{}}
	sub := &subscription{conversationID: "con", entries: entries}
	api.subscribers[sub] = struct{}{}
	api.publishEntry(core.ConversationEntry{ID: "e1", ConversationID: "con", Seq: 1})
	api.publishEntry(core.ConversationEntry{ID: "e2", ConversationID: "con", Seq: 2})
	<-entries
	select {
	case _, open := <-entries:
		if !open {
			return
		}
		t.Fatal("publisher silently replaced or dropped a queued entry")
	default:
		t.Fatal("publisher silently dropped entry for slow subscriber")
	}
}

func TestSlowConversationSubscriberClosesWithoutSilentGap(t *testing.T) {
	server, client := testServer(t)
	login(t, client, server.URL)
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	header := http.Header{}
	for _, cookie := range client.Jar.Cookies(parsed) {
		header.Add("Cookie", cookie.String())
	}
	parsed.Scheme = "ws"
	parsed.Path = "/v1/ws"
	parsed.RawQuery = "after_seq=0"
	connection, _, err := websocket.Dial(context.Background(), parsed.String(), &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	const total = 100
	for i := 1; i <= total; i++ {
		postMessage(t, client, server.URL, "slow-"+strconv.Itoa(i), strings.Repeat("x", 4096))
	}
	last := int64(0)
	received := 0
	readCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for received < total {
		_, payload, readErr := connection.Read(readCtx)
		if readErr != nil {
			break
		}
		var entry core.ConversationEntry
		if err := json.Unmarshal(payload, &entry); err != nil {
			t.Fatal(err)
		}
		if entry.Seq != last+1 {
			t.Fatalf("live subscriber silently skipped seq: got=%d want=%d", entry.Seq, last+1)
		}
		last = entry.Seq
		received++
	}
	if received == total {
		return
	}
	response, err := client.Get(server.URL + "/v1/conversation?after_seq=" + strconv.FormatInt(last, 10))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var replay []core.ConversationEntry
	if err := json.NewDecoder(response.Body).Decode(&replay); err != nil {
		t.Fatal(err)
	}
	for i, entry := range replay {
		if entry.Seq != last+int64(i)+1 {
			t.Fatalf("replay gap after slow subscriber close: got=%d", entry.Seq)
		}
	}
	if int(last)+len(replay) != total {
		t.Fatalf("slow subscriber neither delivered nor recoverable: received=%d replay=%d", received, len(replay))
	}
}

func TestPendingPairingCanRedeemAfterApproveResponseLoss(t *testing.T) {
	server, owner := testServer(t)
	pair := postJSON(t, server.Client(), server.URL+"/v1/clients/pair", `{"bootstrap_token":"bootstrap","device_id":"redeem-device","display_name":"Redeem","platform":"test"}`)
	pendingToken, pendingOK := pair.body["pending_token"].(string)
	if pair.status != http.StatusCreated || !pendingOK || pendingToken == "" {
		t.Fatalf("pair did not return pending token: %d %#v", pair.status, pair.body)
	}
	clientID, clientOK := pair.body["client_id"].(string)
	if !clientOK || clientID == "" {
		t.Fatalf("pair did not return client id: %d %#v", pair.status, pair.body)
	}
	login(t, owner, server.URL)
	approve := postJSON(t, owner, server.URL+"/v1/clients/"+clientID+"/approve", `{}`)
	if approve.status != http.StatusOK {
		t.Fatalf("approve=%d %#v", approve.status, approve.body)
	}
	request, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/clients/"+clientID+"/redeem", strings.NewReader(`{"idempotency_key":"redeem-once"}`))
	request.Header.Set("Authorization", "Bearer "+pendingToken)
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var redeemed map[string]any
	_ = json.NewDecoder(response.Body).Decode(&redeemed)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || redeemed["credential"] == "" {
		t.Fatalf("redeem=%d %#v", response.StatusCode, redeemed)
	}
	request, _ = http.NewRequest(http.MethodPost, server.URL+"/v1/clients/"+clientID+"/redeem", strings.NewReader(`{"idempotency_key":"redeem-second"}`))
	request.Header.Set("Authorization", "Bearer "+pendingToken)
	request.Header.Set("Content-Type", "application/json")
	second, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	second.Body.Close()
	if second.StatusCode != http.StatusConflict && second.StatusCode != http.StatusUnauthorized {
		t.Fatalf("redeem credential was not one-time: status=%d", second.StatusCode)
	}
}

func TestClientPairingDeviceLifecycleRePairIsSafe(t *testing.T) {
	server, owner := testServer(t)
	pair := postJSON(t, server.Client(), server.URL+"/v1/clients/pair", `{"bootstrap_token":"bootstrap","device_id":"lifecycle-device","display_name":"Lifecycle","platform":"test"}`)
	clientID := pair.body["client_id"].(string)
	pendingToken := pair.body["pending_token"].(string)
	pollRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/clients/"+clientID+"/poll", nil)
	pollRequest.Header.Set("Authorization", "Bearer "+pendingToken)
	poll, err := server.Client().Do(pollRequest)
	if err != nil {
		t.Fatal(err)
	}
	poll.Body.Close()
	if poll.StatusCode != http.StatusOK {
		t.Fatalf("pending poll status=%d", poll.StatusCode)
	}
	login(t, owner, server.URL)
	approveRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/clients/"+clientID+"/approve", strings.NewReader(`{"idempotency_key":"approve-life"}`))
	approveRequest.Header.Set("Content-Type", "application/json")
	firstApprove, err := owner.Do(approveRequest)
	if err != nil {
		t.Fatal(err)
	}
	var firstBody map[string]any
	_ = json.NewDecoder(firstApprove.Body).Decode(&firstBody)
	firstApprove.Body.Close()
	if firstApprove.StatusCode != http.StatusOK {
		t.Fatalf("approve status=%d body=%#v", firstApprove.StatusCode, firstBody)
	}
	credential := firstBody["credential"].(string)
	secondRequest, _ := http.NewRequest(http.MethodPost, approveRequest.URL.String(), strings.NewReader(`{"idempotency_key":"approve-life"}`))
	secondRequest.Header.Set("Content-Type", "application/json")
	secondApprove, err := owner.Do(secondRequest)
	if err != nil {
		t.Fatal(err)
	}
	secondApprove.Body.Close()
	if secondApprove.StatusCode != http.StatusOK {
		t.Fatalf("idempotent approve status=%d", secondApprove.StatusCode)
	}
	if revoke := postJSON(t, owner, server.URL+"/v1/clients/"+clientID+"/revoke", `{"idempotency_key":"revoke-life"}`); revoke.status != http.StatusOK {
		t.Fatalf("revoke status=%d %#v", revoke.status, revoke.body)
	}
	repair := postJSON(t, server.Client(), server.URL+"/v1/clients/pair", `{"bootstrap_token":"bootstrap","device_id":"lifecycle-device","display_name":"Lifecycle 2","platform":"test"}`)
	if repair.status != http.StatusCreated || repair.body["client_id"] != clientID || repair.body["pending_token"] == "" {
		t.Fatalf("repair=%d %#v", repair.status, repair.body)
	}
	oldRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/v1/conversation", nil)
	oldRequest.Header.Set("Authorization", "Bearer "+credential)
	oldResponse, err := server.Client().Do(oldRequest)
	if err != nil {
		t.Fatal(err)
	}
	oldResponse.Body.Close()
	if oldResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("old credential survived re-pair: %d", oldResponse.StatusCode)
	}
}

func TestClientPairIdempotencyConflictsOnDifferentPayload(t *testing.T) {
	server, _ := testServer(t)
	first := postJSON(t, server.Client(), server.URL+"/v1/clients/pair", `{"bootstrap_token":"bootstrap","device_id":"idem-device-1","display_name":"One","platform":"test","idempotency_key":"pair-key"}`)
	if first.status != http.StatusCreated {
		t.Fatalf("first pair=%d %#v", first.status, first.body)
	}
	second := postJSON(t, server.Client(), server.URL+"/v1/clients/pair", `{"bootstrap_token":"bootstrap","device_id":"idem-device-2","display_name":"Two","platform":"test","idempotency_key":"pair-key"}`)
	if second.status != http.StatusConflict {
		t.Fatalf("different payload reused idempotency key: %d %#v", second.status, second.body)
	}
}

func TestClientAcknowledgementUserRevisionAndLegacyResponseRedaction(t *testing.T) {
	store, err := core.Open(context.Background(), filepath.Join(t.TempDir(), "contract.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	userPath := filepath.Join(t.TempDir(), "user.md")
	if err := os.WriteFile(userPath, []byte("prefer short answers"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadUserDocument(context.Background(), userPath); err != nil {
		t.Fatal(err)
	}
	api, err := New(context.Background(), store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	ownerJar, _ := cookiejar.New(nil)
	owner := &http.Client{Jar: ownerJar}
	login(t, owner, server.URL)

	user := getJSON(t, owner, server.URL+"/v1/user")
	if user.status != http.StatusOK || user.body["content"] != "prefer short answers" || user.body["revision"].(float64) != 1 {
		t.Fatalf("user=%d %#v", user.status, user.body)
	}
	updated := requestJSON(t, owner, http.MethodPut, server.URL+"/v1/user", `{"content":"prefer durable context","idempotency_key":"user-update-1"}`)
	if updated.status != http.StatusOK || updated.body["content"] != "prefer durable context" || updated.body["revision"].(float64) != 2 {
		t.Fatalf("updated=%d %#v", updated.status, updated.body)
	}
	invalid := requestJSON(t, owner, http.MethodPut, server.URL+"/v1/user", `{"content":"bad\u0000document","idempotency_key":"user-update-invalid"}`)
	if invalid.status != http.StatusBadRequest {
		t.Fatalf("invalid user status=%d body=%#v", invalid.status, invalid.body)
	}
	still := getJSON(t, owner, server.URL+"/v1/user")
	if still.body["content"] != "prefer durable context" || still.body["revision"].(float64) != 2 {
		t.Fatalf("invalid update replaced snapshot: %#v", still.body)
	}

	message := postJSON(t, owner, server.URL+"/v1/messages", `{"external_message_id":"client-message-1","body":"hello","idempotency_key":"message-first"}`)
	for _, key := range []string{"message_id", "entry_seq", "state", "duplicate"} {
		if _, ok := message.body[key]; !ok {
			t.Fatalf("ack missing %q: %#v", key, message.body)
		}
	}
	duplicate := postJSON(t, owner, server.URL+"/v1/messages", `{"external_message_id":"client-message-1","body":"hello","idempotency_key":"message-duplicate"}`)
	if duplicate.body["duplicate"] != true || duplicate.body["message_id"] != message.body["message_id"] || duplicate.body["entry_seq"] != message.body["entry_seq"] {
		t.Fatalf("duplicate acknowledgement=%#v first=%#v", duplicate.body, message.body)
	}
}

func TestClientSurfaceUsesWorkerEntitiesAndOrderedReplayRoute(t *testing.T) {
	server, owner := testServer(t)
	login(t, owner, server.URL)
	before := postMessage(t, owner, server.URL, "replay-1", "before")
	if _, ok := before["message_id"]; !ok {
		t.Fatalf("legacy Web acknowledgement missing message_id: %#v", before)
	}
	response, err := owner.Get(server.URL + "/v1/conversation?after_seq=0")
	if err != nil {
		t.Fatal(err)
	}
	var entries []core.ConversationEntry
	if err := json.NewDecoder(response.Body).Decode(&entries); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if len(entries) != 1 || entries[0].Seq != 1 {
		t.Fatalf("replay=%#v", entries)
	}
	response, err = owner.Get(server.URL + "/v1/conversation/ws?after_seq=bad")
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("ws replay alias status=%d", response.StatusCode)
	}
	response.Body.Close()
}

type jsonResponse struct {
	status int
	body   map[string]any
}

func postJSON(t *testing.T, client *http.Client, endpoint, payload string) jsonResponse {
	return requestJSON(t, client, http.MethodPost, endpoint, payload)
}

func requestJSON(t *testing.T, client *http.Client, method, endpoint, payload string) jsonResponse {
	t.Helper()
	request, err := http.NewRequest(method, endpoint, bytes.NewBufferString(payload))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	if !strings.Contains(payload, `"idempotency_key"`) {
		digest := sha256.Sum256([]byte(endpoint + "\x00" + payload))
		request.Header.Set("Idempotency-Key", "test-"+hex.EncodeToString(digest[:]))
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body := map[string]any{}
	_ = json.NewDecoder(response.Body).Decode(&body)
	return jsonResponse{status: response.StatusCode, body: body}
}

func getJSON(t *testing.T, client *http.Client, endpoint string) jsonResponse {
	t.Helper()
	response, err := client.Get(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body := map[string]any{}
	_ = json.NewDecoder(response.Body).Decode(&body)
	return jsonResponse{status: response.StatusCode, body: body}
}
