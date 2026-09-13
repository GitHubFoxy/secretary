package webapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/coder/websocket"
)

func (s *Server) secretaryTurnRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/secretary/turns/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" || parts[1] != "stream" || (len(parts) == 3 && parts[2] != "ws") {
		http.NotFound(w, r)
		return
	}
	query := r.URL.Query()
	query.Set("turn_id", parts[0])
	r.URL.RawQuery = query.Encode()
	if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/ws") {
		s.secretaryWebsocket(w, r)
		return
	}
	s.secretaryStream(w, r)
}

func (s *Server) secretaryStream(w http.ResponseWriter, r *http.Request) {
	conversation, ok := s.authorizedConversationScope(w, r, core.ScopeConversationRead)
	if !ok {
		return
	}
	turnID := strings.TrimSpace(r.URL.Query().Get("turn_id"))
	if turnID == "" {
		http.Error(w, "turn_id is required", http.StatusBadRequest)
		return
	}
	turn, turnErr := s.store.SecretaryTurn(r.Context(), turnID)
	if errors.Is(turnErr, core.ErrNotFound) || (turnErr == nil && turn.ConversationID != conversation.ID) {
		http.NotFound(w, r)
		return
	}
	if turnErr != nil {
		http.Error(w, "read Secretary turn", http.StatusInternalServerError)
		return
	}
	after, err := parseAfter(r)
	if err != nil {
		http.Error(w, "invalid after_seq", http.StatusBadRequest)
		return
	}
	replay, err := s.store.ReplaySecretaryStream(r.Context(), turnID, after, 500)
	if err != nil {
		http.Error(w, "read Secretary stream", http.StatusInternalServerError)
		return
	}
	for i := range replay.Events {
		replay.Events[i] = sanitizePublicEvent(replay.Events[i])
	}
	writeJSON(w, http.StatusOK, replay)
}

func (s *Server) secretaryWebsocket(w http.ResponseWriter, r *http.Request) {
	person, client, ok := s.authorizedPerson(w, r)
	if !ok {
		return
	}
	if client != nil && !client.HasScope(core.ScopeConversationRead) {
		http.Error(w, "Client scope required", http.StatusForbidden)
		return
	}
	conversation, err := s.store.ConversationForPerson(r.Context(), person.ID)
	if err != nil {
		http.Error(w, "read conversation", http.StatusInternalServerError)
		return
	}
	turnID := strings.TrimSpace(r.URL.Query().Get("turn_id"))
	if turnID == "" {
		http.Error(w, "turn_id is required", http.StatusBadRequest)
		return
	}
	turn, turnErr := s.store.SecretaryTurn(r.Context(), turnID)
	if errors.Is(turnErr, core.ErrNotFound) || (turnErr == nil && turn.ConversationID != conversation.ID) {
		http.NotFound(w, r)
		return
	}
	if turnErr != nil {
		http.Error(w, "read Secretary turn", http.StatusInternalServerError)
		return
	}
	after, err := parseAfter(r)
	if err != nil {
		http.Error(w, "invalid after_seq", http.StatusBadRequest)
		return
	}
	connection, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer connection.CloseNow()
	streamContext, cancel := context.WithCancel(r.Context())
	clientID := ""
	if client != nil {
		clientID = client.ID
	}
	stream, accepted := s.registerStream(streamContext, clientID, bearerToken(r), cancel)
	if !accepted {
		_ = connection.Close(websocket.StatusPolicyViolation, "Client revoked")
		return
	}
	defer s.unregisterStream(clientID, stream)
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		events, readErr := s.store.SecretaryEvents(streamContext, turnID, after, 500)
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
