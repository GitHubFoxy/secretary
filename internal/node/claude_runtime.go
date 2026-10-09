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

var ErrClaudeCodeUnavailable = errors.New("claude_code runtime unavailable")

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
	return r.startProcess(ctx, request, id, args)
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
	return r.startProcess(ctx, request, runtimeSessionID, args, true)
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
	if prompt := request.Profile.EffectivePrompt(); prompt != "" {
		args = append(args, "--append-system-prompt", prompt)
	}
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

func (r ClaudeCodeRuntime) startProcess(ctx context.Context, request StartRequest, sessionID string, args []string, resume ...bool) (*claudeSession, error) {
	command := r.command()
	if _, err := exec.LookPath(command); err != nil {
		return nil, fmt.Errorf("%w: executable %q is unavailable: %v", ErrClaudeCodeUnavailable, command, err)
	}
	cmd := exec.CommandContext(ctx, command, args...)
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
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: start %q: %v", ErrClaudeCodeUnavailable, command, err)
	}
	session := &claudeSession{
		id:       sessionID,
		stdin:    stdin,
		seen:     make(map[string]bool),
		ready:    make(chan error, 1),
		cmd:      cmd,
		stdout:   stdout,
		stderr:   stderr,
		activity: make(chan Activity, 64),
		result:   make(chan Result, 16),
	}
	go session.run()
	if len(resume) > 0 && resume[0] {
		session.writeMu.Lock()
		err = json.NewEncoder(session.stdin).Encode(map[string]any{"type": "control_request", "request_id": "resume-initialize", "request": map[string]any{"subtype": "initialize"}})
		session.writeMu.Unlock()
		if err == nil {
			select {
			case err = <-session.ready:
			case <-ctx.Done():
				err = ctx.Err()
			}
		}
		if err != nil {
			_ = session.Close()
			return nil, fmt.Errorf("%w: resume initialization failed: %v", ErrClaudeCodeUnavailable, err)
		}
	}
	if !request.DeferInitialPrompt && request.Task != "" {
		if err := session.Prompt(ctx, request.Task); err != nil {
			_ = session.Close()
			return nil, err
		}
	}
	return session, nil
}

type claudeSession struct {
	id       string
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	stderr   *bytes.Buffer
	activity chan Activity
	result   chan Result
	ready    chan error
	stateMu  sync.Mutex
	writeMu  sync.Mutex
	closed   bool
	cancel   bool
	active   bool
	inputID  string
	seen     map[string]bool
}

func (s *claudeSession) ID() string                { return s.id }
func (s *claudeSession) Activity() <-chan Activity { return s.activity }
func (s *claudeSession) Result() <-chan Result     { return s.result }
func (s *claudeSession) Prompt(ctx context.Context, text string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if strings.TrimSpace(text) == "" {
		return errors.New("claude: empty prompt")
	}
	id, err := newClaudeSessionID()
	if err != nil {
		return err
	}
	s.stateMu.Lock()
	if s.closed || s.active {
		s.stateMu.Unlock()
		return errors.New("claude: session closed or Attempt still active")
	}
	s.active = true
	s.cancel = false
	s.inputID = id
	s.stateMu.Unlock()
	err = json.NewEncoder(s.stdin).Encode(map[string]any{"type": "user", "uuid": id, "session_id": s.id, "message": map[string]any{"role": "user", "content": text}, "parent_tool_use_id": nil})
	if err != nil {
		s.stateMu.Lock()
		s.active = false
		s.stateMu.Unlock()
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
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.stateMu.Lock()
	if s.closed || !s.active {
		s.stateMu.Unlock()
		return nil
	}
	s.cancel = true
	s.stateMu.Unlock()
	id, err := newClaudeSessionID()
	if err != nil {
		return err
	}
	err = json.NewEncoder(s.stdin).Encode(map[string]any{"type": "control_request", "request_id": id, "request": map[string]any{"subtype": "interrupt", "cancel_queued": true}})
	if err != nil {
		s.stateMu.Lock()
		s.cancel = false
		s.stateMu.Unlock()
	}
	return err
}
func (s *claudeSession) Close() error {
	s.stateMu.Lock()
	if s.closed {
		s.stateMu.Unlock()
		return nil
	}
	s.closed = true
	s.stateMu.Unlock()
	s.writeMu.Lock()
	_ = s.stdin.Close()
	s.writeMu.Unlock()
	if err := s.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
func (s *claudeSession) run() {
	defer close(s.activity)
	defer close(s.result)
	scanner := bufio.NewScanner(s.stdout)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		s.handleLine(scanner.Bytes())
	}
	if scanner.Err() != nil {
		_ = s.cmd.Process.Kill()
	}
	waitErr := s.cmd.Wait()
	select {
	case s.ready <- errors.New("Claude Code exited before resume initialization"):
	default:
	}
	s.stateMu.Lock()
	if s.inputID == "" && !s.closed {
		s.active = true
	}
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
		if summary == "" {
			summary = "Claude Code exited without a result"
		}
		s.emitResult(Result{Status: status, Summary: summary})
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
		_ = s.Close()
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
	case "assistant":
		id := valueString(record["uuid"])
		if id != "" {
			if s.seen[id] {
				return
			}
			s.seen[id] = true
		}
		if message, ok := record["message"].(map[string]any); ok {
			s.handleContent(message["content"])
		}
	case "result":
		summary := valueString(record["result"])
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
		if summary == "" {
			summary = "Claude Code returned no answer"
			if status == "succeeded" {
				status = "failed"
			}
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
