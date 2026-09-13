package webapi

import (
	"encoding/json"
	"net/http"
	"strings"

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
	value, callErr := s.callSecretaryTool(r, request.Name, request.Arguments)
	if callErr != nil {
		writeJSON(w, http.StatusOK, secretaryToolCallResponse{Error: callErr.Error()})
		return
	}
	writeJSON(w, http.StatusOK, secretaryToolCallResponse{Value: value})
}

func (s *Server) callSecretaryTool(r *http.Request, name string, raw json.RawMessage) (any, error) {
	var args struct {
		WorkerRef string `json:"worker_ref"`
		Text      string `json:"text"`
		RequestID string `json:"request_id"`
		ctl.SpawnWorkerRequest
	}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
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
		return s.secretaryTools.SpawnWorker(r.Context(), args.SpawnWorkerRequest)
	case "message_worker":
		return s.secretaryTools.MessageWorker(r.Context(), ctl.MessageWorkerRequest{WorkerRef: args.WorkerRef, Text: args.Text, RequestID: args.RequestID, IdempotencyKey: args.IdempotencyKey})
	case "cancel_worker":
		return s.secretaryTools.CancelWorker(r.Context(), args.WorkerRef)
	case "close_worker":
		return s.secretaryTools.CloseWorker(r.Context(), args.WorkerRef)
	default:
		return nil, &unknownSecretaryToolError{name: name}
	}
}

type unknownSecretaryToolError struct{ name string }

func (e *unknownSecretaryToolError) Error() string { return "unknown Secretary tool " + e.name }
