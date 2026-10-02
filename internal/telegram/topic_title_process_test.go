package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func setupTitleProcessFixture(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Unix title process fixture")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(executable, filepath.Join(bin, "opencode")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OPENCODE_SECRETARY_TITLE_TEST_PEER", "1")
	t.Setenv("SECRETARY_TITLE_TEST_SECRET", "fixture-private")
	t.Setenv("CODEX_CONFIG", "fixture-private")
	t.Setenv("GORACE", "atexit_sleep_ms=0")
}

func TestOpenCodeTitleProcess(t *testing.T) {
	setupTitleProcessFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got, err := (OpenCodeTitleGenerator{Model: "fixture-provider/fixture-model", Reasoning: "minimal", Prompt: "Короткое название."}).Generate(ctx, "Проверь настройки.")
	if err != nil || got != "Проверка настроек" {
		t.Fatalf("title=%q err=%v", got, err)
	}
}

func TestOpenCodeTitleProcessTimeoutKillsHarness(t *testing.T) {
	setupTitleProcessFixture(t)
	t.Setenv("OPENCODE_SECRETARY_TITLE_TEST_MODE", "slow")
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	got, err := (OpenCodeTitleGenerator{Model: "fixture-provider/fixture-model", Reasoning: "minimal", Prompt: "Короткое название."}).Generate(ctx, "Проверь настройки.")
	if err == nil || got != "" || time.Since(start) > 2*time.Second {
		t.Fatalf("title=%q elapsed=%s err=%v", got, time.Since(start), err)
	}
}

func init() {
	if os.Getenv("OPENCODE_SECRETARY_TITLE_TEST_PEER") != "1" {
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("opencode v2.0.22")
		os.Exit(0)
	}
	if !validTitleProcessFixture() {
		os.Exit(1)
	}
	fmt.Fprintln(os.Stderr, "fixture-private diagnostic")
	if os.Getenv("OPENCODE_SECRETARY_TITLE_TEST_MODE") == "slow" {
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(5 * time.Second)
	}
	_, _ = os.Stdout.Write(titleTextFrame("fixture-part", "fixture-message", "Проверка настроек", 1))
	os.Exit(0)
}

func validTitleProcessFixture() bool {
	if os.Getenv("SECRETARY_TITLE_TEST_SECRET") != "" || os.Getenv("CODEX_CONFIG") != "" || os.Getenv("OPENCODE_DISABLE_PROJECT_CONFIG") != "1" {
		return false
	}
	args := strings.Join(os.Args[1:], " ")
	if !strings.Contains(args, "run --standalone --agent "+titleAgent) || !strings.Contains(args, "--model fixture-provider/fixture-model#"+titleVariant) || !strings.Contains(args, "--format json") {
		return false
	}
	body, err := io.ReadAll(os.Stdin)
	var input map[string]string
	if err != nil || json.Unmarshal(body, &input) != nil || len(input) != 1 || input["task_prompt"] != "Проверь настройки." {
		return false
	}
	data, err := os.ReadFile(os.Getenv("OPENCODE_CONFIG"))
	var cfg map[string]any
	if err != nil || json.Unmarshal(data, &cfg) != nil {
		return false
	}
	agent := cfg["agents"].(map[string]any)[titleAgent].(map[string]any)
	permission := agent["permissions"].([]any)[0].(map[string]any)
	model := cfg["providers"].(map[string]any)["fixture-provider"].(map[string]any)["models"].(map[string]any)["fixture-model"].(map[string]any)
	variant := model["variants"].([]any)[0].(map[string]any)
	return agent["system"] == "Короткое название." && agent["steps"] == float64(1) && permission["effect"] == "deny" && permission["action"] == "*" && model["capabilities"].(map[string]any)["tools"] == false && variant["settings"].(map[string]any)["reasoningEffort"] == "minimal"
}
