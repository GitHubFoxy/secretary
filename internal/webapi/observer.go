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
	person, client, ok := s.authorizedPerson(w, r)
	if !ok {
		return
	}
	if client != nil && !client.HasScope(scope) {
		http.Error(w, "Client scope required", http.StatusForbidden)
		return
	}
	conversation, err := s.store.ConversationForPerson(r.Context(), person.ID)
	if err != nil {
		http.Error(w, "read conversation", http.StatusInternalServerError)
		return
	}
	if client != nil {
		r = r.WithContext(context.WithValue(r.Context(), authenticatedClientContextKey{}, client.ID))
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/workers/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 0 && parts[0] != "" {
		if details, err := s.store.WorkerDetailsForConversation(r.Context(), conversation.ID, parts[0]); err == nil {
			if s.phase4WorkerRoute(w, r, client, details, parts[1:]) {
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
		key, ok := requireIdempotencyKey(w, r, request.IdempotencyKey)
		if !ok {
			return
		}
		operation := "worker.legacy:respond:" + workerRef
		payload := struct {
			RequestID string
			Response  string
		}{request.RequestID, request.Response}
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
		if encoded, found, lookupErr := s.store.IdempotencyOutcomeForPayload(r.Context(), operation, key, payload); lookupErr != nil {
			writeClientMutationError(w, lookupErr)
			return
		} else if found {
			var details core.WorkerDetails
			if err := json.Unmarshal(encoded, &details); err != nil {
				http.Error(w, "decode idempotency record", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, publicWorkerMutationDTO(details))
			return
		}
		clientID, _ := r.Context().Value(authenticatedClientContextKey{}).(string)
		if clientID == "" {
			clientID = "web-session"
		}
		details, err := s.responder.RespondWorker(r.Context(), ctl.MessageWorkerRequest{WorkerRef: workerRef, Text: request.Response, RequestID: request.RequestID, ClientID: clientID, IdempotencyKey: key})
		if err != nil {
			writeClientMutationError(w, err)
			return
		}
		publicResult := publicWorkerMutationDTO(details)
		if err := s.store.RecordIdempotencyOutcomeWithPayload(r.Context(), operation, key, payload, publicResult); err != nil {
			writeClientMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, publicResult)
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
			Text           string `json:"text"`
			IdempotencyKey string `json:"idempotency_key,omitempty"`
		}
		if !decodeJSON(w, r, &request) {
			return
		}
		if request.Text == "" {
			http.Error(w, "text is required", http.StatusBadRequest)
			return
		}
		key, ok := requireIdempotencyKey(w, r, request.IdempotencyKey)
		if !ok {
			return
		}
		operation := "worker.legacy:steer:" + workerRef
		payload := struct{ Text string }{request.Text}
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
		if encoded, found, lookupErr := s.store.IdempotencyOutcomeForPayload(r.Context(), operation, key, payload); lookupErr != nil {
			writeClientMutationError(w, lookupErr)
			return
		} else if found {
			var outcome map[string]bool
			if err := json.Unmarshal(encoded, &outcome); err != nil {
				http.Error(w, "decode idempotency record", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusAccepted, outcome)
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
		outcome := map[string]bool{"injected": injected}
		if err := s.store.RecordIdempotencyOutcomeWithPayload(r.Context(), operation, key, payload, outcome); err != nil {
			writeClientMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, outcome)
	case parts[1] == "queue" && r.Method == http.MethodPost:
		var request struct {
			Text           string `json:"text"`
			IdempotencyKey string `json:"idempotency_key,omitempty"`
		}
		if !decodeJSON(w, r, &request) {
			return
		}
		if request.Text == "" {
			http.Error(w, "text is required", http.StatusBadRequest)
			return
		}
		key, ok := requireIdempotencyKey(w, r, request.IdempotencyKey)
		if !ok {
			return
		}
		operation := "worker.legacy:queue:" + workerRef
		payload := struct{ Text string }{request.Text}
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
		if encoded, found, lookupErr := s.store.IdempotencyOutcomeForPayload(r.Context(), operation, key, payload); lookupErr != nil {
			writeClientMutationError(w, lookupErr)
			return
		} else if found {
			var outcome map[string]string
			if err := json.Unmarshal(encoded, &outcome); err != nil {
				http.Error(w, "decode idempotency record", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusAccepted, outcome)
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
		outcome := map[string]string{"status": "queued"}
		if err := s.store.RecordIdempotencyOutcomeWithPayload(r.Context(), operation, key, payload, outcome); err != nil {
			writeClientMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, outcome)
	case parts[1] == "stop" && r.Method == http.MethodPost:
		key, ok := requireIdempotencyKey(w, r, "")
		if !ok {
			return
		}
		operation := "worker.legacy:stop:" + workerRef
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
		if encoded, found, lookupErr := s.store.IdempotencyOutcomeForPayload(r.Context(), operation, key, struct{}{}); lookupErr != nil {
			writeClientMutationError(w, lookupErr)
			return
		} else if found {
			var outcome map[string]string
			if err := json.Unmarshal(encoded, &outcome); err != nil {
				http.Error(w, "decode idempotency record", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusAccepted, outcome)
			return
		}
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
		outcome := map[string]string{"status": "cancel_requested"}
		if err := s.store.RecordIdempotencyOutcomeWithPayload(r.Context(), operation, key, struct{}{}, outcome); err != nil {
			writeClientMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, outcome)
	case parts[1] == "activity" && r.Method == http.MethodGet:
		s.workerActivity(w, r, session)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) phase4WorkerRoute(w http.ResponseWriter, r *http.Request, client *core.Client, details core.WorkerDetails, suffix []string) bool {
	if len(suffix) == 0 && r.Method == http.MethodGet {
		if client != nil {
			writeJSON(w, http.StatusOK, sanitizePublicJSON(publicWorkerDetailsStrictFromDetails(details)))
			return true
		}
		writeJSON(w, http.StatusOK, sanitizePublicJSON(details))
		return true
	}
	if len(suffix) == 1 && suffix[0] == "turns" && r.Method == http.MethodGet {
		if client != nil {
			writeJSON(w, http.StatusOK, sanitizePublicJSON(publicTurnsStrict(details)))
			return true
		}
		writeJSON(w, http.StatusOK, sanitizePublicJSON(details.Turns))
		return true
	}
	if len(suffix) == 1 && suffix[0] == "diagnostics" && r.Method == http.MethodGet {
		if client != nil {
			http.Error(w, "diagnostics are not available to Client credentials", http.StatusForbidden)
			return true
		}
		s.workerDiagnostics(w, r, details)
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
	if len(suffix) == 1 && suffix[0] == "respond" {
		return false
	}
	if len(suffix) != 1 || r.Method != http.MethodPost || s.actions == nil {
		return false
	}
	authenticatedClientID, _ := r.Context().Value(authenticatedClientContextKey{}).(string)
	if spoofed := strings.TrimSpace(r.Header.Get("X-Client-ID")); spoofed != "" && spoofed != authenticatedClientID {
		http.Error(w, "X-Client-ID does not match authenticated Client", http.StatusBadRequest)
		return true
	}
	if authenticatedClientID == "" {
		authenticatedClientID = "web-session"
	}
	request := ctl.MessageWorkerRequest{WorkerRef: details.Worker.WorkerRef, ClientID: authenticatedClientID}
	var payload struct {
		Text           string `json:"text,omitempty"`
		RequestID      string `json:"request_id,omitempty"`
		IdempotencyKey string `json:"idempotency_key,omitempty"`
	}
	if !decodeJSON(w, r, &payload) {
		return true
	}
	request.Text, request.RequestID = payload.Text, payload.RequestID
	if suffix[0] == "approve" && strings.TrimSpace(request.Text) == "" {
		request.Text = "approve"
	}
	key, ok := requireIdempotencyKey(w, r, payload.IdempotencyKey)
	if !ok {
		return true
	}
	request.IdempotencyKey = key
	operation := "worker.action:" + suffix[0] + ":" + details.Worker.WorkerRef
	payloadFingerprint := struct {
		Text      string
		RequestID string
	}{request.Text, request.RequestID}
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
		writeJSON(w, http.StatusAccepted, publicWorkerMutationDTO(stored))
		return true
	}
	var result core.WorkerDetails
	var err error
	switch suffix[0] {
	case "message", "follow-up":
		result, err = s.actions.MessageWorker(r.Context(), request)
	case "approve":
		if strings.TrimSpace(request.RequestID) == "" {
			http.Error(w, "request_id is required", http.StatusBadRequest)
			return true
		}
		if strings.TrimSpace(request.Text) == "" {
			request.Text = "approve"
		}
		result, err = s.actions.MessageWorker(r.Context(), request)
	case "cancel":
		result, err = s.actions.CancelWorker(r.Context(), details.Worker.WorkerRef)
	case "close":
		result, err = s.actions.CloseWorker(r.Context(), details.Worker.WorkerRef)
	default:
		return false
	}
	if err != nil {
		writeClientMutationError(w, err)
		return true
	}
	publicResult := publicWorkerMutationDTO(result)
	if err := s.store.RecordIdempotencyOutcomeWithPayload(r.Context(), operation, request.IdempotencyKey, payloadFingerprint, publicResult); err != nil {
		http.Error(w, "save idempotency record", http.StatusInternalServerError)
		return true
	}
	writeJSON(w, http.StatusAccepted, publicResult)
	return true
}

func (s *Server) workerDiagnostics(w http.ResponseWriter, r *http.Request, details core.WorkerDetails) {
	diagnostics, err := s.buildWorkerDiagnostics(details)
	if err != nil {
		http.Error(w, "read worker diagnostics", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, diagnostics)
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
	clientID, _ := r.Context().Value(authenticatedClientContextKey{}).(string)
	if clientID == "" {
		clientID = s.requestClientID(r)
	}
	stream, accepted := s.registerStream(streamContext, clientID, bearerToken(r), cancel)
	if !accepted {
		_ = connection.Close(websocket.StatusPolicyViolation, "Client revoked")
		return
	}
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
			if stream != nil && stream.revoked.Load() {
				_ = connection.Close(websocket.StatusPolicyViolation, "Client revoked")
			}
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

func publicWorkerMutationDTO(details core.WorkerDetails) any {
	return sanitizePublicJSON(details)
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
	case string:
		trimmed := strings.TrimSpace(current)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			var nested any
			if json.Unmarshal([]byte(trimmed), &nested) == nil {
				cleaned := sanitizePublicValue(nested)
				if encoded, err := json.Marshal(cleaned); err == nil {
					return string(encoded)
				}
				return "[redacted]"
			}
		}
		if forbiddenPublicText(current) {
			return "[redacted]"
		}
		return current
	default:
		return value
	}
}

func forbiddenPublicText(value string) bool {
	if containsKnownControlSensitiveValue(value) {
		return true
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"chain-of-thought", "chain of thought", "chain_of_thought", "raw thought", "raw_thought",
		"internal reasoning", "internal_reasoning", "thought process", "thought_process", "<think>", "</think>",
		"analysis:", "reasoning:", "thought:", "chain-of-thought:",
		"bearer ", "api_key=", "apikey=", "access_token", "api_token", "token=", "secret=", "credential=", "password=", "callback=",
		"runtime_session_id", "session_id", "sessionid", "sk-", "ghp_", "xoxb-", "xoxb_",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func sanitizePublicConversationEntry(entry core.ConversationEntry) core.ConversationEntry {
	entry.Body = sanitizePublicValue(entry.Body).(string)
	return entry
}

func forbiddenPublicKey(key string) bool {
	words := publicKeyWords(key)
	for i, word := range words {
		switch word {
		case "secret", "secrets", "credential", "credentials", "callback", "callbacks", "token", "tokens", "policy", "policies", "context", "contexts", "diagnostic", "diagnostics":
			return true
		case "task", "tasks":
			return true
		case "session", "sessions":
			if len(words) == 1 || (i+1 < len(words) && (words[i+1] == "id" || words[i+1] == "identifier")) || (i > 0 && words[i-1] == "runtime") {
				return true
			}
		case "id", "identifier":
			if i > 0 && (words[i-1] == "task" || words[i-1] == "session" || (i > 1 && words[i-2] == "runtime" && words[i-1] == "session")) {
				return true
			}
		}
	}

	// Uppercase acronyms such as RUNTIMESESSIONID have no case transition to
	// split. Compare their compact spelling against the forbidden compound keys.
	compact := strings.ToLower(strings.Map(func(character rune) rune {
		if unicode.IsLetter(character) || unicode.IsDigit(character) {
			return character
		}
		return -1
	}, key))
	for _, forbidden := range []string{
		"secret", "secrets", "credential", "credentials", "callback", "callbacks", "token", "tokens", "policy", "policies", "context", "contexts", "diagnostic", "diagnostics",
		"task", "tasks", "taskid", "tasksid", "session", "sessions", "sessionid", "sessionsid", "sessionidentifier", "sessionsidentifier", "runtimesession", "runtimesessionid", "accesstoken", "callbackcapability",
	} {
		if compact == forbidden || strings.HasSuffix(compact, forbidden) {
			return true
		}
	}
	for _, marker := range []string{"analysis", "reasoning", "thought", "chainofthought"} {
		if strings.Contains(compact, marker) {
			return true
		}
	}
	return false
}

func publicKeyWords(key string) []string {
	runes := []rune(strings.TrimSpace(key))
	words := make([]string, 0, 4)
	current := make([]rune, 0, len(runes))
	flush := func() {
		if len(current) == 0 {
			return
		}
		words = append(words, strings.ToLower(string(current)))
		current = current[:0]
	}
	for i, character := range runes {
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			flush()
			continue
		}
		if unicode.IsUpper(character) && len(current) > 0 {
			previous := runes[i-1]
			var next rune
			if i+1 < len(runes) {
				next = runes[i+1]
			}
			if unicode.IsLower(previous) || unicode.IsDigit(previous) || (unicode.IsUpper(previous) && unicode.IsLower(next)) {
				flush()
			}
		}
		current = append(current, unicode.ToLower(character))
	}
	flush()
	return words
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
	clientID, _ := r.Context().Value(authenticatedClientContextKey{}).(string)
	if clientID == "" {
		clientID = s.requestClientID(r)
	}
	stream, accepted := s.registerStream(streamContext, clientID, bearerToken(r), cancel)
	if !accepted {
		_ = connection.Close(websocket.StatusPolicyViolation, "Client revoked")
		return
	}
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
			if stream != nil && stream.revoked.Load() {
				_ = connection.Close(websocket.StatusPolicyViolation, "Client revoked")
			}
			return
		}
	}
}
