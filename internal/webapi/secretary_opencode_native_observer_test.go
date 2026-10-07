package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
	secretaryruntime "github.com/beruseruko/secretary/internal/secretary"
)

// Unpaid native process -> real secretary-mcp -> capability-authorized broker ->
// Core. Only a synthetic loopback provider and private empty native store are used.
func TestOpenCodeNativeSecretaryObservedDiscoveryAndRequiredReply(t *testing.T) {
	if os.Getenv("SECRETARY_OPENCODE_ACP_E2E") != "1" {
		t.Skip("opt-in private native fixture")
	}
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	binary, err := exec.LookPath("opencode")
	if err != nil {
		t.Fatal("native OpenCode unavailable")
	}
	version, err := exec.Command(binary, "--version").Output()
	if err != nil || strings.TrimSpace(string(version)) != "opencode v2.0.22" {
		t.Fatal("fixture requires exact native v2.0.22")
	}
	cachePaths, err := exec.Command("go", "env", "GOMODCACHE", "GOCACHE").Output()
	if err != nil {
		t.Fatal("Go build cache paths unavailable")
	}
	caches := strings.Split(strings.TrimSpace(string(cachePaths)), "\n")
	if len(caches) != 2 {
		t.Fatal("Go build cache paths invalid")
	}
	t.Setenv("GOMODCACHE", caches[0])
	t.Setenv("GOCACHE", caches[1])
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	personal := filepath.Join(root, "personal", "opencode")
	if err := os.MkdirAll(personal, 0o700); err != nil {
		t.Fatal(err)
	}
	canary := filepath.Join(personal, "opencode.db")
	if err := os.WriteFile(canary, []byte("personal canary"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", filepath.Dir(personal))
	t.Setenv("OPENAI_API_KEY", "ambient-canary-never-use")
	dataDir := filepath.Join(root, "secretary")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(dataDir, "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api, err := New(ctx, store, "synthetic-bootstrap")
	if err != nil {
		t.Fatal(err)
	}
	api.AttachSecretaryWorkerTools(fakeSecretaryWorkerTools{})
	api.AttachSecretaryReplyContract(func() string { return core.SecretaryReplyContractAddressedV1 })
	broker := httptest.NewServer(api.Handler())
	defer broker.Close()
	conversation, err := store.ConversationForPerson(ctx, api.OwnerID())
	if err != nil {
		t.Fatal(err)
	}
	identity, err := store.EnsureSecretaryIdentity(ctx, api.OwnerID(), conversation.ID)
	if err != nil {
		t.Fatal(err)
	}
	capability, err := store.RotateSecretaryCapability(ctx, api.OwnerID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveUserDocument(ctx, filepath.Join(dataDir, "user.md"), "synthetic owner"); err != nil {
		t.Fatal(err)
	}
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("fixture source unavailable")
	}
	repo := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	mcpBinary := filepath.Join(root, "secretary-mcp")
	command := exec.CommandContext(ctx, "go", "build", "-o", mcpBinary, "./cmd/secretary-mcp")
	command.Dir = repo
	if output, err := command.CombinedOutput(); err != nil {
		_ = output
		t.Fatal("private MCP binary build failed")
	}
	var sawSchemas, sawReasoning, noLeak atomic.Bool
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "invalid fixture", 400)
			return
		}
		encoded, _ := json.Marshal(body)
		noLeak.Store(!strings.Contains(string(encoded), capability) && !strings.Contains(string(encoded), "ambient-canary-never-use") && !strings.Contains(string(encoded), "mcpobs_"))
		tools, _ := body["tools"].([]any)
		hasReply, hasSpawn := false, false
		for _, raw := range tools {
			tool, _ := raw.(map[string]any)
			function, _ := tool["function"].(map[string]any)
			hasReply = hasReply || function["name"] == "secretary_reply_to_user"
			hasSpawn = hasSpawn || function["name"] == "secretary_spawn_worker"
		}
		if len(tools) == 9 && hasReply && hasSpawn {
			sawSchemas.Store(true)
		}
		if body["reasoning_effort"] == "xhigh" {
			sawReasoning.Store(true)
		}
		messages, _ := body["messages"].([]any)
		hasResult := false
		for _, raw := range messages {
			message, _ := raw.(map[string]any)
			if message["role"] == "tool" {
				hasResult = true
			}
		}
		calls.Add(1)
		write := func(delta map[string]any, finish any) {
			chunk := map[string]any{"id": "synthetic-native", "object": "chat.completion.chunk", "created": 1, "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}}
			data, _ := json.Marshal(chunk)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\n", data)
		}
		if hasReply && !hasResult {
			turns, err := store.SecretaryTurns(r.Context(), identity.ID)
			if err != nil || len(turns) == 0 {
				http.Error(w, "origin unavailable", 500)
				return
			}
			turn := turns[len(turns)-1]
			args, _ := json.Marshal(map[string]string{"secretary_turn_id": turn.ID, "input_id": turn.InputID, "text": "synthetic addressed answer"})
			write(map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "synthetic-reply", "type": "function", "function": map[string]string{"name": "secretary_reply_to_user", "arguments": string(args)}}}}, nil)
			write(map[string]any{}, "tool_calls")
		} else {
			write(map[string]any{"role": "assistant", "content": "unaddressed synthetic final"}, nil)
			write(map[string]any{}, "stop")
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer provider.Close()
	t.Setenv("TEST_SECRETARY_NATIVE_WRAPPER", "1")
	t.Setenv("TEST_SECRETARY_NATIVE_BINARY", binary)
	t.Setenv("TEST_SECRETARY_NATIVE_PROVIDER", provider.URL+"/v1")
	profile := node.ManagedProfile{Name: "secretary", Version: "v1", Hash: "synthetic-hash", Content: "Use reply_to_user once for the server-issued active input. Do not publish assistant text as a reply.", Runtime: "opencode", Model: "fixture/fixture-model", Reasoning: "xhigh", Delivery: "native", ReplyContractVersion: core.SecretaryReplyContractAddressedV1}
	native := node.OpenCodeRuntime{Command: os.Args[0], Arguments: []string{"-test.run=^TestSecretaryNativeObserverWrapperProcess$"}, DataHome: filepath.Join(root, "native")}
	agent := secretaryruntime.NewRuntime(node.NewLocal(native), capability)
	agent.AttachIdentity(identity)
	agent.AttachConversation(store, conversation.ID)
	agent.AttachProfile(func() node.ManagedProfile { return profile })
	agent.AttachMCPServer(mcpBinary, dataDir, broker.URL)
	if err := agent.Start(ctx); err != nil {
		t.Fatal("native Secretary startup failed")
	}
	defer agent.Stop(context.Background())
	if err := agent.HandleMessage(ctx, "synthetic independent input"); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	var turn core.SecretaryTurn
	for {
		turns, err := store.SecretaryTurns(ctx, identity.ID)
		if err == nil && len(turns) == 1 && turns[0].State.Terminal() {
			turn = turns[0]
			break
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("native Secretary completion timed out")
		}
	}
	evidence, err := store.SecretaryMCPDiscovery(ctx, turn.ID)
	if err != nil || turn.State != core.SecretaryTurnSucceeded || !evidence.Started || !evidence.Initialized || !evidence.ToolsListed || evidence.ToolCount != 9 || !evidence.HasReplyToUser || !evidence.HasSpawnWorker || !evidence.GenerationMatches || evidence.Failed {
		t.Fatalf("native completion/discovery mismatch state=%s code=%s", turn.State, evidence.Code)
	}
	entries, err := store.EntriesAfter(ctx, conversation.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.Kind == core.EntrySecretary {
			count++
			if entry.Body != "synthetic addressed answer" {
				t.Fatal("native assistant fallback leaked")
			}
		}
	}
	if count != 1 || !sawSchemas.Load() || !sawReasoning.Load() || !noLeak.Load() {
		t.Fatal("native reply/model/schema/privacy gate failed")
	}
	events, err := store.SecretaryEvents(ctx, turn.ID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	committed := false
	for _, event := range events {
		if event.Kind == core.SecretaryTurnFinishedEvent {
			var payload struct {
				Completion core.SecretaryCompletionEvidence `json:"completion"`
			}
			if json.Unmarshal(event.Payload, &payload) == nil {
				committed = payload.Completion.Committed && payload.Completion.EntryPresent && payload.Completion.ReplyCount == 1
			}
		}
	}
	if !committed {
		t.Fatal("native completion lacked committed evidence")
	}
	data, err := os.ReadFile(canary)
	if err != nil || string(data) != "personal canary" {
		t.Fatal("native fixture touched personal store")
	}
	t.Logf("native2.0.22 observer/reply PASS tools=%d provider_requests=%d; synthetic loopback only, not live acceptance", evidence.ToolCount, calls.Load())
}

func TestSecretaryNativeObserverWrapperProcess(t *testing.T) {
	if os.Getenv("TEST_SECRETARY_NATIVE_WRAPPER") != "1" {
		return
	}
	path := os.Getenv("OPENCODE_CONFIG")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("private native config unavailable")
	}
	var config map[string]any
	if json.Unmarshal(data, &config) != nil {
		t.Fatal("private native config invalid")
	}
	providers, _ := config["providers"].(map[string]any)
	provider, _ := providers["fixture"].(map[string]any)
	if provider == nil {
		t.Fatal("private fixture provider unavailable")
	}
	provider["package"] = "@opencode/ai/providers/openai-compatible"
	provider["env"] = []string{}
	provider["settings"] = map[string]string{"baseURL": os.Getenv("TEST_SECRETARY_NATIVE_PROVIDER")}
	models, _ := provider["models"].(map[string]any)
	for _, raw := range models {
		model, _ := raw.(map[string]any)
		model["capabilities"] = map[string]any{"tools": true, "reasoning": true, "input": []string{"text"}, "output": []string{"text"}}
	}
	data, err = json.Marshal(config)
	if err != nil || os.WriteFile(path, data, 0o600) != nil {
		t.Fatal("private provider wiring failed")
	}
	if os.Setenv("OPENCODE_CONFIG_CONTENT", "") != nil {
		t.Fatal("private environment wiring failed")
	}
	binary := os.Getenv("TEST_SECRETARY_NATIVE_BINARY")
	if err := syscall.Exec(binary, []string{binary, "acp"}, os.Environ()); err != nil {
		t.Fatal("native OpenCode exec failed")
	}
}
