package webapi

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

var artifactExtensions = map[string]bool{
	".txt": true, ".md": true, ".pdf": true, ".csv": true, ".json": true,
	".xml": true, ".yaml": true, ".yml": true, ".html": true, ".log": true,
	".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".ppt": true, ".pptx": true, ".odt": true, ".ods": true,
	".rtf": true, ".zip": true, ".png": true, ".jpg": true,
	".jpeg": true, ".webp": true,
}

func artifactNameValid(name string) bool {
	if len(name) == 0 || len(name) > 180 || name == "." || name == ".." ||
		strings.ContainsAny(name, "/\\") || strings.HasPrefix(name, ".") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	return artifactExtensions[strings.ToLower(filepath.Ext(name))]
}

func (s *Server) uploadArtifact(w http.ResponseWriter, r *http.Request) {
	conversation, ok := s.authorizedConversationScope(w, r, core.ScopeConversationWrite)
	if !ok {
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if !artifactNameValid(name) {
		http.Error(w, "Unsupported file type or invalid filename. Allowed: documents, text, images, ZIP (max 8 MiB).", http.StatusUnsupportedMediaType)
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 160 {
		http.Error(w, "Idempotency-Key is required (max 160 bytes)", http.StatusBadRequest)
		return
	}
	if r.ContentLength > core.MaxArtifactBytes {
		http.Error(w, "File exceeds the 8 MiB upload limit", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, core.MaxArtifactBytes+1)
	data, err := io.ReadAll(r.Body)
	if err != nil || len(data) > core.MaxArtifactBytes {
		http.Error(w, "File exceeds the 8 MiB upload limit or could not be read", http.StatusRequestEntityTooLarge)
		return
	}
	if len(data) == 0 {
		http.Error(w, "Empty files are not supported", http.StatusBadRequest)
		return
	}
	mediaType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	artifact, duplicate, err := s.store.SaveArtifact(r.Context(), conversation.PersonID, conversation.ID, key, name, mediaType, data)
	if err != nil {
		if errors.Is(err, core.ErrIdempotencyConflict) {
			http.Error(w, "Idempotency key belongs to another file", http.StatusConflict)
			return
		}
		http.Error(w, "Could not save uploaded original", http.StatusInternalServerError)
		return
	}
	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"artifact": artifact, "duplicate": duplicate, "reference": "artifact://" + artifact.ID})
}

func (s *Server) artifactRoute(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/v1/artifacts/")
	if !node.ValidArtifactID(id) {
		http.Error(w, "Invalid artifact reference", http.StatusBadRequest)
		return
	}
	personID := ""
	byNode := r.Header.Get("X-Secretary-Node") != "" || r.Header.Get("X-Artifact-Signature") != ""
	if byNode {
		nodeID := core.NodeReference(r.Header.Get("X-Secretary-Node"))
		record, err := s.store.NodeRecord(r.Context(), nodeID)
		if err != nil || record.Revoked {
			http.Error(w, "Node not authorized", http.StatusForbidden)
			return
		}
		secret, err := s.store.NodeCredential(r.Context(), nodeID)
		if err != nil {
			http.Error(w, "Node not authorized", http.StatusForbidden)
			return
		}
		epoch, err := strconv.ParseInt(r.Header.Get("X-Artifact-Timestamp"), 10, 64)
		if err != nil || node.NewAuthenticator(secret).VerifyArtifactRequest(nodeID, id, epoch, r.Header.Get("X-Artifact-Signature"), time.Now()) != nil {
			http.Error(w, "Invalid Node artifact authorization", http.StatusUnauthorized)
			return
		}
		personID = s.owner.ID
	} else {
		person, client, ok := s.authorizedPerson(w, r)
		if !ok {
			return
		}
		if client != nil && !client.HasScope(core.ScopeConversationRead) {
			http.Error(w, "Client scope required", http.StatusForbidden)
			return
		}
		personID = person.ID
	}
	artifact, body, err := s.store.ArtifactForPerson(r.Context(), personID, id)
	if errors.Is(err, core.ErrNotFound) {
		http.Error(w, "Artifact not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Could not read Artifact", http.StatusInternalServerError)
		return
	}
	if !byNode && r.URL.Query().Get("download") != "1" {
		writeJSON(w, http.StatusOK, artifact)
		return
	}
	w.Header().Set("Content-Type", artifact.MediaType)
	w.Header().Set("Content-Length", fmt.Sprint(artifact.Size))
	w.Header().Set("X-Artifact-SHA256", artifact.SHA256)
	w.Header().Set("X-Artifact-Name", base64.RawURLEncoding.EncodeToString([]byte(artifact.Name)))
	w.Header().Set("Content-Disposition", "attachment")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
