package node

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReviewClaudePromptCancellationUnderBackpressure(t *testing.T) {
	p := filepath.Join(t.TempDir(), "claude")
	if e := os.WriteFile(p, []byte("#!/bin/sh\nexec sleep 30\n"), 0700); e != nil {
		t.Fatal(e)
	}
	life, kill := context.WithCancel(context.Background())
	defer kill()
	s, e := (ClaudeCodeRuntime{Command: p}).Start(life, StartRequest{Workspace: t.TempDir(), DeferInitialPrompt: true})
	if e != nil {
		t.Fatal(e)
	}
	prompt, done := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer done()
	returned := make(chan error, 1)
	go func() { returned <- s.Prompt(prompt, strings.Repeat("x", 1<<20)) }()
	time.Sleep(100 * time.Millisecond)
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case <-returned:
	case <-time.After(150 * time.Millisecond):
		t.Error("Prompt ignored expired context while native stdin blocked")
	}
	select {
	case <-closed:
	case <-time.After(150 * time.Millisecond):
		t.Error("Close waits on blocked Prompt writeMu and cannot kill process")
	}
	kill()
}

func TestReviewClaudeCloseWithInheritedOutputPipe(t *testing.T) {
	for _, cancelContext := range []bool{false, true} {
		t.Run(map[bool]string{false: "Close", true: "ContextCancellation"}[cancelContext], func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "claude")
			pid := filepath.Join(t.TempDir(), "child.pid")
			body := "#!/bin/sh\nsleep 30 &\necho $! > " + pid + "\nread prompt\nprintf '%s\\n' '{\"type\":\"result\",\"subtype\":\"success\",\"result\":\"done\"}'\nwait\n"
			if err := os.WriteFile(p, []byte(body), 0700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s, err := (ClaudeCodeRuntime{Command: p}).Start(ctx, StartRequest{Workspace: t.TempDir(), Task: "work"})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			select {
			case result := <-s.Result():
				if result.Status != "succeeded" {
					t.Fatalf("terminal %#v", result)
				}
			case <-time.After(time.Second):
				t.Fatal("missing native terminal")
			}
			child, err := os.ReadFile(pid)
			if err != nil {
				t.Fatal(err)
			}
			childID := strings.TrimSpace(string(child))
			defer exec.Command("kill", childID).Run()
			if cancelContext {
				cancel()
				select {
				case _, ok := <-s.Result():
					if ok {
						t.Fatal("duplicate terminal on context cancellation")
					}
				case <-time.After(time.Second):
					t.Fatal("context cancellation leaves inherited output pipes open")
				}
			}
			closed := make(chan error, 1)
			go func() { closed <- s.Close() }()
			select {
			case err := <-closed:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(200 * time.Millisecond):
				t.Fatal("Close blocks on inherited stdout after parent process was killed")
			}
			deadline := time.Now().Add(time.Second)
			for {
				state, err := exec.Command("ps", "-p", childID, "-o", "stat=").Output()
				if err != nil || strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("native child remains alive after shutdown: %s", state)
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
}
