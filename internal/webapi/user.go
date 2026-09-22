package webapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/beruseruko/secretary/internal/core"
)

const secretaryModelSetting = "secretary.model"

type bootstrapResponse struct {
	OwnerID        string              `json:"owner_id"`
	ConversationID string              `json:"conversation_id"`
	Secretary      secretaryModelState `json:"secretary"`
	Workers        []publicWorkerDTO   `json:"workers"`
}

type secretaryModelState struct {
	Selected string            `json:"selected"`
	Models   map[string]string `json:"models"`
}

func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	conversation, ok := s.authorizedConversationScope(w, r, core.ScopeConversationRead)
	if !ok {
		return
	}
	workers, err := s.publicWorkersForConversation(r.Context(), conversation.ID)
	if err != nil {
		http.Error(w, "read workers", http.StatusInternalServerError)
		return
	}
	if workers == nil {
		workers = []publicWorkerDTO{}
	}
	writeJSON(w, http.StatusOK, sanitizePublicJSON(bootstrapResponse{
		OwnerID:        s.owner.ID,
		ConversationID: conversation.ID,
		Secretary:      s.secretaryModelState(r),
		Workers:        workers,
	}))
}

func (s *Server) secretaryModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizedConversationScope(w, r, core.ScopeConversationRead); !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.secretaryModelState(r))
}

func (s *Server) secretaryModelState(r *http.Request) secretaryModelState {
	models := map[string]string{}
	if s.modelCatalog != nil {
		for name, value := range s.modelCatalog() {
			models[name] = value
		}
	}
	selected, found, err := s.store.GetSetting(r.Context(), secretaryModelSetting)
	if err != nil || !found {
		selected = ""
		if s.modelDefault != nil {
			selected = s.modelDefault()
		}
	}
	if selected != "" && len(models) > 0 {
		if _, ok := models[selected]; !ok {
			selected = ""
			if s.modelDefault != nil {
				selected = s.modelDefault()
			}
		}
	}
	if selected == "" && len(models) > 0 {
		keys := make([]string, 0, len(models))
		for key := range models {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		selected = keys[0]
	}
	return secretaryModelState{Selected: selected, Models: models}
}

func (s *Server) setSecretaryModel(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizedConversationScope(w, r, core.ScopeUserWrite); !ok {
		return
	}
	var request struct {
		Model          string `json:"model"`
		IdempotencyKey string `json:"idempotency_key,omitempty"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	canonical, ok := s.canonicalModel(request.Model)
	if !ok {
		http.Error(w, "unknown Secretary model", http.StatusBadRequest)
		return
	}
	key, ok := requireIdempotencyKey(w, r, request.IdempotencyKey)
	if !ok {
		return
	}
	payload := struct{ Model string }{canonical}
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	encoded, found, err := s.store.IdempotencyOutcomeForPayload(r.Context(), "secretary.model", key, payload)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, core.ErrIdempotencyConflict) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	if found {
		var state secretaryModelState
		if err := json.Unmarshal(encoded, &state); err != nil {
			http.Error(w, "decode idempotency record", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, state)
		return
	}
	if err := s.store.SetSetting(r.Context(), secretaryModelSetting, canonical); err != nil {
		http.Error(w, "save Secretary model", http.StatusInternalServerError)
		return
	}
	if s.modelChanged != nil {
		if err := s.modelChanged(canonical); err != nil {
			_, _ = s.store.RecordEvent(r.Context(), "control.secretary_model_failed", "", "", "", map[string]string{"model": canonical, "error": err.Error()})
			http.Error(w, "apply Secretary model", http.StatusBadGateway)
			return
		}
	}
	_, _ = s.store.RecordEvent(r.Context(), "secretary.model_changed", "", "", "", map[string]string{"model": canonical})
	state := s.secretaryModelState(r)
	if err := s.store.RecordIdempotencyOutcomeWithPayload(r.Context(), "secretary.model", key, payload, state); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, core.ErrIdempotencyConflict) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) canonicalModel(requested string) (string, bool) {
	if requested == "" {
		return "", false
	}
	models := map[string]string{}
	if s.modelCatalog != nil {
		models = s.modelCatalog()
	}
	if len(models) == 0 {
		return requested, true
	}
	if _, ok := models[requested]; ok {
		return requested, true
	}
	for key, value := range models {
		if value == requested {
			return key, true
		}
	}
	return "", false
}

func (s *Server) workerList(w http.ResponseWriter, r *http.Request) {
	person, client, ok := s.authorizedPerson(w, r)
	if !ok {
		return
	}
	if client != nil && !client.HasScope(core.ScopeWorkerRead) {
		http.Error(w, "Client scope required", http.StatusForbidden)
		return
	}
	conversation, err := s.store.ConversationForPerson(r.Context(), person.ID)
	if err != nil {
		http.Error(w, "read workers", http.StatusInternalServerError)
		return
	}
	limit, err := parseSnapshotLimit(r)
	if err != nil {
		http.Error(w, "invalid limit", http.StatusBadRequest)
		return
	}
	if client != nil {
		workers, err := s.publicWorkersStrictForConversationLimit(r.Context(), conversation.ID, limit)
		if err != nil {
			http.Error(w, "read workers", http.StatusInternalServerError)
			return
		}
		if workers == nil {
			workers = []publicWorkerStrictDTO{}
		}
		writeJSON(w, http.StatusOK, sanitizePublicJSON(workers))
		return
	}
	workers, err := s.publicWorkersForConversationLimit(r.Context(), conversation.ID, limit)
	if err != nil {
		http.Error(w, "read workers", http.StatusInternalServerError)
		return
	}
	if workers == nil {
		workers = []publicWorkerDTO{}
	}
	writeJSON(w, http.StatusOK, sanitizePublicJSON(workers))
}

func (s *Server) parentWorkerDetails(r *http.Request, conversationID string) ([]core.TaskDetails, error) {
	tasks, err := s.store.TasksForConversation(r.Context(), conversationID)
	if err != nil {
		return nil, err
	}
	workers := make([]core.TaskDetails, 0, len(tasks))
	for _, task := range tasks {
		if task.ParentTaskID != "" {
			continue
		}
		details, err := s.store.TaskDetails(r.Context(), task.ID)
		if err != nil {
			return nil, err
		}
		workers = append(workers, details)
	}
	return workers, nil
}

type userDocumentRequest struct {
	Content          string `json:"content"`
	Markdown         string `json:"markdown,omitempty"`
	ExpectedRevision int64  `json:"expected_revision,omitempty"`
	IdempotencyKey   string `json:"idempotency_key,omitempty"`
}

func (s *Server) userPathname() string { return s.userPath }

func (s *Server) user(w http.ResponseWriter, r *http.Request) {
	scope := core.ScopeUserRead
	if r.Method == http.MethodPut {
		scope = core.ScopeUserWrite
	}
	if _, ok := s.authorizedConversationScope(w, r, scope); !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		document, err := s.store.LoadUserDocument(r.Context(), s.userPathname())
		if err != nil {
			http.Error(w, "read user.md", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, document)
	case http.MethodPut:
		var request userDocumentRequest
		if !decodeJSON(w, r, &request) {
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
			Content          string
			ExpectedRevision int64
		}{request.Content, request.ExpectedRevision}
		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
		if encoded, found, lookupErr := s.store.IdempotencyOutcomeForPayload(r.Context(), "user.update", key, payload); lookupErr != nil {
			status := http.StatusInternalServerError
			if errors.Is(lookupErr, core.ErrIdempotencyConflict) {
				status = http.StatusConflict
			}
			http.Error(w, lookupErr.Error(), status)
			return
		} else if found {
			var document core.UserDocument
			if json.Unmarshal(encoded, &document) != nil {
				http.Error(w, "decode idempotency record", http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, document)
			return
		}
		document, err := s.store.SaveUserDocumentIfRevision(r.Context(), s.userPathname(), request.Content, request.ExpectedRevision)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, core.ErrUserDocumentRevisionConflict) {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
		if err := s.store.RecordIdempotencyOutcomeWithPayload(r.Context(), "user.update", key, payload, document); err != nil {
			http.Error(w, "save idempotency record", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, document)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
