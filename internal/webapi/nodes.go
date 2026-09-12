package webapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/beruseruko/secretary/internal/core"
)

type publicNode struct {
	Node            core.NodeReference            `json:"node"`
	Online          bool                          `json:"online"`
	Draining        bool                          `json:"draining"`
	Revoked         bool                          `json:"revoked"`
	EnrolledAt      time.Time                     `json:"enrolled_at"`
	LastSeenAt      time.Time                     `json:"last_seen_at,omitempty"`
	LastHeartbeatAt time.Time                     `json:"last_heartbeat_at,omitempty"`
	Capacity        int                           `json:"capacity"`
	ActiveAttempts  []core.NodeActiveAttempt      `json:"active_attempts,omitempty"`
	LastProcessed   string                        `json:"last_processed_command,omitempty"`
	Inventory       core.HarnessInventorySnapshot `json:"inventory,omitempty"`
}

func (s *Server) nodeDispatch(w http.ResponseWriter, r *http.Request) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		credential := strings.TrimSpace(header[len("Bearer "):])
		if _, err := s.store.AuthenticateClient(r.Context(), credential); err == nil {
			if r.URL.Path == "/v1/nodes" {
				s.nodeList(w, r)
				return
			}
			s.nodeRoute(w, r)
			return
		}
	}
	if s.remoteNodes != nil {
		s.remoteNodes.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) nodeRoute(w http.ResponseWriter, r *http.Request) {
	scope := core.ScopeNodeRead
	if r.Method != http.MethodGet {
		scope = core.ScopeNodeWrite
	}
	if _, ok := s.authorizedConversationScope(w, r, scope); !ok {
		return
	}
	parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/nodes/"), "/"), "/")
	if len(parts) != 2 || parts[0] == "" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	nodeRef := core.NodeReference(parts[0])
	var record core.NodeRecord
	var err error
	switch parts[1] {
	case "drain":
		draining := true
		if r.ContentLength != 0 {
			var request struct {
				Draining *bool `json:"draining"`
			}
			if !decodeJSON(w, r, &request) {
				return
			}
			if request.Draining != nil {
				draining = *request.Draining
			}
		}
		record, err = s.store.SetNodeDraining(r.Context(), nodeRef, draining)
	case "revoke":
		record, err = s.store.RevokeNode(r.Context(), nodeRef)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, publicNode{Node: record.Node, Online: record.Online, Draining: record.Draining, Revoked: record.Revoked, EnrolledAt: record.EnrolledAt, LastSeenAt: record.LastSeenAt, LastHeartbeatAt: record.LastHeartbeatAt, Capacity: record.Capacity, ActiveAttempts: record.ActiveAttempts, LastProcessed: record.LastProcessedCommand, Inventory: record.Inventory})
}

func (s *Server) nodeList(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") == "" && r.Header.Get("Cookie") == "" {
		http.NotFound(w, r)
		return
	}
	if _, ok := s.authorizedConversationScope(w, r, core.ScopeNodeRead); !ok {
		return
	}
	nodes, err := s.store.NodeRecords(r.Context())
	if err != nil {
		http.Error(w, "list Nodes", http.StatusInternalServerError)
		return
	}
	public := make([]publicNode, 0, len(nodes))
	for _, node := range nodes {
		public = append(public, publicNode{Node: node.Node, Online: node.Online, Draining: node.Draining, Revoked: node.Revoked, EnrolledAt: node.EnrolledAt, LastSeenAt: node.LastSeenAt, LastHeartbeatAt: node.LastHeartbeatAt, Capacity: node.Capacity, ActiveAttempts: node.ActiveAttempts, LastProcessed: node.LastProcessedCommand, Inventory: node.Inventory})
	}
	writeJSON(w, http.StatusOK, public)
}
