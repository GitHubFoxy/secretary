package webapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

func (s *Server) approvalList(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizedConversation(w, r); !ok {
		return
	}
	approvals, err := s.store.Approvals(r.Context())
	if err != nil {
		http.Error(w, "read approvals", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, approvals)
}

func (s *Server) approvalRoute(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizedConversation(w, r); !ok {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/approvals/"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || (parts[1] != "approve" && parts[1] != "deny") || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	approval, err := s.store.Approval(r.Context(), parts[0])
	if errors.Is(err, core.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "read approval", http.StatusInternalServerError)
		return
	}
	if s.responder == nil {
		http.Error(w, "worker response service is unavailable", http.StatusServiceUnavailable)
		return
	}
	worker, err := s.store.Worker(r.Context(), approval.WorkerID)
	if err != nil {
		http.Error(w, "read approval worker", http.StatusInternalServerError)
		return
	}
	response := "denied"
	if parts[1] == "approve" {
		response = "approved"
	}
	details, err := s.responder.RespondWorker(r.Context(), ctl.MessageWorkerRequest{WorkerRef: worker.WorkerRef, Text: response, RequestID: approval.RequestID, ClientID: "web-session"})
	if err != nil {
		http.Error(w, "resolve approval: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, details)
}
