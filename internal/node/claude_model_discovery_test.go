package node

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

func claudeModelProbeFixture(t *testing.T, response string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix native executable fixture")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "claude-fixture")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then printf '%s\\n' '2.1.306'; exit 0; fi\n" +
		"if [ \"$1\" = \"auth\" ] && [ \"$2\" = \"status\" ]; then printf '%s\\n' '{\"loggedIn\":true,\"authMethod\":\"credential\"}'; exit 0; fi\n" +
		"case \" $* \" in *' --input-format stream-json '*' --output-format stream-json '*) ;; *) exit 15 ;; esac\n" +
		"if [ \"$CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY\" != \"1\" ]; then exit 16; fi\n" +
		"IFS= read -r request || exit 17\n" +
		"case \"$request\" in *secretary-model-inventory*initialize*) ;; *) exit 18 ;; esac\n" +
		"printf '%s\\n' " + shellQuoteClaudeProbe(response) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func shellQuoteClaudeProbe(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func TestClaudeNativeDiscoveryReachesWorkerPinContract(t *testing.T) {
	t.Setenv("CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY", "0")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "synthetic-test-credential")
	response := `{"type":"control_response","response":{"request_id":"secretary-model-inventory","subtype":"success","response":{"models":[{"value":"haiku","resolvedModel":"claude-haiku-5-5"},{"value":"sonnet","resolvedModel":"claude-sonnet-5"},{"value":"haiku","resolvedModel":"claude-haiku-5-5"}]}}}`
	command := claudeModelProbeFixture(t, response)
	spec := DefaultClaudeCodeProbeSpec()
	spec.Binary = command
	result := (HarnessProbe{Node: "macbook", Runner: ExecCommandRunner{}, Spec: spec}).Probe(context.Background())
	if !result.Available() || !result.Instance.SupportsModel("claude-haiku-5-5") {
		t.Fatalf("native Claude inventory failed: status=%s error=%s models=%v", result.Instance.Status, result.ErrorCode, result.Instance.ModelIDs)
	}
	if len(result.Instance.ModelIDs) != 2 {
		t.Fatalf("concrete observed models: %v", result.Instance.ModelIDs)
	}
	inventory := core.HarnessInventorySnapshot{Node: "macbook", Instances: []core.HarnessInstance{result.Instance}, ObservedAt: time.Now().UTC()}
	envelope := WorkerEnvelope{WorkerRef: "worker", TurnID: "turn", AttemptID: "attempt", OriginalUserIntent: "inspect", HarnessInstance: result.Instance, Model: "claude-haiku-5-5", Profile: workerTemplateFixture(result.Instance, "claude-haiku-5-5", "")}
	if err := envelope.ValidateAgainstInventory("macbook", inventory); err != nil {
		t.Fatalf("observed Claude dispatch pin rejected: %v", err)
	}
	envelope.Model = "claude-haiku-unobserved"
	envelope.Profile = workerTemplateFixture(result.Instance, envelope.Model, "")
	if err := envelope.ValidateAgainstInventory("macbook", inventory); !errors.Is(err, core.ErrObservedPinUnavailable) {
		t.Fatalf("unobserved model pin accepted: %v", err)
	}
}

func TestClaudeNativeDiscoveryFailsClosed(t *testing.T) {
	cases := []struct {
		name string
		response string
	}{
		{"no models", `{"type":"control_response","response":{"request_id":"secretary-model-inventory","subtype":"success","response":{"models":[]}}}`},
		{"native rejection", `{"type":"control_response","response":{"request_id":"secretary-model-inventory","subtype":"error","error":"sensitive-credential-must-not-surface"}}`},
		{"malformed", `{"type":"control_response","response":{"request_id":"secretary-model-inventory","subtype":"success","response":{"models":"unexpected"}}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := DefaultClaudeCodeProbeSpec()
			spec.Binary = claudeModelProbeFixture(t, tc.response)
			result := (HarnessProbe{Node: "macbook", Runner: ExecCommandRunner{}, Spec: spec}).Probe(context.Background())
			if result.Instance.Status != core.HarnessDegraded || result.ErrorCode != "native_models_probe_failed" || len(result.Instance.ModelIDs) != 0 {
				t.Fatalf("invalid native discovery advertised models: status=%s error=%s models=%v", result.Instance.Status, result.ErrorCode, result.Instance.ModelIDs)
			}
			if strings.Contains(result.Err.Error(), "sensitive-credential") {
				t.Fatal("native model discovery exposed raw failure payload")
			}
		})
	}
}

func TestClaudeNativeDiscoveryTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix native executable fixture")
	}
	path := filepath.Join(t.TempDir(), "claude-slow-fixture")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 2.1.306; exit 0; fi\nif [ \"$1\" = \"auth\" ]; then echo '{\"loggedIn\":true}'; exit 0; fi\nsleep 5\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	spec := DefaultClaudeCodeProbeSpec()
	spec.Binary = path
	spec.StepTimeout = 50 * time.Millisecond
	start := time.Now()
	result := (HarnessProbe{Node: "macbook", Runner: ExecCommandRunner{}, Spec: spec}).Probe(context.Background())
	if result.Instance.Status != core.HarnessDegraded || len(result.Instance.ModelIDs) != 0 || result.ErrorCode != "native_models_probe_failed" {
		t.Fatalf("timed-out discovery advertised models: %#v", result)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("native model discovery did not respect timeout")
	}
}
