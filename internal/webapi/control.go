package webapi

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

// ControlOptions contains server-owned operator actions. The normal User UI
// never receives these routes. The main process supplies callbacks so this
// package does not need to own config or runtime lifecycle state.
type ControlOptions struct {
	ConfigPath     string
	ConfigContent  func() (string, error)
	ConfigSnapshot func() any
	WriteConfig    func([]byte) error
	ApplyConfig    func([]byte) (any, error)
	ReloadConfig   func() (any, error)
	ProfileFiles   func() ([]ProfileFile, error)
	ApplyProfile   func(string, []byte) error
	RuntimeRestart func(context.Context) error
	RetryTask      func(context.Context, string) (core.Task, error)
	CloseTask      func(context.Context, string) (core.CloseOutcome, error)
	RawLogDir      string
}

type ProfileFile struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	Content   string `json:"content"`
	Hash      string `json:"hash"`
	Runtime   string `json:"runtime"`
	Model     string `json:"model"`
	Reasoning string `json:"reasoning"`
}

type controlOverview struct {
	GeneratedAt time.Time           `json:"generated_at"`
	Workers     []core.TaskDetails  `json:"workers"`
	Events      []core.Event        `json:"events"`
	Secretary   secretaryModelState `json:"secretary"`
	Config      any                 `json:"config,omitempty"`
}

func (s *Server) ControlHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/control/overview", s.controlOverview)
	mux.HandleFunc("GET /v1/control/events", s.controlEvents)
	mux.HandleFunc("GET /v1/control/config", s.controlConfig)
	mux.HandleFunc("PUT /v1/control/config", s.controlConfigWrite)
	mux.HandleFunc("POST /v1/control/config/reload", s.controlConfigReload)
	mux.HandleFunc("GET /v1/control/profiles", s.controlProfiles)
	mux.HandleFunc("POST /v1/control/profiles/reload", s.controlProfilesReload)
	mux.HandleFunc("/v1/control/profiles/", s.controlProfileWrite)
	mux.HandleFunc("POST /v1/control/runtime/restart", s.controlRuntimeRestart)
	mux.HandleFunc("GET /v1/control/export", s.controlExport)
	mux.HandleFunc("GET /v1/control/raw-log/", s.controlRawLog)
	mux.HandleFunc("/v1/control/tasks/", s.controlTaskAction)
	return mux
}

func (s *Server) controlAllowed(w http.ResponseWriter, r *http.Request) bool {
	if !s.debug {
		http.NotFound(w, r)
		return false
	}
	_, ok := s.authorizedConversation(w, r)
	return ok
}

func (s *Server) controlOverview(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	conversation, err := s.store.ConversationForPerson(r.Context(), s.owner.ID)
	if err != nil {
		http.Error(w, "read conversation", http.StatusInternalServerError)
		return
	}
	workers, err := s.parentWorkerDetails(r, conversation.ID)
	if err != nil {
		http.Error(w, "read workers", http.StatusInternalServerError)
		return
	}
	events, err := s.store.EventsRecent(r.Context(), 100)
	if err != nil {
		http.Error(w, "read events", http.StatusInternalServerError)
		return
	}
	var snapshot any
	if s.control.ConfigSnapshot != nil {
		snapshot = s.control.ConfigSnapshot()
	}
	writeJSON(w, http.StatusOK, controlOverview{GeneratedAt: time.Now().UTC(), Workers: workers, Events: events, Secretary: s.secretaryModelState(r), Config: snapshot})
}

func (s *Server) controlEvents(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	after, err := parseControlTime(r.URL.Query().Get("after"))
	if err != nil {
		http.Error(w, "invalid after timestamp", http.StatusBadRequest)
		return
	}
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			http.Error(w, "invalid event limit", http.StatusBadRequest)
			return
		}
	}
	events, err := s.store.EventsAfter(r.Context(), after, limit)
	if err != nil {
		http.Error(w, "read events", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) controlConfig(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	result := map[string]any{"path": s.control.ConfigPath}
	if s.control.ConfigContent != nil {
		content, err := s.control.ConfigContent()
		if err != nil {
			http.Error(w, "read config: "+err.Error(), http.StatusInternalServerError)
			return
		}
		result["content"] = content
	}
	if s.control.ConfigSnapshot != nil {
		result["snapshot"] = s.control.ConfigSnapshot()
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) controlConfigWrite(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	var request struct {
		Content string `json:"content"`
		TOML    string `json:"toml"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	content := request.Content
	if content == "" {
		content = request.TOML
	}
	if content == "" {
		http.Error(w, "config content is required", http.StatusBadRequest)
		return
	}
	var result any
	var err error
	if s.control.ApplyConfig != nil {
		result, err = s.control.ApplyConfig([]byte(content))
	} else if s.control.WriteConfig != nil {
		err = s.control.WriteConfig([]byte(content))
		if err == nil && s.control.ReloadConfig != nil {
			result, err = s.control.ReloadConfig()
		}
	} else {
		err = errors.New("config editing is not configured")
	}
	if err != nil {
		http.Error(w, "apply config: "+err.Error(), http.StatusBadRequest)
		return
	}
	_, _ = s.store.RecordEvent(r.Context(), "control.config_changed", "", "", "", map[string]string{"path": s.control.ConfigPath})
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) controlConfigReload(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	if s.control.ReloadConfig == nil {
		http.Error(w, "config reload is not configured", http.StatusNotImplemented)
		return
	}
	result, err := s.control.ReloadConfig()
	if err != nil {
		http.Error(w, "reload config: "+err.Error(), http.StatusBadRequest)
		return
	}
	_, _ = s.store.RecordEvent(r.Context(), "control.config_reloaded", "", "", "", map[string]string{"path": s.control.ConfigPath})
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) controlProfiles(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.control.ProfileFiles == nil {
		http.Error(w, "profile editing is not configured", http.StatusNotImplemented)
		return
	}
	profiles, err := s.control.ProfileFiles()
	if err != nil {
		http.Error(w, "read profiles: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if profiles == nil {
		profiles = []ProfileFile{}
	}
	writeJSON(w, http.StatusOK, profiles)
}

func (s *Server) controlProfilesReload(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	if s.control.ReloadConfig == nil {
		http.Error(w, "profile reload is not configured", http.StatusNotImplemented)
		return
	}
	result, err := s.control.ReloadConfig()
	if err != nil {
		http.Error(w, "reload profiles: "+err.Error(), http.StatusBadRequest)
		return
	}
	_, _ = s.store.RecordEvent(r.Context(), "control.profiles_reloaded", "", "", "", map[string]string{})
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) controlProfileWrite(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", http.MethodPut)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/control/profiles/"), "/")
	if !validProfileName(name) {
		http.Error(w, "invalid profile name", http.StatusBadRequest)
		return
	}
	if s.control.ApplyProfile == nil {
		http.Error(w, "profile editing is not configured", http.StatusNotImplemented)
		return
	}
	var request struct {
		Content  string `json:"content"`
		Markdown string `json:"markdown"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	content := request.Content
	if content == "" {
		content = request.Markdown
	}
	if strings.TrimSpace(content) == "" {
		http.Error(w, "profile content is required", http.StatusBadRequest)
		return
	}
	if err := s.control.ApplyProfile(name, []byte(content)); err != nil {
		http.Error(w, "apply profile: "+err.Error(), http.StatusBadRequest)
		return
	}
	_, _ = s.store.RecordEvent(r.Context(), "control.profile_changed", "", "", "", map[string]string{"profile": name})
	if s.control.ProfileFiles != nil {
		profiles, err := s.control.ProfileFiles()
		if err != nil {
			http.Error(w, "read profiles: "+err.Error(), http.StatusInternalServerError)
			return
		}
		for _, profile := range profiles {
			if profile.Name == name {
				writeJSON(w, http.StatusOK, profile)
				return
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": name, "status": "updated"})
}

func validProfileName(name string) bool {
	switch name {
	case "secretary", "worker", "child_worker":
		return true
	default:
		return false
	}
}

func (s *Server) controlRuntimeRestart(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	if s.control.RuntimeRestart == nil {
		http.Error(w, "runtime restart is not configured", http.StatusNotImplemented)
		return
	}
	if err := s.control.RuntimeRestart(r.Context()); err != nil {
		_, _ = s.store.RecordEvent(r.Context(), "control.runtime_restart_failed", "", "", "", map[string]string{"error": err.Error()})
		http.Error(w, "restart runtime: "+err.Error(), http.StatusBadGateway)
		return
	}
	_, _ = s.store.RecordEvent(r.Context(), "control.runtime_restarted", "", "", "", map[string]string{})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "restarted"})
}

func (s *Server) controlTaskAction(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/v1/control/tasks/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	taskID, action := parts[0], parts[1]
	switch action {
	case "retry":
		if s.control.RetryTask == nil {
			http.Error(w, "task retry is not configured", http.StatusNotImplemented)
			return
		}
		task, err := s.control.RetryTask(r.Context(), taskID)
		if err != nil {
			_, _ = s.store.RecordEvent(r.Context(), "control.task_retry_failed", "", "", "", map[string]string{"task_id": taskID, "error": err.Error()})
			http.Error(w, "retry task: "+err.Error(), http.StatusBadRequest)
			return
		}
		_, _ = s.store.RecordEvent(r.Context(), "control.task_retried", "", "", "", map[string]string{"task_id": taskID})
		writeJSON(w, http.StatusAccepted, task)
	case "close":
		if s.control.CloseTask == nil {
			http.Error(w, "task close is not configured", http.StatusNotImplemented)
			return
		}
		outcome, err := s.control.CloseTask(r.Context(), taskID)
		if err != nil {
			http.Error(w, "close task: "+err.Error(), http.StatusBadRequest)
			return
		}
		_, _ = s.store.RecordEvent(r.Context(), "control.task_closed", "", "", "", map[string]string{"task_id": taskID})
		writeJSON(w, http.StatusAccepted, outcome)
	case "cancel":
		s.controlCancelTask(w, r, taskID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) controlCancelTask(w http.ResponseWriter, r *http.Request, taskID string) {
	details, err := s.store.TaskDetails(r.Context(), taskID)
	conversation, conversationErr := s.store.ConversationForPerson(r.Context(), s.owner.ID)
	if err != nil || conversationErr != nil || details.Task.ConversationID != conversation.ID || details.Binding == nil {
		http.Error(w, "task worker not found", http.StatusNotFound)
		return
	}
	session, found := s.workerSession(details.Binding.WorkerRef)
	if !found {
		http.Error(w, "task worker session unavailable", http.StatusNotFound)
		return
	}
	if err := session.Cancel(r.Context()); err != nil {
		http.Error(w, "cancel task: "+err.Error(), http.StatusBadGateway)
		return
	}
	_, _ = s.store.RecordEvent(r.Context(), "control.task_cancel_requested", details.Binding.WorkerRef, "", details.Binding.RuntimeSessionID, map[string]string{"task_id": taskID})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "cancel_requested"})
}

func (s *Server) controlRawLog(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	workerRef := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/control/raw-log/"), "/")
	if workerRef == "" || workerRef != filepath.Base(workerRef) || strings.Contains(workerRef, "..") {
		http.Error(w, "invalid worker reference", http.StatusBadRequest)
		return
	}
	if s.control.RawLogDir == "" {
		http.Error(w, "raw logs are not configured", http.StatusNotImplemented)
		return
	}
	path := node.RawACPLogPath(s.control.RawLogDir, workerRef)
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		http.Error(w, "raw log not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "read raw log", http.StatusInternalServerError)
		return
	}
	if len(content) > 4<<20 {
		content = content[len(content)-(4<<20):]
	}
	_, _ = s.store.RecordEvent(r.Context(), "control.raw_log_read", workerRef, "", "", map[string]string{"worker_ref": workerRef})
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	_, _ = w.Write(content)
}

func (s *Server) controlExport(w http.ResponseWriter, r *http.Request) {
	if !s.controlAllowed(w, r) {
		return
	}
	conversation, err := s.store.ConversationForPerson(r.Context(), s.owner.ID)
	if err != nil {
		http.Error(w, "read conversation", http.StatusInternalServerError)
		return
	}
	workers, err := s.parentWorkerDetails(r, conversation.ID)
	if err != nil {
		http.Error(w, "read workers", http.StatusInternalServerError)
		return
	}
	events, err := s.store.EventsRecent(r.Context(), 500)
	if err != nil {
		http.Error(w, "read events", http.StatusInternalServerError)
		return
	}
	var snapshot any
	if s.control.ConfigSnapshot != nil {
		snapshot = s.control.ConfigSnapshot()
	}
	_, _ = s.store.RecordEvent(r.Context(), "control.diagnostic_exported", "", "", "", map[string]string{"workers": strconv.Itoa(len(workers)), "events": strconv.Itoa(len(events))})
	writeJSON(w, http.StatusOK, map[string]any{"generated_at": time.Now().UTC(), "config": snapshot, "workers": workers, "events": events})
}

func parseControlTime(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed, nil
}
