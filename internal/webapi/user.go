package webapi

import (
	"net/http"
	"sort"

	"github.com/beruseruko/secretary/internal/core"
)

const secretaryModelSetting = "secretary.model"

type bootstrapResponse struct {
	OwnerID        string              `json:"owner_id"`
	ConversationID string              `json:"conversation_id"`
	Secretary      secretaryModelState `json:"secretary"`
	Workers        []core.TaskDetails  `json:"workers"`
}

type secretaryModelState struct {
	Selected string            `json:"selected"`
	Models   map[string]string `json:"models"`
}

func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	conversation, ok := s.authorizedConversation(w, r)
	if !ok {
		return
	}
	workers, err := s.parentWorkerDetails(r, conversation.ID)
	if err != nil {
		http.Error(w, "read workers", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, bootstrapResponse{
		OwnerID:        s.owner.ID,
		ConversationID: conversation.ID,
		Secretary:      s.secretaryModelState(r),
		Workers:        workers,
	})
}

func (s *Server) secretaryModels(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizedConversation(w, r); !ok {
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
	if _, ok := s.authorizedConversation(w, r); !ok {
		return
	}
	var request struct {
		Model string `json:"model"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	canonical, ok := s.canonicalModel(request.Model)
	if !ok {
		http.Error(w, "unknown Secretary model", http.StatusBadRequest)
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
	writeJSON(w, http.StatusOK, s.secretaryModelState(r))
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
	conversation, ok := s.authorizedConversation(w, r)
	if !ok {
		return
	}
	workers, err := s.parentWorkerDetails(r, conversation.ID)
	if err != nil {
		http.Error(w, "read workers", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, workers)
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
