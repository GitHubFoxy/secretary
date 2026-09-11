package node

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
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
// It intentionally has no access to native runtime session IDs or local harness
// credentials; those remain on the Node.
type ServerManager struct {
	store        *core.Store
	pairingToken string
	adminToken   string
	masterSecret []byte

	mu          sync.Mutex
	connections map[core.NodeReference]*ProtocolConnection
	outcomes    map[core.NodeReference]CommandOutcome
	eventSink   func(context.Context, NodeEvent) error
	outcomeSink func(context.Context, core.NodeReference, CommandOutcome) error
}

func NewServerManager(ctx context.Context, store *core.Store, pairingToken, adminToken string) (*ServerManager, error) {
	if store == nil {
		return nil, errors.New("node server: store is required")
	}
	if strings.TrimSpace(pairingToken) == "" {
		return nil, errors.New("node server: pairing token is required")
	}
	if strings.TrimSpace(adminToken) == "" {
		return nil, errors.New("node server: admin token is required")
	}
	if err := store.EnsureNodeRegistry(ctx); err != nil {
		return nil, err
	}
	secret, err := store.EnsureNodeTransportSecret(ctx)
	if err != nil {
		return nil, err
	}
	if err := store.MarkAllNodesOffline(ctx); err != nil {
		return nil, err
	}
	return &ServerManager{
		store: store, pairingToken: pairingToken, adminToken: adminToken, masterSecret: secret,
		connections: map[core.NodeReference]*ProtocolConnection{}, outcomes: map[core.NodeReference]CommandOutcome{},
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
	if subtle.ConstantTimeCompare([]byte(request.PairingToken), []byte(m.pairingToken)) != 1 {
		http.Error(w, "invalid Node pairing token", http.StatusUnauthorized)
		return
	}
	if request.Node == "" {
		request.Node = randomNodeReference()
	}
	if !validNodeReference(request.Node) {
		http.Error(w, "invalid Node reference", http.StatusBadRequest)
		return
	}
	if _, err := m.store.EnrollNode(r.Context(), request.Node); err != nil {
		if errors.Is(err, core.ErrNodeAlreadyEnrolled) {
			http.Error(w, "Node reference is already enrolled", http.StatusConflict)
			return
		}
		http.Error(w, "enroll Node", http.StatusInternalServerError)
		return
	}
	credential := base64.RawURLEncoding.EncodeToString(m.credential(request.Node))
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
	mac := hmac.New(sha256.New, m.masterSecret)
	_, _ = mac.Write([]byte("secretary-node-v1\n" + string(nodeRef)))
	return mac.Sum(nil)
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
