package webapi

import (
	"net/http"
	"strings"

	"github.com/beruseruko/secretary/internal/node"
	"github.com/coder/websocket"
)

type workerStatus struct {
	WorkerRef string `json:"worker_ref"`
	SessionID string `json:"session_id"`
	State     string `json:"state"`
}

func (s *Server) workerRoute(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizedConversation(w, r); !ok {
		return
	}
	if s.node == nil {
		http.Error(w, "local node unavailable", http.StatusServiceUnavailable)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/workers/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	workerRef := parts[0]
	session, found := s.node.Session(workerRef)
	if !found {
		http.Error(w, "worker not found", http.StatusNotFound)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		s.mu.Lock()
		state := s.workerStates[workerRef]
		if state == "" {
			state = "active"
			s.workerStates[workerRef] = state
		}
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, workerStatus{WorkerRef: workerRef, SessionID: session.ID(), State: state})
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
		injected, err := session.Steer(r.Context(), request.Text)
		if err != nil {
			http.Error(w, "steer worker", http.StatusBadGateway)
			return
		}
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
		queue, ok := session.(node.Queueer)
		if !ok {
			http.Error(w, "worker runtime does not support queued input", http.StatusNotImplemented)
			return
		}
		if err := queue.Queue(r.Context(), request.Text); err != nil {
			http.Error(w, "queue worker input", http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
	case parts[1] == "stop" && r.Method == http.MethodPost:
		s.mu.Lock()
		s.workerStates[workerRef] = "stopping"
		s.mu.Unlock()
		if err := session.Cancel(r.Context()); err != nil {
			s.mu.Lock()
			s.workerStates[workerRef] = "active"
			s.mu.Unlock()
			http.Error(w, "stop worker", http.StatusBadGateway)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "cancel_requested"})
	case parts[1] == "activity" && r.Method == http.MethodGet:
		s.workerActivity(w, r, session)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) workerActivity(w http.ResponseWriter, r *http.Request, session node.Session) {
	connection, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer connection.CloseNow()
	for {
		select {
		case activity, open := <-session.Activity():
			if !open {
				_ = connection.Close(websocket.StatusNormalClosure, "worker completed")
				return
			}
			if err := connection.Write(r.Context(), websocket.MessageText, mustJSON(activity)); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}
