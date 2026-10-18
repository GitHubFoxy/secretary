package webapi

import (
	"encoding/json"
	"fmt"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
	"github.com/beruseruko/secretary/internal/node"
)

func (s *Server) secretaryMCPObserve(w http.ResponseWriter, r *http.Request) {
	var observation core.SecretaryMCPObservation
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&observation); err != nil {
		http.Error(w, "invalid MCP observation", http.StatusBadRequest)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		http.Error(w, "invalid MCP observation", http.StatusBadRequest)
		return
	}
	err := s.store.RecordSecretaryMCPObservation(r.Context(), s.owner.ID, strings.TrimSpace(bearerToken(r)), observation)
	switch {
	case errors.Is(err, core.ErrMCPObservationUnauthorized):
		http.Error(w, "MCP observation unauthorized", http.StatusUnauthorized)
	case errors.Is(err, core.ErrMCPObservationInvalid):
		http.Error(w, "invalid MCP observation", http.StatusBadRequest)
	case err != nil:
		http.Error(w, "MCP observation unavailable", http.StatusInternalServerError)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

type secretaryToolCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type secretaryToolCallResponse struct {
	Value any    `json:"value,omitempty"`
	Error string `json:"error,omitempty"`
}

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

func (s *Server) secretaryWebImageIDs(r *http.Request, turnID, inputID string) ([]string, error) {
	canonical, err := s.store.SecretaryWebInput(r.Context(), s.owner.ID, turnID, inputID)
	if err != nil { return nil, err }
	refs := node.ArtifactIDs(canonical)
	if len(refs) > 16 { return nil, errors.New("web media: too many Artifact references (max 16)") }
	mediaIDs := make([]string, 0, len(refs))
	imageCount := 0
	maxNativeFrames := 0
	for _, id := range refs {
		artifact, body, err := s.store.ArtifactForPerson(r.Context(), s.owner.ID, id)
		if err != nil { return nil, fmt.Errorf("web media: bound Artifact %s unavailable: %w", id, err) }
		switch artifact.MediaType {
		case "image/png", "image/jpeg", "image/webp":
			if _, err := node.ValidateStillImage(artifact.Name, body); err != nil {
				return nil, fmt.Errorf("web media: bound Artifact %s invalid: %w", id, err)
			}
			imageCount++
			maxNativeFrames++
			mediaIDs = append(mediaIDs, id)
		case "video/mp4", "video/webm", "video/quicktime":
			if artifact.ProcessingStatus != "queued" && artifact.ProcessingStatus != "processing" && artifact.ProcessingStatus != "ready" && artifact.ProcessingStatus != "failed" {
				return nil, fmt.Errorf("web video: Artifact %s has no valid processing record", id)
			}
			if artifact.ProcessingStatus == "ready" {
				maxNativeFrames += len(artifact.VideoFrames)
			}
			mediaIDs = append(mediaIDs, id)
		}
	}
	if imageCount > node.MaxPromptImages { return nil, errors.New("web media: too many attached still images") }
	if maxNativeFrames > 16 { return nil, errors.New("web media: too many video frames in one Worker turn") }
	return mediaIDs, nil
}

func (s *Server) callSecretaryTool(r *http.Request, name string, raw json.RawMessage, personID, capability string) (any, error) {
	var args struct {
		Kind            string `json:"kind"`
		Entry           string `json:"entry"`
		ExpectedRevision int64 `json:"expected_revision"`
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
	case "read_memory":
		return s.store.ReadMemoryDocument(r.Context(), s.userPathname(), args.Kind)
	case "remember":
		return s.store.RememberOwnerMemory(r.Context(), s.userPathname(), args.Kind, args.Entry, args.ExpectedRevision, personID, capability, args.SecretaryTurnID, args.InputID)
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
		imageIDs, err := s.secretaryWebImageIDs(r, args.SecretaryTurnID, args.InputID)
		if err != nil { return nil, err }
		spawnRequest := args.SpawnWorkerRequest
		spawnRequest.WebImageArtifactIDs = imageIDs
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
		imageIDs, err := s.secretaryWebImageIDs(r, args.SecretaryTurnID, args.InputID)
		if err != nil { return nil, err }
		messageRequest := ctl.MessageWorkerRequest{WorkerRef: args.WorkerRef, Text: args.Text, RequestID: args.RequestID, IdempotencyKey: args.IdempotencyKey, WebImageArtifactIDs: imageIDs}
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
	case "acknowledge_user":
		if addressedReplyEnabled {
			return nil, &unknownSecretaryToolError{name: name}
		}
		entry, duplicate, err := s.store.RecordSecretaryAcknowledgement(r.Context(), personID, capability, args.SecretaryTurnID, args.InputID, args.Text)
		if err != nil {
			return nil, err
		}
		return map[string]any{"entry_id": entry.ID, "duplicate": duplicate}, nil
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
