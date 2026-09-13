package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/beruseruko/secretary/internal/acp"
)

type ACPRuntime struct {
	Command        string
	Arguments      []string
	RawLogDir      string
	RawLogMaxBytes int64
	RawLogFiles    int
	Environment    []string
}

func (r ACPRuntime) Start(ctx context.Context, request StartRequest) (Session, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	client, err := r.connect(ctx, request.WorkerRef, profile)
	if err != nil {
		return nil, err
	}
	var created struct {
		SessionID string `json:"sessionId"`
	}
	mcpServers := request.MCPServers
	if mcpServers == nil {
		mcpServers = []MCPServer{}
	}
	newParams := map[string]any{"cwd": request.Workspace, "mcpServers": mcpServers}
	if metadata := profileMetadata(request.Profile); metadata != nil {
		newParams["_meta"] = metadata
	}
	if err := client.Request(ctx, "session/new", newParams, &created); err != nil {
		client.Close()
		return nil, err
	}
	if created.SessionID == "" {
		client.Close()
		return nil, fmt.Errorf("acp: session/new returned no sessionId")
	}
	session := newACPSession(created.SessionID, client, true)
	session.setRequestHandler()
	go session.watch()
	go func() { _ = session.promptTurn(context.Background(), request.Task) }()
	return session, nil
}

func (r ACPRuntime) Resume(ctx context.Context, request StartRequest, runtimeSessionID string) (Session, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	client, err := r.connect(ctx, request.WorkerRef, profile)
	if err != nil {
		return nil, err
	}
	mcpServers := request.MCPServers
	if mcpServers == nil {
		mcpServers = []MCPServer{}
	}
	session := newACPSession(runtimeSessionID, client, false)
	// ACP may issue a permission/input request while session/load is still in
	// flight. Install the handler and durable Node-local IDs first, otherwise
	// the request gets a native ACP ID and cannot be answered after reconnect.
	session.setRequestHandler()
	session.RebindPendingRequests(effectivePendingRequests(request))
	loadParams := map[string]any{"sessionId": runtimeSessionID, "cwd": request.Workspace, "mcpServers": mcpServers}
	if metadata := profileMetadata(request.Profile); metadata != nil {
		loadParams["_meta"] = metadata
	}
	if err := client.Request(ctx, "session/load", loadParams, &map[string]any{}); err != nil {
		client.Close()
		return nil, err
	}
	go session.watch()
	return session, nil
}

func profileMetadata(profile ManagedProfile) map[string]string {
	if profile.Name == "" && profile.Version == "" && profile.Hash == "" {
		return nil
	}
	return map[string]string{
		"secretaryProfile":         profile.Name,
		"secretaryProfileVersion":  profile.Version,
		"secretaryProfileHash":     profile.Hash,
		"secretaryProfileDelivery": profile.Delivery,
	}
}

func profileEnvironment(base []string, profile ManagedProfile) ([]string, error) {
	model := strings.TrimSpace(profile.Model)
	reasoning := strings.TrimSpace(profile.Reasoning)
	if (model == "" || isModelAlias(model)) && (reasoning == "" || reasoning == "default") {
		return append([]string(nil), base...), nil
	}
	overrides := make(map[string]any)
	for _, value := range base {
		if !strings.HasPrefix(value, "CODEX_CONFIG=") {
			continue
		}
		configured := make(map[string]any)
		if err := json.Unmarshal([]byte(strings.TrimPrefix(value, "CODEX_CONFIG=")), &configured); err != nil {
			return nil, fmt.Errorf("node: invalid CODEX_CONFIG: %w", err)
		}
		for key, item := range configured {
			overrides[key] = item
		}
	}
	if model != "" && !isModelAlias(model) {
		overrides["model"] = model
	}
	if reasoning != "" && reasoning != "default" {
		overrides["model_reasoning_effort"] = reasoning
	}
	encoded, err := json.Marshal(overrides)
	if err != nil {
		return nil, fmt.Errorf("node: encode profile CODEX_CONFIG: %w", err)
	}
	environment := make([]string, 0, len(base)+1)
	for _, value := range base {
		if !strings.HasPrefix(value, "CODEX_CONFIG=") {
			environment = append(environment, value)
		}
	}
	environment = append(environment, "CODEX_CONFIG="+string(encoded))
	return environment, nil
}

func isModelAlias(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "default", "fast", "smart", "cheap":
		return true
	default:
		return false
	}
}

func (r ACPRuntime) connect(ctx context.Context, workerRef string, profile ManagedProfile) (*acp.Client, error) {
	var rawLog io.WriteCloser
	if r.RawLogDir != "" {
		var err error
		rawLog, err = openRotatingLog(filepath.Join(r.RawLogDir, workerRef+".jsonl"), r.RawLogMaxBytes, r.RawLogFiles)
		if err != nil {
			return nil, fmt.Errorf("open raw ACP log: %w", err)
		}
	}
	environment, err := profileEnvironment(r.Environment, profile)
	if err != nil {
		if rawLog != nil {
			_ = rawLog.Close()
		}
		return nil, err
	}
	client, err := acp.StartWithLogEnv(ctx, rawLog, environment, r.Command, r.Arguments...)
	if err != nil {
		if rawLog != nil {
			_ = rawLog.Close()
		}
		return nil, err
	}
	if err := client.Request(ctx, "initialize", map[string]any{
		"protocolVersion": 1, "clientInfo": map[string]string{"name": "secretary", "version": "0.1.0"},
		"clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": true, "writeTextFile": true}, "terminal": false},
	}, &map[string]any{}); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}

func effectivePendingRequests(request StartRequest) []PendingRequest {
	if len(request.PendingRequests) > 0 {
		return append([]PendingRequest(nil), request.PendingRequests...)
	}
	pending := make([]PendingRequest, 0, len(request.PendingRequestIDs))
	for _, requestID := range request.PendingRequestIDs {
		if strings.TrimSpace(requestID) != "" {
			pending = append(pending, PendingRequest{RequestID: requestID, Kind: request.PendingRequestKinds[requestID]})
		}
	}
	return pending
}

type pendingACPResponse struct {
	response     chan string
	delivered    chan error
	retry        func(string) error
	responseSent bool
	retryable    bool
}

func newACPSession(id string, client *acp.Client, busy bool) *acpSession {
	session := &acpSession{id: id, client: client, activity: make(chan Activity, 64), result: make(chan Result, 64), pending: make(map[string]*pendingACPResponse), resolved: make(map[string]struct{}), reboundResponses: make(map[string]*pendingACPResponse), nativeRequests: make(map[string]string), nativeDeliveries: make(map[string]*pendingACPResponse), busy: busy}
	session.activityDone = make(chan struct{})
	client.SetServerRequestDeliveryHandler(session.serverRequestDelivered)
	return session
}

type acpSession struct {
	id       string
	client   *acp.Client
	activity chan Activity
	result   chan Result

	activityMu     sync.Mutex
	activityDone   chan struct{}
	activityClosed bool
	activitySendWG sync.WaitGroup

	requestMu        sync.Mutex
	pending          map[string]*pendingACPResponse
	resolved         map[string]struct{}
	rebound          []PendingRequest
	reboundResponses map[string]*pendingACPResponse
	nativeRequests   map[string]string
	nativeDeliveries map[string]*pendingACPResponse

	turnMu sync.Mutex
	busy   bool
	queued []string

	textMu   sync.Mutex
	turnText strings.Builder
}

func (s *acpSession) ID() string                { return s.id }
func (s *acpSession) Activity() <-chan Activity { return s.activity }
func (s *acpSession) Result() <-chan Result     { return s.result }
func (s *acpSession) Close() error {
	s.closeActivity()
	return s.client.Close()
}

func (s *acpSession) closeActivity() {
	s.activityMu.Lock()
	defer s.activityMu.Unlock()
	if s.activityClosed {
		return
	}
	s.activityClosed = true
	close(s.activityDone)
}

func (s *acpSession) emitActivity(activity Activity) bool {
	s.activityMu.Lock()
	if s.activityClosed {
		s.activityMu.Unlock()
		return false
	}
	s.activitySendWG.Add(1)
	s.activityMu.Unlock()
	defer s.activitySendWG.Done()
	select {
	case s.activity <- activity:
		return true
	case <-s.activityDone:
		return false
	}
}
func (s *acpSession) Steer(ctx context.Context, text string) (bool, error) {
	var response struct {
		Outcome string `json:"outcome"`
	}
	err := s.client.Request(ctx, "_session/steering", map[string]any{"sessionId": s.id, "prompt": []map[string]string{{"type": "text", "text": text}}}, &response)
	return response.Outcome == "injected" || response.Outcome == "startedNewTurn", err
}
func (s *acpSession) Cancel(ctx context.Context) error {
	_ = ctx
	return s.client.Notify("session/cancel", map[string]string{"sessionId": s.id})
}

func (s *acpSession) RebindRequests(requestIDs []string) {
	pending := make([]PendingRequest, 0, len(requestIDs))
	for _, requestID := range requestIDs {
		pending = append(pending, PendingRequest{RequestID: requestID})
	}
	s.RebindPendingRequests(pending)
}

func (s *acpSession) RebindPendingRequests(requests []PendingRequest) {
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	for _, request := range requests {
		request.RequestID = strings.TrimSpace(request.RequestID)
		if request.RequestID == "" {
			continue
		}
		if _, resolved := s.resolved[request.RequestID]; resolved {
			continue
		}
		if _, pending := s.pending[request.RequestID]; pending {
			continue
		}
		alreadyRebound := false
		for _, rebound := range s.rebound {
			if rebound.RequestID == request.RequestID {
				alreadyRebound = true
				break
			}
		}
		if !alreadyRebound {
			s.rebound = append(s.rebound, request)
		}
	}
}

func (s *acpSession) Respond(ctx context.Context, requestID, response string) error {
	if strings.TrimSpace(requestID) == "" || strings.TrimSpace(response) == "" {
		return errors.New("acp: request_id and response are required")
	}
	s.requestMu.Lock()
	pending, ok := s.pending[requestID]
	if !ok {
		pending = s.reboundResponses[requestID]
	}
	if pending == nil {
		for _, rebound := range s.rebound {
			if rebound.RequestID == requestID {
				pending = &pendingACPResponse{response: make(chan string, 1), delivered: make(chan error, 1)}
				s.reboundResponses[requestID] = pending
				break
			}
		}
	}
	if pending == nil {
		_, alreadyResolved := s.resolved[requestID]
		s.requestMu.Unlock()
		if alreadyResolved {
			return nil
		}
		return errors.New("acp: unknown worker request")
	}
	if pending.responseSent {
		delivered := pending.delivered
		s.requestMu.Unlock()
		return waitForACPDelivery(ctx, delivered)
	}
	pending.responseSent = true
	if pending.retryable {
		pending.retryable = false
		pending.delivered = make(chan error, 1)
		delivered := pending.delivered
		retry := pending.retry
		s.requestMu.Unlock()
		if retry == nil {
			return errors.New("acp: failed response is not retryable")
		}
		_ = retry(response)
		return waitForACPDelivery(ctx, delivered)
	}
	delivered := pending.delivered
	responseCh := pending.response
	s.requestMu.Unlock()
	select {
	case responseCh <- response:
		return waitForACPDelivery(ctx, delivered)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitForACPDelivery(ctx context.Context, delivered <-chan error) error {
	select {
	case err := <-delivered:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *acpSession) setRequestHandler() {
	s.client.SetServerRequestHandler(s.handleServerRequest)
}

var acpRequestSequence atomic.Uint64

type acpPermissionOption struct {
	OptionID string `json:"optionId"`
	Kind     string `json:"kind"`
}

func (s *acpSession) serverRequestDelivered(message acp.Message, err error) {
	s.requestMu.Lock()
	nativeID := string(message.ID)
	requestID := s.nativeRequests[nativeID]
	pending := s.nativeDeliveries[nativeID]
	if pending == nil {
		pending = s.pending[requestID]
	}
	if pending == nil {
		pending = s.reboundResponses[requestID]
	}
	if pending == nil {
		s.requestMu.Unlock()
		return
	}
	delivered := pending.delivered
	if err == nil {
		delete(s.nativeRequests, nativeID)
		delete(s.nativeDeliveries, nativeID)
		delete(s.pending, requestID)
		delete(s.reboundResponses, requestID)
		s.resolved[requestID] = struct{}{}
	} else {
		pending.retryable = true
		pending.responseSent = false
		s.pending[requestID] = pending
	}
	s.requestMu.Unlock()
	delivered <- err
}

func (s *acpSession) handleServerRequest(message acp.Message) (any, error) {
	var params map[string]any
	if err := json.Unmarshal(message.Params, &params); err != nil {
		return nil, errors.New("invalid harness request")
	}
	kind := ActivityPermission
	summary := "Worker request"
	if value, ok := params["question"].(string); ok && strings.TrimSpace(value) != "" {
		summary = value
	}
	if value, ok := params["prompt"].(string); ok && strings.TrimSpace(value) != "" {
		summary = value
	}
	if message.Method == "session/request_permission" {
		var permission struct {
			Options []acpPermissionOption `json:"options"`
		}
		if err := json.Unmarshal(message.Params, &permission); err != nil {
			return nil, errors.New("invalid permission request")
		}
		for _, option := range permission.Options {
			if strings.TrimSpace(option.OptionID) != "" && strings.TrimSpace(option.Kind) != "" {
				summary = "Permission request"
				break
			}
		}
	} else if message.Method == "session/request_input" || message.Method == "session/request_user_input" {
		kind = ActivityUserInput
	} else {
		return nil, fmt.Errorf("unsupported harness request: %s", message.Method)
	}

	requestID, err := s.reboundRequestID(params, kind)
	if err != nil {
		return nil, err
	}
	if requestID == "" {
		requestID = fmt.Sprintf("request-%d-%d", time.Now().UnixNano(), acpRequestSequence.Add(1))
	}
	var pending *pendingACPResponse
	retry := func(value string) error {
		result, handlerErr := s.serverRequestResponse(kind, message.Params, value)
		return s.client.RetryServerRequest(message, result, handlerErr)
	}
	s.requestMu.Lock()
	nativeID := string(message.ID)
	valueResponse, alreadyResponded := s.reboundResponses[requestID]
	if alreadyResponded {
		delete(s.reboundResponses, requestID)
		pending = valueResponse
	} else {
		pending = &pendingACPResponse{response: make(chan string, 1), delivered: make(chan error, 1)}
		s.pending[requestID] = pending
	}
	pending.retry = retry
	s.nativeRequests[nativeID] = requestID
	s.nativeDeliveries[nativeID] = pending
	s.requestMu.Unlock()
	if !alreadyResponded {
		// Requests are the durable approval/input boundary. Unlike optional
		// activity updates, they must reach the Node outbox and cannot be
		// silently dropped when the activity buffer is full.
		if !s.emitActivity(Activity{Kind: kind, RequestID: requestID, Summary: summary}) {
			return nil, errors.New("acp: session closed before worker request was observed")
		}
	}
	value := <-pending.response
	return s.serverRequestResponse(kind, message.Params, value)
}

func (s *acpSession) reboundRequestID(params map[string]any, kind ActivityKind) (string, error) {
	durableID := firstString(params, "request_id", "requestId")
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	if durableID != "" {
		for index, request := range s.rebound {
			if request.RequestID != durableID {
				continue
			}
			if request.Kind != "" && request.Kind != kind {
				return "", fmt.Errorf("acp: durable request %q kind mismatch", durableID)
			}
			s.rebound = append(s.rebound[:index], s.rebound[index+1:]...)
			return durableID, nil
		}
		return "", fmt.Errorf("acp: durable request %q was not pending", durableID)
	}
	match := -1
	for index, request := range s.rebound {
		if request.Kind != "" && request.Kind != kind {
			continue
		}
		if match >= 0 {
			return "", fmt.Errorf("acp: multiple pending %s requests lack durable request_id", kind)
		}
		match = index
	}
	if match < 0 {
		return "", nil
	}
	requestID := s.rebound[match].RequestID
	s.rebound = append(s.rebound[:match], s.rebound[match+1:]...)
	return requestID, nil
}

func (s *acpSession) serverRequestResponse(kind ActivityKind, rawParams json.RawMessage, value string) (any, error) {
	if kind == ActivityUserInput {
		return map[string]string{"input": value}, nil
	}
	var permission struct {
		Options []acpPermissionOption `json:"options"`
	}
	_ = json.Unmarshal(rawParams, &permission)
	valueLower := strings.ToLower(strings.TrimSpace(value))
	approved := strings.Contains(valueLower, "approve") || strings.Contains(valueLower, "allow") || strings.Contains(valueLower, `"approved":true`)
	// Both ordinary approval and explicit trusted-local policy are one-shot.
	// The policy audit is recorded by WorkerService, not encoded as a durable
	// ACP permission choice.
	selected := selectPermissionOption(permission.Options, approved, false)
	if selected == "" {
		return nil, errors.New("harness has no matching permission option")
	}
	return map[string]any{"outcome": map[string]string{"outcome": "selected", "optionId": selected}}, nil
}

func selectPermissionOption(options []acpPermissionOption, approved, persistent bool) string {
	preferred := ""
	fallback := ""
	for _, option := range options {
		kind := strings.ToLower(strings.TrimSpace(option.Kind))
		matches := approved && strings.Contains(kind, "allow") || !approved && (strings.Contains(kind, "deny") || strings.Contains(kind, "reject"))
		if !matches || strings.TrimSpace(option.OptionID) == "" {
			continue
		}
		if strings.Contains(kind, "always") {
			if fallback == "" {
				fallback = option.OptionID
			}
			continue
		}
		if preferred == "" {
			preferred = option.OptionID
		}
	}
	if persistent {
		return fallback
	}
	return preferred
}

func (s *acpSession) Prompt(ctx context.Context, task string) error {
	if !s.beginTurn() {
		return errors.New("acp: session is already busy")
	}
	return s.promptTurn(ctx, task)
}

func (s *acpSession) Queue(ctx context.Context, task string) error {
	if task == "" {
		return errors.New("acp: queued prompt is empty")
	}
	s.turnMu.Lock()
	if s.busy {
		s.queued = append(s.queued, task)
		s.turnMu.Unlock()
		return nil
	}
	s.busy = true
	s.turnMu.Unlock()
	go func() { _ = s.promptTurn(ctx, task) }()
	return nil
}

func (s *acpSession) beginTurn() bool {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	if s.busy {
		return false
	}
	s.busy = true
	return true
}

func (s *acpSession) promptTurn(ctx context.Context, task string) error {
	defer s.finishTurn()
	s.textMu.Lock()
	s.turnText.Reset()
	s.textMu.Unlock()
	var response struct {
		Summary    string `json:"summary"`
		StopReason string `json:"stopReason"`
	}
	err := s.client.Request(ctx, "session/prompt", map[string]any{"sessionId": s.id, "prompt": []map[string]string{{"type": "text", "text": task}}}, &response)
	if err != nil {
		s.result <- Result{Status: "failed", Summary: err.Error()}
		return err
	}
	if response.Summary == "" {
		s.textMu.Lock()
		response.Summary = strings.TrimSpace(s.turnText.String())
		s.textMu.Unlock()
		if response.Summary == "" {
			response.Summary = "completed"
		}
	}
	status := "succeeded"
	if response.StopReason == "cancelled" || response.StopReason == "canceled" {
		status = "canceled"
	}
	s.result <- Result{Status: status, Summary: response.Summary}
	return nil
}

func (s *acpSession) finishTurn() {
	s.turnMu.Lock()
	s.busy = false
	var next string
	if len(s.queued) > 0 {
		next = s.queued[0]
		s.queued = s.queued[1:]
		s.busy = true
	}
	s.turnMu.Unlock()
	if next != "" {
		go func() { _ = s.promptTurn(context.Background(), next) }()
	}
}
func (s *acpSession) watch() {
	defer func() {
		s.closeActivity()
		s.activitySendWG.Wait()
		close(s.activity)
	}()
	for event := range s.client.Events() {
		if event.Method != "session/update" {
			continue
		}
		var envelope map[string]any
		if json.Unmarshal(event.Params, &envelope) != nil {
			continue
		}
		payload, _ := envelope["update"].(map[string]any)
		if payload == nil {
			payload = envelope
		}
		kind := valueString(payload["sessionUpdate"])
		if kind == "" {
			kind = valueString(envelope["sessionUpdate"])
		}
		content, _ := payload["content"].(map[string]any)
		if content == nil {
			content, _ = envelope["content"].(map[string]any)
		}
		emit := func(activity Activity) {
			s.emitActivity(activity)
		}
		switch kind {
		case "agent_message_chunk", "user_message_chunk":
			text := valueString(content["text"])
			if text == "" {
				continue
			}
			s.textMu.Lock()
			s.turnText.WriteString(text)
			s.textMu.Unlock()
			emit(Activity{Kind: ActivityText, Text: text})
		case "agent_thought_chunk", "thinking", "thinking_summary":
			// ACP thought chunks are not safe to display. Adapters may provide an
			// explicit short summary, but raw thought content is discarded.
			summary := valueString(payload["summary"])
			if summary == "" {
				summary = valueString(payload["title"])
			}
			if summary == "" {
				summary = "Working on the request."
			}
			if safeRuntimeSummary(summary) {
				emit(Activity{Kind: ActivityThinkingSummary, Summary: summary})
			}
		case "tool_call":
			tool := firstString(payload, "title", "name", "tool")
			if tool == "" {
				continue
			}
			emit(Activity{Kind: ActivityToolCall, Tool: tool, Arguments: jsonValue(payload, "rawInput", "input", "arguments")})
		case "tool_call_update", "tool_result":
			tool := firstString(payload, "title", "name", "tool")
			if tool == "" {
				continue
			}
			output := jsonValue(payload, "rawOutput", "output", "result")
			status := valueString(payload["status"])
			if len(output) == 0 && status != "completed" && status != "failed" && status != "error" {
				continue
			}
			result := string(output)
			if result == "" {
				result = status
			}
			emit(Activity{Kind: ActivityToolResult, Tool: tool, Result: result, Error: valueString(payload["error"]), Status: status})
		default:
			// Unknown ACP notifications are not converted into synthetic activity.
		}
	}
}

func firstString(value map[string]any, keys ...string) string {
	for _, key := range keys {
		if text := strings.TrimSpace(valueString(value[key])); text != "" {
			return text
		}
	}
	return ""
}

func jsonValue(value map[string]any, keys ...string) json.RawMessage {
	for _, key := range keys {
		if item, ok := value[key]; ok && item != nil {
			encoded, err := json.Marshal(item)
			if err == nil && json.Valid(encoded) {
				return encoded
			}
		}
	}
	return json.RawMessage(`{}`)
}
