package node

import (
	"encoding/base64"
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
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/nativeexec"
)

const instructionClaudeUserResponse = "For every response, set user_response to the exact user-facing answer or question, without process narration or JSON. task_outcome, reason, and recovered_tool_use_ids are internal verdict metadata; never repeat them in user_response. For a simple direct question, reply with only what the user requested."

var ErrClaudeCodeUnavailable = errors.New("claude_code runtime unavailable")

var errClaudeRootAlreadyExited = errors.New("Claude process exited before root identity capture")

var ErrClaudeCodeConfiguration = errors.New("claude_code runtime configuration error")

var ErrClaudeCodeSteeringUnsupported = errors.New("claude: same-Attempt steering has not been verified for this native runtime")

var claudeStaticArgumentAllowlist = map[string]struct{}{
	"--disable-slash-commands": {},
}

type ClaudeCodeRuntime struct {
	Command        string
	Arguments      []string
	Environment    []string
	RawLogDir      string
	RawLogMaxBytes int64
	RawLogFiles    int
	captureRoot func(*exec.Cmd) (claudeChild, error)
}

type ClaudeRuntime = ClaudeCodeRuntime

func (r ClaudeCodeRuntime) Start(ctx context.Context, request StartRequest) (Session, error) {
	if err := validateClaudeArguments(r.Arguments); err != nil {
		return nil, err
	}
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	if request.Profile.ReplyContractVersion != "" {
		return nil, fmt.Errorf("%w: addressed reply grouping is not verified", ErrClaudeCodeConfiguration)
	}
	if err := validateClaudeRequest(request); err != nil {
		return nil, err
	}
	id, err := newClaudeSessionID()
	if err != nil {
		return nil, fmt.Errorf("%w: create session id: %v", ErrClaudeCodeUnavailable, err)
	}
	args, err := r.authoritativeArguments(request, id, false)
	if err != nil {
		return nil, err
	}
	session, err := r.startProcess(ctx, request, id, args)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func (r ClaudeCodeRuntime) Resume(ctx context.Context, request StartRequest, runtimeSessionID string) (Session, error) {
	if err := validateClaudeArguments(r.Arguments); err != nil {
		return nil, err
	}
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	if request.Profile.ReplyContractVersion != "" {
		return nil, fmt.Errorf("%w: addressed reply grouping is not verified", ErrClaudeCodeConfiguration)
	}
	if err := validateClaudeRequest(request); err != nil {
		return nil, err
	}
	if strings.TrimSpace(runtimeSessionID) == "" {
		return nil, fmt.Errorf("%w: runtime session id is required", ErrClaudeCodeUnavailable)
	}
	args, err := r.authoritativeArguments(request, runtimeSessionID, true)
	if err != nil {
		return nil, err
	}
	session, err := r.startProcess(ctx, request, runtimeSessionID, args, true)
	if err != nil {
		return nil, err
	}
	return session, nil
}

func validateClaudeRequest(request StartRequest) error {
	if request.HarnessInstance.Kind != "" && request.HarnessInstance.Kind != core.HarnessClaudeCode {
		return fmt.Errorf("node: Claude Code runtime cannot execute harness %q", request.HarnessInstance.Kind)
	}
	if request.HarnessInstance.Kind == core.HarnessClaudeCode {
		for _, capability := range []core.ExecutionCapability{core.CapabilitySteering, core.CapabilityApprovals} {
			if request.HarnessInstance.Capabilities.SupportsExecution(capability) {
				return fmt.Errorf("%w: Claude Code runtime cannot provide %q", ErrClaudeCodeConfiguration, capability)
			}
		}
		if !request.HarnessInstance.Available() {
			if !request.HarnessInstance.Authentication.Authenticated {
				return fmt.Errorf("%w: Claude Code is not authenticated", ErrClaudeCodeUnavailable)
			}
			return fmt.Errorf("%w: Claude Code HarnessInstance %s is not ready", ErrClaudeCodeUnavailable, request.HarnessInstance.ID)
		}
	}
	if request.Workspace == "" {
		return fmt.Errorf("%w: workspace is required", ErrClaudeCodeUnavailable)
	}
	if request.Task == "" && request.HarnessInstance.Kind != "" && !request.DeferInitialPrompt {
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

func validateClaudeArguments(configured []string) error {
	for _, argument := range configured {
		if _, allowed := claudeStaticArgumentAllowlist[argument]; !allowed {
			return fmt.Errorf("%w: configured argument %q is not in the Claude Code static argument allowlist", ErrClaudeCodeConfiguration, argument)
		}
	}
	return nil
}

func (r ClaudeCodeRuntime) authoritativeArguments(request StartRequest, sessionID string, resume bool) ([]string, error) {
	if err := validateClaudeArguments(r.Arguments); err != nil {
		return nil, err
	}
	args := append([]string(nil), r.Arguments...)
	args = append(args, "--print", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--replay-user-messages", "--strict-mcp-config")
	args = append(args, "--permission-mode", "dontAsk")
	args = append(args, "--json-schema", `{"type":"object","properties":{"task_outcome":{"type":"string","enum":["succeeded","failed"]},"reason":{"type":"string"},"recovered_tool_use_ids":{"type":"array","items":{"type":"string"}},"user_response":{"type":"string","minLength":1}},"required":["task_outcome","reason","recovered_tool_use_ids","user_response"],"additionalProperties":false}`)
	toolNames := map[string]string{"read": "Read", "bash": "Bash", "shell": "Bash", "edit": "Edit", "write": "Write", "patch": "Edit", "glob": "Glob", "find": "Glob", "grep": "Grep", "webfetch": "WebFetch", "websearch": "WebSearch", "task": "Agent", "subagent": "Agent", "skill": "Skill"}
	tools := []string{}
	for _, tool := range request.Profile.AllowTools {
		name, ok := toolNames[tool]
		if !ok {
			return nil, fmt.Errorf("%w: unsupported managed tool %q", ErrClaudeCodeConfiguration, tool)
		}
		tools = append(tools, name)
	}
	if request.Profile.Name != "" || request.Profile.Content != "" || len(tools) > 0 {
		args = append(args, "--tools", strings.Join(tools, ","))
	}
	allowed := append([]string(nil), tools...)
	for _, server := range request.MCPServers {
		if request.Profile.Name == "secretary" && server.Name != "secretary" {
			return nil, fmt.Errorf("%w: unexpected Secretary MCP identity", ErrClaudeCodeConfiguration)
		}
		allowed = append(allowed, "mcp__"+server.Name+"__*")
	}
	if len(allowed) > 0 {
		args = append(args, "--allowedTools", strings.Join(allowed, ","))
	}
	prompt := instructionClaudeUserResponse + "\n" + request.Profile.EffectivePrompt()
	args = append(args, "--append-system-prompt", strings.TrimSpace(prompt))
	servers := map[string]any{}
	for index, server := range request.MCPServers {
		if server.Name == "" || server.Command == "" {
			return nil, fmt.Errorf("%w: MCP name and command required", ErrClaudeCodeConfiguration)
		}
		if _, exists := servers[server.Name]; exists {
			return nil, fmt.Errorf("%w: duplicate MCP name", ErrClaudeCodeConfiguration)
		}
		environment := map[string]string{}
		for _, variable := range server.Env {
			if !validEnvironmentName(variable.Name) {
				return nil, fmt.Errorf("%w: invalid MCP environment name", ErrClaudeCodeConfiguration)
			}
			if _, exists := environment[variable.Name]; exists {
				return nil, fmt.Errorf("%w: duplicate MCP environment name", ErrClaudeCodeConfiguration)
			}
			environment[variable.Name] = fmt.Sprintf("${SECRETARY_CLAUDE_MCP_%d_%s}", index, variable.Name)
		}
		serverArgs := server.Args
		if serverArgs == nil {
			serverArgs = []string{}
		}
		servers[server.Name] = map[string]any{"command": server.Command, "args": serverArgs, "env": environment}
	}
	config, _ := json.Marshal(map[string]any{"mcpServers": servers})
	args = append(args, "--mcp-config", string(config))
	model, reasoning := request.Model, request.Reasoning
	if request.HarnessInstance.Kind == "" {
		if model == "" {
			model = request.Profile.Model
		}
		if reasoning == "" {
			reasoning = request.Profile.Reasoning
		}
	}
	if model != "" && !isModelAlias(model) {
		args = append(args, "--model", model)
	}
	if reasoning != "" && reasoning != "default" {
		args = append(args, "--effort", reasoning)
	}
	if resume {
		args = append(args, "--resume", sessionID)
	} else {
		args = append(args, "--session-id", sessionID)
	}

	return args, nil
}

type claudeNativeCleanupConfirmedError struct {
	cause error
}

func (e *claudeNativeCleanupConfirmedError) Error() string { return e.cause.Error() }
func (e *claudeNativeCleanupConfirmedError) Unwrap() error { return e.cause }

func claudeResumeInitializationFailure(session Session, initErr error) error {
	if closeErr := session.Close(); closeErr != nil {
		return fmt.Errorf("%w: resume initialization failed: %v; native cleanup unverified: %v", ErrClaudeCodeUnavailable, initErr, closeErr)
	}
	return &claudeNativeCleanupConfirmedError{cause: fmt.Errorf("%w: resume initialization failed: %v", ErrClaudeCodeUnavailable, initErr)}
}

func (r ClaudeCodeRuntime) startProcess(ctx context.Context, request StartRequest, sessionID string, args []string, resume ...bool) (*claudeSession, error) {
	command := r.command()
	if _, err := exec.LookPath(command); err != nil {
		return nil, fmt.Errorf("%w: executable %q is unavailable: %v", ErrClaudeCodeUnavailable, command, err)
	}
	cmd, err := nativeexec.CommandContext(ctx, command, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: start %q: %v", ErrClaudeCodeUnavailable, command, err)
	}
	configureClaudeProcess(cmd)
	cmd.WaitDelay = 250 * time.Millisecond
	cmd.Dir = request.Workspace
	cmd.Env = append(os.Environ(), r.Environment...)
	for index, server := range request.MCPServers {
		for _, variable := range server.Env {
			cmd.Env = append(cmd.Env, fmt.Sprintf("SECRETARY_CLAUDE_MCP_%d_%s=%s", index, variable.Name, variable.Value))
		}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("%w: open stdin: %v", ErrClaudeCodeUnavailable, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("%w: open stdout: %v", ErrClaudeCodeUnavailable, err)
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	session := &claudeSession{
		id:        sessionID,
		stdin:     stdin,
		seen:      make(map[string]bool),
		bashCalls: make(map[string]bool),
		ready:     make(chan error, 1),
		writeGate: make(chan struct{}, 1),
		done:      make(chan struct{}),
		cmd:       cmd,
		stdout:    stdout,
		stderr:    stderr,
		activity:  make(chan Activity, 64),
		result:    make(chan Result, 16),
	}
	cmd.Cancel = session.stopProcess
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: start %q: %v", ErrClaudeCodeUnavailable, command, err)
	}
	capture := r.captureRoot
	if capture == nil {
		capture = captureClaudeRoot
	}
	root, rootErr := capture(cmd)
	if rootErr != nil && !errors.Is(rootErr, errClaudeRootAlreadyExited) {
		_ = session.stopProcess()
		_ = cmd.Wait()
		return nil, fmt.Errorf("%w: capture native process identity: %v", ErrClaudeCodeUnavailable, rootErr)
	}
	if rootErr == nil {
		session.claudeRoot = root
	}
	go session.run()
	if len(resume) > 0 && resume[0] {
		err = session.writeMessage(ctx, map[string]any{"type": "control_request", "request_id": "resume-initialize", "request": map[string]any{"subtype": "initialize"}})
		if err == nil {
			select {
			case err = <-session.ready:
			case <-ctx.Done():
				err = ctx.Err()
			}
		}
		if err != nil {
			return nil, claudeResumeInitializationFailure(session, err)
		}
	}
	if len(resume) > 0 && resume[0] && request.DeferInitialPrompt {
		session.stateMu.Lock()
		session.initialPromptPending = true
		session.stateMu.Unlock()
	}
	if !request.DeferInitialPrompt && request.Task != "" {
		if err := promptWithImages(ctx, session, request.Task, request.Images); err != nil {
			if len(resume) > 0 && resume[0] {
				return nil, claudeResumeInitializationFailure(session, err)
			}
			_ = session.Close()
			return nil, err
		}
	}
	return session, nil
}

type claudeSession struct {
	id        string
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    io.ReadCloser
	stderr    *bytes.Buffer
	activity  chan Activity
	result    chan Result
	ready     chan error
	stateMu   sync.Mutex
	writeGate chan struct{}
	stopOnce  sync.Once
	stopErr   error
	claudeRoot claudeChild
	cleanupAttempt func(context.Context, *exec.Cmd, claudeChild) error
	canceling bool
	cancelWait chan struct{}
	pendingTerminal *Result
	nativeErrorDuringCancel string
	failedCancel bool
	initialPromptPending bool
	initialPromptWriting bool
	beforeInitialPromptAdmission func()
	beforeInitialPromptWrite func()
	done      chan struct{}
	closed    bool
	cancel    bool
	active    bool
	inputID   string
	seen      map[string]bool
	bashCalls map[string]bool
	failedBash string
	failedBashIDs map[string]string
	sawAssistantText bool
	sawToolUse bool
}

func (s *claudeSession) ID() string                { return s.id }
func (s *claudeSession) Activity() <-chan Activity { return s.activity }
func (s *claudeSession) Result() <-chan Result     { return s.result }
func (s *claudeSession) Prompt(ctx context.Context, text string) error {
	return s.PromptImages(ctx, text, nil)
}

func (s *claudeSession) PromptImages(ctx context.Context, text string, images []ImageInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(text) == "" {
		return errors.New("claude: empty prompt")
	}
	id, err := newClaudeSessionID()
	if err != nil {
		return err
	}
	s.stateMu.Lock()
	firstPrompt := s.initialPromptPending
	s.stateMu.Unlock()
	if firstPrompt && s.beforeInitialPromptAdmission != nil {
		s.beforeInitialPromptAdmission()
	}
	if firstPrompt {
		select {
		case s.writeGate <- struct{}{}:
			defer func() { <-s.writeGate }()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.stateMu.Lock()
	if s.closed || s.active || s.canceling || (firstPrompt && !s.initialPromptPending) {
		s.stateMu.Unlock()
		return errors.New("claude: session closed or Attempt still active")
	}
	s.initialPromptPending = false
	s.initialPromptWriting = firstPrompt
	s.active = true
	s.cancel = false
	s.nativeErrorDuringCancel = ""
	s.failedCancel = false
	s.inputID = id
	s.bashCalls = make(map[string]bool)
	s.failedBash = ""
	s.failedBashIDs = make(map[string]string)
	s.sawAssistantText = false
	s.sawToolUse = false
	s.stateMu.Unlock()
	var content any = text
	if len(images) > 0 {
		blocks := []any{map[string]any{"type": "text", "text": text}}
		for _, image := range images {
			blocks = append(blocks, map[string]any{"type": "image", "source": map[string]any{
				"type": "base64", "media_type": image.MediaType, "data": base64.StdEncoding.EncodeToString(image.Data),
			}})
		}
		content = blocks
	}
	message := map[string]any{"type": "user", "uuid": id, "session_id": s.id, "message": map[string]any{"role": "user", "content": content}, "parent_tool_use_id": nil}
	if firstPrompt {
		if s.beforeInitialPromptWrite != nil {
			s.beforeInitialPromptWrite()
		}
		err = s.writeMessageWithGate(ctx, message)
	} else {
		err = s.writeMessage(ctx, message)
	}
	s.stateMu.Lock()
	s.initialPromptWriting = false
	if err != nil {
		if firstPrompt && s.active && !s.cancel {
			s.active = false
			s.result <- Result{Status: "failed", FailureCode: "native_prompt_write_failed", Summary: "Claude Code initial prompt write failed: " + err.Error()}
		} else if !firstPrompt {
			s.active = false
		}
	}
	s.stateMu.Unlock()
	if err != nil && firstPrompt {
		_ = s.stopProcess()
	}
	return err
}
func (s *claudeSession) Steer(context.Context, string) (bool, error) {
	return false, ErrClaudeCodeSteeringUnsupported
}

func (s *claudeSession) Queue(context.Context, string) error {
	return errors.New("claude: queued input must remain server-owned until idle")
}
func (s *claudeSession) Cancel(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.stateMu.Lock()
	serializeInitialPrompt := s.initialPromptPending || s.initialPromptWriting
	s.stateMu.Unlock()
	if serializeInitialPrompt {
		select {
		case s.writeGate <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	s.stateMu.Lock()
	if s.closed || (!s.active && !s.initialPromptPending) || s.cancel {
		s.stateMu.Unlock()
		if serializeInitialPrompt {
			<-s.writeGate
		}
		return nil
	}
	if s.initialPromptPending {
		s.initialPromptPending = false
		s.active = true
		s.inputID = s.id + "-initial-prompt-preempted"
	}
	attempt := s.inputID
	s.cancel = true
	s.canceling = true
	s.cancelWait = make(chan struct{})
	root := s.claudeRoot
	s.stateMu.Unlock()
	if serializeInitialPrompt {
		<-s.writeGate
	}
	var err error
	if root.pid == 0 {
		err = errors.New("Claude root process identity was not captured")
	} else {
		cleanup := s.cleanupAttempt
		if cleanup == nil {
			cleanup = cleanupClaudeAttemptProcesses
		}
		err = cleanup(ctx, s.cmd, root)
	}
	if err == nil {
		var id string
		id, err = newClaudeSessionID()
		if err == nil {
			s.stateMu.Lock()
			canInterrupt := s.active && s.inputID == attempt && !s.closed && s.pendingTerminal == nil
			s.stateMu.Unlock()
			if canInterrupt {
				err = s.writeMessage(ctx, map[string]any{"type": "control_request", "request_id": id, "request": map[string]any{"subtype": "interrupt", "cancel_queued": true}})
			}
		}
	}
	s.stateMu.Lock()
	if err != nil && s.active && s.inputID == attempt {
		s.cancel = false
		s.failedCancel = true
	}
	s.canceling = false
	pending := s.pendingTerminal
	s.pendingTerminal = nil
	if pending != nil && s.active && s.inputID == attempt {
		if err != nil && pending.Status == "canceled" {
			pending.Status = "failed"
			pending.Summary = "Claude Code cancellation cleanup failed: " + err.Error()
		}
		s.active = false
		s.result <- *pending
	}
	stalled := err == nil && pending == nil && s.active && s.cancel && s.inputID == attempt
	stillActive := s.active && s.inputID == attempt
	nativeError := s.nativeErrorDuringCancel
	s.nativeErrorDuringCancel = ""
	close(s.cancelWait)
	s.cancelWait = nil
	s.stateMu.Unlock()
	if err != nil {
		if stillActive {
			summary := "Claude Code cancellation cleanup or interrupt failed"
			if nativeError != "" {
				summary = nativeError
			}
			s.emitResult(Result{Status: "failed", Summary: summary, FailureCode: "cancel_failed", Technical: err.Error()})
		}
		_ = s.stopProcess()
	} else if stalled && nativeError != "" {
		_ = s.stopProcess()
	} else if stalled {
		go s.stopStalledCanceledAttempt(attempt)
	}
	return err
}

func (s *claudeSession) stopStalledCanceledAttempt(attempt string) {
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-s.done:
		return
	}
	s.stateMu.Lock()
	stalled := s.active && s.cancel && s.inputID == attempt && !s.canceling && !s.closed
	s.stateMu.Unlock()
	if stalled {
		_ = s.stopProcess()
	}
}

func (s *claudeSession) stopProcess() error {
	s.stopOnce.Do(func() {
		s.stateMu.Lock()
		s.closed = true
		s.stateMu.Unlock()
		if s.stdin != nil {
			_ = s.stdin.Close()
		}
		if s.stdout != nil {
			_ = s.stdout.Close()
		}
		if s.cmd == nil || s.cmd.Process == nil {
			s.stopErr = errors.New("claude: native process identity unavailable for termination")
			return
		}
		s.stopErr = killClaudeProcess(s.cmd)
	})
	return s.stopErr
}
func (s *claudeSession) Close() error {
	if err := s.stopProcess(); err != nil {
		return err
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-s.done:
		return nil
	case <-timer.C:
		return errors.New("claude: timed out waiting for native session shutdown")
	}
}
func (s *claudeSession) writeMessage(ctx context.Context, message any) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.writeGate <- struct{}{}:
	}
	defer func() { <-s.writeGate }()
	return s.writeMessageWithGate(ctx, message)
}

func (s *claudeSession) writeMessageWithGate(ctx context.Context, message any) error {
	written := make(chan error, 1)
	go func() { written <- json.NewEncoder(s.stdin).Encode(message) }()
	select {
	case err := <-written:
		return err
	case <-ctx.Done():
		_ = s.stopProcess()
		<-written
		return ctx.Err()
	}
}
func (s *claudeSession) run() {
	defer close(s.done)
	defer close(s.activity)
	defer close(s.result)
	scanner := bufio.NewScanner(s.stdout)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		s.handleLine(scanner.Bytes())
	}
	s.stateMu.Lock()
	if s.inputID == "" && !s.closed {
		s.active = true
	}
	s.stateMu.Unlock()
	_ = s.stopProcess()
	waitErr := s.cmd.Wait()
	select {
	case s.ready <- errors.New("Claude Code exited before resume initialization"):
	default:
	}
	s.stateMu.Lock()
	active := s.active
	canceled := s.cancel
	s.closed = true
	s.stateMu.Unlock()
	if active {
		status := "failed"
		if canceled {
			status = "canceled"
		}
		summary := strings.TrimSpace(s.stderr.String())
		if summary == "" && scanner.Err() != nil {
			summary = scanner.Err().Error()
		}
		if summary == "" && waitErr != nil {
			summary = waitErr.Error()
		}
		if canceled {
			summary = "Claude Code attempt canceled"
		} else if summary == "" {
			summary = "Claude Code exited without a result"
		}
		s.emitResult(Result{Status: status, Summary: summary})
	}
	s.stateMu.Lock()
	cancelWait := s.cancelWait
	s.stateMu.Unlock()
	if cancelWait != nil {
		<-cancelWait
	}
}
func (s *claudeSession) handleLine(line []byte) {
	var record map[string]any
	if json.Unmarshal(line, &record) != nil {
		return
	}
	if valueString(record["type"]) == "control_response" {
		response, _ := record["response"].(map[string]any)
		if valueString(response["request_id"]) == "resume-initialize" {
			var err error
			if valueString(response["subtype"]) != "success" {
				err = errors.New("native resume rejected")
			}
			select {
			case s.ready <- err:
			default:
			}
		}
		return
	}
	if sid := valueString(record["session_id"]); sid != "" && sid != s.id {
		_ = s.stopProcess()
		s.emitResult(Result{Status: "failed", Summary: "Claude Code returned a different native session identity"})
		return
	}
	s.stateMu.Lock()
	active := s.active
	inputID := s.inputID
	s.stateMu.Unlock()
	if !active {
		return
	}
	if input := valueString(record["user_message_uuid"]); input != "" && input != inputID {
		return
	}
	if valueString(record["parent_tool_use_id"]) != "" {
		return
	}
	switch valueString(record["type"]) {
	case "user":
		s.handleToolResults(record)
	case "assistant":
		id := valueString(record["uuid"])
		if id != "" {
			if s.seen[id] {
				return
			}
			s.seen[id] = true
		}
		if message, ok := record["message"].(map[string]any); ok {
			s.registerBashCalls(message["content"])
			s.handleContent(message["content"])
		}
	case "result":
		summary := valueString(record["result"])
		structured, valid := claudeTaskOutcome(record)
		if valid {
			summary = structured.userResponse
		} else if _, supplied := record["structured_output"]; supplied {
			summary = "Claude Code returned invalid structured task outcome"
		} else if claudeServiceJSON(summary) {
			summary = ""
		}
		status := "succeeded"
		if subtype := valueString(record["subtype"]); subtype != "success" {
			status = "failed"
		}
		if isError, _ := record["is_error"].(bool); isError {
			status = "failed"
		}
		if reason := valueString(record["terminal_reason"]); strings.Contains(reason, "error") {
			status = "failed"
		}
		s.stateMu.Lock()
		if s.cancel {
			status = "canceled"
		}
		s.stateMu.Unlock()
		if status == "succeeded" && !valid {
			if _, supplied := record["structured_output"]; !supplied {
				summary = "Claude Code returned no structured task outcome"
			}
		}
		if summary == "" {
			summary = "Claude Code returned no answer"
			if status == "canceled" {
				summary = "Claude Code attempt canceled"
			}
			if status == "succeeded" {
				status = "failed"
			}
		}
		result := Result{Status: status, Summary: summary}
		if status == "succeeded" {
			if s.failedBash != "" && (valid && structured.status == "failed" || !s.claudeStructuredRecovery(structured, valid)) {
				result.Status = "failed"
				result.FailureCode = "tool_execution_failed"
				result.Technical = s.failedBash
				if valid && structured.status == "failed" {
					result.Technical += "; Claude Code reported structured task failure: " + structured.reason
				} else if _, supplied := record["structured_output"]; supplied && !valid {
					result.Technical += "; Claude Code returned invalid structured task outcome"
				}
			} else if valid && structured.status == "failed" {
				result.Status = "failed"
				result.FailureCode = "task_outcome_failed"
				result.Technical = "Claude Code reported structured task failure: " + structured.reason
			} else if _, supplied := record["structured_output"]; supplied && !valid {
				result.Status = "failed"
				result.FailureCode = "task_outcome_failed"
				result.Technical = "Claude Code returned invalid structured task outcome"
			} else if !valid {
				result.Status = "failed"
				result.FailureCode = "task_outcome_failed"
				result.Technical = "Claude Code returned no structured task outcome"
			}
		}
		s.emitResult(result)
	case "error":
		summary := valueString(record["error"])
		if summary == "" {
			summary = valueString(record["message"])
		}
		if summary == "" {
			summary = "Claude Code native error"
		}
		s.stateMu.Lock()
		canceled := s.cancel
		canceling := s.canceling
		failedCancel := s.failedCancel
		if canceled && canceling {
			s.nativeErrorDuringCancel = summary
		}
		s.stateMu.Unlock()
		if canceled {
			if !canceling {
				_ = s.stopProcess()
			}
			return
		}
		s.emitResult(Result{Status: "failed", Summary: summary})
		if failedCancel {
			_ = s.stopProcess()
		}
	}
}
type claudeTaskVerdict struct {
	status string
	reason string
	recovered []string
	userResponse string
}

func claudeTaskOutcome(record map[string]any) (claudeTaskVerdict, bool) {
	output, ok := record["structured_output"].(map[string]any)
	if !ok {
		return claudeTaskVerdict{}, false
	}
	status, statusOK := output["task_outcome"].(string)
	reason, reasonOK := output["reason"].(string)
	ids, idsOK := output["recovered_tool_use_ids"].([]any)
	userResponse, responseOK := output["user_response"].(string)
	if !statusOK || (status != "succeeded" && status != "failed") || !reasonOK || strings.TrimSpace(reason) == "" || !idsOK || !responseOK || strings.TrimSpace(userResponse) == "" {
		return claudeTaskVerdict{}, false
	}
	verdict := claudeTaskVerdict{status: status, reason: reason, userResponse: strings.TrimSpace(userResponse)}
	seen := make(map[string]bool)
	for _, raw := range ids {
		id, ok := raw.(string)
		if !ok || id == "" || seen[id] {
			return claudeTaskVerdict{}, false
		}
		seen[id] = true
		verdict.recovered = append(verdict.recovered, id)
	}
	return verdict, true
}

func claudeServiceJSON(text string) bool {
	candidate := strings.TrimSpace(text)
	candidate = strings.TrimSpace(strings.TrimPrefix(candidate, "Internal verdict:"))
	if strings.HasPrefix(candidate, "```json\n") && strings.HasSuffix(candidate, "\n```") {
		candidate = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(candidate, "```json\n"), "\n```"))
	}
	var value map[string]any
	if json.Unmarshal([]byte(candidate), &value) != nil {
		return false
	}
	_, reason := value["reason"]
	_, recovered := value["recovered_tool_use_ids"]
	_, outcome := value["task_outcome"]
	return reason && recovered || outcome && (reason || recovered)
}

func (s *claudeSession) claudeStructuredRecovery(verdict claudeTaskVerdict, valid bool) bool {
	if !valid || verdict.status != "succeeded" || len(s.failedBashIDs) == 0 || len(verdict.recovered) != len(s.failedBashIDs) {
		return false
	}
	for _, id := range verdict.recovered {
		if _, ok := s.failedBashIDs[id]; !ok {
			return false
		}
	}
	return true
}

func (s *claudeSession) registerBashCalls(content any) {
	items, _ := content.([]any)
	for _, item := range items {
		block, _ := item.(map[string]any)
		if valueString(block["type"]) == "tool_use" && valueString(block["name"]) == "Bash" {
			if id := valueString(block["id"]); id != "" {
				s.bashCalls[id] = true
			}
		}
	}
}

func (s *claudeSession) handleToolResults(record map[string]any) {
	message, _ := record["message"].(map[string]any)
	items, _ := message["content"].([]any)
	for _, item := range items {
		block, _ := item.(map[string]any)
		if valueString(block["type"]) != "tool_result" {
			continue
		}
		id := valueString(block["tool_use_id"])
		if !s.bashCalls[id] {
			continue
		}
		delete(s.bashCalls, id)
		failed, _ := block["is_error"].(bool)
		exitCode := ""
		details, _ := record["toolUseResult"].(map[string]any)
		if details == nil {
			details, _ = block["toolUseResult"].(map[string]any)
		}
		if code, ok := details["exitCode"].(float64); ok {
			exitCode = fmt.Sprintf("%.0f", code)
			failed = failed || code != 0
		}
		if code, ok := details["exit_code"].(float64); ok {
			exitCode = fmt.Sprintf("%.0f", code)
			failed = failed || code != 0
		}
		if failed {
			if s.failedBashIDs == nil {
				s.failedBashIDs = make(map[string]string)
			}
			s.failedBash = "Claude Bash tool failed"
			if exitCode != "" {
				s.failedBash += " with exit code " + exitCode
			}
			s.failedBashIDs[id] = s.failedBash
		}
	}
}

func (s *claudeSession) handleContent(content any) {
	items, _ := content.([]any)
	for _, item := range items {
		block, _ := item.(map[string]any)
		switch valueString(block["type"]) {
		case "text":
			if strings.TrimSpace(valueString(block["text"])) != "" {
				if !claudeServiceJSON(valueString(block["text"])) {
					s.sawAssistantText = true
				}
			}
			if !claudeServiceJSON(valueString(block["text"])) {
				s.emitText(valueString(block["text"]))
			}
		case "tool_use":
			s.sawToolUse = true
			s.emitActivity(Activity{Kind: ActivityTool, Text: valueString(block["name"])})
		}
	}
}
func (s *claudeSession) emitText(text string) {
	if text == "" {
		return
	}
	s.emitActivity(Activity{Kind: ActivityText, Text: text})
}
func (s *claudeSession) emitActivity(activity Activity) {
	if (activity.Kind == ActivityText && activity.Text == "") || (activity.Kind != ActivityText && strings.TrimSpace(activity.Text) == "") {
		return
	}
	select {
	case s.activity <- activity:
	default:
	}
}
func (s *claudeSession) emitResult(result Result) {
	s.stateMu.Lock()
	if !s.active {
		s.stateMu.Unlock()
		return
	}
	if s.canceling {
		copy := result
		s.pendingTerminal = &copy
		s.stateMu.Unlock()
		return
	}
	s.active = false
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
