package webapi

import (
	"context"
	"net/http"
	"time"
)

// health is a bounded liveness probe for runbooks. The response is a fixed
// two-field JSON document with no domain data, and the endpoint needs no
// credential: access is limited by the loopback listener and the Tailscale
// Serve ACL documented in docs/always-on-runbook.md.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
