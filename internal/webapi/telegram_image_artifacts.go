package webapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/beruseruko/secretary/internal/core"
	"github.com/beruseruko/secretary/internal/node"
)

func (s *Server) telegramImageSource(w http.ResponseWriter, r *http.Request) {
	conversation, ok := s.authorizedTelegramVoice(w, r)
	if !ok {
		return
	}
	updateID, errUpdate := strconv.ParseInt(r.Header.Get("X-Image-Update-ID"), 10, 64)
	chatID, errChat := strconv.ParseInt(r.Header.Get("X-Image-Chat-ID"), 10, 64)
	messageID, errMessage := strconv.ParseInt(r.Header.Get("X-Image-Message-ID"), 10, 64)
	identity, valid := telegramVoiceIdentity(updateID, chatID, messageID)
	if errUpdate != nil || errChat != nil || errMessage != nil || !valid {
		http.Error(w, "Invalid Telegram image source identity", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Header.Get("X-Image-Name"))
	if !artifactNameValid(name) {
		http.Error(w, "Invalid image filename or extension (PNG, JPEG, WebP required)", http.StatusUnsupportedMediaType)
		return
	}
	if r.ContentLength > core.MaxArtifactBytes {
		http.Error(w, "Image exceeds the 8 MiB Artifact limit", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, core.MaxArtifactBytes+1)
	data, err := io.ReadAll(r.Body)
	if err != nil || len(data) > core.MaxArtifactBytes {
		http.Error(w, "Image exceeds the 8 MiB Artifact limit", http.StatusRequestEntityTooLarge)
		return
	}
	validated, err := node.ValidateStillImage(name, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if declared := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Image-Mime-Type"))); declared != "" && declared != validated.MediaType {
		http.Error(w, "Telegram image MIME type and image content disagree", http.StatusUnprocessableEntity)
		return
	}
	key := fmt.Sprintf("telegram:image:%d:%d:%d:source", updateID, chatID, messageID)
	original, duplicate, err := s.store.SaveArtifact(r.Context(), conversation.PersonID, conversation.ID, key, name, validated.MediaType, data)
	if errors.Is(err, core.ErrIdempotencyConflict) {
		http.Error(w, "Conflicting Telegram image source identity", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "Store Telegram image source failed", http.StatusInternalServerError)
		return
	}
	status := http.StatusCreated
	if duplicate {
		status = http.StatusOK
	}
	writeJSON(w, status, map[string]any{"artifact":original,"reference":"artifact://"+original.ID,"duplicate":duplicate,"source_identity":identity})
}
