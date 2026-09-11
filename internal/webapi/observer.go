package webapi

import (
	"net/http"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
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
	SessionID string `json:"session_id"`
	State     string `json:"state"`
}

func (s *Server) workerRoute(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizedConversation(w, r); !ok {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/workers/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	workerRef := parts[0]
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
			writeJSON(w, http.StatusOK, workerStatus{WorkerRef: workerRef, SessionID: session.ID(), State: state})
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
		writeJSON(w, http.StatusOK, workerStatus{WorkerRef: workerRef, SessionID: details.Binding.RuntimeSessionID, State: state})
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
		writeJSON(w, http.StatusOK, details)
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
