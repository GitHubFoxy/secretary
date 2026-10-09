package node

import (
	"context"
	"os"
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
