package webapi

import (
	"net/http"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
)

func (s *Server) telegramPairing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	_, client, ok := s.authorizedPerson(w, r)
	if !ok {
		return
	}
	if client != nil && !client.HasScope(core.ScopeClientManage) {
		http.Error(w, "Client scope required", http.StatusForbidden)
		return
	}
	if s.telegramPairer == nil {
		http.Error(w, "Telegram adapter is unavailable", http.StatusServiceUnavailable)
		return
	}
	var request struct {
		BotUsername string `json:"bot_username,omitempty"`
	}
	if r.Body != nil && r.ContentLength != 0 && !decodeJSON(w, r, &request) {
		return
	}
	pairing, err := s.telegramPairer.CreatePairing(strings.TrimSpace(request.BotUsername))
	if err != nil {
		http.Error(w, "create Telegram pairing", http.StatusInternalServerError)
		return
	}
	// Pairing response contains only a short-lived code and deep link. The
	// server-issued Client credential stays in the adapter process.
	writeJSON(w, http.StatusCreated, pairing)
}
