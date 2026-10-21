package acp

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

const acpDeadHarnessStderrTail = "error: Cannot find module '@openai/codex/bin/codex.js' from '/$bunfs/root/codex-acp-2.2.2'"

func TestACPDyingHarnessProcess(t *testing.T) {
	scenario := os.Getenv("TEST_ACP_DYING_HARNESS")
	pinned := false
	for _, arg := range os.Args {
		if arg == "-test.run=^TestACPDyingHarnessProcess$" {
			pinned = true
			break
		}
	}
	if !pinned || scenario == "" {
		return
	}
	switch scenario {
	case "stderr-death":
		_, _ = os.Stderr.WriteString("trace banner came first\n")
		_, _ = os.Stderr.WriteString(acpDeadHarnessStderrTail + "\n")
	case "noisy-death":
		_, _ = os.Stderr.WriteString("EARLYMARKER " + strings.Repeat("noise ", 2600) + acpDeadHarnessStderrTail + "\n")
	}
}

type capturingACPFrames struct {
	mu     sync.Mutex
	frames map[string][]string
}

func newCapturingACPFrames() *capturingACPFrames {
	return &capturingACPFrames{frames: make(map[string][]string)}
}

func (w *capturingACPFrames) Write(raw []byte) (int, error) {
	return w.WriteACPFrame("unknown", raw)
}

func (w *capturingACPFrames) WriteACPFrame(direction string, raw []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.frames[direction] = append(w.frames[direction], string(raw))
	return len(raw), nil
}

func (w *capturingACPFrames) joined(direction string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.frames[direction], "\n")
}

func runDeadHarness(t *testing.T, scenario string, log io.Writer) string {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestACPDyingHarnessProcess$")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := StartWithLogEnv(ctx, log, []string{"TEST_ACP_DYING_HARNESS=" + scenario}, command.Path, command.Args[1:]...)
	if err != nil {
		t.Fatal(err)
	}
	var answer map[string]any
	promptErr := client.Request(ctx, "initialize", map[string]any{}, &answer)
	_ = client.Close()
	select {
	case <-client.done:
	case <-time.After(3 * time.Second):
		t.Fatal("dying harness client never finished")
	}
	if promptErr == nil {
		return ""
	}
	return promptErr.Error()
}

func TestStoppedACPHarnessReportsCapturedStderr(t *testing.T) {
	log := newCapturingACPFrames()
	message := runDeadHarness(t, "stderr-death", log)
	if !strings.Contains(message, "acp process stopped") {
		t.Fatalf("dying harness error lost the stopped prefix: %s", message)
	}
	if !strings.Contains(message, "trace banner came first") || !strings.Contains(message, "Cannot find module '@openai/codex/bin/codex.js'") {
		t.Fatalf("dying harness error lost the captured stderr: %s", message)
	}
	stored := log.joined("stderr")
	if !strings.Contains(stored, "Cannot find module '@openai/codex/bin/codex.js'") || !strings.Contains(stored, "trace banner came first") {
		t.Fatalf("raw ACP log did not keep the harness stderr frame with contents: %s", stored)
	}
}

func TestStoppedACPHarnessWithoutStderrKeepsPlainMessage(t *testing.T) {
	message := runDeadHarness(t, "silent-death", nil)
	if message != "acp process stopped" {
		t.Fatalf("silent dying harness error = %q, want %q", message, "acp process stopped")
	}
}

func TestStoppedACPHarnessKeepsOnlyTailOfLargeStderr(t *testing.T) {
	log := newCapturingACPFrames()
	message := runDeadHarness(t, "noisy-death", log)
	if !strings.Contains(message, "Cannot find module '@openai/codex/bin/codex.js'") {
		t.Fatalf("bounded stderr tail lost the final failure line: %.160s", message)
	}
	if strings.Contains(message, "EARLYMARKER") {
		t.Fatalf("error message exceeded the bounded stderr tail: %.160s", message)
	}
	stored := log.joined("stderr")
	if !strings.Contains(stored, "Cannot find module '@openai/codex/bin/codex.js'") {
		t.Fatalf("raw ACP log stderr frame lost the final failure line: %.200s", stored)
	}
	if strings.Contains(stored, "EARLYMARKER") {
		t.Fatalf("raw ACP log stderr frame exceeded the bounded tail: %.200s", stored)
	}
}

func TestCompactACPStderrCollapsesAndTruncates(t *testing.T) {
	compact := compactACPStderr("first line\n\n  second line\n\n")
	if compact != "first line; second line" {
		t.Fatalf("compactACPStderr = %q", compact)
	}
	long := strings.Repeat("rune ", 300)
	if got := compactACPStderr(long); len([]rune(got)) != acpStderrMessageCap+1 || !strings.HasPrefix(got, "…") {
		t.Fatalf("compactACPStderr did not enforce the bounded rune cap: %d runes", len([]rune(got)))
	}
}
