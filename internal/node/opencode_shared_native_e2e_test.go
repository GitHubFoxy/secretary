package node

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type nativePublicSetup struct {
	Home             string
	DataHome         string
	PersonalDataHome string
	PersonalCanary   string
	CommandLog       string
}

func runPublicNativeSetup(t *testing.T, root, opencodeBinary string) nativePublicSetup {
	t.Helper()
	_, sourcePath, _, ok := goruntime.Caller(0)
	if !ok {
		t.Fatal("could not locate repository for public setup")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(sourcePath), "..", ".."))
	home := filepath.Join(root, "public-setup-home")
	personalDataHome := filepath.Join(root, "personal-data")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal("private public-setup HOME unavailable")
	}
	versionCommand := exec.Command(opencodeBinary, "--version")
	versionCommand.Env = []string{"HOME=" + home, "XDG_DATA_HOME=" + filepath.Join(root, "version-data"), "XDG_CONFIG_HOME=" + filepath.Join(root, "version-config"), "XDG_STATE_HOME=" + filepath.Join(root, "version-state"), "PATH=" + os.Getenv("PATH")}
	version, versionErr := versionCommand.CombinedOutput()
	if versionErr != nil || !strings.Contains(string(version), "opencode v2.0.22") {
		t.Fatal("native acceptance requires real OpenCode v2.0.22")
	}
	personalCanary := filepath.Join(personalDataHome, "opencode", "opencode.db")
	commandLog := filepath.Join(root, "native-setup-commands.log")
	if err := os.MkdirAll(filepath.Dir(personalCanary), 0o700); err != nil {
		t.Fatal("private personal-store canary directory unavailable")
	}
	if err := os.WriteFile(personalCanary, []byte("untouched-personal-store"), 0o600); err != nil {
		t.Fatal("private personal-store canary unavailable")
	}
	cliWrapper := filepath.Join(root, "real-opencode-cli-wrapper")
	wrapperScript := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + shellQuoteForTest(commandLog) + "\nexec " + shellQuoteForTest(opencodeBinary) + " \"$@\"\n"
	if err := os.WriteFile(cliWrapper, []byte(wrapperScript), 0o700); err != nil {
		t.Fatal("private OpenCode command wrapper unavailable")
	}
	miseDataDir := os.Getenv("MISE_DATA_DIR")
	if miseDataDir == "" {
		miseDataDir = filepath.Join(os.Getenv("HOME"), ".local", "share", "mise")
	}
	moduleCache := os.Getenv("GOMODCACHE")
	if moduleCache == "" {
		moduleCache = filepath.Join(os.Getenv("HOME"), "go", "pkg", "mod")
	}
	goCache := os.Getenv("GOCACHE")
	if goCache == "" {
		cacheHome, err := os.UserCacheDir()
		if err != nil {
			t.Fatal("Go build cache unavailable")
		}
		goCache = filepath.Join(cacheHome, "go-build")
	}
	pathValue := os.Getenv("PATH")
	if pathValue == "" {
		pathValue = "/usr/bin:/bin:/opt/homebrew/bin:/usr/local/bin"
	}
	env := []string{
		"PATH=" + pathValue,
		"HOME=" + home,
		"XDG_DATA_HOME=" + personalDataHome,
		"XDG_CONFIG_HOME=" + filepath.Join(root, "personal-config"),
		"XDG_STATE_HOME=" + filepath.Join(root, "personal-state"),
		"MISE_DATA_DIR=" + miseDataDir,
		"MISE_TRUSTED_CONFIG_PATHS=" + filepath.Join(repoRoot, ".mise.toml"),
		"MISE_YES=1",
		"GOMODCACHE=" + moduleCache,
		"GOCACHE=" + goCache,
		"SECRETARY_OPENCODE_COMMAND=" + cliWrapper,
		"TICKET33_NATIVE_SETUP_LOG=" + commandLog,
		"TMPDIR=" + os.TempDir(),
	}
	for _, key := range []string{"MISE_INSTALLS_DIR", "MISE_CACHE_DIR", "GOFLAGS", "GOTOOLCHAIN"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	cmd := exec.Command(filepath.Join(repoRoot, "secretary"), "setup")
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "Setup complete") {
		t.Fatal("public secretary setup did not complete native store initialization")
	}
	dataHome := filepath.Join(home, ".local", "share", "secretary", "opencode-native")
	database := filepath.Join(dataHome, "opencode", "opencode.db")
	info, err := os.Lstat(database)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		t.Fatal("public secretary setup did not create a private native database")
	}
	if _, err := os.Lstat(filepath.Join(dataHome, "opencode", "auth.json")); !os.IsNotExist(err) {
		t.Fatal("public native setup unexpectedly performed provider login")
	}
	commands, err := os.ReadFile(commandLog)
	if err != nil || strings.Count(string(commands), "serve --port 0 --stdio") != 1 || strings.Contains(string(commands), "auth list") || strings.Contains(string(commands), "auth login") {
		t.Fatal("public setup did not use exactly one local native DB initialization without auth commands")
	}
	canary, err := os.ReadFile(personalCanary)
	if err != nil || string(canary) != "untouched-personal-store" {
		t.Fatal("public native setup touched the personal OpenCode store")
	}
	return nativePublicSetup{Home: home, DataHome: dataHome, PersonalDataHome: personalDataHome, PersonalCanary: personalCanary, CommandLog: commandLog}
}

func shellQuoteForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestOpenCodeNativeSetupCreatesDatabaseWithoutAuth(t *testing.T) {
	if os.Getenv("SECRETARY_OPENCODE_ACP_E2E") != "1" {
		t.Skip("native OpenCode setup acceptance is opt-in")
	}
	binary, err := exec.LookPath("opencode")
	if err != nil {
		t.Fatal("native OpenCode unavailable")
	}
	setup := runPublicNativeSetup(t, t.TempDir(), binary)
	if setup.DataHome == "" {
		t.Fatal("public native setup did not return its selected store")
	}
	t.Log("safe setup metadata: public_setup=true native_db=true provider_login=false personal_store_unchanged=true")
}

// Both actual OpenCode ACP processes use the DB created by public secretary setup
// and make concurrent turns. The local unpaid HTTP provider checks safe
// metadata only and never logs prompts, tool arguments, or response payloads.
func TestOpenCodeNativeSharedStoreConcurrentSecretaryAndWorker(t *testing.T) {
	oldUmask := syscall.Umask(0o077)
	defer syscall.Umask(oldUmask)
	if os.Getenv("SECRETARY_OPENCODE_ACP_E2E") != "1" {
		t.Skip("native OpenCode shared-store acceptance is opt-in")
	}
	binary, err := exec.LookPath("opencode")
	if err != nil {
		t.Fatal("native OpenCode unavailable")
	}
	root := t.TempDir()
	setup := runPublicNativeSetup(t, root, binary)
	dataHome := setup.DataHome
	personalDataHome := setup.PersonalDataHome
	personalCanary := setup.PersonalCanary
	t.Setenv("HOME", setup.Home)
	t.Setenv("XDG_DATA_HOME", personalDataHome)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("FIXTURE_API_KEY", "fixture-key-never-log")
	t.Setenv("TEST_OPENCODE_WRAPPER", "1")
	t.Setenv("TEST_NATIVE_OPENCODE", binary)
	wrapper, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Fatal("private native wrapper unavailable")
	}

	workerNonceBytes := make([]byte, 16)
	if _, err := rand.Read(workerNonceBytes); err != nil {
		t.Fatal("private fixture nonce unavailable")
	}
	workerNonce := hex.EncodeToString(workerNonceBytes)
	workerWorkspace := filepath.Join(root, "worker-workspace")
	secretaryWorkspace := filepath.Join(root, "secretary-workspace")
	for _, workspace := range []string{workerWorkspace, secretaryWorkspace} {
		if err := os.MkdirAll(workspace, 0o700); err != nil {
			t.Fatal("private role workspace unavailable")
		}
	}
	if err := os.WriteFile(filepath.Join(workerWorkspace, "fixture.txt"), []byte(workerNonce), 0o600); err != nil {
		t.Fatal("private Worker fixture file unavailable")
	}
	mcpState := filepath.Join(root, "secretary-mcp-state")
	secretaryMCP := MCPServer{Name: "secretary", Command: wrapper, Args: []string{"-test.run=^TestOpenCodeConfigWrapperProcess$", "--", "mcp-server"}, Env: []MCPEnv{
		{Name: "SECRETARY_MCP_DATA_DIR", Value: mcpState},
		{Name: "SECRETARY_MCP_CAPABILITY", Value: "native-fixture-capability"},
	}}

	type roleFixture struct {
		name, model, reasoning, profile, tool, arguments, workspace string
		mcpServers                                                  []MCPServer
		checks                                                      atomic.Int32
	}
	worker := &roleFixture{name: "worker", model: "concurrent-worker-model", reasoning: "low", profile: "private-concurrent-worker-profile-marker", tool: "read", arguments: `{"path":"fixture.txt"}`, workspace: workerWorkspace}
	secretary := &roleFixture{name: "secretary", model: "concurrent-secretary-model", reasoning: "high", profile: "private-concurrent-secretary-profile-marker", tool: "secretary_list_workers", arguments: `{}`, workspace: secretaryWorkspace, mcpServers: []MCPServer{secretaryMCP}}
	roles := map[string]*roleFixture{worker.model: worker, secretary.model: secretary}
	var policyFailure atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			http.Error(w, "invalid fixture", http.StatusBadRequest)
			return
		}
		messages, _ := body["messages"].([]any)
		tools, _ := body["tools"].([]any)
		if len(tools) == 0 {
			writeFixtureCompletion(w, "fixture-title", "Fixture title")
			return
		}
		role, ok := roles[fmt.Sprint(body["model"])]
		text := fixtureMessageText(messages)
		encoded, _ := json.Marshal(body)
		other := worker
		if role == worker {
			other = secretary
		}
		valid := ok && body["reasoning_effort"] == role.reasoning && strings.Contains(text, role.profile) && !strings.Contains(text, other.profile) && !strings.Contains(text, "worker-concurrent-result") && !strings.Contains(text, "secretary-concurrent-result") && !strings.Contains(string(encoded), "native-fixture-capability") && len(tools) == 1
		for _, raw := range tools {
			tool, _ := raw.(map[string]any)
			function, _ := tool["function"].(map[string]any)
			valid = valid && function["name"] == role.tool
		}
		if !valid {
			policyFailure.Store(true)
			writeFixtureCompletion(w, "fixture-invalid", "fixture-invalid-policy")
			return
		}
		role.checks.Add(1)
		toolResult := false
		for _, raw := range messagesAfterLastUser(messages) {
			message, _ := raw.(map[string]any)
			if message["role"] != "tool" {
				continue
			}
			toolText := fixtureMessageText([]any{message})
			if role == worker {
				toolResult = toolResult || strings.Contains(toolText, workerNonce)
			} else {
				toolResult = toolResult || strings.Contains(toolText, `"workers":[]`)
			}
		}
		if !toolResult {
			writeFixtureToolCall(w, "allowed-"+role.name, role.tool, role.arguments)
			return
		}
		writeFixtureCompletion(w, "fixture-final", role.name+"-concurrent-result")
	}))
	defer provider.Close()
	t.Setenv("TEST_FIXTURE_URL", provider.URL+"/v1")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	runtime := OpenCodeRuntime{Command: wrapper, Arguments: []string{"-test.run=^TestOpenCodeConfigWrapperProcess$", "--", "acp"}, DataHome: dataHome}
	type startedRole struct {
		fixture *roleFixture
		session Session
		err     error
	}
	started := make(chan startedRole, 2)
	for _, fixture := range []*roleFixture{worker, secretary} {
		fixture := fixture
		go func() {
			profile := ManagedProfile{Name: fixture.name, Content: fixture.profile, AllowTools: []string(nil), Model: "fixture/" + fixture.model, Reasoning: fixture.reasoning}
			if fixture == worker {
				profile.AllowTools = []string{"read"}
			}
			profile.Hash = HashProfile(profile.Content, nil, profile.Model, profile.Reasoning)
			session, err := runtime.Start(ctx, StartRequest{WorkerRef: "shared-" + fixture.name, Workspace: fixture.workspace, Profile: profile, MCPServers: fixture.mcpServers, DeferInitialPrompt: true})
			started <- startedRole{fixture: fixture, session: session, err: err}
		}()
	}
	sessions := make(map[string]Session, 2)
	for range 2 {
		select {
		case result := <-started:
			if result.err != nil {
				for _, marker := range []string{"model not found", "mode not found", "invalid params", "variant", "effort", "initialize", "process stopped", "deadline", "unauthorized", "401", "permission", "database is locked", "busy"} {
					if strings.Contains(strings.ToLower(result.err.Error()), marker) {
						t.Logf("safe concurrent startup error category: role=%s category=%s", result.fixture.name, marker)
					}
				}
				for _, session := range sessions {
					_ = session.Close()
				}
				t.Fatal("concurrent native role session startup failed")
			}
			sessions[result.fixture.name] = result.session
		case <-ctx.Done():
			t.Fatal("concurrent native role session startup timed out")
		}
	}
	defer func() {
		for _, session := range sessions {
			_ = session.Close()
		}
	}()
	if sessions[worker.name].ID() == sessions[secretary.name].ID() {
		t.Fatal("concurrent Secretary and Worker reused the same native session ID")
	}

	type completedRole struct {
		name   string
		result Result
		err    error
	}
	completed := make(chan completedRole, 2)
	for _, fixture := range []*roleFixture{worker, secretary} {
		fixture := fixture
		go func() {
			result, err := awaitNativeFixtureTurn(ctx, sessions[fixture.name], "Run your role-specific native fixture tool once.")
			completed <- completedRole{name: fixture.name, result: result, err: err}
		}()
	}
	for range 2 {
		select {
		case result := <-completed:
			if result.err != nil || result.result.Status != "succeeded" || result.result.Summary != result.name+"-concurrent-result" {
				t.Fatalf("concurrent native role result failed: role=%s status=%s", result.name, result.result.Status)
			}
		case <-ctx.Done():
			t.Fatal("concurrent native role turns timed out")
		}
	}
	if policyFailure.Load() || worker.checks.Load() < 2 || secretary.checks.Load() < 2 {
		t.Fatal("shared DB mixed managed Profiles, model/reasoning, role permissions, or history")
	}
	if _, err := os.Stat(filepath.Join(dataHome, "opencode", "opencode.db")); err != nil {
		t.Fatal("shared native DB did not retain both role sessions")
	}
	if err := checkPrivateNativeStorePermissions(dataHome); err != nil {
		t.Fatal("shared native DB or its sidecars are not private")
	}
	canary, err := os.ReadFile(personalCanary)
	if err != nil || string(canary) != "untouched-personal-store" {
		t.Fatal("concurrent native roles touched the personal OpenCode store")
	}
	t.Logf("safe shared-store metadata: concurrent_sessions=true role_profiles_separate=true mcp_scope_narrow=true worker_tools_narrow=true model_reasoning_separate=true history_separate=true personal_store_unchanged=true")
}

func awaitNativeFixtureTurn(ctx context.Context, session Session, prompt string) (Result, error) {
	promptDone := make(chan error, 1)
	go func() { promptDone <- session.Prompt(ctx, prompt) }()
	activityCh := session.Activity()
	var promptDoneCh <-chan error = promptDone
	for {
		select {
		case activity, ok := <-activityCh:
			if !ok {
				activityCh = nil
				continue
			}
			if activity.Kind == ActivityPermission {
				return Result{}, errors.New("native fixture unexpectedly requested permission")
			}
		case err := <-promptDoneCh:
			if err != nil {
				return Result{}, errors.New("native fixture prompt failed")
			}
			promptDoneCh = nil
		case result, ok := <-session.Result():
			if !ok {
				return Result{}, errors.New("native fixture closed without Result")
			}
			if promptDoneCh != nil {
				select {
				case err := <-promptDoneCh:
					if err != nil {
						return Result{}, errors.New("native fixture prompt failed")
					}
				case <-ctx.Done():
					return Result{}, ctx.Err()
				}
			}
			return result, nil
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
}
