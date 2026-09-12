package webapi

import (
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
	if _, ok := s.authorizedConversationScope(w, r, core.ScopeConversationRead); !ok {
		return
	}
	turnID := strings.TrimSpace(r.URL.Query().Get("turn_id"))
	if turnID == "" {
		http.Error(w, "turn_id is required", http.StatusBadRequest)
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
	writeJSON(w, http.StatusOK, replay)
}

func (s *Server) secretaryWebsocket(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizedConversationScope(w, r, core.ScopeConversationRead); !ok {
		return
	}
	turnID := strings.TrimSpace(r.URL.Query().Get("turn_id"))
	if turnID == "" {
		http.Error(w, "turn_id is required", http.StatusBadRequest)
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
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		events, readErr := s.store.SecretaryEvents(r.Context(), turnID, after, 500)
		if readErr != nil {
			return
		}
		for _, event := range events {
			if err := connection.Write(r.Context(), websocket.MessageText, mustJSON(event)); err != nil {
				return
			}
			after = event.Seq
		}
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}
