package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/mcp"
)

func TestSecretaryMCPDiscoveryIsWrittenNativeEvidenceBoundToLaunchAndTurns(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "secretary.db")
	store, err := core.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	api, err := New(ctx, store, "bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := store.ConversationForPerson(ctx, api.OwnerID())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, api.OwnerID(), conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	cap, err := store.RotateSecretaryCapability(ctx, api.OwnerID())
	if err != nil {
		t.Fatal(err)
	}
	api.AttachSecretaryWorkerTools(fakeSecretaryWorkerTools{})
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	launch, err := store.BeginSecretaryRuntime(ctx, identity.ID, cap, "opencode", "fixture/model", "xhigh")
	if err != nil {
		t.Fatal(err)
	}
	setPolicy := func() {
		if err := store.SetSecretaryPolicySnapshot(ctx, core.SecretaryPolicySnapshot{Version: "v1", Harness: "opencode", Model: "fixture/model", Reasoning: "xhigh", ProfileVersion: "v1", ProfileName: "secretary", ProfileHash: "h", ProfileContent: "synthetic"}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.SaveUserDocument(ctx, filepath.Join(t.TempDir(), "user.md"), "synthetic owner"); err != nil {
			t.Fatal(err)
		}
	}
	setPolicy()
	newTurn := func() core.SecretaryTurn {
		turn, err := store.EnqueueSecretaryTurn(ctx, identity.ID, "synthetic input")
		if err != nil {
			t.Fatal(err)
		}
		turn, err = store.StartSecretaryTurn(ctx, turn.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err = store.BindSecretaryRuntimeTurn(ctx, launch, turn.ID, turn.InputID); err != nil {
			t.Fatal(err)
		}
		return turn
	}
	turn := newTurn()
	before, err := store.SecretaryMCPDiscovery(ctx, turn.ID)
	if err != nil || before.ToolsListed || before.Started {
		t.Fatal("missing discovery was presented as ready")
	}
	observer := mcp.RemoteMCPObserver{BaseURL: server.URL, Capability: launch.ObserverCapability, Client: server.Client()}
	// Lazy discovery starts after a turn is already active. No pre-prompt barrier.
	var out bytes.Buffer
	observed := func(ctx context.Context, observation core.SecretaryMCPObservation) error {
		if observation.Phase == "initialize" || observation.Phase == "tools_list" {
			var frames []map[string]any
			for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
				var frame map[string]any
				if json.Unmarshal(line, &frame) != nil {
					t.Fatal("observation preceded valid stdio write")
				}
				frames = append(frames, frame)
			}
			if observation.Phase == "tools_list" && len(frames) != 2 {
				t.Fatal("discovery preceded response write")
			}
		}
		return observer.Observe(ctx, observation)
	}
	input := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/list\"}\n")
	if err := (mcp.Server{Handler: mcp.Secretary{ReplyContractVersion: core.SecretaryReplyContractAddressedV1}, Observe: observed}).Serve(ctx, input, &out); err != nil {
		t.Fatal("stdio discovery failed")
	}
	discovery, err := store.SecretaryMCPDiscovery(ctx, turn.ID)
	if err != nil || !discovery.Started || !discovery.Initialized || !discovery.ToolsListed || discovery.ToolCount != 9 || !discovery.HasSpawnWorker || !discovery.HasReplyToUser || !discovery.GenerationMatches {
		t.Fatalf("discovery allowlist mismatch=%t", true)
	}
	if _, err := store.FinishSecretaryTurn(ctx, turn.ID, core.SecretaryTurnFailed, "synthetic"); err != nil {
		t.Fatal(err)
	}
	second := newTurn()
	inherited, err := store.SecretaryMCPDiscovery(ctx, second.ID)
	if err != nil || inherited != discovery {
		t.Fatal("subsequent turn lost same-launch catalogue")
	}
	encoded, _ := json.Marshal(discovery)
	for _, forbidden := range []string{launch.ObserverCapability, identity.ID, turn.ID, turn.InputID, "fixture/model", "capability", "launch_id", "runtime_session"} {
		if bytes.Contains(encoded, []byte(forbidden)) {
			t.Fatal("discovery DTO leaked private linkage")
		}
	}
	// Observer credential cannot invoke lifecycle operations, and lifecycle/bootstrap
	// credentials cannot fabricate observation.
	post := func(token, path, body string) int {
		req, _ := http.NewRequest(http.MethodPost, server.URL+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	if post(launch.ObserverCapability, "/v1/internal/secretary/tools/call", `{"name":"spawn_worker","arguments":{}}`) != 401 {
		t.Fatal("observer credential acquired lifecycle authority")
	}
	for _, token := range []string{cap, "bootstrap", ""} {
		if post(token, "/v1/internal/secretary/mcp/observe", `{"phase":"startup","success":true}`) != 401 {
			t.Fatal("observer capability fallback")
		}
	}
	if post(launch.ObserverCapability, "/v1/internal/secretary/mcp/observe", `{"phase":"startup","success":true,"arguments":{"secret":"sentinel"}}`) != 400 {
		t.Fatal("observer accepted arbitrary payload")
	}
	for _, body := range []string{
		`{"phase":"unknown_phase","success":true}`,
		`{"phase":"","success":false}`,
		`{"phase":42,"success":true}`,
		`{"phase":"startup","success":true,"tool_count":1}`,
		`{"phase":"initialize","success":true,"has_spawn_worker":true}`,
		`{"phase":"tools_list","success":false,"tool_count":9}`,
		`{"phase":"tools_list","success":true,"tool_count":-1}`,
		`{"phase":"tools_list","success":true,"tool_count":101}`,
		`{"phase":"tools_list","success":true,"has_reply_to_user":true}`,
	} {
		if post(launch.ObserverCapability, "/v1/internal/secretary/mcp/observe", body) != 400 {
			t.Fatal("observer accepted unknown phase or invalid phase fields")
		}
	}
	afterInvalid, err := store.SecretaryMCPDiscovery(ctx, second.ID)
	if err != nil || afterInvalid != discovery {
		t.Fatal("invalid observation mutated the committed discovery")
	}
	if _, _, err := store.RecordSecretaryReply(ctx, api.OwnerID(), cap, second.ID, second.InputID, "typed reply before replacement"); err != nil {
		t.Fatal(err)
	}
	next, err := store.BeginSecretaryRuntime(ctx, identity.ID, cap, "opencode", "fixture/model", "xhigh")
	if err != nil || next.Generation != launch.Generation+1 {
		t.Fatal("same-pins launch did not advance generation")
	}
	if err := observer.Observe(ctx, core.SecretaryMCPObservation{Phase: "tools_list", Success: true, ToolCount: 9, HasSpawnWorker: true, HasReplyToUser: true}); err == nil {
		t.Fatal("stale observer changed evidence")
	}
	stale, err := store.SecretaryMCPDiscovery(ctx, second.ID)
	if err != nil || !stale.Revoked || stale.GenerationMatches {
		t.Fatal("old turn silently rebound to new launch")
	}
	// No observation/response on a new launch is missing, not an empty successful list.
	if err := store.BindSecretaryRuntimeTurn(ctx, next, second.ID, second.InputID); !errors.Is(err, core.ErrInvalidSecretaryOrigin) {
		t.Fatal("turn launch binding was mutable")
	}
	completed, err := store.FinishSecretaryTurnWithRequiredReply(ctx, second.ID, second.InputID, core.SecretaryTurnSucceeded, core.SecretaryCompletionEvidence{Branch: "addressed_reply_only", TerminalClass: "end_turn", TerminalValid: true, RPCSucceeded: true, DrainCompleted: true})
	if err != nil || completed.State != core.SecretaryTurnFailed || completed.Error != "mcp_launch_revoked" {
		t.Fatal("revoked launch hid failure behind its durable reply")
	}
	if _, err := store.RotateSecretaryCapability(ctx, api.OwnerID()); err != nil {
		t.Fatal(err)
	}
	revoked := mcp.RemoteMCPObserver{BaseURL: server.URL, Capability: next.ObserverCapability, Client: server.Client()}
	if err := revoked.Observe(ctx, core.SecretaryMCPObservation{Phase: "startup", Success: true}); err == nil {
		t.Fatal("revoked parent capability retained observer authority")
	}
	jar, _ := cookiejar.New(nil)
	reader := &http.Client{Jar: jar}
	login(t, reader, server.URL)
	response, err := reader.Get(server.URL + "/v1/secretary/turns/" + second.ID + "/stream")
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatal("public Secretary stream unavailable")
	}
	var replay struct {
		Events []struct {
			Kind    string                     `json:"kind"`
			Payload map[string]json.RawMessage `json:"payload"`
		} `json:"events"`
	}
	if err := json.NewDecoder(response.Body).Decode(&replay); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	found := false
	for _, event := range replay.Events {
		if event.Kind != core.SecretaryTurnFinishedEvent {
			continue
		}
		found = true
		for key, allowed := range map[string][]string{
			"completion":    {"branch", "terminal_class", "assistant_chunks", "terminal_valid", "rpc_succeeded", "drain_completed", "response_present", "reply_count", "entry_present", "committed", "code"},
			"mcp_discovery": {"started", "initialized", "tools_listed", "tool_count", "has_spawn_worker", "has_reply_to_user", "failed", "revoked", "generation_matches", "code"},
		} {
			var fields map[string]any
			if json.Unmarshal(event.Payload[key], &fields) != nil || len(fields) != len(allowed) {
				t.Fatal("public diagnostic allowlist mismatch")
			}
			for _, name := range allowed {
				if _, exists := fields[name]; !exists {
					t.Fatal("public diagnostic field missing")
				}
			}
		}
	}
	if !found {
		t.Fatal("public completion evidence missing")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = core.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := store.SecretaryMCPDiscovery(ctx, second.ID)
	if err != nil || !persisted.Revoked || persisted.ToolCount != 9 {
		t.Fatal("discovery did not survive reopen")
	}
}
