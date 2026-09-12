package webapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/ctl"
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

type workerResponder interface {
	RespondWorker(context.Context, ctl.MessageWorkerRequest) (core.WorkerDetails, error)
}

type workerActions interface {
	MessageWorker(context.Context, ctl.MessageWorkerRequest) (core.WorkerDetails, error)
	CancelWorker(context.Context, string) (core.WorkerDetails, error)
	CloseWorker(context.Context, string) (core.WorkerDetails, error)
}

type Server struct {
	store          *core.Store
	bootstrapToken string
	owner          core.Person
	node           *node.LocalNode
	remoteNodes    *node.ServerManager
	workers        workerController
	responder      workerResponder
	actions        workerActions
	secretary      interface {
		HandleMessage(context.Context, string) error
	}
	workerStates map[string]string
	debug        bool
	modelCatalog func() map[string]string
	modelDefault func() string
	modelChanged func(string) error
	control      ControlOptions
	userPath     string

	mu            sync.Mutex
	idempotencyMu sync.Mutex
	subscribers   map[*subscription]struct{}
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

func (s *Server) AttachNode(local *node.LocalNode)              { s.node = local }
func (s *Server) AttachNodeService(manager *node.ServerManager) { s.remoteNodes = manager }
func (s *Server) AttachWorkerController(controller workerController) {
	s.workers = controller
	if responder, ok := controller.(workerResponder); ok {
		s.responder = responder
	}
	if actions, ok := controller.(workerActions); ok {
		s.actions = actions
	}
}
func (s *Server) AttachWorkerResponder(responder workerResponder) {
	s.responder = responder
	if actions, ok := responder.(workerActions); ok {
		s.actions = actions
	}
}
func (s *Server) AttachUserDocument(path string)                    { s.userPath = strings.TrimSpace(path) }
func (s *Server) AttachSecretary(runtime *secretaryruntime.Runtime) { s.secretary = runtime }
func (s *Server) SetDebug(debug bool)                               { s.debug = debug }
func (s *Server) AttachSecretaryModelCatalog(catalog func() map[string]string, defaultModel func() string, changed func(string) error) {
	s.modelCatalog, s.modelDefault, s.modelChanged = catalog, defaultModel, changed
}
func (s *Server) AttachControl(options ControlOptions) { s.control = options }
func (s *Server) OwnerID() string                      { return s.owner.ID }
func (s *Server) RemoteNodes() *node.ServerManager     { return s.remoteNodes }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/web/session", s.exchangeBootstrapToken)
	mux.HandleFunc("GET /v1/web/session", s.currentWebSession)
	mux.HandleFunc("POST /v1/clients/pair", s.pairClient)
	mux.HandleFunc("GET /v1/clients", s.listClients)
	mux.HandleFunc("/v1/clients/", s.clientRoute)
	mux.HandleFunc("GET /v1/conversation", s.conversation)
	mux.HandleFunc("GET /v1/conversation/ws", s.websocket)
	mux.HandleFunc("POST /v1/messages", s.message)
	mux.HandleFunc("GET /v1/ws", s.websocket)
	mux.HandleFunc("GET /v1/user", s.user)
	mux.HandleFunc("PUT /v1/user", s.user)
	mux.HandleFunc("GET /v1/secretary/stream", s.secretaryStream)
	mux.HandleFunc("GET /v1/secretary/ws", s.secretaryWebsocket)
	mux.HandleFunc("/v1/secretary/turns/", s.secretaryTurnRoute)
	mux.HandleFunc("GET /v1/bootstrap", s.bootstrap)
	mux.HandleFunc("GET /v1/secretary/models", s.secretaryModels)
	mux.HandleFunc("POST /v1/secretary/model", s.setSecretaryModel)
	mux.HandleFunc("PUT /v1/secretary/model", s.setSecretaryModel)
	mux.HandleFunc("GET /v1/workers", s.workerList)
	mux.HandleFunc("GET /v1/approvals", s.approvalList)
	mux.HandleFunc("/v1/approvals/", s.approvalRoute)
	mux.HandleFunc("/v1/projects", s.projectRoot)
	mux.HandleFunc("/v1/projects/", s.projectRoute)
	mux.HandleFunc("/v1/workers/", s.workerRoute)
	if s.remoteNodes != nil {
		mux.HandleFunc("GET /v1/nodes/connect", s.remoteNodes.ServeProtocolHTTP)
		mux.HandleFunc("GET /v1/nodes", s.nodeDispatch)
		mux.HandleFunc("/v1/nodes/", s.nodeDispatch)
	} else {
		mux.HandleFunc("GET /v1/nodes", s.nodeList)
	}
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
	conversation, ok := s.authorizedConversationScope(w, r, core.ScopeConversationRead)
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
	person, client, ok := s.authorizedPerson(w, r)
	if !ok {
		return
	}
	if client != nil && !client.HasScope(core.ScopeConversationWrite) {
		http.Error(w, "Client scope required", http.StatusForbidden)
		return
	}
	conversation, err := s.store.ConversationForPerson(r.Context(), person.ID)
	if err != nil {
		http.Error(w, "read conversation", http.StatusInternalServerError)
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

	adapterID := "web"
	if client != nil {
		adapterID = "client:" + client.ID
	}
	entry, duplicate, err := s.store.AppendInbound(r.Context(), conversation.ID, adapterID, request.ExternalMessageID, request.Body)
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
	writeJSON(w, http.StatusAccepted, messageAcknowledgement{Entry: entry, MessageID: entry.ID, EntrySeq: entry.Seq, State: "saved", Duplicate: duplicate})
}

func (s *Server) websocket(w http.ResponseWriter, r *http.Request) {
	conversation, ok := s.authorizedConversationScope(w, r, core.ScopeConversationRead)
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
	return s.authorizedConversationScope(w, r, "")
}

func (s *Server) authorizedConversationScope(w http.ResponseWriter, r *http.Request, scope core.ClientScope) (core.Conversation, bool) {
	person, client, ok := s.authorizedPerson(w, r)
	if !ok {
		return core.Conversation{}, false
	}
	if scope != "" && client != nil && !client.HasScope(scope) {
		http.Error(w, "Client scope required", http.StatusForbidden)
		return core.Conversation{}, false
	}
	conversation, err := s.store.ConversationForPerson(r.Context(), person.ID)
	if err != nil {
		http.Error(w, "read conversation", http.StatusInternalServerError)
		return core.Conversation{}, false
	}
	return conversation, true
}

func (s *Server) authorizedPerson(w http.ResponseWriter, r *http.Request) (core.Person, *core.Client, bool) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		person, sessionErr := s.store.WebSessionPerson(r.Context(), cookie.Value)
		if sessionErr == nil && person.ID == s.owner.ID {
			return person, nil, true
		}
	}
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) > len("Bearer ") && strings.EqualFold(header[:len("Bearer ")], "Bearer ") {
		client, err := s.store.AuthenticateClient(r.Context(), strings.TrimSpace(header[len("Bearer "):]))
		if err == nil && client.PersonID == s.owner.ID {
			person := s.owner
			return person, &client, true
		}
	}
	http.Error(w, "Client or web session required", http.StatusUnauthorized)
	return core.Person{}, nil, false
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
