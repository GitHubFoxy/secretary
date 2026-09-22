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
	"github.com/beruseruko/secretary/internal/telegram"
	"github.com/coder/websocket"
)

const sessionCookie = "secretary_session"
const telegramInternalClientID = "telegram-adapter"

type authenticatedClientContextKey struct{}

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

type secretaryWorkerTools interface {
	ListNodes(context.Context) ([]core.NodeRecord, error)
	ListProjects(context.Context) ([]core.Project, error)
	ListWorkers(context.Context) ([]core.Worker, error)
	GetWorker(context.Context, string) (core.WorkerDetails, error)
	SpawnWorker(context.Context, ctl.SpawnWorkerRequest) (core.WorkerDetails, error)
	MessageWorker(context.Context, ctl.MessageWorkerRequest) (core.WorkerDetails, error)
	CancelWorker(context.Context, string) (core.WorkerDetails, error)
	CloseWorker(context.Context, string) (core.WorkerDetails, error)
}

type TelegramPairingService interface {
	CreatePairing(string) (telegram.Pairing, error)
}

type Server struct {
	store              *core.Store
	bootstrapToken     string
	internalCredential string
	owner              core.Person
	node               *node.LocalNode
	remoteNodes        *node.ServerManager
	workers            workerController
	responder          workerResponder
	actions            workerActions
	secretaryTools     secretaryWorkerTools
	secretary          interface {
		HandleMessage(context.Context, string) error
	}
	workerStates     map[string]string
	debug            bool
	modelCatalog     func() map[string]string
	modelDefault     func() string
	modelChanged     func(string) error
	control          ControlOptions
	userPath         string
	diagnosticLogDir string
	telegramPairer   TelegramPairingService

	mu             sync.Mutex
	controlWriteMu sync.Mutex
	idempotencyMu  sync.Mutex
	subscribers    map[*subscription]struct{}
	streams        map[string]map[*activeStream]struct{}
}

type activeStream struct {
	cancel context.CancelFunc
}

type subscription struct {
	conversationID string
	clientID       string
	entries        chan core.ConversationEntry
	cancel         context.CancelFunc
	slow           bool
	cursor         int64
	pending        map[int64]core.ConversationEntry
}

func (s *subscription) enqueue(entry core.ConversationEntry) bool {
	if entry.Seq <= s.cursor {
		return true
	}
	if s.pending == nil {
		s.pending = make(map[int64]core.ConversationEntry)
	}
	if _, exists := s.pending[entry.Seq]; exists {
		return true
	}
	s.pending[entry.Seq] = entry
	for {
		next, exists := s.pending[s.cursor+1]
		if !exists {
			return true
		}
		select {
		case s.entries <- next:
			delete(s.pending, next.Seq)
			s.cursor = next.Seq
		default:
			s.slow = true
			return false
		}
	}
}

func New(ctx context.Context, store *core.Store, bootstrapToken string) (*Server, error) {
	if bootstrapToken == "" {
		return nil, errors.New("webapi: bootstrap token is required")
	}
	owner, _, err := store.EnsureOwner(ctx)
	if err != nil {
		return nil, fmt.Errorf("create owner: %w", err)
	}
	server := &Server{store: store, bootstrapToken: bootstrapToken, owner: owner, workerStates: make(map[string]string), subscribers: make(map[*subscription]struct{}), streams: make(map[string]map[*activeStream]struct{})}
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
func (s *Server) AttachSecretaryWorkerTools(tools secretaryWorkerTools) { s.secretaryTools = tools }
func (s *Server) AttachUserDocument(path string)                        { s.userPath = strings.TrimSpace(path) }
func (s *Server) AttachDiagnosticLogDir(path string)                    { s.diagnosticLogDir = strings.TrimSpace(path) }
func (s *Server) AttachTelegramPairer(pairer TelegramPairingService)    { s.telegramPairer = pairer }
func (s *Server) AttachInternalCredential(credential string) {
	s.internalCredential = strings.TrimSpace(credential)
}
func (s *Server) telegramInternalClient() core.Client {
	return core.Client{
		ID:       telegramInternalClientID,
		PersonID: s.owner.ID,
		Scopes:   []core.ClientScope{core.ScopeConversationWrite, core.ScopeWorkerWrite},
		Status:   core.ClientActive,
	}
}
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
	mux.HandleFunc("GET /v1/health", s.health)
	mux.HandleFunc("POST /v1/clients/pair", s.pairClient)
	mux.HandleFunc("GET /v1/clients", s.listClients)
	mux.HandleFunc("/v1/clients/", s.clientRoute)
	mux.HandleFunc("GET /v1/conversation", s.conversation)
	mux.HandleFunc("GET /v1/conversation/ws", s.websocket)
	mux.HandleFunc("POST /v1/messages", s.message)
	mux.HandleFunc("POST /v1/internal/secretary/tools/call", s.secretaryToolCall)
	mux.HandleFunc("POST /v1/telegram/pairing", s.telegramPairing)
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
	for i := range entries {
		entries[i] = sanitizePublicConversationEntry(entries[i])
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
		IdempotencyKey    string `json:"idempotency_key,omitempty"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.ExternalMessageID == "" || request.Body == "" {
		http.Error(w, "external_message_id and body are required", http.StatusBadRequest)
		return
	}
	key, ok := requireIdempotencyKey(w, r, request.IdempotencyKey)
	if !ok {
		return
	}
	operation := "message:" + person.ID
	payload := struct {
		ExternalMessageID string
		Body              string
	}{request.ExternalMessageID, request.Body}
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	encoded, found, lookupErr := s.store.IdempotencyOutcomeForPayload(r.Context(), operation, key, payload)
	if lookupErr != nil {
		status := http.StatusInternalServerError
		if errors.Is(lookupErr, core.ErrIdempotencyConflict) {
			status = http.StatusConflict
		}
		http.Error(w, lookupErr.Error(), status)
		return
	}
	if found {
		var acknowledgement messageAcknowledgement
		if err := json.Unmarshal(encoded, &acknowledgement); err != nil {
			http.Error(w, "decode idempotency record", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusAccepted, acknowledgement)
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
	acknowledgement := messageAcknowledgement{Entry: entry, MessageID: entry.ID, EntrySeq: entry.Seq, State: "saved", Duplicate: duplicate}
	if err := s.store.RecordIdempotencyOutcomeWithPayload(r.Context(), operation, key, payload, acknowledgement); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, core.ErrIdempotencyConflict) {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusAccepted, acknowledgement)
}

func (s *Server) websocket(w http.ResponseWriter, r *http.Request) {
	person, client, ok := s.authorizedPerson(w, r)
	if !ok {
		return
	}
	if client != nil && !client.HasScope(core.ScopeConversationRead) {
		http.Error(w, "Client scope required", http.StatusForbidden)
		return
	}
	conversation, err := s.store.ConversationForPerson(r.Context(), person.ID)
	if err != nil {
		http.Error(w, "read conversation", http.StatusInternalServerError)
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
	streamContext, cancel := context.WithCancel(r.Context())
	clientID := ""
	if client != nil {
		clientID = client.ID
	}
	stream, accepted := s.registerStream(streamContext, clientID, bearerToken(r), cancel)
	if !accepted {
		_ = conn.Close(websocket.StatusPolicyViolation, "Client revoked")
		return
	}
	defer s.unregisterStream(clientID, stream)

	s.mu.Lock()
	entries, err := s.store.EntriesAfter(streamContext, conversation.ID, after)
	if err != nil {
		s.mu.Unlock()
		return
	}
	sub := &subscription{conversationID: conversation.ID, clientID: clientID, entries: make(chan core.ConversationEntry, len(entries)+32), cancel: cancel, cursor: after, pending: make(map[int64]core.ConversationEntry)}
	for _, entry := range entries {
		sub.entries <- entry
		if entry.Seq > sub.cursor {
			sub.cursor = entry.Seq
		}
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
		case entry, open := <-sub.entries:
			if !open {
				if sub.slow {
					_ = conn.Close(websocket.StatusPolicyViolation, "subscriber too slow; reconnect with after_seq")
				}
				return
			}
			entry = sanitizePublicConversationEntry(entry)
			if err := conn.Write(streamContext, websocket.MessageText, mustJSON(entry)); err != nil {
				return
			}
		case <-streamContext.Done():
			if sub.slow {
				_ = conn.Close(websocket.StatusPolicyViolation, "subscriber too slow; reconnect with after_seq")
			}
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
		if subscriber.enqueue(entry) {
			continue
		}
		// Never drop a durable entry. Close the slow subscriber so it can
		// reconnect from its last acknowledged entry_seq.
		delete(s.subscribers, subscriber)
		if subscriber.cancel != nil {
			subscriber.cancel()
		}
		close(subscriber.entries)
	}
}

func (s *Server) registerStream(ctx context.Context, clientID, credential string, cancel context.CancelFunc) (*activeStream, bool) {
	if clientID == "" {
		return nil, true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Authentication happened before the WebSocket handshake. Recheck the
	// credential and current Client state while holding the same lock used by
	// revoke's stream sweep, so a revoked or re-paired generation cannot enter
	// the stream registry after the initial check.
	client, err := s.store.AuthenticateClient(ctx, credential)
	if err != nil || client.ID != clientID {
		return nil, false
	}
	stream := &activeStream{cancel: cancel}
	if s.streams[clientID] == nil {
		s.streams[clientID] = make(map[*activeStream]struct{})
	}
	s.streams[clientID][stream] = struct{}{}
	return stream, true
}

func (s *Server) unregisterStream(clientID string, stream *activeStream) {
	if stream == nil || clientID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if streams := s.streams[clientID]; streams != nil {
		delete(streams, stream)
		if len(streams) == 0 {
			delete(s.streams, clientID)
		}
	}
}

func (s *Server) cancelClientStreams(clientID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelClientStreamsLocked(clientID)
}

func (s *Server) cancelClientStreamsLocked(clientID string) {
	streams := s.streams[clientID]
	delete(s.streams, clientID)
	for stream := range streams {
		stream.cancel()
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
		credential := strings.TrimSpace(header[len("Bearer "):])
		if s.internalCredential != "" && subtle.ConstantTimeCompare([]byte(credential), []byte(s.internalCredential)) == 1 {
			client := s.telegramInternalClient()
			return s.owner, &client, true
		}
		client, err := s.store.AuthenticateClient(r.Context(), credential)
		if err == nil && client.PersonID == s.owner.ID {
			person := s.owner
			return person, &client, true
		}
	}
	http.Error(w, "Client or web session required", http.StatusUnauthorized)
	return core.Person{}, nil, false
}

func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) <= len("Bearer ") || !strings.EqualFold(header[:len("Bearer ")], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(header[len("Bearer "):])
}

func (s *Server) requestClientID(r *http.Request) string {
	client, err := s.store.AuthenticateClient(r.Context(), bearerToken(r))
	if err != nil || client.PersonID != s.owner.ID {
		return ""
	}
	return client.ID
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
