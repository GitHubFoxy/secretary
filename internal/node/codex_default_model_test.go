package node

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func TestCodexChatGPTDefaultModelConfiguration(t *testing.T) {
	cases := []struct {
		name string
		auth string
		model string
		native string
		want string
		wantError string
	}{
		{name: "ChatGPT default", auth: "chatgpt", model: "default", want: "gpt-5.4"},
		{name: "Secretary without observed auth", model: "default", want: "gpt-5.4"},
		{name: "ChatGPT empty model", auth: "chatgpt", want: "gpt-5.4"},
		{name: "API key native default", auth: "api_key", model: "default"},
		{name: "agent identity native default", auth: "agent_identity", model: "default"},
		{name: "explicit ChatGPT model", auth: "chatgpt", model: "gpt-5.5", want: "gpt-5.5"},
		{name: "explicit API model", auth: "api_key", model: "gpt-5.5", want: "gpt-5.5"},
		{name: "existing native model", auth: "chatgpt", model: "default", native: `{"model":"custom-model"}`, want: "custom-model"},
		{name: "native model is null", auth: "chatgpt", model: "default", native: `{"model":null}`, wantError: "configured model must be a non-empty string"},
		{name: "native model is empty", auth: "chatgpt", model: "default", native: `{"model":""}`, wantError: "configured model must be a non-empty string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var env []string
			if tc.native != "" {
				env = []string{"CODEX_CONFIG=" + tc.native}
			}
			runtime := CodexRuntime{ACPRuntime: ACPRuntime{Command: "fixture", Environment: env}}
			req := StartRequest{
				Profile: ManagedProfile{Name: "secretary", Runtime: "codex", Content: "fixture", Model: tc.model},
				HarnessInstance: core.HarnessInstance{Authentication: core.HarnessAuthentication{Method: tc.auth}},
			}
			configured, err := runtime.configured(req)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("configured error = %v, want %q", err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var model any
			for _, value := range configured.Environment {
				if strings.HasPrefix(value, "CODEX_CONFIG=") {
					var overrides map[string]any
					if err := json.Unmarshal([]byte(strings.TrimPrefix(value, "CODEX_CONFIG=")), &overrides); err != nil {
						t.Fatal(err)
					}
					model = overrides["model"]
				}
			}
			if model != nil && model != tc.want || model == nil && tc.want != "" {
				t.Fatalf("native model = %v, want %q", model, tc.want)
			}
			if tc.model == "gpt-5.5" && configured.ModelSelection != tc.model {
				t.Fatalf("explicit ACP model selection = %q, want %q", configured.ModelSelection, tc.model)
			}
		})
	}
}

func TestCodexChatGPTDefaultModelStartAndResume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runtime := CodexRuntime{ACPRuntime: ACPRuntime{
		Command: os.Args[0],
		Arguments: []string{"-test.run=^TestCodexFixtureProcess$"},
		Environment: []string{"TEST_CODEX_FIXTURE=model-default"},
	}}
	request := StartRequest{
		WorkerRef: "fixture",
		Workspace: t.TempDir(),
		DeferInitialPrompt: true,
		Profile: ManagedProfile{Name: "secretary", Runtime: "codex", Content: "UNIQUE_PROFILE_MARKER", Model: "default"},
	}
	session, err := runtime.Start(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	id := session.ID()
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	resumed, err := runtime.Resume(ctx, request, id)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	if resumed.ID() != id {
		t.Fatalf("resume changed native session from %q to %q", id, resumed.ID())
	}
}

func TestCodexUnsupportedExplicitModelFailsClosed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	runtime := CodexRuntime{ACPRuntime: ACPRuntime{
		Command: os.Args[0],
		Arguments: []string{"-test.run=^TestCodexFixtureProcess$"},
		Environment: []string{"TEST_CODEX_FIXTURE=model-unsupported"},
	}}
	request := StartRequest{
		WorkerRef: "fixture",
		Workspace: t.TempDir(),
		DeferInitialPrompt: true,
		Profile: ManagedProfile{Name: "secretary", Runtime: "codex", Content: "UNIQUE_PROFILE_MARKER", Model: "gpt-5.5"},
	}
	session, err := runtime.Start(ctx, request)
	if session != nil || err == nil || !strings.Contains(err.Error(), "native model/variant selection failed") || !strings.Contains(err.Error(), "model not found: gpt-5.5") {
		t.Fatalf("unsupported native model yielded session=%v, error=%v", session, err)
	}
}
