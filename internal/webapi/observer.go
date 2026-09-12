package webapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/node"
	"github.com/coder/websocket"
)

func (s *Server) workerSession(workerRef string) (node.Session, bool) {
	if s.workers != nil {
		return s.workers.Session(workerRef)
	}
	if s.node != nil {
		return s.node.Session(workerRef)
	}
	return nil, false
}

type workerStatus struct {
	WorkerRef string `json:"worker_ref"`
	State     string `json:"state"`
}

func (s *Server) workerRoute(w http.ResponseWriter, r *http.Request) {
	scope := core.ScopeWorkerRead
	if r.Method != http.MethodGet {
		scope = core.ScopeWorkerWrite
	}
	conversation, ok := s.authorizedConversationScope(w, r, scope)
	if !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/workers/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 0 && parts[0] != "" {
		if details, err := s.store.WorkerDetailsForConversation(r.Context(), conversation.ID, parts[0]); err == nil {
			if s.phase4WorkerRoute(w, r, details, parts[1:]) {
				return
			}
		}
	}
	if r.Header.Get("Authorization") != "" {
		http.NotFound(w, r)
		return
	}
	parts = strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	workerRef := parts[0]
	if len(parts) == 2 && parts[1] == "respond" && r.Method == http.MethodPost {
		if s.responder == nil {
			http.Error(w, "worker response service is unavailable", http.StatusServiceUnavailable)
			return
		}
		var request struct {
			RequestID      string `json:"request_id"`
			Response       string `json:"response"`
			ClientID       string `json:"client_id,omitempty"`
			IdempotencyKey string `json:"idempotency_key,omitempty"`
		}
		if !decodeJSON(w, r, &request) {
			return
		}
		if request.RequestID == "" || request.Response == "" {
			http.Error(w, "request_id and response are required", http.StatusBadRequest)
			return
		}
		details, err := s.responder.RespondWorker(r.Context(), ctl.MessageWorkerRequest{WorkerRef: workerRef, Text: request.Response, RequestID: request.RequestID, ClientID: "web-session", IdempotencyKey: request.IdempotencyKey})
		if err != nil {
			http.Error(w, "respond worker: "+err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, details)
		return
	}
	session, found := s.workerSession(workerRef)
	if len(parts) == 1 && r.Method == http.MethodGet {
		if found {
			s.mu.Lock()
			state := s.workerStates[workerRef]
			if state == "" {
				state = "active"
				s.workerStates[workerRef] = state
			}
			s.mu.Unlock()
			if task, taskErr := s.store.TaskForWorker(r.Context(), workerRef); taskErr == nil {
				if details, detailsErr := s.store.TaskDetails(r.Context(), task.ID); detailsErr == nil && len(details.Attempts) > 0 {
					attemptState := details.Attempts[len(details.Attempts)-1].State
					if attemptState.Terminal() {
						terminalState := string(attemptState)
						if state != "stopping" {
							state = terminalState
						}
						s.mu.Lock()
						if s.workerStates[workerRef] == "stopping" {
							s.workerStates[workerRef] = terminalState
						}
						s.mu.Unlock()
					}
				}
			}
			writeJSON(w, http.StatusOK, workerStatus{WorkerRef: workerRef, State: state})
			return
		}
		task, taskErr := s.store.TaskForWorker(r.Context(), workerRef)
		if taskErr != nil {
			http.Error(w, "worker not found", http.StatusNotFound)
			return
		}
		details, detailsErr := s.store.TaskDetails(r.Context(), task.ID)
		if detailsErr != nil || details.Binding == nil {
			http.Error(w, "worker not found", http.StatusNotFound)
			return
		}
		state := string(task.State)
		if len(details.Attempts) > 0 {
			attemptState := details.Attempts[len(details.Attempts)-1].State
			if attemptState.Terminal() {
				state = string(attemptState)
			}
		}
		writeJSON(w, http.StatusOK, workerStatus{WorkerRef: workerRef, State: state})
		return
	}
	if len(parts) == 2 && parts[1] == "thread" && r.Method == http.MethodGet {
		task, err := s.store.TaskForWorker(r.Context(), workerRef)
		if err != nil {
			http.Error(w, "worker not found", http.StatusNotFound)
			return
		}
		details, err := s.store.TaskDetails(r.Context(), task.ID)
		if err != nil {
			http.Error(w, "read worker thread", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, sanitizePublicJSON(details))
		return
	}
	if !found {
		http.Error(w, "worker not found", http.StatusNotFound)
		return
	}
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	switch {
	case parts[1] == "steer" && r.Method == http.MethodPost:
		var request struct {
			Text string `json:"text"`
		}
		if !decodeJSON(w, r, &request) {
			return
		}
		if request.Text == "" {
			http.Error(w, "text is required", http.StatusBadRequest)
			return
		}
		var injected bool
		var err error
		if s.workers != nil {
			injected, err = s.workers.Steer(r.Context(), workerRef, request.Text)
		} else {
			injected, err = session.Steer(r.Context(), request.Text)
		}
		if err != nil {
			http.Error(w, "steer worker", http.StatusBadGateway)
			return
		}
		_, _ = s.store.RecordEvent(r.Context(), "worker.steer_requested", workerRef, "", session.ID(), map[string]any{"text": request.Text, "injected": injected})
		writeJSON(w, http.StatusAccepted, map[string]bool{"injected": injected})
	case parts[1] == "queue" && r.Method == http.MethodPost:
		var request struct {
			Text string `json:"text"`
		}
		if !decodeJSON(w, r, &request) {
			return
		}
		if request.Text == "" {
			http.Error(w, "text is required", http.StatusBadRequest)
			return
		}
		var queuedAttempt core.Attempt
		if s.workers != nil {
			var queueErr error
			queuedAttempt, queueErr = s.workers.Queue(r.Context(), workerRef, request.Text)
			if queueErr != nil {
				http.Error(w, "queue worker input", http.StatusBadGateway)
				return
			}
		} else {
			queue, ok := session.(node.Queueer)
			if !ok {
				http.Error(w, "worker runtime does not support queued input", http.StatusNotImplemented)
				return
			}
			if err := queue.Queue(r.Context(), request.Text); err != nil {
				http.Error(w, "queue worker input", http.StatusBadGateway)
				return
			}
		}
		_, _ = s.store.RecordEvent(r.Context(), "worker.input_queued", workerRef, queuedAttempt.ID, session.ID(), map[string]string{"text": request.Text})
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
	case parts[1] == "stop" && r.Method == http.MethodPost:
		s.mu.Lock()
		s.workerStates[workerRef] = "stopping"
		s.mu.Unlock()
		var cancelErr error
		if s.workers != nil {
			cancelErr = s.workers.Stop(r.Context(), workerRef)
		} else {
			cancelErr = session.Cancel(r.Context())
		}
		if cancelErr != nil {
			s.mu.Lock()
			s.workerStates[workerRef] = "active"
			s.mu.Unlock()
			http.Error(w, "stop worker", http.StatusBadGateway)
			return
		}
		_, _ = s.store.RecordEvent(r.Context(), "worker.cancel_requested", workerRef, "", session.ID(), map[string]string{"source": "observer"})
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "cancel_requested"})
	case parts[1] == "activity" && r.Method == http.MethodGet:
		s.workerActivity(w, r, session)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) phase4WorkerRoute(w http.ResponseWriter, r *http.Request, details core.WorkerDetails, suffix []string) bool {
	if len(suffix) == 0 && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, sanitizePublicJSON(details))
		return true
	}
	if len(suffix) == 1 && suffix[0] == "turns" && r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, sanitizePublicJSON(details.Turns))
		return true
	}
	if len(suffix) >= 1 && suffix[0] == "activity" {
		if len(suffix) == 2 && suffix[1] == "ws" && r.Method == http.MethodGet {
			s.workerActivityReplay(w, r, details.Worker.WorkerRef)
			return true
		}
		if len(suffix) == 1 && r.Method == http.MethodGet {
			s.workerActivityReplayJSON(w, r, details.Worker.WorkerRef)
			return true
		}
	}
	if len(suffix) != 1 || r.Method != http.MethodPost || s.actions == nil {
		return false
	}
	request := ctl.MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, ClientID: r.Header.Get("X-Client-ID")}
	var payload struct {
		Text           string `json:"text,omitempty"`
		RequestID      string `json:"request_id,omitempty"`
		IdempotencyKey string `json:"idempotency_key,omitempty"`
	}
	if !decodeJSON(w, r, &payload) {
		return true
	}
	request.Text, request.RequestID, request.IdempotencyKey = payload.Text, payload.RequestID, payload.IdempotencyKey
	if request.IdempotencyKey == "" {
		request.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	operation := "worker.action:" + suffix[0] + ":" + details.Worker.WorkerRef
	payloadFingerprint := struct {
		Text      string
		RequestID string
	}{request.Text, request.RequestID}
	if request.IdempotencyKey != "" {
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
		if encoded, found, lookupErr := s.store.IdempotencyOutcomeForPayload(r.Context(), operation, request.IdempotencyKey, payloadFingerprint); lookupErr != nil {
			status := http.StatusInternalServerError
			if errors.Is(lookupErr, core.ErrIdempotencyConflict) {
				status = http.StatusConflict
			}
			http.Error(w, lookupErr.Error(), status)
			return true
		} else if found {
			var stored core.WorkerDetails
			if err := json.Unmarshal(encoded, &stored); err != nil {
				http.Error(w, "decode idempotency record", http.StatusInternalServerError)
				return true
			}
			writeJSON(w, http.StatusAccepted, stored)
			return true
		}
	}
	var result core.WorkerDetails
	var err error
	switch suffix[0] {
	case "message":
		result, err = s.actions.MessageWorker(r.Context(), request)
	case "cancel":
		result, err = s.actions.CancelWorker(r.Context(), details.Worker.WorkerRef)
	case "close":
		result, err = s.actions.CloseWorker(r.Context(), details.Worker.WorkerRef)
	default:
		return false
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return true
	}
	if request.IdempotencyKey != "" {
		if err := s.store.RecordIdempotencyOutcomeWithPayload(r.Context(), operation, request.IdempotencyKey, payloadFingerprint, result); err != nil {
			http.Error(w, "save idempotency record", http.StatusInternalServerError)
			return true
		}
	}
	writeJSON(w, http.StatusAccepted, result)
	return true
}

func (s *Server) workerActivityReplayJSON(w http.ResponseWriter, r *http.Request, workerRef string) {
	after, err := parseAfter(r)
	if err != nil {
		http.Error(w, "invalid after_seq", http.StatusBadRequest)
		return
	}
	events, err := s.workerEvents(r, workerRef, after)
	if err != nil {
		http.Error(w, "read worker activity", http.StatusInternalServerError)
		return
	}
	for i := range events {
		events[i] = sanitizePublicEvent(events[i])
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) workerActivityReplay(w http.ResponseWriter, r *http.Request, workerRef string) {
	connection, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer connection.CloseNow()
	streamContext, cancel := context.WithCancel(r.Context())
	clientID := s.requestClientID(r)
	stream := s.registerStream(clientID, cancel)
	defer s.unregisterStream(clientID, stream)
	after, err := parseAfter(r)
	if err != nil {
		_ = connection.Close(websocket.StatusPolicyViolation, "invalid after_seq")
		return
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		events, readErr := s.workerEventsWithContext(streamContext, workerRef, after)
		if readErr != nil {
			return
		}
		for _, event := range events {
			event = sanitizePublicEvent(event)
			if err := connection.Write(streamContext, websocket.MessageText, mustJSON(event)); err != nil {
				return
			}
			after = event.Seq
		}
		select {
		case <-streamContext.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) workerEvents(r *http.Request, workerRef string, after int64) ([]core.Event, error) {
	return s.workerEventsWithContext(r.Context(), workerRef, after)
}

func (s *Server) workerEventsWithContext(ctx context.Context, workerRef string, after int64) ([]core.Event, error) {
	return s.store.EventsForWorkerAfterSeq(ctx, workerRef, after, 500)
}

func sanitizePublicEvent(event core.Event) core.Event {
	var payload any
	if json.Unmarshal(event.Payload, &payload) == nil {
		if encoded, err := json.Marshal(sanitizePublicValue(payload)); err == nil {
			event.Payload = encoded
		}
	}
	return event
}

func sanitizePublicJSON(value any) any {
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return value
	}
	return sanitizePublicValue(decoded)
}

func sanitizePublicValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(current))
		for key, child := range current {
			if forbiddenPublicKey(key) {
				continue
			}
			result[key] = sanitizePublicValue(child)
		}
		return result
	case []any:
		result := make([]any, len(current))
		for i, child := range current {
			result[i] = sanitizePublicValue(child)
		}
		return result
	default:
		return value
	}
}

func forbiddenPublicKey(key string) bool {
	var normalizedBuilder strings.Builder
	for i, character := range strings.TrimSpace(key) {
		if unicode.IsUpper(character) {
			if i > 0 {
				normalizedBuilder.WriteByte('_')
			}
			character = unicode.ToLower(character)
		}
		if character == '-' || character == ' ' {
			character = '_'
		}
		normalizedBuilder.WriteRune(character)
	}
	normalized := normalizedBuilder.String()
	parts := strings.Split(normalized, "_")
	for i, part := range parts {
		switch part {
		case "secret", "secrets", "credential", "credentials", "callback", "callbacks", "token", "tokens":
			return true
		case "task", "tasks":
			return true
		case "session", "sessions":
			if i > 0 && parts[i-1] == "runtime" {
				return true
			}
		case "id":
			if i > 0 && (parts[i-1] == "task" || (i > 1 && parts[i-2] == "runtime" && parts[i-1] == "session")) {
				return true
			}
		}
	}
	return false
}

func sanitizePublicTaskDetails(details *core.TaskDetails) {
	if details == nil {
		return
	}
	if details.Binding != nil {
		details.Binding.RuntimeSessionID = ""
	}
	for i := range details.Children {
		sanitizePublicTaskDetails(&details.Children[i])
	}
}

func (s *Server) workerActivity(w http.ResponseWriter, r *http.Request, session node.Session) {
	connection, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer connection.CloseNow()
	streamContext, cancel := context.WithCancel(r.Context())
	clientID := s.requestClientID(r)
	stream := s.registerStream(clientID, cancel)
	defer s.unregisterStream(clientID, stream)
	for {
		select {
		case activity, open := <-session.Activity():
			if !open {
				_ = connection.Close(websocket.StatusNormalClosure, "worker completed")
				return
			}
			if err := connection.Write(streamContext, websocket.MessageText, mustJSON(sanitizePublicJSON(activity))); err != nil {
				return
			}
		case <-streamContext.Done():
			return
		}
	}
}
