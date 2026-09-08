package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/beruseruko/secretary/internal/acp"
)

type ACPRuntime struct {
	Command   string
	Arguments []string
}

func (r ACPRuntime) Start(ctx context.Context, request StartRequest) (Session, error) {
	client, err := acp.Start(ctx, r.Command, r.Arguments...)
	if err != nil {
		return nil, err
	}
	if err := client.Request(ctx, "initialize", map[string]any{"protocolVersion": 1}, &map[string]any{}); err != nil {
		client.Close()
		return nil, err
	}
	var created struct {
		SessionID string `json:"sessionId"`
	}
	if err := client.Request(ctx, "session/new", map[string]any{"cwd": request.Workspace}, &created); err != nil {
		client.Close()
		return nil, err
	}
	if created.SessionID == "" {
		client.Close()
		return nil, fmt.Errorf("acp: session/new returned no sessionId")
	}
	session := &acpSession{id: created.SessionID, client: client, activity: make(chan Activity, 64), result: make(chan Result, 64), busy: true}
	go session.watch()
	go func() { _ = session.promptTurn(context.Background(), request.Task) }()
	return session, nil
}

type acpSession struct {
	id       string
	client   *acp.Client
	activity chan Activity
	result   chan Result

	turnMu sync.Mutex
	busy   bool
	queued []string
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
		response.Summary = "completed"
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
		var payload struct {
			SessionUpdate string `json:"sessionUpdate"`
			Content       struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			Title  string `json:"title"`
			Status string `json:"status"`
		}
		if json.Unmarshal(event.Params, &payload) != nil {
			continue
		}
		activity := Activity{Kind: ActivityStatus, Text: payload.SessionUpdate}
		switch payload.SessionUpdate {
		case "agent_message_chunk", "user_message_chunk":
			activity.Kind = ActivityText
			activity.Text = payload.Content.Text
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
