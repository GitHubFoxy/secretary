package node

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

var nodeReferencePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

type EnrollmentRequest struct {
	PairingToken string             `json:"pairing_token"`
	Node         core.NodeReference `json:"node"`
}

type EnrollmentResponse struct {
	Node       core.NodeReference `json:"node"`
	Credential string             `json:"credential"`
	ConnectURL string             `json:"connect_url"`
}

type ServerNodeStatus struct {
	Node               core.NodeReference            `json:"node"`
	Online             bool                          `json:"online"`
	Draining           bool                          `json:"draining"`
	Revoked            bool                          `json:"revoked"`
	EnrolledAt         time.Time                     `json:"enrolled_at"`
	LastSeenAt         time.Time                     `json:"last_seen_at,omitempty"`
	LastHeartbeatAt    time.Time                     `json:"last_heartbeat_at,omitempty"`
	Inventory          core.HarnessInventorySnapshot `json:"inventory,omitempty"`
	LastCommandOutcome *CommandOutcome               `json:"last_command_outcome,omitempty"`
}

// ServerManager owns server-side Node enrollment and live protocol connections.
// It never receives native runtime session IDs or local harness credentials.
// Transport credentials are separate per-Node secrets used only by this boundary.
type ServerConfig struct {
	PairingTokens        []string
	AdminToken           string
	ClientBootstrapToken string
	HeartbeatTimeout     time.Duration
	WatchdogInterval     time.Duration
	Now                  func() time.Time
}

type ServerManager struct {
	store      *core.Store
	adminToken string
	config     ServerConfig
	now        func() time.Time

	mu          sync.Mutex
	connections map[core.NodeReference]*ProtocolConnection
	outcomes    map[core.NodeReference]CommandOutcome
	credentials map[core.NodeReference][]byte
	eventSink   func(context.Context, NodeEvent) error
	outcomeSink func(context.Context, core.NodeReference, CommandOutcome) error
}

// NewServerManager is kept as a narrow compatibility constructor. Production
// wiring uses NewServerManagerWithConfig so Client bootstrap credentials cannot
// cross the Node service boundary.
func NewServerManager(ctx context.Context, store *core.Store, pairingToken, adminToken string) (*ServerManager, error) {
	return NewServerManagerWithConfig(ctx, store, ServerConfig{PairingTokens: []string{pairingToken}, AdminToken: adminToken})
}

func NewServerManagerWithConfig(ctx context.Context, store *core.Store, config ServerConfig) (*ServerManager, error) {
	if store == nil {
		return nil, errors.New("node server: store is required")
	}
	if strings.TrimSpace(config.AdminToken) == "" {
		return nil, errors.New("node server: admin token is required")
	}
	if bootstrap := strings.TrimSpace(config.ClientBootstrapToken); bootstrap != "" && subtle.ConstantTimeCompare([]byte(bootstrap), []byte(config.AdminToken)) == 1 {
		return nil, errors.New("node server: Client bootstrap and Node admin credentials must be distinct")
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.HeartbeatTimeout <= 0 {
		config.HeartbeatTimeout = 45 * time.Second
	}
	if config.WatchdogInterval <= 0 {
		config.WatchdogInterval = config.HeartbeatTimeout / 3
		if config.WatchdogInterval <= 0 {
			config.WatchdogInterval = time.Second
		}
	}
	for _, token := range config.PairingTokens {
		if strings.TrimSpace(token) == "" {
			return nil, errors.New("node server: pairing token is required")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(config.AdminToken)) == 1 {
			return nil, errors.New("node server: Node pairing and admin credentials must be distinct")
		}
		if bootstrap := strings.TrimSpace(config.ClientBootstrapToken); bootstrap != "" && subtle.ConstantTimeCompare([]byte(token), []byte(bootstrap)) == 1 {
			return nil, errors.New("node server: Client bootstrap and Node pairing credentials must be distinct")
		}
	}
	if len(config.PairingTokens) == 0 {
		return nil, errors.New("node server: pairing token is required")
	}
	if err := store.EnsureNodeRegistry(ctx); err != nil {
		return nil, err
	}
	for _, token := range config.PairingTokens {
		if err := store.ConfigureNodePairingToken(ctx, token); err != nil {
			return nil, err
		}
	}
	if err := store.MarkAllNodesOffline(ctx); err != nil {
		return nil, err
	}
	credentials := make(map[core.NodeReference][]byte)
	records, err := store.NodeRecords(ctx)
	if err != nil {
		return nil, err
	}
	for _, record := range records {
		if credential, credentialErr := store.NodeCredential(ctx, record.Node); credentialErr == nil {
			credentials[record.Node] = credential
		}
	}
	return &ServerManager{
		store: store, adminToken: config.AdminToken, config: config, now: config.Now,
		connections: map[core.NodeReference]*ProtocolConnection{}, outcomes: map[core.NodeReference]CommandOutcome{}, credentials: credentials,
	}, nil
}

func (m *ServerManager) SetEventSink(sink func(context.Context, NodeEvent) error) {
	m.mu.Lock()
	m.eventSink = sink
	m.mu.Unlock()
}

func (m *ServerManager) SetCommandOutcomeSink(sink func(context.Context, core.NodeReference, CommandOutcome) error) {
	m.mu.Lock()
	m.outcomeSink = sink
	m.mu.Unlock()
}

// ServeHTTP exposes only the minimal enrollment/control API needed by 13a.
// Node protocol traffic uses ServeProtocolHTTP and never accepts web/client
// session credentials as an authentication mechanism.
func (m *ServerManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	if path == "/v1/nodes/pair" {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		m.pair(w, r)
		return
	}
	if !m.authorizeAdmin(r) {
		http.Error(w, "Node admin authorization required", http.StatusUnauthorized)
		return
	}
	if path == "/v1/nodes" {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		statuses, err := m.Statuses(r.Context())
		if err != nil {
			http.Error(w, "read Nodes", http.StatusInternalServerError)
			return
		}
		writeNodeJSON(w, http.StatusOK, statuses)
		return
	}

	parts := strings.Split(strings.TrimPrefix(path, "/v1/nodes/"), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	nodeRef := core.NodeReference(parts[0])
	switch parts[1] {
	case "drain":
		var request struct {
			Draining *bool `json:"draining"`
		}
		defaultDraining := true
		request.Draining = &defaultDraining
		if r.ContentLength != 0 {
			if err := decodeNodeJSON(w, r, &request); err != nil {
				return
			}
		}
		if request.Draining == nil {
			http.Error(w, "draining must be true or false", http.StatusBadRequest)
			return
		}
		record, err := m.store.SetNodeDraining(r.Context(), nodeRef, *request.Draining)
		if err != nil {
			writeNodeStoreError(w, err)
			return
		}
		writeNodeJSON(w, http.StatusOK, m.statusFor(record))
	case "revoke":
		record, err := m.store.RevokeNode(r.Context(), nodeRef)
		if err != nil {
			writeNodeStoreError(w, err)
			return
		}
		m.closeConnection(nodeRef)
		writeNodeJSON(w, http.StatusOK, m.statusFor(record))
	default:
		http.NotFound(w, r)
	}
}

func (m *ServerManager) ServeProtocolHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	nodeRef := core.NodeReference(strings.TrimSpace(r.URL.Query().Get("node")))
	if !validNodeReference(nodeRef) {
		http.Error(w, "valid Node reference is required", http.StatusBadRequest)
		return
	}
	record, err := m.store.NodeRecord(r.Context(), nodeRef)
	if err != nil {
		writeNodeStoreError(w, err)
		return
	}
	if record.Revoked {
		http.Error(w, "Node is revoked", http.StatusForbidden)
		return
	}
	handler := &serverProtocolHandler{manager: m, expected: nodeRef}
	server := &ProtocolServer{Auth: NewAuthenticator(m.credential(nodeRef)), Handler: handler}
	server.ServeHTTP(w, r)
	if handler.connection != nil {
		m.unregisterConnection(nodeRef, handler.connection)
	}
}

func (m *ServerManager) SendCommand(ctx context.Context, nodeRef core.NodeReference, command Command) error {
	record, err := m.store.NodeRecord(ctx, nodeRef)
	if err != nil {
		return err
	}
	if record.Revoked {
		return core.ErrNodeRevoked
	}
	if record.Draining && (command.Kind == CommandDispatch || command.Kind == CommandResume) {
		return errors.New("node server: Node is draining")
	}
	m.mu.Lock()
	connection := m.connections[nodeRef]
	m.mu.Unlock()
	if !record.Online || connection == nil {
		return errors.New("node server: Node is offline")
	}
	return connection.SendCommand(ctx, command)
}

func (m *ServerManager) Statuses(ctx context.Context) ([]ServerNodeStatus, error) {
	records, err := m.store.NodeRecords(ctx)
	if err != nil {
		return nil, err
	}
	statuses := make([]ServerNodeStatus, 0, len(records))
	for _, record := range records {
		statuses = append(statuses, m.statusFor(record))
	}
	return statuses, nil
}

func (m *ServerManager) Status(ctx context.Context, nodeRef core.NodeReference) (ServerNodeStatus, error) {
	record, err := m.store.NodeRecord(ctx, nodeRef)
	if err != nil {
		return ServerNodeStatus{}, err
	}
	return m.statusFor(record), nil
}

func (m *ServerManager) statusFor(record core.NodeRecord) ServerNodeStatus {
	status := ServerNodeStatus{
		Node: record.Node, Online: record.Online, Draining: record.Draining, Revoked: record.Revoked,
		EnrolledAt: record.EnrolledAt, LastSeenAt: record.LastSeenAt, LastHeartbeatAt: record.LastHeartbeatAt, Inventory: record.Inventory,
	}
	m.mu.Lock()
	if outcome, ok := m.outcomes[record.Node]; ok {
		copy := outcome
		status.LastCommandOutcome = &copy
	}
	m.mu.Unlock()
	return status
}

func (m *ServerManager) pair(w http.ResponseWriter, r *http.Request) {
	var request EnrollmentRequest
	if err := decodeNodeJSON(w, r, &request); err != nil {
		return
	}
	if request.Node == "" {
		request.Node = randomNodeReference()
	}
	if !validNodeReference(request.Node) {
		http.Error(w, "invalid Node reference", http.StatusBadRequest)
		return
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		http.Error(w, "generate Node credential", http.StatusInternalServerError)
		return
	}
	if _, err := m.store.EnrollNodeWithPairing(r.Context(), request.PairingToken, request.Node, secret); err != nil {
		switch {
		case errors.Is(err, core.ErrNodePairingTokenUsed), errors.Is(err, core.ErrNodePairingTokenAbsent):
			http.Error(w, "invalid or already used Node pairing token", http.StatusUnauthorized)
		case errors.Is(err, core.ErrNodeAlreadyEnrolled):
			http.Error(w, "Node reference is already enrolled", http.StatusConflict)
		default:
			http.Error(w, "enroll Node", http.StatusInternalServerError)
		}
		return
	}
	m.mu.Lock()
	m.credentials[request.Node] = append([]byte(nil), secret...)
	m.mu.Unlock()
	credential := base64.RawURLEncoding.EncodeToString(secret)
	writeNodeJSON(w, http.StatusCreated, EnrollmentResponse{Node: request.Node, Credential: credential, ConnectURL: nodeConnectURL(r, request.Node)})
}

func (m *ServerManager) authorizeAdmin(r *http.Request) bool {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(strings.TrimPrefix(header, prefix))), []byte(m.adminToken)) == 1
}

func (m *ServerManager) credential(nodeRef core.NodeReference) []byte {
	m.mu.Lock()
	credential := append([]byte(nil), m.credentials[nodeRef]...)
	m.mu.Unlock()
	if len(credential) > 0 {
		return credential
	}
	credential, err := m.store.NodeCredential(context.Background(), nodeRef)
	if err != nil {
		return nil
	}
	m.mu.Lock()
	m.credentials[nodeRef] = append([]byte(nil), credential...)
	m.mu.Unlock()
	return credential
}

// Run watches the last accepted heartbeat independently from socket lifetime.
// This turns silent network loss into an explicit offline state.
func (m *ServerManager) Run(ctx context.Context) error {
	ticker := time.NewTicker(m.config.WatchdogInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := m.WatchdogTick(ctx); err != nil {
				return err
			}
		}
	}
}

func (m *ServerManager) WatchdogTick(ctx context.Context) error {
	return m.store.MarkSilentNodesOffline(ctx, m.now().Add(-m.config.HeartbeatTimeout))
}

func (m *ServerManager) registerConnection(nodeRef core.NodeReference, connection *ProtocolConnection) {
	m.mu.Lock()
	previous := m.connections[nodeRef]
	m.connections[nodeRef] = connection
	m.mu.Unlock()
	if previous != nil && previous != connection {
		_ = previous.Close()
	}
}

func (m *ServerManager) unregisterConnection(nodeRef core.NodeReference, connection *ProtocolConnection) {
	m.mu.Lock()
	current := m.connections[nodeRef]
	if current == connection {
		delete(m.connections, nodeRef)
	}
	m.mu.Unlock()
	if current == connection {
		_ = m.store.MarkNodeDisconnected(context.Background(), nodeRef)
	}
}

func (m *ServerManager) closeConnection(nodeRef core.NodeReference) {
	m.mu.Lock()
	connection := m.connections[nodeRef]
	delete(m.connections, nodeRef)
	m.mu.Unlock()
	if connection != nil {
		_ = connection.Close()
	}
}

func validNodeReference(nodeRef core.NodeReference) bool {
	return nodeReferencePattern.MatchString(string(nodeRef))
}

func randomNodeReference() core.NodeReference {
	buffer := make([]byte, 6)
	if _, err := rand.Read(buffer); err != nil {
		return core.NodeReference(fmt.Sprintf("node-%d", time.Now().UnixNano()))
	}
	return core.NodeReference("node-" + hex.EncodeToString(buffer))
}

func nodeConnectURL(r *http.Request, nodeRef core.NodeReference) string {
	scheme := "ws"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "wss"
	}
	u := url.URL{Scheme: scheme, Host: r.Host, Path: "/v1/nodes/connect"}
	query := u.Query()
	query.Set("node", string(nodeRef))
	u.RawQuery = query.Encode()
	return u.String()
}

func decodeNodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return err
	}
	return nil
}

func writeNodeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeNodeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, core.ErrNotFound):
		http.Error(w, "Node not found", http.StatusNotFound)
	case errors.Is(err, core.ErrNodeRevoked):
		http.Error(w, "Node is revoked", http.StatusForbidden)
	default:
		http.Error(w, "Node state error", http.StatusInternalServerError)
	}
}

type serverProtocolHandler struct {
	manager    *ServerManager
	expected   core.NodeReference
	inventory  core.HarnessInventorySnapshot
	connection *ProtocolConnection
}

func (h *serverProtocolHandler) HandleNodeHandshake(ctx context.Context, handshake Handshake) (HandshakeAccepted, error) {
	if handshake.Node != h.expected {
		return HandshakeAccepted{}, errors.New("node server: handshake identity does not match connection identity")
	}
	record, err := h.manager.store.NodeRecord(ctx, h.expected)
	if err != nil {
		return HandshakeAccepted{}, err
	}
	if record.Revoked {
		return HandshakeAccepted{}, core.ErrNodeRevoked
	}
	// Do not mark the Node online until the authenticated handshake response
	// has been written and ProtocolServer has promoted this socket to a live
	// connection. Otherwise a failed acceptance can leave a ghost online Node.
	h.inventory = handshake.Inventory
	return HandshakeAccepted{Node: h.expected, ProtocolVersion: ProtocolVersion, ReplayFromSequence: handshake.LastAcknowledgedSequence}, nil
}

func (h *serverProtocolHandler) HandleNodeConnection(connection *ProtocolConnection) {
	if err := h.manager.store.MarkNodeConnected(context.Background(), h.expected, h.inventory); err != nil {
		_ = connection.Close()
		return
	}
	h.connection = connection
	h.manager.registerConnection(h.expected, connection)
}

func (h *serverProtocolHandler) HandleNodeHeartbeat(ctx context.Context, heartbeat Heartbeat) error {
	if heartbeat.Node != h.expected {
		return errors.New("node server: heartbeat identity mismatch")
	}
	return h.manager.store.UpdateNodeHeartbeat(ctx, h.expected, heartbeat.Inventory)
}

func (h *serverProtocolHandler) HandleNodeInventory(ctx context.Context, inventory core.HarnessInventorySnapshot) error {
	if inventory.Node != h.expected {
		return errors.New("node server: inventory identity mismatch")
	}
	return h.manager.store.UpdateNodeInventory(ctx, h.expected, inventory)
}

func (h *serverProtocolHandler) HandleNodeEvent(ctx context.Context, event NodeEvent) error {
	if event.Node != h.expected {
		return errors.New("node server: event identity mismatch")
	}
	h.manager.mu.Lock()
	sink := h.manager.eventSink
	h.manager.mu.Unlock()
	if sink == nil {
		return errors.New("node server: event sink is not configured")
	}
	return sink(ctx, event)
}

func (h *serverProtocolHandler) HandleNodeCommandOutcome(ctx context.Context, outcome CommandOutcome) error {
	h.manager.mu.Lock()
	h.manager.outcomes[h.expected] = outcome
	sink := h.manager.outcomeSink
	h.manager.mu.Unlock()
	if sink != nil {
		return sink(ctx, h.expected, outcome)
	}
	return nil
}
