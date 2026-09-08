package webapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
	secretaryruntime "github.com/beruseruko/secretary/internal/secretary"
	"github.com/coder/websocket"
)

const sessionCookie = "secretary_session"

type workerController interface {
	Session(string) (node.Session, bool)
	Steer(context.Context, string, string) (bool, error)
	Queue(context.Context, string, string) (core.Attempt, error)
	Stop(context.Context, string) error
}

type Server struct {
	store          *core.Store
	bootstrapToken string
	owner          core.Person
	node           *node.LocalNode
	workers        workerController
	secretary      interface {
		HandleMessage(context.Context, string) error
	}
	workerStates map[string]string

	mu          sync.Mutex
	subscribers map[*subscription]struct{}
}

type subscription struct {
	conversationID string
	entries        chan core.ConversationEntry
}

func New(ctx context.Context, store *core.Store, bootstrapToken string) (*Server, error) {
	if bootstrapToken == "" {
		return nil, errors.New("webapi: bootstrap token is required")
	}
	owner, _, err := store.EnsureOwner(ctx)
	if err != nil {
		return nil, fmt.Errorf("create owner: %w", err)
	}
	server := &Server{store: store, bootstrapToken: bootstrapToken, owner: owner, workerStates: make(map[string]string), subscribers: make(map[*subscription]struct{})}
	store.SetEntryObserver(server.publishEntry)
	return server, nil
}

func (s *Server) AttachNode(local *node.LocalNode)                   { s.node = local }
func (s *Server) AttachWorkerController(controller workerController) { s.workers = controller }
func (s *Server) AttachSecretary(runtime *secretaryruntime.Runtime)  { s.secretary = runtime }
func (s *Server) OwnerID() string                                    { return s.owner.ID }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/web/session", s.exchangeBootstrapToken)
	mux.HandleFunc("GET /v1/web/session", s.currentWebSession)
	mux.HandleFunc("GET /v1/conversation", s.conversation)
	mux.HandleFunc("POST /v1/messages", s.message)
	mux.HandleFunc("GET /v1/ws", s.websocket)
	mux.HandleFunc("/v1/workers/", s.workerRoute)
	return mux
}

func (s *Server) exchangeBootstrapToken(w http.ResponseWriter, r *http.Request) {
	var request struct {
		BootstrapToken string `json:"bootstrap_token"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if subtle.ConstantTimeCompare([]byte(request.BootstrapToken), []byte(s.bootstrapToken)) != 1 {
		http.Error(w, "invalid bootstrap token", http.StatusUnauthorized)
		return
	}
	token, err := s.store.CreateWebSession(r.Context(), s.owner.ID)
	if err != nil {
		http.Error(w, "create web session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, HttpOnly: true, SameSite: http.SameSiteLaxMode, Path: "/", MaxAge: int((30 * 24 * time.Hour).Seconds())})
	writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
}

func (s *Server) currentWebSession(w http.ResponseWriter, r *http.Request) {
	conversation, ok := s.authorizedConversation(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"conversation_id": conversation.ID})
}

func (s *Server) conversation(w http.ResponseWriter, r *http.Request) {
	conversation, ok := s.authorizedConversation(w, r)
	if !ok {
		return
	}
	after, err := parseAfter(r)
	if err != nil {
		http.Error(w, "invalid after_seq", http.StatusBadRequest)
		return
	}
	entries, err := s.store.EntriesAfter(r.Context(), conversation.ID, after)
	if err != nil {
		http.Error(w, "read conversation", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) message(w http.ResponseWriter, r *http.Request) {
	conversation, ok := s.authorizedConversation(w, r)
	if !ok {
		return
	}
	var request struct {
		ExternalMessageID string `json:"external_message_id"`
		Body              string `json:"body"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.ExternalMessageID == "" || request.Body == "" {
		http.Error(w, "external_message_id and body are required", http.StatusBadRequest)
		return
	}

	entry, duplicate, err := s.store.AppendInbound(r.Context(), conversation.ID, "web", request.ExternalMessageID, request.Body)
	if err != nil {
		http.Error(w, "store inbound message", http.StatusInternalServerError)
		return
	}
	if s.secretary != nil && !duplicate {
		go func(message string) {
			if err := s.secretary.HandleMessage(context.Background(), message); err != nil {
				// The inbound entry is durable. A later reconnect or runtime recovery can retry delivery.
			}
		}(request.Body)
	}
	writeJSON(w, http.StatusAccepted, struct {
		Entry     core.ConversationEntry `json:"entry"`
		Duplicate bool                   `json:"duplicate"`
	}{Entry: entry, Duplicate: duplicate})
}

func (s *Server) websocket(w http.ResponseWriter, r *http.Request) {
	conversation, ok := s.authorizedConversation(w, r)
	if !ok {
		return
	}
	after, err := parseAfter(r)
	if err != nil {
		http.Error(w, "invalid after_seq", http.StatusBadRequest)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()

	s.mu.Lock()
	entries, err := s.store.EntriesAfter(r.Context(), conversation.ID, after)
	if err != nil {
		s.mu.Unlock()
		return
	}
	sub := &subscription{conversationID: conversation.ID, entries: make(chan core.ConversationEntry, len(entries)+32)}
	for _, entry := range entries {
		sub.entries <- entry
	}
	s.subscribers[sub] = struct{}{}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.subscribers, sub)
		s.mu.Unlock()
	}()

	for {
		select {
		case entry := <-sub.entries:
			if err := conn.Write(r.Context(), websocket.MessageText, mustJSON(entry)); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) publishEntry(entry core.ConversationEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publishLocked(entry)
}

func (s *Server) publishLocked(entry core.ConversationEntry) {
	for subscriber := range s.subscribers {
		if subscriber.conversationID != entry.ConversationID {
			continue
		}
		select {
		case subscriber.entries <- entry:
		default:
			// A slow connection must reconnect with entry_seq rather than block all writers.
		}
	}
}

func (s *Server) authorizedConversation(w http.ResponseWriter, r *http.Request) (core.Conversation, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		http.Error(w, "web session required", http.StatusUnauthorized)
		return core.Conversation{}, false
	}
	person, err := s.store.WebSessionPerson(r.Context(), cookie.Value)
	if err != nil || person.ID != s.owner.ID {
		http.Error(w, "invalid web session", http.StatusUnauthorized)
		return core.Conversation{}, false
	}
	conversation, err := s.store.ConversationForPerson(r.Context(), person.ID)
	if err != nil {
		http.Error(w, "read conversation", http.StatusInternalServerError)
		return core.Conversation{}, false
	}
	return conversation, true
}

func parseAfter(r *http.Request) (int64, error) {
	value := r.URL.Query().Get("after_seq")
	if value == "" {
		return 0, nil
	}
	return strconv.ParseInt(value, 10, 64)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return false
	}
	return true
}

func mustJSON(value any) []byte {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
