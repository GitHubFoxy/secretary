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
	session.RebindRequests(request.PendingRequestIDs)
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

type pendingACPResponse struct {
	response  chan string
	delivered chan error
}

func newACPSession(id string, client *acp.Client, busy bool) *acpSession {
	session := &acpSession{id: id, client: client, activity: make(chan Activity, 64), result: make(chan Result, 64), pending: make(map[string]*pendingACPResponse), resolved: make(map[string]struct{}), reboundResponses: make(map[string]*pendingACPResponse), nativeRequests: make(map[string]string), nativeDeliveries: make(map[string]*pendingACPResponse), busy: busy}
	client.SetServerRequestDeliveryHandler(session.serverRequestDelivered)
	return session
}

type acpSession struct {
	id       string
	client   *acp.Client
	activity chan Activity
	result   chan Result

	requestMu        sync.Mutex
	pending          map[string]*pendingACPResponse
	resolved         map[string]struct{}
	rebound          []string
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
func (s *acpSession) Close() error              { return s.client.Close() }
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
	s.requestMu.Lock()
	defer s.requestMu.Unlock()
	for _, requestID := range requestIDs {
		requestID = strings.TrimSpace(requestID)
		if requestID == "" {
			continue
		}
		if _, resolved := s.resolved[requestID]; resolved {
			continue
		}
		if _, pending := s.pending[requestID]; pending {
			continue
		}
		alreadyRebound := false
		for _, reboundID := range s.rebound {
			if reboundID == requestID {
				alreadyRebound = true
				break
			}
		}
		if !alreadyRebound {
			s.rebound = append(s.rebound, requestID)
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
		for i, reboundID := range s.rebound {
			if reboundID == requestID {
				s.rebound = append(s.rebound[:i], s.rebound[i+1:]...)
				pending = &pendingACPResponse{response: make(chan string, 1), delivered: make(chan error, 1)}
				s.reboundResponses[requestID] = pending
				s.resolved[requestID] = struct{}{}
				s.requestMu.Unlock()
				pending.response <- response
				return waitForACPDelivery(ctx, pending.delivered)
			}
		}
		_, alreadyResolved := s.resolved[requestID]
		s.requestMu.Unlock()
		if alreadyResolved {
			return nil
		}
		return errors.New("acp: unknown worker request")
	}
	s.resolved[requestID] = struct{}{}
	s.requestMu.Unlock()
	select {
	case pending.response <- response:
		return waitForACPDelivery(ctx, pending.delivered)
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
	requestID := s.nativeRequests[string(message.ID)]
	delete(s.nativeRequests, string(message.ID))
	pending := s.nativeDeliveries[string(message.ID)]
	delete(s.nativeDeliveries, string(message.ID))
	if pending == nil {
		pending = s.pending[requestID]
	}
	if pending == nil {
		pending = s.reboundResponses[requestID]
	}
	if pending != nil {
		delete(s.pending, requestID)
		delete(s.reboundResponses, requestID)
	}
	s.requestMu.Unlock()
	if pending != nil {
		pending.delivered <- err
	}
}

func (s *acpSession) handleServerRequest(message acp.Message) (any, error) {
	requestID := ""
	s.requestMu.Lock()
	if len(s.rebound) > 0 {
		requestID = s.rebound[0]
		s.rebound = s.rebound[1:]
	}
	s.requestMu.Unlock()
	if requestID == "" {
		requestID = fmt.Sprintf("request-%d-%d", time.Now().UnixNano(), acpRequestSequence.Add(1))
	}
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
	var value string
	var pending *pendingACPResponse
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
	s.nativeRequests[nativeID] = requestID
	s.nativeDeliveries[nativeID] = pending
	s.requestMu.Unlock()
	if !alreadyResponded {
		response := pending.response
		// Requests are the durable approval/input boundary. Unlike optional
		// activity updates, they must reach the Node outbox and cannot be
		// silently dropped when the activity buffer is full.
		s.activity <- Activity{Kind: kind, RequestID: requestID, Summary: summary}
		value = <-response
	} else {
		value = <-pending.response
	}
	if kind == ActivityUserInput {
		return map[string]string{"input": value}, nil
	}
	var permission struct {
		Options []acpPermissionOption `json:"options"`
	}
	_ = json.Unmarshal(message.Params, &permission)
	valueLower := strings.ToLower(strings.TrimSpace(value))
	approved := strings.Contains(valueLower, "approve") || strings.Contains(valueLower, "allow") || strings.Contains(valueLower, `"approved":true`)
	selected := selectPermissionOption(permission.Options, approved, strings.Contains(valueLower, "trusted-local"))
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
	defer close(s.activity)
	for event := range s.client.Events() {
		if event.Method != "session/update" {
			continue
		}
		var envelope struct {
			Update struct {
				SessionUpdate string `json:"sessionUpdate"`
				Content       struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
				Title  string `json:"title"`
				Status string `json:"status"`
			} `json:"update"`
			SessionUpdate string `json:"sessionUpdate"`
			Content       struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Title  string `json:"title"`
			Status string `json:"status"`
		}
		if json.Unmarshal(event.Params, &envelope) != nil {
			continue
		}
		payload := envelope.Update
		if payload.SessionUpdate == "" {
			payload.SessionUpdate = envelope.SessionUpdate
			payload.Content = envelope.Content
			payload.Title = envelope.Title
			payload.Status = envelope.Status
		}
		activity := Activity{Kind: ActivityStatus, Text: payload.SessionUpdate}
		switch payload.SessionUpdate {
		case "agent_message_chunk", "user_message_chunk":
			activity.Kind = ActivityText
			activity.Text = payload.Content.Text
			if payload.Content.Text != "" {
				s.textMu.Lock()
				s.turnText.WriteString(payload.Content.Text)
				s.textMu.Unlock()
			}
		case "tool_call", "tool_call_update":
			activity.Kind = ActivityTool
			activity.Text = payload.Title
			if activity.Text == "" {
				activity.Text = payload.Status
			}
		}
		if activity.Text == "" {
			continue
		}
		select {
		case s.activity <- activity:
		default:
		}
	}
}
