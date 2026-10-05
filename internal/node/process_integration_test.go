package node

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

// This acceptance test starts two independent secretary-node executables. The
// test does not substitute Daemon goroutines for process boundaries.
func TestTwoSecretaryNodeProcessesPairInventoryAndReconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("process acceptance test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := core.Open(ctx, filepath.Join(t.TempDir(), "secretary.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	manager, err := NewServerManagerWithConfig(ctx, store, ServerConfig{
		PairingTokens: []string{"process-pair-a", "process-pair-b"}, AdminToken: "process-admin",
		HeartbeatTimeout: 2 * time.Second, WatchdogInterval: 100 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/nodes/connect", manager.ServeProtocolHTTP)
	mux.Handle("/v1/nodes", manager)
	mux.Handle("/v1/nodes/", manager)
	server := httptest.NewServer(mux)
	defer server.Close()

	binary := buildNodeBinary(t)
	fakeBin := writeFakeHarnesses(t)
	opencodeProbeMarker := filepath.Join(t.TempDir(), "opencode-probe.log")
	firstDataDir := filepath.Join(t.TempDir(), "process-a")
	secondDataDir := filepath.Join(t.TempDir(), "process-b")
	startAt := func(name, token, dataDir string) *exec.Cmd {
		// This process test covers transport and saved-identity reconnect. Keep
		// native OpenCode probing in its dedicated checks, not in every restart.
		cmd := exec.Command(binary, "--data-dir", dataDir, "--server", server.URL, "--name", name, "--capacity", "2", "--include-opencode=false")
		cmd.Env = append(os.Environ(),
			"SECRETARY_NODE_PAIRING_TOKEN="+token,
			"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
			"SECRETARY_ACP_COMMAND=missing-acp",
			"SECRETARY_OPENCODE_COMMAND="+filepath.Join(fakeBin, "opencode"),
			"TEST_NATIVE_PROBE_MARKER="+opencodeProbeMarker,
		)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		return cmd
	}
	// Keep the two processes on fixed durable directories so one can be restarted.
	first := startAt("process-a", "process-pair-a", firstDataDir)
	second := startAt("process-b", "process-pair-b", secondDataDir)
	defer stopProcess(t, first)
	defer stopProcess(t, second)

	waitFor(t, ctx, "two real Node processes online", func() bool {
		firstStatus, firstErr := manager.Status(ctx, "process-a")
		secondStatus, secondErr := manager.Status(ctx, "process-b")
		return firstErr == nil && secondErr == nil && firstStatus.Online && secondStatus.Online &&
			firstStatus.Inventory.Node == "process-a" && secondStatus.Inventory.Node == "process-b" &&
			inventoryHasHarness(firstStatus.Inventory, core.HarnessFX) && inventoryHasHarness(secondStatus.Inventory, core.HarnessFX)
	})

	if err := first.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := first.Wait(); err != nil {
		t.Fatalf("first Node exit: %v", err)
	}
	waitFor(t, ctx, "first Node reconnect offline transition", func() bool {
		status, err := manager.Status(ctx, "process-a")
		return err == nil && !status.Online
	})
	// The identity file is durable. Restarting without the pairing token proves
	// reconnect uses the saved per-Node credential rather than Client bootstrap.
	first = startAt("process-a", "", firstDataDir)
	defer stopProcess(t, first)
	waitFor(t, ctx, "first Node authenticated reconnect", func() bool {
		status, err := manager.Status(ctx, "process-a")
		return err == nil && status.Online && status.Inventory.Node == "process-a" && inventoryHasHarness(status.Inventory, core.HarnessFX)
	})
	if _, err := os.Stat(opencodeProbeMarker); err == nil {
		t.Fatal("transport integration unexpectedly ran the OpenCode inventory probe")
	} else if !os.IsNotExist(err) {
		t.Fatalf("check OpenCode probe marker: %v", err)
	}
}

func buildNodeBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secretary-node")
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("mise", "exec", "--", "go", "build", "-o", path, "./cmd/secretary-node")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build secretary-node: %v\n%s", err, output)
	}
	return path
}

func writeFakeHarnesses(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
case "$0:$*" in
  *fx*--version*|*claude*--version*|*codex*--version*) echo "fake 1.2.3" ;;
  *fx*models*) echo "models: test-model" ;;
  *claude*auth*) echo '{"loggedIn":true,"authMethod":"test"}' ;;
  *codex*login*) echo "Logged in using an api key" ;;
  *codex*doctor*) echo "ready" ;;
  *codex*debug*) echo "models: test-model"; echo "reasoning: medium" ;;
  *) echo "ready" ;;
esac
`
	for _, name := range []string{"fx", "claude", "codex"} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// This tripwire only records accidental process-level probes. It does not
	// emulate or claim any native OpenCode acceptance behavior.
	opencode := `#!/bin/sh
printf '%s\n' "$*" >> "$TEST_NATIVE_PROBE_MARKER"
case "$*" in
  --version) echo "opencode v2.0.22" ;;
  "auth list") exit 0 ;;
  *) echo "ready" ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte(opencode), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func inventoryHasHarness(inventory core.HarnessInventorySnapshot, kind core.HarnessKind) bool {
	for _, instance := range inventory.Instances {
		if instance.Kind == kind {
			return true
		}
	}
	return false
}

func stopProcess(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Signal(os.Interrupt)
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	select {
	case <-wait:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		<-wait
	}
}
