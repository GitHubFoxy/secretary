package webapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

func (s *Server) approvalList(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizedConversationScope(w, r, core.ScopeApprovalRead); !ok {
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
	if _, ok := s.authorizedConversationScope(w, r, core.ScopeApprovalWrite); !ok {
		return
	}
	_, client, _ := s.authorizedPerson(w, r)
	clientID := "web-session"
	if client != nil {
		clientID = client.ID
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
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	operation := "approval:" + approval.RequestID
	payload := struct {
		Action   string
		Response string
		ClientID string
	}{parts[1], response, clientID}
	if key != "" {
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
		encoded, found, lookupErr := s.store.IdempotencyOutcomeForPayload(r.Context(), operation, key, payload)
		if lookupErr != nil {
			status := http.StatusInternalServerError
			if errors.Is(lookupErr, core.ErrIdempotencyConflict) {
				status = http.StatusConflict
			}
			http.Error(w, lookupErr.Error(), status)
			return
		}
		if found {
			var details core.WorkerDetails
			if err := json.Unmarshal(encoded, &details); err != nil {
				http.Error(w, "decode idempotency record", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, details)
			return
		}
	}
	details, err := s.responder.RespondWorker(r.Context(), ctl.MessageWorkerRequest{WorkerRef: worker.WorkerRef, Text: response, RequestID: approval.RequestID, ClientID: clientID, IdempotencyKey: key})
	if err != nil {
		http.Error(w, "resolve approval: "+err.Error(), http.StatusBadRequest)
		return
	}
	if key != "" {
		if err := s.store.RecordIdempotencyOutcomeWithPayload(r.Context(), operation, key, payload, details); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, core.ErrIdempotencyConflict) {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
	}
	writeJSON(w, http.StatusOK, details)
}
