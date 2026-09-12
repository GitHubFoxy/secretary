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
	ClientID   string `json:"client_id"`
	Credential string `json:"credential,omitempty"`
}

func (s *Server) pairClient(w http.ResponseWriter, r *http.Request) {
	var request clientPairRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	// Bootstrap is accepted only for the initial pairing request. It never
	// becomes a Client credential and is never accepted on normal API routes.
	if request.BootstrapToken != "" && subtle.ConstantTimeCompare([]byte(request.BootstrapToken), []byte(s.bootstrapToken)) != 1 {
		http.Error(w, "invalid bootstrap token", http.StatusUnauthorized)
		return
	}
	key := strings.TrimSpace(request.IdempotencyKey)
	if key == "" {
		key = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	}
	client, err := s.store.PairClient(r.Context(), s.owner.ID, request.DeviceID, request.DisplayName, request.Platform, request.Scopes, key)
	if err != nil {
		http.Error(w, "pair Client: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, clientCredentialResponse{Client: client, ClientID: client.ID})
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
	if !strings.HasSuffix(strings.Trim(r.URL.Path, "/"), "/approve") && !strings.HasSuffix(strings.Trim(r.URL.Path, "/"), "/revoke") {
		// A client credential cannot manage itself without the management scope.
		if _, ok := s.authorizedConversationScope(w, r, core.ScopeClientManage); !ok {
			return
		}
		http.NotFound(w, r)
		return
	}
	if _, ok := s.authorizedConversationScope(w, r, core.ScopeClientManage); !ok {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/clients/"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	id, action := parts[0], parts[1]
	switch action {
	case "approve":
		client, credential, err := s.store.ApproveClient(r.Context(), id)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				http.NotFound(w, r)
			} else {
				http.Error(w, "approve Client: "+err.Error(), http.StatusBadRequest)
			}
			return
		}
		writeJSON(w, http.StatusOK, clientCredentialResponse{Client: client, ClientID: client.ID, Credential: credential})
	case "revoke":
		client, err := s.store.RevokeClient(r.Context(), id)
		if err != nil {
			if errors.Is(err, core.ErrNotFound) {
				http.NotFound(w, r)
			} else {
				http.Error(w, "revoke Client: "+err.Error(), http.StatusBadRequest)
			}
			return
		}
		writeJSON(w, http.StatusOK, client)
	}
}
