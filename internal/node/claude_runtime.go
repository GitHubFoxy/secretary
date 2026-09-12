package node

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/beruseruko/secretary/internal/core"
)

// ErrClaudeCodeUnavailable identifies a Claude Code process that cannot be
// started. It is deliberately distinct from Codex ACP errors so callers never
// turn a missing Claude executable into a different harness execution.
var ErrClaudeCodeUnavailable = errors.New("claude_code runtime unavailable")

// ClaudeCodeRuntime runs the real Claude Code CLI in its documented headless
// print mode with stream-json output. It is intentionally not an ACP adapter.
type ClaudeCodeRuntime struct {
	Command        string
	Arguments      []string
	Environment    []string
	RawLogDir      string
	RawLogMaxBytes int64
	RawLogFiles    int
}

// ClaudeRuntime is kept as a descriptive compatibility name for the adapter.
type ClaudeRuntime = ClaudeCodeRuntime

func (r ClaudeCodeRuntime) Start(ctx context.Context, request StartRequest) (Session, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	if err := validateClaudeRequest(request); err != nil {
		return nil, err
	}
	id, err := newClaudeSessionID()
	if err != nil {
		return nil, fmt.Errorf("%w: create session id: %v", ErrClaudeCodeUnavailable, err)
	}
	return r.startProcess(ctx, request, id, r.arguments(request, id, false))
}

func (r ClaudeCodeRuntime) Resume(ctx context.Context, request StartRequest, runtimeSessionID string) (Session, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	if err := validateClaudeRequest(request); err != nil {
		return nil, err
	}
	if strings.TrimSpace(runtimeSessionID) == "" {
		return nil, fmt.Errorf("%w: runtime session id is required", ErrClaudeCodeUnavailable)
	}
	return r.startProcess(ctx, request, runtimeSessionID, r.arguments(request, runtimeSessionID, true))
}

func validateClaudeRequest(request StartRequest) error {
	if request.HarnessInstance.Kind != "" && request.HarnessInstance.Kind != core.HarnessClaudeCode {
		return fmt.Errorf("node: Claude Code runtime cannot execute harness %q", request.HarnessInstance.Kind)
	}
	if request.HarnessInstance.Kind == core.HarnessClaudeCode && !request.HarnessInstance.Available() {
		if !request.HarnessInstance.Authentication.Authenticated {
			return fmt.Errorf("%w: Claude Code is not authenticated", ErrClaudeCodeUnavailable)
		}
		return fmt.Errorf("%w: Claude Code HarnessInstance %s is not ready", ErrClaudeCodeUnavailable, request.HarnessInstance.ID)
	}
	if request.Workspace == "" {
		return fmt.Errorf("%w: workspace is required", ErrClaudeCodeUnavailable)
	}
	if request.Task == "" && request.HarnessInstance.Kind != "" {
		return fmt.Errorf("%w: task is required", ErrClaudeCodeUnavailable)
	}
	return nil
}

func (r ClaudeCodeRuntime) command() string {
	if strings.TrimSpace(r.Command) == "" {
		return "claude"
	}
	return r.Command
}

func (r ClaudeCodeRuntime) arguments(request StartRequest, sessionID string, resume bool) []string {
	args := append([]string(nil), r.Arguments...)
	appendFlag := func(flag string) {
		for _, arg := range args {
			if arg == flag {
				return
			}
		}
		args = append(args, flag)
	}
	appendOption := func(flag, value string) {
		for i, arg := range args {
			if arg == flag {
				if i+1 < len(args) {
					return
				}
			}
		}
		args = append(args, flag, value)
	}

	appendFlag("--print")
	appendOption("--output-format", "stream-json")
	appendFlag("--verbose")
	if request.Model != "" && !isModelAlias(request.Model) {
		appendOption("--model", request.Model)
	}
	if request.Reasoning != "" && request.Reasoning != "default" {
		appendOption("--effort", request.Reasoning)
	}
	if resume {
		appendOption("--resume", sessionID)
	} else {
		appendOption("--session-id", sessionID)
	}
	if request.Task != "" {
		args = append(args, request.Task)
	}
	return args
}

func (r ClaudeCodeRuntime) startProcess(ctx context.Context, request StartRequest, sessionID string, args []string) (*claudeSession, error) {
	command := r.command()
	if _, err := exec.LookPath(command); err != nil {
		return nil, fmt.Errorf("%w: executable %q is unavailable: %v", ErrClaudeCodeUnavailable, command, err)
	}
	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = request.Workspace
	cmd.Env = append(os.Environ(), r.Environment...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%w: open stdout: %v", ErrClaudeCodeUnavailable, err)
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: start %q: %v", ErrClaudeCodeUnavailable, command, err)
	}
	session := &claudeSession{
		id:       sessionID,
		cmd:      cmd,
		stdout:   stdout,
		stderr:   stderr,
		activity: make(chan Activity, 64),
		result:   make(chan Result, 16),
	}
	go session.run()
	return session, nil
}

type claudeSession struct {
	id       string
	cmd      *exec.Cmd
	stdout   io.ReadCloser
	stderr   *bytes.Buffer
	activity chan Activity
	result   chan Result

	stateMu    sync.Mutex
	closed     bool
	cancel     bool
	seenResult bool
	text       strings.Builder
}

func (s *claudeSession) ID() string                { return s.id }
func (s *claudeSession) Activity() <-chan Activity { return s.activity }
func (s *claudeSession) Result() <-chan Result     { return s.result }

func (s *claudeSession) Prompt(context.Context, string) error {
	return errors.New("claude: print session requires a new Attempt for another prompt")
}

func (s *claudeSession) Steer(context.Context, string) (bool, error) {
	return false, errors.New("claude: print session does not support steering")
}

func (s *claudeSession) Queue(context.Context, string) error {
	return errors.New("claude: print session requires a new Attempt for a queued prompt")
}

func (s *claudeSession) Cancel(context.Context) error {
	s.stateMu.Lock()
	if s.closed {
		s.stateMu.Unlock()
		return nil
	}
	s.cancel = true
	s.closed = true
	s.stateMu.Unlock()
	return s.cmd.Process.Kill()
}

func (s *claudeSession) Close() error {
	s.stateMu.Lock()
	if s.closed {
		s.stateMu.Unlock()
		return nil
	}
	s.closed = true
	s.stateMu.Unlock()
	return s.cmd.Process.Kill()
}

func (s *claudeSession) run() {
	defer close(s.activity)
	defer close(s.result)
	scanner := bufio.NewScanner(s.stdout)
	for scanner.Scan() {
		s.handleLine(scanner.Bytes())
	}
	waitErr := s.cmd.Wait()
	s.stateMu.Lock()
	canceled := s.cancel
	seenResult := s.seenResult
	s.stateMu.Unlock()
	if !seenResult {
		status := "failed"
		if canceled {
			status = "canceled"
		}
		summary := strings.TrimSpace(s.stderr.String())
		if summary == "" && waitErr != nil {
			summary = waitErr.Error()
		}
		if summary == "" {
			summary = "Claude Code exited without a result"
		}
		s.emitResult(Result{Status: status, Summary: summary})
	}
}

func (s *claudeSession) handleLine(line []byte) {
	var record map[string]any
	if json.Unmarshal(line, &record) != nil {
		text := strings.TrimSpace(string(line))
		if text != "" {
			s.emitText(text)
		}
		return
	}
	switch fmt.Sprint(record["type"]) {
	case "system":
		return
	case "stream_event":
		if event, ok := record["event"].(map[string]any); ok {
			s.handleStreamEvent(event)
		}
	case "assistant":
		if message, ok := record["message"].(map[string]any); ok {
			s.handleContent(message["content"])
		}
	case "result":
		summary := valueString(record["result"])
		if summary == "" {
			s.stateMu.Lock()
			summary = strings.TrimSpace(s.text.String())
			s.stateMu.Unlock()
		}
		if summary == "" {
			summary = "completed"
		}
		status := "succeeded"
		if subtype := strings.ToLower(valueString(record["subtype"])); subtype != "" && subtype != "success" {
			status = "failed"
		}
		if isError, ok := record["is_error"].(bool); ok && isError {
			status = "failed"
		}
		s.emitResult(Result{Status: status, Summary: summary})
	case "error":
		summary := valueString(record["error"])
		if summary == "" {
			summary = valueString(record["message"])
		}
		s.emitResult(Result{Status: "failed", Summary: summary})
	}
}

func (s *claudeSession) handleStreamEvent(event map[string]any) {
	switch valueString(event["type"]) {
	case "content_block_delta":
		delta, _ := event["delta"].(map[string]any)
		if valueString(delta["type"]) == "text_delta" {
			s.emitText(valueString(delta["text"]))
		}
	case "content_block_start":
		block, _ := event["content_block"].(map[string]any)
		if valueString(block["type"]) == "tool_use" {
			s.emitActivity(Activity{Kind: ActivityTool, Text: valueString(block["name"])})
		}
	}
}

func (s *claudeSession) handleContent(content any) {
	items, _ := content.([]any)
	for _, item := range items {
		block, _ := item.(map[string]any)
		switch valueString(block["type"]) {
		case "text":
			s.emitText(valueString(block["text"]))
		case "tool_use":
			s.emitActivity(Activity{Kind: ActivityTool, Text: valueString(block["name"])})
		}
	}
}

func (s *claudeSession) emitText(text string) {
	if text == "" {
		return
	}
	s.stateMu.Lock()
	s.text.WriteString(text)
	s.stateMu.Unlock()
	s.emitActivity(Activity{Kind: ActivityText, Text: text})
}

func (s *claudeSession) emitActivity(activity Activity) {
	if strings.TrimSpace(activity.Text) == "" {
		return
	}
	select {
	case s.activity <- activity:
	default:
	}
}

func (s *claudeSession) emitResult(result Result) {
	s.stateMu.Lock()
	if s.seenResult {
		s.stateMu.Unlock()
		return
	}
	s.seenResult = true
	s.stateMu.Unlock()
	s.result <- result
}

func valueString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func newClaudeSessionID() (string, error) {
	var data [16]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(data[:])
	return strings.Join([]string{encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32]}, "-"), nil
}

var _ Runtime = ClaudeCodeRuntime{}
var _ Resumer = ClaudeCodeRuntime{}
var _ Queueer = (*claudeSession)(nil)
