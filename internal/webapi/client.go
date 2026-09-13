package webapi

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
)

type clientPairRequest struct {
	DeviceID       string             `json:"device_id"`
	DisplayName    string             `json:"display_name"`
	Platform       string             `json:"platform"`
	Scopes         []core.ClientScope `json:"scopes,omitempty"`
	BootstrapToken string             `json:"bootstrap_token,omitempty"`
	IdempotencyKey string             `json:"idempotency_key,omitempty"`
}

type clientCredentialResponse struct {
	core.Client
	ClientID      string `json:"client_id"`
	Credential    string `json:"credential,omitempty"`
	PendingToken  string `json:"pending_token,omitempty"`
	CredentialRaw bool   `json:"credential_raw,omitempty"`
}

func (s *Server) pairClient(w http.ResponseWriter, r *http.Request) {
	var request clientPairRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	// Bootstrap is accepted only for the initial pairing request. It never
	// becomes a Client credential and is never accepted on normal API routes.
	if subtle.ConstantTimeCompare([]byte(request.BootstrapToken), []byte(s.bootstrapToken)) != 1 {
		http.Error(w, "invalid bootstrap token", http.StatusUnauthorized)
		return
	}
	key, ok := requireIdempotencyKey(w, r, request.IdempotencyKey)
	if !ok {
		return
	}
	pairing, err := s.store.PairClientWithToken(r.Context(), s.owner.ID, request.DeviceID, request.DisplayName, request.Platform, request.Scopes, key)
	if err != nil {
		writeClientMutationError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, clientCredentialResponse{Client: pairing.Client, ClientID: pairing.ID, PendingToken: pairing.PendingToken})
}

func (s *Server) listClients(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizedConversationScope(w, r, core.ScopeClientManage); !ok {
		return
	}
	clients, err := s.store.Clients(r.Context(), s.owner.ID)
	if err != nil {
		http.Error(w, "list Clients", http.StatusInternalServerError)
		return
	}
	if clients == nil {
		clients = []core.Client{}
	}
	writeJSON(w, http.StatusOK, clients)
}

func (s *Server) clientRoute(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/clients/"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id, action := parts[0], parts[1]
	if action == "poll" || action == "redeem" {
		if (action == "poll" && r.Method != http.MethodGet) || (action == "redeem" && r.Method != http.MethodPost) {
			http.NotFound(w, r)
			return
		}
		token := bearerToken(r)
		if token == "" {
			http.Error(w, "pending pairing token required", http.StatusUnauthorized)
			return
		}
		if action == "poll" {
			status, err := s.store.ClientPairingStatus(r.Context(), id, token)
			if err != nil {
				http.Error(w, "poll pairing: "+err.Error(), http.StatusUnauthorized)
				return
			}
			writeJSON(w, http.StatusOK, status)
			return
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
		client, credential, err := s.store.RedeemClient(r.Context(), id, token, key)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				http.Error(w, "redeem Client: "+err.Error(), http.StatusUnauthorized)
				return
			}
			writeClientMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, clientCredentialResponse{Client: client, ClientID: client.ID, Credential: credential, CredentialRaw: true})
		return
	}
	if (action != "approve" && action != "revoke") || r.Method != http.MethodPost {
		if _, ok := s.authorizedConversationScope(w, r, core.ScopeClientManage); !ok {
			return
		}
		http.NotFound(w, r)
		return
	}
	if _, ok := s.authorizedConversationScope(w, r, core.ScopeClientManage); !ok {
		return
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
	switch action {
	case "approve":
		client, credential, err := s.store.ApproveClient(r.Context(), id, key)
		if err != nil {
			writeClientMutationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, clientCredentialResponse{Client: client, ClientID: client.ID, Credential: credential})
	case "revoke":
		s.mu.Lock()
		client, err := s.store.RevokeClient(r.Context(), id, key)
		if err != nil {
			s.mu.Unlock()
			writeClientMutationError(w, err)
			return
		}
		s.cancelClientStreamsLocked(client.ID)
		s.mu.Unlock()
		writeJSON(w, http.StatusOK, client)
	}
}
