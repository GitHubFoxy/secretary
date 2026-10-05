package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestExactEnvironmentDoesNotRestoreExcludedVariables(t *testing.T) {
	t.Setenv("ACP_PARENT_PRIVATE_MARKER", "must-not-be-inherited")
	for _, exact := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy-overlay", true: "exact"}[exact], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			start := StartWithLogEnvDir
			if exact {
				start = StartWithLogExactEnvDir
			}
			client, err := start(ctx, nil, []string{"ACP_PROCESS_HELPER=1", "ACP_ALLOWED_MARKER=scoped"}, t.TempDir(), os.Args[0], "-test.run=^TestACPEnvironmentHelperProcess$")
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			var result struct {
				Inherited bool `json:"inherited"`
				Allowed   bool `json:"allowed"`
			}
			if err := client.Request(ctx, "initialize", map[string]any{}, &result); err != nil {
				t.Fatal(err)
			}
			if result.Inherited == exact || !result.Allowed {
				t.Fatalf("environment boundary: inherited=%t allowed=%t exact=%t", result.Inherited, result.Allowed, exact)
			}
		})
	}
}

func TestACPEnvironmentHelperProcess(t *testing.T) {
	if os.Getenv("ACP_PROCESS_HELPER") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var message Message
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			os.Exit(1)
		}
		_, inherited := os.LookupEnv("ACP_PARENT_PRIVATE_MARKER")
		response := map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": map[string]bool{"inherited": inherited, "allowed": os.Getenv("ACP_ALLOWED_MARKER") == "scoped"}}
		if json.NewEncoder(os.Stdout).Encode(response) != nil {
			os.Exit(1)
		}
	}
	os.Exit(0)
}
