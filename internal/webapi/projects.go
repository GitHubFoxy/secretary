package webapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
)

type projectRequest struct {
	ID               string                        `json:"id,omitempty"`
	Name             string                        `json:"name"`
	Description      string                        `json:"description,omitempty"`
	Mappings         []core.ProjectPathMapping     `json:"mappings,omitempty"`
	PathMappings     map[core.NodeReference]string `json:"path_mappings,omitempty"`
	Policy           core.ProjectPolicy            `json:"policy"`
	ExpectedRevision int64                         `json:"expected_revision,omitempty"`
	IdempotencyKey   string                        `json:"idempotency_key,omitempty"`
}

func (r projectRequest) spec(id string) core.ProjectSpec {
	return core.ProjectSpec{ID: idOr(r.ID, id), Name: r.Name, Description: r.Description, Mappings: r.Mappings, PathMappings: r.PathMappings, Policy: r.Policy}
}
func idOr(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func (s *Server) projectRoot(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.projectList(w, r)
	case http.MethodPost:
		s.projectCreate(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) projectList(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizedConversationScope(w, r, core.ScopeProjectRead); !ok {
		return
	}
	projects, err := s.store.Projects(r.Context())
	if err != nil {
		http.Error(w, "list Projects: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if projects == nil {
		projects = []core.Project{}
	}
	writeJSON(w, http.StatusOK, projects)
}
func (s *Server) projectCreate(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorizedConversationScope(w, r, core.ScopeProjectWrite); !ok {
		return
	}
	var request projectRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	key := strings.TrimSpace(request.IdempotencyKey)
	if key == "" {
		key = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	}
	project, err := s.store.CreateProject(r.Context(), request.spec(""), key)
	if err != nil {
		writeProjectError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, project)
}
func (s *Server) projectRoute(w http.ResponseWriter, r *http.Request) {
	scope := core.ScopeProjectRead
	if r.Method != http.MethodGet {
		scope = core.ScopeProjectWrite
	}
	if _, ok := s.authorizedConversationScope(w, r, scope); !ok {
		return
	}
	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/projects/"), "/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
		project, err := s.store.Project(r.Context(), id)
		if err != nil {
			writeProjectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, project)
	case http.MethodPut:
		var request projectRequest
		if !decodeJSON(w, r, &request) {
			return
		}
		key := strings.TrimSpace(request.IdempotencyKey)
		if key == "" {
			key = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		}
		project, err := s.store.UpdateProject(r.Context(), id, request.spec(id), request.ExpectedRevision, key)
		if err != nil {
			writeProjectError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, project)
	case http.MethodDelete:
		revision, err := strconv.ParseInt(r.URL.Query().Get("expected_revision"), 10, 64)
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if err != nil {
			var request projectRequest
			if r.Body == nil || !decodeJSON(w, r, &request) || request.ExpectedRevision <= 0 {
				if r.URL.Query().Get("expected_revision") == "" {
					http.Error(w, "expected_revision is required", http.StatusBadRequest)
				} else {
					http.Error(w, "invalid expected_revision", http.StatusBadRequest)
				}
				return
			}
			revision = request.ExpectedRevision
			if key == "" {
				key = strings.TrimSpace(request.IdempotencyKey)
			}
		}
		if err := s.store.DeleteProject(r.Context(), id, revision, key); err != nil {
			writeProjectError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
func writeProjectError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, core.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, core.ErrProjectRevisionConflict):
		status = http.StatusConflict
	case strings.Contains(strings.ToLower(err.Error()), "unique constraint") || strings.Contains(strings.ToLower(err.Error()), "already exists"):
		status = http.StatusConflict
	}
	http.Error(w, err.Error(), status)
}
