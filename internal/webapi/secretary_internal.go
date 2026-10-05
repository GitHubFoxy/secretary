package webapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
)

type secretaryToolCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type secretaryToolCallResponse struct {
	Value any    `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

// secretaryToolCall is an internal bridge for the local Secretary MCP process.
// It is intentionally not a Client API: only the server-issued runtime
// capability can authorize it, and the actual WorkerService remains in this
// process where Node connections and runtimes are available.
func (s *Server) secretaryToolCall(w http.ResponseWriter, r *http.Request) {
	capability := strings.TrimSpace(bearerToken(r))
	if capability == "" {
		http.Error(w, "Secretary capability required", http.StatusUnauthorized)
		return
	}
	allowed, err := s.store.AuthorizeSecretaryCapability(r.Context(), s.owner.ID, capability)
	if err != nil {
		http.Error(w, "authorize Secretary capability", http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, "invalid Secretary capability", http.StatusUnauthorized)
		return
	}
	if s.secretaryTools == nil {
		http.Error(w, "Secretary Worker service is unavailable", http.StatusServiceUnavailable)
		return
	}
	var request secretaryToolCallRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if strings.TrimSpace(request.Name) == "" {
		http.Error(w, "tool name is required", http.StatusBadRequest)
		return
	}
	if request.Arguments == nil {
		request.Arguments = json.RawMessage(`{}`)
	}
	value, callErr := s.callSecretaryTool(r, request.Name, request.Arguments, s.owner.ID, capability)
	if callErr != nil {
		writeJSON(w, http.StatusOK, secretaryToolCallResponse{Error: callErr.Error()})
		return
	}
	writeJSON(w, http.StatusOK, secretaryToolCallResponse{Value: value})
}

func (s *Server) callSecretaryTool(r *http.Request, name string, raw json.RawMessage, personID, capability string) (any, error) {
	var args struct {
		WorkerRef       string `json:"worker_ref"`
		Text            string `json:"text"`
		RequestID       string `json:"request_id"`
		SecretaryTurnID string `json:"secretary_turn_id"`
		InputID         string `json:"input_id"`
		ctl.SpawnWorkerRequest
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	addressedReplyEnabled := s.addressedReplyEnabled()
	switch name {
	case "list_nodes":
		return s.secretaryTools.ListNodes(r.Context())
	case "list_projects":
		return s.secretaryTools.ListProjects(r.Context())
	case "list_workers":
		return s.secretaryTools.ListWorkers(r.Context())
	case "get_worker":
		return s.secretaryTools.GetWorker(r.Context(), args.WorkerRef)
	case "spawn_worker":
		if addressedReplyEnabled {
			if err := s.store.ValidateSecretaryOrigin(r.Context(), personID, capability, args.SecretaryTurnID, args.InputID); err != nil {
				return nil, err
			}
		}
		spawnRequest := args.SpawnWorkerRequest
		if addressedReplyEnabled {
			spawnRequest.SecretaryTurnID, spawnRequest.InputID = args.SecretaryTurnID, args.InputID
		}
		details, err := s.secretaryTools.SpawnWorker(r.Context(), spawnRequest)
		if err != nil {
			return nil, err
		}
		if addressedReplyEnabled {
			if _, err := s.store.LinkSecretaryWorkerTurn(r.Context(), personID, capability, args.SecretaryTurnID, args.InputID, details.ActionTurnID); err != nil {
				return nil, err
			}
		}
		return details, nil
	case "message_worker":
		if addressedReplyEnabled {
			if err := s.store.ValidateSecretaryOrigin(r.Context(), personID, capability, args.SecretaryTurnID, args.InputID); err != nil {
				return nil, err
			}
		}
		messageRequest := ctl.MessageWorkerRequest{WorkerRef: args.WorkerRef, Text: args.Text, RequestID: args.RequestID, IdempotencyKey: args.IdempotencyKey}
		if addressedReplyEnabled {
			messageRequest.SecretaryTurnID, messageRequest.InputID = args.SecretaryTurnID, args.InputID
		}
		details, err := s.secretaryTools.MessageWorker(r.Context(), messageRequest)
		if err != nil {
			return nil, err
		}
		if addressedReplyEnabled && details.ActionTurnID != "" {
			if _, err := s.store.LinkSecretaryWorkerTurn(r.Context(), personID, capability, args.SecretaryTurnID, args.InputID, details.ActionTurnID); err != nil {
				return nil, err
			}
		}
		return details, nil
	case "reply_to_user":
		if !addressedReplyEnabled {
			return nil, &unknownSecretaryToolError{name: name}
		}
		entry, duplicate, err := s.store.RecordSecretaryReply(r.Context(), personID, capability, args.SecretaryTurnID, args.InputID, args.Text)
		if err != nil {
			return nil, err
		}
		return map[string]any{"entry_id": entry.ID, "duplicate": duplicate}, nil
	case "cancel_worker":
		return s.secretaryTools.CancelWorker(r.Context(), args.WorkerRef)
	case "close_worker":
		return s.secretaryTools.CloseWorker(r.Context(), args.WorkerRef)
	default:
		return nil, &unknownSecretaryToolError{name: name}
	}
}

func (s *Server) addressedReplyEnabled() bool {
	return s.secretaryReplyContract != nil && s.secretaryReplyContract() == core.SecretaryReplyContractAddressedV1
}

type unknownSecretaryToolError struct{ name string }

func (e *unknownSecretaryToolError) Error() string { return "unknown Secretary tool " + e.name }
