package webapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

// publicApprovalDTO is the allowlisted Approval shape for credential Clients
// and the owner observer. Raw core.Approval carries additional request, node,
// project, attempt and audit identifiers plus the saved response.
type publicApprovalDTO struct {
	ID              string             `json:"id"`
	Kind            core.ApprovalKind  `json:"kind"`
	ActionSummary   string             `json:"action_summary"`
	RiskCategory    string             `json:"risk_category"`
	State           core.ApprovalState `json:"state"`
	ResolutionState core.ApprovalState `json:"resolution_state,omitempty"`
	RequestedAt     time.Time          `json:"requested_at"`
	ExpiresAt       *time.Time         `json:"expires_at"`
}

func publicApprovalDTOFromApproval(approval core.Approval) publicApprovalDTO {
	dto := publicApprovalDTO{
		ID: approval.ID, Kind: approval.Kind, ActionSummary: approval.ActionSummary,
		RiskCategory: approval.RiskCategory, State: approval.State,
		RequestedAt: approval.RequestedAt,
	}
	if approval.ExpiresAt != nil {
		dto.ExpiresAt = approval.ExpiresAt
	}
	if approval.State == core.ApprovalResolving {
		dto.ResolutionState = approval.ResolutionState
	}
	return dto
}

func writeApprovalRouteDetails(w http.ResponseWriter, status int, details core.WorkerDetails, strict bool) {
	if strict {
		writeJSON(w, status, sanitizePublicJSON(publicWorkerDetailsStrictFromDetails(details)))
		return
	}
	writeJSON(w, status, details)
}

func (s *Server) approvalList(w http.ResponseWriter, r *http.Request) {
	conversation, ok := s.authorizedConversationScope(w, r, core.ScopeApprovalRead)
	if !ok {
		return
	}
	limit, err := parseSnapshotLimit(r)
	if err != nil {
		http.Error(w, "invalid limit", http.StatusBadRequest)
		return
	}
	approvals, err := s.store.ApprovalsForConversation(r.Context(), conversation.ID, limit)
	if err != nil {
		http.Error(w, "read approvals", http.StatusInternalServerError)
		return
	}
	public := make([]publicApprovalDTO, 0, len(approvals))
	for _, approval := range approvals {
		public = append(public, publicApprovalDTOFromApproval(approval))
	}
	writeJSON(w, http.StatusOK, public)
}

func (s *Server) approvalRoute(w http.ResponseWriter, r *http.Request) {
	conversation, ok := s.authorizedConversationScope(w, r, core.ScopeApprovalWrite)
	if !ok {
		return
	}
	if s.approvalAuthorizationCheckpoint != nil {
		s.approvalAuthorizationCheckpoint()
	}
	person, client, authorized := s.authorizedPerson(w, r)
	if !authorized {
		return
	}
	if person.ID != conversation.PersonID || (client != nil && !client.HasScope(core.ScopeApprovalWrite)) {
		http.Error(w, "approval write scope required", http.StatusForbidden)
		return
	}
	clientID := "web-session"
	if client != nil {
		clientID = client.ID
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/approvals/"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || (parts[1] != "approve" && parts[1] != "deny" && parts[1] != "retry") || r.Method != http.MethodPost {
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
	var retryer approvalResolutionRetryer
	if parts[1] == "retry" {
		var ok bool
		retryer, ok = s.responder.(approvalResolutionRetryer)
		if !ok {
			http.Error(w, "approval retry service is unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	worker, err := s.store.Worker(r.Context(), approval.WorkerID)
	if err != nil {
		http.Error(w, "read approval worker", http.StatusInternalServerError)
		return
	}
	if parts[1] == "retry" {
		details, scopeErr := s.store.WorkerDetailsForConversation(r.Context(), conversation.ID, worker.WorkerRef)
		if errors.Is(scopeErr, core.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if scopeErr != nil {
			http.Error(w, "read approval worker", http.StatusInternalServerError)
			return
		}
		belongs := false
		for _, item := range details.Approvals {
			if item.ID == approval.ID {
				belongs = true
				break
			}
		}
		if !belongs {
			http.NotFound(w, r)
			return
		}
	}
	response := "denied"
	if parts[1] == "approve" {
		response = "approved"
	}
	var request struct {
		IdempotencyKey string `json:"idempotency_key,omitempty"`
	}
	if r.Body != nil && r.ContentLength != 0 && !decodeJSON(w, r, &request) {
		return
	}
	key, ok := requireIdempotencyKey(w, r, request.IdempotencyKey)
	if !ok {
		return
	}
	operation := "approval:" + approval.RequestID
	var payload any = struct {
		Action   string
		Response string
		ClientID string
	}{parts[1], response, clientID}
	if parts[1] == "retry" {
		operation = "approval-retry:" + approval.RequestID
		payload = struct {
			Action   string
			ClientID string
		}{parts[1], clientID}
	}
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
		writeApprovalRouteDetails(w, http.StatusOK, details, client != nil)
		return
	}
	var details core.WorkerDetails
	if parts[1] == "retry" {
		details, err = retryer.RetryApprovalResolution(r.Context(), approval.RequestID, clientID)
	} else {
		details, err = s.responder.RespondWorker(r.Context(), ctl.MessageWorkerRequest{WorkerRef: worker.WorkerRef, Text: response, RequestID: approval.RequestID, ClientID: clientID, IdempotencyKey: key})
	}
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, core.ErrApprovalResolutionConflict) || errors.Is(err, core.ErrInvalidTransition) || errors.Is(err, ctl.ErrWorkerCommandPending) {
			status = http.StatusConflict
		} else if errors.Is(err, core.ErrNotFound) {
			status = http.StatusNotFound
		}
		http.Error(w, "resolve approval: "+err.Error(), status)
		return
	}
	if err := s.store.RecordIdempotencyOutcomeWithPayload(r.Context(), operation, key, payload, details); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, core.ErrIdempotencyConflict) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeApprovalRouteDetails(w, http.StatusOK, details, client != nil)
}
