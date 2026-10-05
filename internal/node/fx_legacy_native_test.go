package node

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Opt-in unpaid native fx0.0.8 compatibility smoke. Provider endpoints, HOME,
// config/cache/data and environment are private synthetic fixtures. This proves
// legacy delivery, not progress/final separation or Telegram acceptance.
func TestFXLegacyNativeHTTPFixture(t *testing.T) {
	if os.Getenv("SECRETARY_FX_LEGACY_E2E") != "1" {
		t.Skip("set SECRETARY_FX_LEGACY_E2E=1 for private native fx0.0.8 compatibility check")
	}
	binary, err := exec.LookPath("fx")
	if err != nil {
		t.Fatal("native fx unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	version, err := exec.CommandContext(ctx, binary, "--version").Output()
	if err != nil || strings.TrimSpace(string(version)) != "0.0.8" {
		t.Fatal("native fixture requires fx0.0.8")
	}
	const answer = "native fx fixture line one\nnative fx fixture line two"
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/coding-agent/v1/models" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": "openai/gpt-5", "type": "language", "tags": []string{"tool-use"}}}})
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v3/ai/language-model" {
			http.Error(w, "fixture endpoint unavailable", 404)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		for _, event := range []any{
			map[string]any{"type": "text-delta", "id": "answer_1", "delta": "native fx fixture line one\n"},
			map[string]any{"type": "text-delta", "id": "answer_1", "delta": "native fx fixture line two"},
			map[string]any{"type": "finish", "finishReason": map[string]string{"unified": "stop", "raw": "stop"}, "usage": map[string]any{"inputTokens": map[string]int{"total": 3}, "outputTokens": map[string]int{"total": 5}}},
		} {
			body, _ := json.Marshal(event)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", body)
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	root := t.TempDir()
	home, workspace := filepath.Join(root, "home"), filepath.Join(root, "workspace")
	for _, directory := range []string{filepath.Join(home, ".fx"), workspace, filepath.Join(root, "tmp"), filepath.Join(root, "config"), filepath.Join(root, "cache"), filepath.Join(root, "data")} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal("fixture directories unavailable")
		}
	}
	environment := []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "TMPDIR=" + filepath.Join(root, "tmp"),
		"XDG_CONFIG_HOME=" + filepath.Join(root, "config"), "XDG_CACHE_HOME=" + filepath.Join(root, "cache"), "XDG_DATA_HOME=" + filepath.Join(root, "data"),
		"NO_COLOR=1", "FX_AUTO_UPGRADE=0", "FX_MODEL=openai/gpt-5", "AI_GATEWAY_API_KEY=fake-native-fixture-key", "VERCEL_OIDC_TOKEN=",
		"FX_GATEWAY_BASE_URL=" + server.URL, "FX_GATEWAY_CHAT_URL=" + server.URL + "/v3/ai/language-model",
	}
	session, err := (FXRuntime{ACPRuntime: ACPRuntime{Command: binary, Arguments: []string{"acp"}, Environment: environment, ExactEnvironment: true}}).Start(ctx, StartRequest{WorkerRef: "private-native-fx", Workspace: workspace, DeferInitialPrompt: true})
	if err != nil {
		t.Fatal("native fx startup failed")
	}
	defer session.Close()
	base := session.(*fxSession).acpSession
	if base.terminalMessageGrouping || base.drainPromptEvents {
		t.Fatal("native fx unexpectedly opted into OpenCode contract")
	}
	if err := base.client.Request(ctx, "session/set_mode", map[string]string{"sessionId": session.ID(), "modeId": "code"}, &map[string]any{}); err != nil {
		t.Fatal("native fixture code mode unavailable")
	}
	for attempt := 0; attempt < 2; attempt++ {
		done := make(chan error, 1)
		go func() { done <- session.Prompt(ctx, "Return only a synthetic fixture answer.") }()
		var result Result
		for result.Summary == "" {
			select {
			case activity := <-session.Activity():
				if activity.Kind == ActivityPermission {
					if err := session.(Responder).Respond(ctx, activity.RequestID, "denied"); err != nil {
						t.Fatal("native fixture reverse request failed")
					}
				}
			case result = <-session.Result():
			case <-ctx.Done():
				t.Fatal("native fx terminal report timed out")
			}
		}
		waitLegacyPrompt(t, ctx, session, done)
		if result.Status != "succeeded" || result.Summary != answer {
			t.Fatalf("safe native fx metadata: attempt=%d status=%s expected_report=%t summary_bytes=%d", attempt, result.Status, result.Summary == answer, len(result.Summary))
		}
		t.Logf("safe native fx metadata: attempt=%d status=%s expected_report=true summary_bytes=%d strict_grouping=false", attempt, result.Status, len(result.Summary))
	}
	if calls.Load() < 2 {
		t.Fatal("native fx did not use local synthetic provider for both attempts")
	}
	t.Logf("safe provider metadata: local_fixture_calls=%d private_environment=true", calls.Load())
}
