package node

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

type issue209PromptSink struct {
	mu sync.Mutex
	records []map[string]any
	failUser bool
}

func (s *issue209PromptSink) Write(data []byte) (int, error) {
	var message map[string]any
	if err := json.Unmarshal(data, &message); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failUser && message["type"] == "user" {
		return 0, errors.New("Claude stdin write rejected")
	}
	s.records = append(s.records, message)
	return len(data), nil
}

func (*issue209PromptSink) Close() error { return nil }

func (s *issue209PromptSink) Count(kind string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, message := range s.records {
		if message["type"] == kind {
			count++
		}
	}
	return count
}

func issue209DeferredPromptSession(sink io.WriteCloser) *claudeSession {
	return &claudeSession{
		id: "4b22ac0e-109c-4aac-8a17-3e78cba13809",
		stdin: sink,
		writeGate: make(chan struct{}, 1),
		result: make(chan Result, 3),
		claudeRoot: claudeChild{pid: 123},
		initialPromptPending: true,
		cleanupAttempt: func(context.Context, *exec.Cmd, claudeChild) error { return nil },
	}
}

func TestIssue209DeferredClaudePromptNeverWritesAfterAcceptedCancel(t *testing.T) {
	for _, cancelFirst := range []bool{true, false} {
		name := "prompt_is_admitted_first"
		if cancelFirst {
			name = "cancel_is_accepted_first"
		}
		t.Run(name, func(t *testing.T) {
			ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			sink := &issue209PromptSink{}
			session := issue209DeferredPromptSession(sink)
			entered := make(chan struct{})
			release := make(chan struct{})
			if cancelFirst {
				session.beforeInitialPromptAdmission = func() { close(entered); <-release }
			} else {
				session.beforeInitialPromptWrite = func() { close(entered); <-release }
			}
			promptFinished := make(chan error, 1)
			go func() { promptFinished <- session.Prompt(ctx, "start native attempt") }()
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("first Claude prompt did not reach injection barrier")
			}
			cancelFinished := make(chan error, 1)
			go func() { cancelFinished <- session.Cancel(ctx) }()
			if cancelFirst {
				select {
				case err := <-cancelFinished:
					if err != nil {
						t.Fatalf("Cancel before deferred prompt: %v", err)
					}
				case <-ctx.Done():
					t.Fatal("Cancel did not preempt waiting Claude prompt")
				}
				close(release)
				select {
				case err := <-promptFinished:
					if err == nil {
						t.Fatal("native prompt injected after Cancel acceptance")
					}
				case <-ctx.Done():
					t.Fatal("canceled deferred prompt remained blocked")
				}
				if sink.Count("user") != 0 {
					t.Fatal("native user injection occurred after accepted Cancel")
				}
				if err := session.Prompt(ctx, "unexpected replay"); err == nil {
					t.Fatal("a canceled initial Prompt restarted")
				}
			} else {
				select {
				case err := <-cancelFinished:
					t.Fatalf("Cancel accepted before held stdin injection: %v", err)
				case <-time.After(100 * time.Millisecond):
				}
				close(release)
				select {
				case err := <-promptFinished:
					if err != nil {
						t.Fatalf("first Prompt failed: %v", err)
					}
				case <-ctx.Done():
					t.Fatal("first Prompt could not reach stdin")
				}
				select {
				case err := <-cancelFinished:
					if err != nil {
						t.Fatalf("Cancel after injected Prompt failed: %v", err)
					}
				case <-ctx.Done():
					t.Fatal("Cancel blocked after native stdin injection")
				}
				if sink.Count("user") != 1 || sink.Count("control_request") != 1 {
					t.Fatalf("unexpected native injection sequence: user=%d cancel=%d", sink.Count("user"), sink.Count("control_request"))
				}
			}
		})
	}
}

func TestIssue209FailedDeferredClaudePromptDeliversTerminalResult(t *testing.T) {
	sink := &issue209PromptSink{failUser: true}
	session := issue209DeferredPromptSession(sink)
	err := session.Prompt(context.Background(), "native attempt")
	if err == nil || !strings.Contains(err.Error(), "stdin write rejected") {
		t.Fatalf("deferred native stdin write error=%v", err)
	}
	select {
	case result := <-session.Result():
		if result.Status != "failed" || result.FailureCode != "native_prompt_write_failed" {
			t.Fatalf("first deferred Prompt error not terminal: %+v", result)
		}
	default:
		t.Fatal("failed initial Prompt left Attempt without terminal")
	}
	select {
	case result := <-session.Result():
		t.Fatalf("duplicate failed Prompt Result: %+v", result)
	default:
	}
}
