package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

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
	client, err := r.connect(ctx, request.WorkerRef, profile, request.Workspace)
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
	session := newACPSession(created.SessionID, client, !request.DeferInitialPrompt)
	session.setRequestHandler()
	go session.watch()
	if !request.DeferInitialPrompt {
		go func() { _ = session.promptTurn(context.Background(), request.Task) }()
	}
	return session, nil
}

func (r ACPRuntime) Resume(ctx context.Context, request StartRequest, runtimeSessionID string) (Session, error) {
	profile, err := request.effectiveProfile()
	if err != nil {
		return nil, err
	}
	request.Profile = profile
	client, err := r.connect(ctx, request.WorkerRef, profile, request.Workspace)
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

func (r ACPRuntime) connect(ctx context.Context, workerRef string, profile ManagedProfile, workspace string) (*acp.Client, error) {
	var rawLog io.WriteCloser
	if r.RawLogDir != "" {
		var err error
		rawLog, err = openRotatingLog(filepath.Join(r.RawLogDir, workerRef+".jsonl"), r.RawLogMaxBytes, r.RawLogFiles)
		if err != nil {
			return nil, fmt.Errorf("open raw ACP log: %w", err)
		}
		rawLog = newRedactingACPLog(rawLog)
	}
	environment, err := profileEnvironment(r.Environment, profile)
	if err != nil {
		if rawLog != nil {
			_ = rawLog.Close()
		}
		return nil, err
	}
	client, err := acp.StartWithLogEnvDir(ctx, rawLog, environment, workspace, r.Command, r.Arguments...)
	if err != nil {
		if rawLog != nil {
			_ = rawLog.Close()
		}
		return nil, err
	}
	var initialized struct {
		ProtocolVersion *int `json:"protocolVersion"`
	}
	if err := client.Request(ctx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientInfo":         map[string]string{"name": "secretary", "version": "0.1.0"},
		"clientCapabilities": acpClientCapabilities(),
	}, &initialized); err != nil {
		client.Close()
		return nil, err
	}
	if initialized.ProtocolVersion == nil || *initialized.ProtocolVersion != 1 {
		client.Close()
		return nil, fmt.Errorf("acp: unsupported negotiated protocol version %v", initialized.ProtocolVersion)
	}
	return client, nil
}

func acpClientCapabilities() map[string]any {
	return map[string]any{
		"terminal":    false,
		"elicitation": map[string]any{"form": map[string]any{}},
	}
}

type redactingACPLog struct {
	writer io.Writer
	closer io.Closer
}

func newRedactingACPLog(log io.WriteCloser) io.WriteCloser {
	return &redactingACPLog{writer: log, closer: log}
}

func (l *redactingACPLog) Write(raw []byte) (int, error) {
	return l.WriteACPFrame("unknown", raw)
}

func (l *redactingACPLog) WriteACPFrame(direction string, raw []byte) (int, error) {
	redacted := redactACPLogFrame(direction, raw)
	if _, err := l.writer.Write(redacted); err != nil {
		return 0, err
	}
	return len(raw), nil
}

func (l *redactingACPLog) Close() error { return l.closer.Close() }

func redactACPLogLine(raw []byte) []byte { return redactACPLogFrame("unknown", raw) }

func redactACPLogFrame(direction string, raw []byte) []byte {
	var message map[string]any
	if json.Unmarshal(raw, &message) != nil {
		encoded, _ := json.Marshal(map[string]any{
			"observed_at": time.Now().UTC().Format(time.RFC3339Nano),
			"direction":   direction,
			"kind":        "non_json",
			"redacted":    true,
		})
		return append(encoded, '\n')
	}
	redacted := false
	safe := make(map[string]any, 8)
	safe["observed_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	safe["direction"] = direction
	for _, key := range []string{"jsonrpc", "method"} {
		if value, ok := message[key]; ok {
			safe[key] = value
		}
	}
	if _, hasParams := message["params"]; hasParams {
		safe["params"] = map[string]any{"redacted": true}
		redacted = true
	}
	if _, hasResult := message["result"]; hasResult {
		safe["result"] = map[string]any{"redacted": true}
		redacted = true
	}
	if _, hasError := message["error"]; hasError {
		code := 0
		if errorObject, ok := message["error"].(map[string]any); ok {
			if value, ok := errorObject["code"].(float64); ok {
				code = int(value)
			}
		}
		safe["error"] = map[string]any{"code": code, "message": "[redacted]"}
		redacted = true
	}
	for key := range message {
		if key != "jsonrpc" && key != "id" && key != "method" && key != "params" && key != "result" && key != "error" {
			redacted = true
		}
	}
	if id, hasID := message["id"]; hasID {
		fingerprint := sha256.Sum256([]byte(fmt.Sprint(id)))
		safe["id_fingerprint"] = fmt.Sprintf("%x", fingerprint[:6])
		if _, hasMethod := message["method"]; hasMethod {
			safe["kind"] = "request"
		} else {
			safe["kind"] = "response"
		}
		redacted = true
	} else if _, hasMethod := message["method"]; hasMethod {
		safe["kind"] = "notification"
	}
	if method, _ := message["method"].(string); method == "session/update" {
		if params, ok := message["params"].(map[string]any); ok {
			if update, ok := params["update"].(map[string]any); ok {
				if kind, ok := update["sessionUpdate"].(string); ok {
					safe["session_update"] = kind
				}
			}
		}
	}
	safe["redacted"] = redacted
	encoded, err := json.Marshal(safe)
	if err != nil {
		return []byte("[redacted ACP output]\n")
	}
	if len(raw) > 0 && raw[len(raw)-1] == '\n' {
		encoded = append(encoded, '\n')
	}
	return encoded
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
		return s.waitForACPDelivery(ctx, delivered)
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
		return s.waitForACPDelivery(ctx, delivered)
	}
	delivered := pending.delivered
	responseCh := pending.response
	s.requestMu.Unlock()
	select {
	case responseCh <- response:
		return s.waitForACPDelivery(ctx, delivered)
	case <-ctx.Done():
		return ctx.Err()
	case <-s.activityDone:
		return errors.New("acp: session closed before interaction response delivery")
	}
}

func (s *acpSession) waitForACPDelivery(ctx context.Context, delivered <-chan error) error {
	select {
	case err := <-delivered:
		return err
	case <-ctx.Done():
		return ctx.Err()
	case <-s.activityDone:
		return errors.New("acp: session closed before interaction response delivery")
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
	if err := json.Unmarshal(message.Params, &params); err != nil || params == nil {
		return nil, acp.NewRequestError(-32602, "invalid harness request params")
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
	} else if message.Method == "elicitation/create" {
		kind = ActivityUserInput
		var elicitation acpElicitationRequest
		if err := json.Unmarshal(message.Params, &elicitation); err != nil || strings.TrimSpace(elicitation.Mode) != "form" {
			return nil, invalidElicitationRequest("unsupported or invalid ACP elicitation request")
		}
		if strings.TrimSpace(elicitation.Message) == "" {
			return nil, invalidElicitationRequest("ACP elicitation message is required")
		}
		if strings.TrimSpace(elicitation.SessionID) == "" {
			_, present, valid := canonicalElicitationRequestID(elicitation.RequestID)
			if !present || !valid {
				return nil, invalidElicitationRequest("ACP elicitation scope is required")
			}
		}
		if err := validateElicitationSchema(elicitation.RequestedSchema); err != nil {
			return nil, invalidElicitationRequest(err.Error())
		}
		if containsSensitiveElicitationText(elicitation.Message) {
			return nil, invalidElicitationRequest("ACP form elicitation cannot request secrets or credentials")
		}
		summary = strings.TrimSpace(elicitation.Message)
	} else {
		return nil, fmt.Errorf("unsupported harness request: %s", message.Method)
	}

	var requestID string
	var err error
	if message.Method == "elicitation/create" {
		var requestIDRaw json.RawMessage
		var elicitation acpElicitationRequest
		if json.Unmarshal(message.Params, &elicitation) == nil {
			requestIDRaw = elicitation.RequestID
		}
		candidate, present, valid := canonicalElicitationRequestID(requestIDRaw)
		if valid && (!present || candidate == "") && len(message.ID) > 0 {
			requestIDRaw = append(json.RawMessage(nil), message.ID...)
		}
		requestID, err = s.elicitationRequestID(requestIDRaw, kind)
	} else {
		requestID, err = s.reboundRequestID(params, kind)
	}
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
		activity := Activity{Kind: kind, RequestID: requestID, Summary: summary}
		if message.Method == "elicitation/create" {
			var elicitation acpElicitationRequest
			if err := json.Unmarshal(message.Params, &elicitation); err != nil {
				return nil, invalidElicitationRequest("invalid ACP elicitation request")
			}
			activity.RequestSchema = append(json.RawMessage(nil), elicitation.RequestedSchema...)
		}
		if !s.emitActivity(activity) {
			return nil, errors.New("acp: session closed before worker request was observed")
		}
	}
	select {
	case value := <-pending.response:
		return s.serverRequestResponse(kind, message.Params, value)
	case <-s.activityDone:
		return nil, acp.NewRequestError(-32800, "ACP session closed before interaction response")
	}
}

func (s *acpSession) elicitationRequestID(requestID json.RawMessage, kind ActivityKind) (string, error) {
	candidate, present, valid := canonicalElicitationRequestID(requestID)
	if present && !valid {
		return "", invalidElicitationRequest("ACP elicitation requestId must be a string or number")
	}
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	for index, request := range s.rebound {
		if candidate == "" || request.RequestID != candidate {
			continue
		}
		if request.Kind != "" && request.Kind != kind {
			return "", fmt.Errorf("acp: elicitation request %q kind mismatch", candidate)
		}
		s.rebound = append(s.rebound[:index], s.rebound[index+1:]...)
		return candidate, nil
	}
	for _, request := range s.rebound {
		if request.Kind == "" || request.Kind == kind {
			return "", errors.New("acp: elicitation cannot safely rebind a pending request")
		}
	}
	if candidate != "" {
		if _, pending := s.pending[candidate]; pending {
			return "", fmt.Errorf("acp: elicitation request %q is already pending", candidate)
		}
		return candidate, nil
	}
	return "", nil
}

func canonicalElicitationRequestID(raw json.RawMessage) (string, bool, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", false, true
	}
	var typed string
	if json.Unmarshal(trimmed, &typed) == nil {
		if len(typed) > 256 {
			return "", true, false
		}
		return "elicitation:string:" + typed, true, true
	}
	if len(trimmed) > 256 || !json.Valid(trimmed) || strings.ContainsAny(string(trimmed), "{}[]\"") {
		return "", true, false
	}
	var number big.Int
	if _, ok := number.SetString(string(trimmed), 10); !ok {
		return "", true, false
	}
	return "elicitation:number:" + string(trimmed), true, true
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

type acpElicitationRequest struct {
	Mode            string          `json:"mode"`
	Message         string          `json:"message"`
	SessionID       string          `json:"sessionId"`
	RequestID       json.RawMessage `json:"requestId"`
	RequestedSchema json.RawMessage `json:"requestedSchema"`
}

func invalidElicitationRequest(message string) error {
	return acp.NewRequestError(-32602, message)
}

type acpElicitationProperty struct {
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Default     json.RawMessage `json:"default"`
	Description string          `json:"description"`
	Items       *acpArrayItems  `json:"items"`
	Enum        []any           `json:"enum"`
	OneOf       []acpEnumOption `json:"oneOf"`
	MinLength   *int            `json:"minLength"`
	MaxLength   *int            `json:"maxLength"`
	Pattern     string          `json:"pattern"`
	Format      string          `json:"format"`
	Minimum     *float64        `json:"minimum"`
	Maximum     *float64        `json:"maximum"`
	MinItems    *int            `json:"minItems"`
	MaxItems    *int            `json:"maxItems"`
}

type acpArrayItems struct {
	Type  string          `json:"type"`
	Enum  []any           `json:"enum"`
	AnyOf []acpEnumOption `json:"anyOf"`
}

type acpEnumOption struct {
	Title string `json:"title"`
	Const any    `json:"const"`
}

func validateElicitationSchema(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return errors.New("invalid ACP elicitation form schema")
	}
	var schema struct {
		Type        string                     `json:"type"`
		Title       string                     `json:"title"`
		Description string                     `json:"description"`
		Properties  map[string]json.RawMessage `json:"properties"`
		Required    []string                   `json:"required"`
	}
	if json.Unmarshal(trimmed, &schema) != nil || schema.Type != "" && schema.Type != "object" {
		return errors.New("invalid ACP elicitation form schema")
	}
	if containsSensitiveElicitationText(schema.Title) || containsSensitiveElicitationText(schema.Description) {
		return errors.New("ACP form elicitation schema requests a secret or credential")
	}
	for name, property := range schema.Properties {
		if strings.TrimSpace(name) == "" || len(property) == 0 {
			return errors.New("invalid ACP elicitation property")
		}
		if err := validateElicitationProperty(name, property); err != nil {
			return err
		}
	}
	for _, name := range schema.Required {
		if _, ok := schema.Properties[name]; !ok {
			return fmt.Errorf("ACP elicitation required property %q is not declared", name)
		}
	}
	return nil
}

func validateElicitationProperty(name string, raw json.RawMessage) error {
	var definition acpElicitationProperty
	if json.Unmarshal(raw, &definition) != nil || !allowedElicitationPropertyType(definition.Type) {
		return fmt.Errorf("unsupported ACP elicitation property %q", name)
	}
	if definition.Type == "array" && (definition.Items == nil || definition.Items.Type != "string" && len(definition.Items.AnyOf) == 0) {
		return fmt.Errorf("ACP elicitation array property %q must contain strings", name)
	}
	if len(definition.Default) > 0 {
		var defaultValue any
		if json.Unmarshal(definition.Default, &defaultValue) != nil || !elicitationValueMatchesType(defaultValue, definition) {
			return fmt.Errorf("ACP elicitation default has invalid type for %q", name)
		}
	}
	if definition.Type != "string" && (len(definition.Enum) > 0 || len(definition.OneOf) > 0) {
		return fmt.Errorf("ACP elicitation enum is only supported for string property %q", name)
	}
	for _, candidate := range definition.Enum {
		if !elicitationPrimitiveMatchesType(candidate, definition.Type) {
			return fmt.Errorf("ACP elicitation enum has invalid type for %q", name)
		}
	}
	for _, option := range definition.OneOf {
		if strings.TrimSpace(option.Title) == "" || !elicitationPrimitiveMatchesType(option.Const, definition.Type) {
			return fmt.Errorf("ACP elicitation oneOf has invalid option for %q", name)
		}
	}
	if definition.Items != nil {
		for _, candidate := range definition.Items.Enum {
			if !elicitationPrimitiveMatchesType(candidate, "string") {
				return fmt.Errorf("ACP elicitation array enum has invalid type for %q", name)
			}
		}
		for _, option := range definition.Items.AnyOf {
			if strings.TrimSpace(option.Title) == "" || !elicitationPrimitiveMatchesType(option.Const, "string") {
				return fmt.Errorf("ACP elicitation array anyOf has invalid option for %q", name)
			}
		}
	}
	if containsSensitiveElicitationText(name) || containsSensitiveElicitationText(definition.Title) || containsSensitiveElicitationText(definition.Description) {
		return fmt.Errorf("ACP form elicitation property %q requests a secret or credential", name)
	}
	if definition.Format != "" && !allowedElicitationStringFormat(definition.Format) {
		return fmt.Errorf("unsupported ACP elicitation format for %q", name)
	}
	if definition.Pattern != "" {
		if _, err := regexp.Compile(definition.Pattern); err != nil {
			return fmt.Errorf("invalid ACP elicitation pattern for %q", name)
		}
	}
	if definition.MinLength != nil && *definition.MinLength < 0 || definition.MaxLength != nil && *definition.MaxLength < 0 {
		return fmt.Errorf("invalid ACP elicitation string bounds for %q", name)
	}
	if definition.MinItems != nil && *definition.MinItems < 0 || definition.MaxItems != nil && *definition.MaxItems < 0 {
		return fmt.Errorf("invalid ACP elicitation item bounds for %q", name)
	}
	if definition.MinLength != nil && definition.MaxLength != nil && *definition.MinLength > *definition.MaxLength {
		return fmt.Errorf("invalid ACP elicitation length range for %q", name)
	}
	if definition.Minimum != nil && definition.Maximum != nil && *definition.Minimum > *definition.Maximum {
		return fmt.Errorf("invalid ACP elicitation numeric range for %q", name)
	}
	if definition.Type == "integer" && (definition.Minimum != nil && !validIntegerBound(*definition.Minimum) || definition.Maximum != nil && !validIntegerBound(*definition.Maximum)) {
		return fmt.Errorf("invalid ACP elicitation integer range for %q", name)
	}
	if definition.MinItems != nil && definition.MaxItems != nil && *definition.MinItems > *definition.MaxItems {
		return fmt.Errorf("invalid ACP elicitation item range for %q", name)
	}
	return nil
}

func validIntegerBound(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && math.Trunc(value) == value
}

func containsSensitiveElicitationText(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{"password", "passphrase", "api key", "apikey", "access token", "refresh token", "private key", "recovery code", "credential", "secret", "passcode", "pin", "token"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func allowedElicitationStringFormat(format string) bool {
	switch format {
	case "email", "uri", "date", "date-time":
		return true
	default:
		return false
	}
}

func allowedElicitationPropertyType(kind string) bool {
	switch kind {
	case "string", "number", "integer", "boolean", "array":
		return true
	default:
		return false
	}
}

func (s *acpSession) serverRequestResponse(kind ActivityKind, rawParams json.RawMessage, value string) (any, error) {
	if kind == ActivityUserInput {
		var params acpElicitationRequest
		if json.Unmarshal(rawParams, &params) == nil && strings.TrimSpace(params.Mode) == "form" {
			response, err := elicitationResponse(params.RequestedSchema, value)
			if err != nil {
				return nil, invalidElicitationRequest(err.Error())
			}
			return response, nil
		}
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

func elicitationResponse(rawSchema json.RawMessage, value string) (any, error) {
	trimmed := strings.TrimSpace(value)
	switch strings.ToLower(trimmed) {
	case "decline":
		return map[string]string{"action": "decline"}, nil
	case "cancel":
		return map[string]string{"action": "cancel"}, nil
	case "":
		return nil, errors.New("ACP elicitation response is empty")
	}
	if err := validateElicitationSchema(rawSchema); err != nil {
		return nil, err
	}
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if err := json.Unmarshal(rawSchema, &schema); err != nil {
		return nil, errors.New("invalid ACP elicitation form schema")
	}
	content := make(map[string]any)
	if json.Unmarshal([]byte(trimmed), &content) != nil {
		if len(schema.Properties) != 1 {
			return nil, errors.New("ACP elicitation response must be a JSON object for multiple fields")
		}
		for name := range schema.Properties {
			content[name] = trimmed
		}
	}
	if err := validateElicitationContent(content, schema.Properties, schema.Required); err != nil {
		return nil, err
	}
	return map[string]any{"action": "accept", "content": content}, nil
}

func validateElicitationContent(content map[string]any, properties map[string]json.RawMessage, required []string) error {
	for name, value := range content {
		property, ok := properties[name]
		if !ok {
			return fmt.Errorf("ACP elicitation response contains unknown property %q", name)
		}
		var definition acpElicitationProperty
		if err := json.Unmarshal(property, &definition); err != nil || !elicitationValueMatchesType(value, definition) {
			return fmt.Errorf("ACP elicitation response has invalid value for %q", name)
		}
	}
	for _, name := range required {
		if _, ok := content[name]; !ok {
			return fmt.Errorf("ACP elicitation response is missing required property %q", name)
		}
	}
	return nil
}

func elicitationValueMatchesType(value any, definition acpElicitationProperty) bool {
	switch definition.Type {
	case "string":
		text, ok := value.(string)
		if !ok || (definition.MinLength != nil && utf8.RuneCountInString(text) < *definition.MinLength) || (definition.MaxLength != nil && utf8.RuneCountInString(text) > *definition.MaxLength) {
			return false
		}
		if !validElicitationStringFormat(text, definition.Format) {
			return false
		}
		if definition.Pattern != "" {
			matched, err := regexp.MatchString(definition.Pattern, text)
			if err != nil || !matched {
				return false
			}
		}
		return elicitationValueAllowed(text, definition.Enum, definition.OneOf)
	case "number":
		number, ok := value.(float64)
		return ok && number >= optionalFloat(definition.Minimum, number) && number <= optionalFloat(definition.Maximum, number) && elicitationValueAllowed(number, definition.Enum, definition.OneOf)
	case "integer":
		number, ok := value.(float64)
		return ok && number == float64(int64(number)) && number >= optionalFloat(definition.Minimum, number) && number <= optionalFloat(definition.Maximum, number) && elicitationValueAllowed(number, definition.Enum, definition.OneOf)
	case "boolean":
		_, ok := value.(bool)
		return ok && elicitationValueAllowed(value, definition.Enum, definition.OneOf)
	case "array":
		items, ok := value.([]any)
		if !ok || (definition.MinItems != nil && len(items) < *definition.MinItems) || (definition.MaxItems != nil && len(items) > *definition.MaxItems) {
			return false
		}
		for _, item := range items {
			text, ok := item.(string)
			if !ok || definition.Items == nil || !elicitationValueAllowed(text, definition.Items.Enum, definition.Items.AnyOf) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func validElicitationStringFormat(value, format string) bool {
	switch format {
	case "":
		return true
	case "email":
		return regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`).MatchString(value)
	case "uri":
		parsed, err := url.ParseRequestURI(value)
		return err == nil && parsed.IsAbs()
	case "date":
		_, err := time.Parse("2006-01-02", value)
		return err == nil
	case "date-time":
		_, err := time.Parse(time.RFC3339, value)
		return err == nil
	default:
		return false
	}
}

func optionalFloat(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
}

func elicitationPrimitiveMatchesType(value any, kind string) bool {
	switch kind {
	case "string":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		number, ok := value.(float64)
		return ok && number == float64(int64(number))
	case "boolean":
		_, ok := value.(bool)
		return ok
	default:
		return false
	}
}

func elicitationValueAllowed(value any, enum []any, oneOf []acpEnumOption) bool {
	if len(enum) == 0 && len(oneOf) == 0 {
		return true
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return false
	}
	for _, candidate := range enum {
		candidateJSON, candidateErr := json.Marshal(candidate)
		if candidateErr == nil && string(candidateJSON) == string(encoded) {
			return true
		}
	}
	for _, option := range oneOf {
		candidateJSON, candidateErr := json.Marshal(option.Const)
		if candidateErr == nil && string(candidateJSON) == string(encoded) {
			return true
		}
	}
	return false
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
