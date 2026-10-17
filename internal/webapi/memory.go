package webapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
)

// /v1/memory/memory and /v1/memory/preferences are owner-scoped operations.
// A PUT is an explicit owner edit; normal conversation never calls this route.
func (s *Server) memoryDocument(w http.ResponseWriter, r *http.Request) {
	kind := strings.TrimPrefix(r.URL.Path, "/v1/memory/")
	if kind != core.MemoryFacts && kind != core.MemoryPreferences {
		http.NotFound(w, r)
		return
	}
	if s.userPathname() == "" {
		http.Error(w, "memory path is not configured", http.StatusServiceUnavailable)
		return
	}
	scope := core.ScopeUserRead
	if r.Method == http.MethodPut {
		scope = core.ScopeUserWrite
	}
	if _, ok := s.authorizedConversationScope(w, r, scope); !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		document, err := s.store.ReadMemoryDocument(r.Context(), s.userPathname(), kind)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, document)
	case http.MethodPut:
		var request userDocumentRequest
		if !decodeJSON(w, r, &request) {
			return
		}
		if request.ExpectedRevision < 1 {
			http.Error(w, "expected_revision is required", http.StatusBadRequest)
			return
		}
		if request.Content == "" {
			request.Content = request.Markdown
		}
		key, ok := requireIdempotencyKey(w, r, request.IdempotencyKey)
		if !ok {
			return
		}
		payload := struct {
			Kind string
			Content string
			ExpectedRevision int64
		}{kind, request.Content, request.ExpectedRevision}
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
		encoded, found, err := s.store.IdempotencyOutcomeForPayload(r.Context(), "memory.update."+kind, key, payload)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, core.ErrIdempotencyConflict) {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
		if found {
			var document core.UserDocument
			if err := json.Unmarshal(encoded, &document); err != nil {
				http.Error(w, "decode idempotency record", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, document)
			return
		}
		document, err := s.store.SaveMemoryDocumentIfRevision(r.Context(), s.userPathname(), kind, request.Content, request.ExpectedRevision)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, core.ErrMemoryRevisionConflict) {
				status = http.StatusConflict
			} else if errors.Is(err, core.ErrInvalidMemory) {
				status = http.StatusBadRequest
			}
			http.Error(w, err.Error(), status)
			return
		}
		if err := s.store.RecordIdempotencyOutcomeWithPayload(r.Context(), "memory.update."+kind, key, payload, document); err != nil {
			http.Error(w, "save idempotency record", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, document)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
