package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

// Native, unpaid regression: every turn must receive the managed system, the
// deny-first tool catalog, real tool output and prior history. Phase 1 is a
// same-process Follow-up; phase 2 reloads the SAME session in a fresh process.
func TestOpenCodeNativeProfilePersistence(t *testing.T) {
	oldUmask := syscall.Umask(0o077)
	defer syscall.Umask(oldUmask)
	if os.Getenv("SECRETARY_OPENCODE_ACP_E2E") != "1" {
		t.Skip("native OpenCode acceptance is opt-in")
	}
	binary, err := exec.LookPath("opencode")
	if err != nil {
		t.Fatal("native OpenCode unavailable")
	}
	sharedRoot := t.TempDir()
	publicSetup := runPublicNativeSetup(t, sharedRoot, binary)
	sharedDataHome := publicSetup.DataHome
	for _, role := range []string{"worker", "secretary"} {
		t.Run(role, func(t *testing.T) {
			root := t.TempDir()
			workspace := filepath.Join(root, "workspace")
			if os.MkdirAll(workspace, 0o700) != nil {
				t.Fatal("private workspace unavailable")
			}
			dataHome := sharedDataHome
			personalDataHome := filepath.Join(root, "personal-data")
			if os.MkdirAll(filepath.Join(personalDataHome, "opencode"), 0o700) != nil {
				t.Fatal("personal-store canary directory unavailable")
			}
			personalCanary := filepath.Join(personalDataHome, "opencode", "opencode.db")
			if os.WriteFile(personalCanary, []byte("untouched-personal-store"), 0o600) != nil {
				t.Fatal("personal-store canary unavailable")
			}
			t.Setenv("HOME", filepath.Join(root, "home"))
			t.Setenv("XDG_DATA_HOME", personalDataHome)
			t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
			t.Setenv("FIXTURE_API_KEY", "fixture-key-never-log")
			t.Setenv("TEST_OPENCODE_WRAPPER", "1")
			t.Setenv("TEST_NATIVE_OPENCODE", binary)
			command, err := filepath.Abs(os.Args[0])
			if err != nil {
				t.Fatal("private fixture command unavailable")
			}
			state := filepath.Join(root, "mcp-state")
			otherRole := "secretary"
			model, reasoning := "fixture/worker-model", "low"
			profileContent := "private-persistence-worker-system-marker"
			if role == "worker" {
				otherRole = "secretary"
			} else {
				otherRole = "worker"
				model, reasoning = "fixture/secretary-model", "high"
				profileContent = "private-persistence-secretary-system-marker"
			}
			profile := ManagedProfile{Name: role, Content: profileContent, Model: model, Reasoning: reasoning}
			var servers []MCPServer
			toolName := "read"
			toolArgs := `{"path":"fixture.txt"}`
			if role == "worker" {
				profile.AllowTools = []string{"read"}
			} else {
				toolName, toolArgs = "secretary_list_workers", `{}`
				servers = []MCPServer{{Name: "secretary", Command: command, Args: []string{"-test.run=^TestOpenCodeConfigWrapperProcess$", "--", "mcp-server"}, Env: []MCPEnv{{Name: "SECRETARY_MCP_DATA_DIR", Value: state}, {Name: "SECRETARY_MCP_CAPABILITY", Value: "native-fixture-capability"}}}}
			}
			profile.Hash = HashProfile(profile.Content, nil, profile.Model, profile.Reasoning)
			if role == "worker" {
				profile.Version, profile.SourceHash, profile.Runtime, profile.Delivery = "synthetic-v1", profile.Hash, "opencode", "native"
				profile.Skills = []ManagedSkill{}
				profile.Hash = profile.SnapshotHash()
			}
			providerModel := strings.TrimPrefix(model, "fixture/")
			var phase atomic.Int32
			var checks [3]atomic.Int32
			var policyFailure atomic.Bool
			var denied [3]atomic.Bool
			nonces := make([]string, 3)
			for i := range nonces {
				var nonce [16]byte
				if _, err := rand.Read(nonce[:]); err != nil {
					t.Fatal("fixture nonce unavailable")
				}
				nonces[i] = hex.EncodeToString(nonce[:])
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					http.Error(w, "invalid fixture", 400)
					return
				}
				messages, _ := body["messages"].([]any)
				tools, _ := body["tools"].([]any)
				if len(tools) == 0 {
					writeFixtureCompletion(w, "fixture-title", "Fixture title")
					return
				}
				p := int(phase.Load())
				text := fixtureMessageText(messages)
				encoded, _ := json.Marshal(body)
				otherProfileMarker := "private-persistence-" + otherRole + "-system-marker"
				valid := body["model"] == providerModel && body["reasoning_effort"] == reasoning && strings.Contains(text, profile.Content) && !strings.Contains(text, otherProfileMarker) && !strings.Contains(text, fmt.Sprintf("fixture-%s-turn-0-completed", otherRole)) && !strings.Contains(string(encoded), "native-fixture-capability") && len(tools) == 1
				for _, raw := range tools {
					tool, _ := raw.(map[string]any)
					function, _ := tool["function"].(map[string]any)
					valid = valid && function["name"] == toolName
				}
				for earlier := 0; earlier < p; earlier++ {
					valid = valid && strings.Contains(text, fmt.Sprintf("fixture-%s-turn-%d-completed", role, earlier))
				}
				if !valid {
					t.Logf("safe provider check: role=%s phase=%d model=%t effort=%t system=%t tools=%d credential_leak=%t", role, p, body["model"] == providerModel, body["reasoning_effort"] == reasoning, strings.Contains(text, profile.Content), len(tools), strings.Contains(string(encoded), "native-fixture-capability"))
					policyFailure.Store(true)
				}
				checks[p].Add(1)
				current := messagesAfterLastUser(messages)
				toolText := fixtureMessageText(current)
				toolResult, deniedResult := false, false
				for _, raw := range current {
					message, _ := raw.(map[string]any)
					if message["role"] != "tool" {
						continue
					}
					one := fixtureMessageText([]any{message})
					if role == "worker" {
						toolResult = toolResult || strings.Contains(one, nonces[p])
					} else {
						toolResult = toolResult || strings.Contains(one, `"workers":[]`)
					}
					// Native disabled tools produce an error result, not a file
					// side effect. Never log its arguments or response content.
					deniedResult = deniedResult || message["tool_call_id"] == fmt.Sprintf("forbidden-%d", p) && strings.Contains(one, "No tool named") && strings.Contains(one, "is currently available")
				}
				switch {
				case !toolResult:
					writeFixtureToolCall(w, fmt.Sprintf("allowed-%d", p), toolName, toolArgs)
				case !deniedResult:
					// A malicious provider can request an unadvertised builtin.
					// It must remain unavailable both before and after restart.
					if strings.Contains(toolText, "forbidden-") {
						policyFailure.Store(true)
						writeFixtureCompletion(w, "fixture-invalid", "invalid")
						return
					}
					writeFixtureToolCall(w, fmt.Sprintf("forbidden-%d", p), "shell", `{"command":"touch forbidden-side-effect"}`)
				default:
					denied[p].Store(true)
					writeFixtureCompletion(w, "fixture-final", fmt.Sprintf("fixture-%s-turn-%d-completed", role, p))
				}
			}))
			defer server.Close()
			t.Setenv("TEST_FIXTURE_URL", server.URL+"/v1")
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			runtime := OpenCodeRuntime{Command: command, Arguments: []string{"-test.run=^TestOpenCodeConfigWrapperProcess$", "--", "acp"}, DataHome: dataHome}
			request := StartRequest{WorkerRef: "private-persistence-fixture", Workspace: workspace, Profile: profile, MCPServers: servers, DeferInitialPrompt: true}
			if role == "worker" {
				request.Profile = nativeWorkerTemplateJSONRoundTrip(t, profile)
			}
			session, err := runtime.Start(ctx, request)
			if err != nil {
				for _, marker := range []string{"model not found", "mode not found", "invalid params", "variant", "effort", "initialize", "process stopped", "deadline", "unauthorized", "401", "permission", "directory", "symlink"} {
					if strings.Contains(strings.ToLower(err.Error()), marker) {
						t.Logf("safe native startup error category: %s", marker)
					}
				}
				t.Fatal("native managed profile startup failed")
			}
			defer func() {
				if session != nil {
					_ = session.Close()
				}
			}()
			id := session.ID()
			for p := 0; p < 3; p++ {
				phase.Store(int32(p))
				if role == "worker" {
					request.Profile = nativeWorkerTemplateJSONRoundTrip(t, request.Profile)
				}
				if p == 2 {
					if session.Close() != nil {
						t.Fatal("native private close failed")
					}
					session, err = runtime.Resume(ctx, request, id)
					if err != nil {
						t.Fatal("native managed profile Resume failed")
					}
					if session.ID() != id {
						t.Fatal("native Resume replaced its session")
					}
				}
				if os.WriteFile(filepath.Join(workspace, "fixture.txt"), []byte(nonces[p]), 0o600) != nil {
					t.Fatal("private nonce file unavailable")
				}
				result := runNativeFixtureTurn(t, ctx, session, fmt.Sprintf("Perform private %s fixture turn %d.", role, p))
				if result.Status != "succeeded" || result.Summary != fmt.Sprintf("fixture-%s-turn-%d-completed", role, p) || checks[p].Load() < 3 || !denied[p].Load() || policyFailure.Load() {
					t.Fatalf("safe persistence metadata: phase=%d status=%s calls=%d denied=%t policy_failure=%t", p, result.Status, checks[p].Load(), denied[p].Load(), policyFailure.Load())
				}
				if _, err := os.Stat(filepath.Join(workspace, "forbidden-side-effect")); !os.IsNotExist(err) {
					t.Fatal("deny-first native policy permitted a forbidden side effect")
				}
				t.Logf("safe persistence metadata: role=%s phase=%d managed_profile=true role_tool_only=true model_reasoning_isolated=true prior_role_history=true same_session=true", role, p)
			}
			canary, err := os.ReadFile(personalCanary)
			if err != nil || string(canary) != "untouched-personal-store" {
				t.Fatal("private native round-trip touched the personal OpenCode store")
			}
			if _, err := os.Stat(filepath.Join(dataHome, "opencode", "opencode.db")); err != nil {
				t.Fatal("private target store did not retain native history")
			}
			if err := checkPrivateNativeStorePermissions(dataHome); err != nil {
				t.Fatal("native DB/WAL/SHM permissions are not private")
			}
		})
	}
}

func checkPrivateNativeStorePermissions(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return os.ErrPermission
		}
		if info.IsDir() {
			if info.Mode().Perm() != 0o700 {
				return os.ErrPermission
			}
			return nil
		}
		if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
			return os.ErrPermission
		}
		return nil
	})
}

func messagesAfterLastUser(messages []any) []any {
	for i := len(messages) - 1; i >= 0; i-- {
		message, _ := messages[i].(map[string]any)
		if message["role"] == "user" {
			return messages[i+1:]
		}
	}
	return nil
}

func writeFixtureToolCall(w http.ResponseWriter, id, name, args string) {
	writeOpenAIStream(w, map[string]any{"id": "fixture-call", "object": "chat.completion.chunk", "created": 1, "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]string{"name": name, "arguments": args}}}}, "finish_reason": nil}}})
	writeOpenAIStream(w, map[string]any{"id": "fixture-call", "object": "chat.completion.chunk", "created": 1, "model": "fixture-model", "choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}}})
	fmt.Fprint(w, "data: [DONE]\n\n")
}

func runNativeFixtureTurn(t *testing.T, ctx context.Context, session Session, prompt string) Result {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- session.Prompt(ctx, prompt) }()
	activityCh := session.Activity()
	for {
		select {
		case activity, ok := <-activityCh:
			if !ok {
				activityCh = nil
				continue
			}
			if activity.Kind == ActivityPermission {
				t.Fatal("deny-first fixture unexpectedly requested approval")
			}
		case result, ok := <-session.Result():
			if !ok {
				t.Fatal("native fixture closed without terminal Result")
			}
			if done != nil {
				select {
				case err := <-done:
					if err != nil {
						t.Fatal("native fixture prompt failed")
					}
				case <-ctx.Done():
					t.Fatal("native fixture completion timed out")
				}
			}
			return result
		case err := <-done:
			if err != nil {
				t.Fatal("native fixture prompt failed")
			}
			done = nil
		case <-ctx.Done():
			t.Fatal("native fixture timed out")
		}
	}
}

func nativeWorkerTemplateJSONRoundTrip(t *testing.T, profile ManagedProfile) ManagedProfile {
	t.Helper()
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal("native synthetic Worker template encoding failed")
	}
	var decoded ManagedProfile
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.Hash != profile.Hash || decoded.SourceHash != profile.SourceHash || decoded.Skills == nil ||
		decoded.ValidateWorkerBinding(profile.Runtime, profile.Model, profile.Reasoning, core.ProjectPolicy{}) != nil {
		t.Fatal("native Worker template lost frozen hash or empty Skills across JSON")
	}
	return decoded
}

func TestOpenCodeNativeInventoryMissingAuthDoesNotFallback(t *testing.T) {
	if os.Getenv("SECRETARY_OPENCODE_ACP_E2E") != "1" {
		t.Skip("native OpenCode inventory acceptance is opt-in")
	}
	binary, err := exec.LookPath("opencode")
	if err != nil {
		t.Skip("native OpenCode binary unavailable")
	}
	oldUmask := syscall.Umask(0o077)
	defer syscall.Umask(oldUmask)
	root := t.TempDir()
	personalHome := filepath.Join(root, "personal-data")
	if err := os.MkdirAll(filepath.Join(personalHome, "opencode"), 0o700); err != nil {
		t.Fatal("personal-store canary directory unavailable")
	}
	personalCanary := filepath.Join(personalHome, "opencode", "opencode.db")
	if err := os.WriteFile(personalCanary, []byte("personal-store-must-not-authenticate-Node"), 0o600); err != nil {
		t.Fatal("personal-store canary unavailable")
	}
	dataDir := filepath.Join(root, "node-data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal("private Node data directory unavailable")
	}
	dataHome := OpenCodeNativeDataHome(dataDir)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("XDG_DATA_HOME", personalHome)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	inventory, err := (HarnessDiscovery{
		Node: "private-inventory", Runner: ExecCommandRunner{}, IncludeOpenCode: true,
		Probes:           []HarnessProbe{{Node: "private-inventory", Runner: ExecCommandRunner{}, Spec: DefaultOpenCodeProbeSpec()}},
		OpenCodeDataHome: dataHome, BinaryOverrides: map[core.HarnessKind]string{core.HarnessOpenCode: binary},
	}).Discover(ctx)
	if err != nil || len(inventory.Instances) != 1 {
		t.Fatal("private OpenCode inventory probe did not return an explicit status")
	}
	instance := inventory.Instances[0]
	if instance.Authentication.Authenticated || instance.Status == core.HarnessReady {
		t.Fatal("Node inventory borrowed authentication from the personal OpenCode store")
	}
	canary, err := os.ReadFile(personalCanary)
	if err != nil || string(canary) != "personal-store-must-not-authenticate-Node" {
		t.Fatal("Node inventory touched the personal OpenCode store")
	}
}

func TestOpenCodeSelectedStoreAuthCatalogAndACPReadiness(t *testing.T) {
	if os.Getenv("SECRETARY_OPENCODE_SELECTED_STORE_E2E") != "1" {
		t.Skip("read-only selected-store auth/catalog/ACP acceptance is opt-in")
	}
	dataHome := strings.TrimSpace(os.Getenv("TEST_OPENCODE_SELECTED_DATA_HOME"))
	if dataHome == "" || !filepath.IsAbs(dataHome) {
		t.Fatal("selected native data home must be supplied as an absolute path")
	}
	binary, err := exec.LookPath("opencode")
	if err != nil {
		t.Fatal("selected-store OpenCode v2 executable unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := CheckOpenCodeAuthentication(ctx, binary, dataHome); err != nil {
		t.Fatal("selected-store native credential check failed")
	}
	const nodeRef core.NodeReference = "private-selected-store"
	inventory, err := (HarnessDiscovery{
		Node: nodeRef, Runner: ExecCommandRunner{}, OpenCodeDataHome: dataHome, IncludeOpenCode: true,
		Probes:          []HarnessProbe{{Node: nodeRef, Spec: DefaultOpenCodeProbeSpec()}},
		BinaryOverrides: map[core.HarnessKind]string{core.HarnessOpenCode: binary},
	}).Discover(ctx)
	if err != nil || len(inventory.Instances) != 1 {
		t.Fatal("selected-store OpenCode inventory unavailable")
	}
	instance := inventory.Instances[0]
	if !instance.Available() || !instance.Authentication.Authenticated {
		t.Fatal("selected-store OpenCode auth or ACP readiness failed")
	}
	for _, model := range []core.ObservedModelID{"openai/gpt-6.1-sol", "openai/gpt-6-luna"} {
		if !instance.SupportsModel(model) {
			t.Fatalf("selected-store native inventory omitted approved model %s", model)
		}
	}
	if !instance.SupportsReasoning("xhigh") {
		t.Fatal("selected-store native inventory omitted xhigh reasoning")
	}
	t.Logf("selected-store native metadata: status=%s authenticated=%t model_count=%d reasoning_count=%d model_ids=openai/gpt-6.1-sol,openai/gpt-6-luna reasoning=xhigh data_home=%s", instance.Status, instance.Authentication.Authenticated, len(instance.ModelIDs), len(instance.ReasoningLevels), dataHome)
}

func TestOpenCodeNativeInventory(t *testing.T) {
	if os.Getenv("SECRETARY_OPENCODE_ACP_E2E") != "1" {
		t.Skip("native OpenCode inventory acceptance is opt-in")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Real production discovery seam, private observer only. No Node pairing,
	// service configuration, Worker or Conversation mutation is performed.
	dataHome := filepath.Join(t.TempDir(), "node-native-data")
	if err := EnsurePrivateOpenCodeNativeDataHome(dataHome); err != nil {
		t.Fatal("private native inventory store unavailable")
	}
	inventory, err := (HarnessDiscovery{Node: "private-inventory", Runner: ExecCommandRunner{}, OpenCodeDataHome: dataHome, Probes: []HarnessProbe{{Node: "private-inventory", Runner: ExecCommandRunner{}, Spec: DefaultOpenCodeProbeSpec()}}}).Discover(ctx)
	if err != nil || len(inventory.Instances) != 1 {
		t.Fatal("native Node inventory unavailable")
	}
	instance := inventory.Instances[0]
	var catalog openCodeModelCatalog
	_, _, metadataErr := observeOpenCodeModels(ctx, "opencode", dataHome, false, func(observed openCodeModelCatalog) { catalog = observed })
	if metadataErr != nil {
		t.Fatal("native per-model variant metadata unavailable")
	}
	for _, model := range []core.ObservedModelID{"openai/gpt-6.1-sol", "openai/gpt-6-luna"} {
		valid := instance.ValidateSelection(string(model), "xhigh") == nil && catalog.supportsEffort(string(model), "xhigh")
		t.Logf("safe inventory metadata: model=%s xhigh_observed=%t ready=%t model_count=%d reasoning_count=%d", model, valid, instance.Status == core.HarnessReady, len(instance.ModelIDs), len(instance.ReasoningLevels))
		if !valid {
			t.Fatal("approved model/effort absent from native Node inventory")
		}
	}
	if instance.Capabilities.SupportsActivity(core.ActivityToolCall) || instance.Capabilities.SupportsActivity(core.ActivityToolResult) {
		t.Fatal("native inventory invents explicit tool identity support")
	}
}
